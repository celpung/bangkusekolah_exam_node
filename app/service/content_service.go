package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// ContentService caches immutable exam content keyed by exam ID. One VPS
// hosts multiple exams (typically 3-10), so every lookup and rebuild is
// scoped to a single exam — there is no global cache to cross-serve.
//
// Publication protocol (version-aware): BeginRebuild issues a generation
// token and marks the exam unready before the DB swap commits. Only the
// operation holding the CURRENT token may publish (RebuildExam) or cancel;
// stale operations cannot mutate readiness belonging to a newer version.
type ContentService struct {
	repo    outbound_repository.NodeRepository
	mu      sync.RWMutex
	cache   map[string]*contentCache // key: exam ID
	unready map[string]error         // exams whose last publication attempt failed or is in flight

	// per-exam load serialization + generation counters
	loadMu     sync.Map // examID -> *sync.Mutex
	generation map[string]uint64
}

type contentCache struct {
	content   *inbound.ExamContent
	etag      string
	gzipBytes []byte
	rawBytes  []byte
}

func NewContentService(repo outbound_repository.NodeRepository) *ContentService {
	return &ContentService{
		repo:       repo,
		cache:      map[string]*contentCache{},
		unready:    map[string]error{},
		generation: map[string]uint64{},
	}
}

// LockExam serializes bundle loads per exam so two pushes for the same exam
// cannot interleave their publication windows.
func (s *ContentService) LockExam(examID string) func() {
	v, _ := s.loadMu.LoadOrStore(examID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// BeginRebuild marks an exam as rebuilding before its DB replacement commits
// and returns the generation token for this publication. Only this token may
// publish or cancel; a newer BeginRebuild invalidates older tokens.
func (s *ContentService) BeginRebuild(examID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation[examID]++
	token := s.generation[examID]
	s.unready[examID] = fmt.Errorf("%w: rebuild in progress (generation %d)", node_error.ErrExamContentNotReady, token)
	return token
}

// CancelRebuild handles the state after a rolled-back transaction.
//
//   - NEW exam (never persisted): the rollback removed the only attempt to
//     create it — drop the transient marker so a failed new bundle cannot
//     take unrelated healthy exams offline.
//   - EXISTING exam: the DB still holds the last successfully published
//     version but the service cannot re-verify it from here — keep the exam
//     unready with an explicit cause until a retry succeeds and publishes.
//
// Both paths are generation-guarded: a stale operation never mutates newer
// state.
func (s *ContentService) CancelRebuild(examID string, token uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation[examID] != token {
		return false // superseded by a newer operation
	}
	if _, cached := s.cache[examID]; !cached {
		// New exam whose first load rolled back: nothing was ever served for
		// it, so removing the marker cannot resurrect stale content.
		delete(s.unready, examID)
		return true
	}
	// Existing exam: conservative fail-closed until a successful rebuild.
	s.unready[examID] = fmt.Errorf("%w: publication rolled back; retry required", node_error.ErrExamContentNotReady)
	return true
}

// RebuildExam publishes the cache for the exam's current DB rows and clears
// the rebuilding state for the given generation. Stale generations are
// rejected without touching state.
func (s *ContentService) RebuildExam(ctx context.Context, examID string, token ...uint64) error {
	if len(token) > 0 {
		s.mu.RLock()
		current := s.generation[examID]
		s.mu.RUnlock()
		if current != token[0] {
			return fmt.Errorf("%w: generation %d superseded by %d", node_error.ErrExamContentNotReady, token[0], current)
		}
	}
	exam, err := s.repo.FindExamByID(ctx, examID)
	if err != nil {
		s.markUnready(examID, err)
		return err
	}
	items, err := s.repo.ListItemsByExamID(ctx, examID)
	if err != nil {
		s.markUnready(examID, err)
		return err
	}
	content := &inbound.ExamContent{Exam: exam}
	for i := range items {
		it := &items[i]
		content.Items = append(content.Items, inbound.ExamItemDTO{
			ID: it.ID, SectionID: it.SectionID, SectionTitle: it.SectionTitle,
			SectionSortOrder: it.SectionSortOrder, SortOrder: it.SortOrder,
			QuestionType: string(it.QuestionType), Prompt: it.PromptSnapshot,
			Options: it.OptionsSnapshotJSON, Points: it.Points,
			RequiresManualGrading: it.RequiresManualGrading,
		})
	}
	// Deterministic JSON — encoding/json emits struct fields in declaration
	// order and map keys sorted, so the ETag is stable across restarts.
	// The public content contract is the flattened item array. The cached exam
	// metadata remains available to readiness/internal callers, but must not be
	// serialized as a student-facing object (and must never include answer keys).
	raw, err := json.Marshal(content.Items)
	if err != nil {
		err = fmt.Errorf("marshal content: %w", err)
		s.markUnready(examID, err)
		return err
	}
	etagView, err := json.Marshal(struct {
		ExamID string                `json:"exam_id"`
		Items  []inbound.ExamItemDTO `json:"items"`
	}{ExamID: exam.ID, Items: content.Items})
	if err != nil {
		err = fmt.Errorf("marshal content etag view: %w", err)
		s.markUnready(examID, err)
		return err
	}
	sum := sha256.Sum256(etagView)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"` // quoted per RFC 7232
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		err = fmt.Errorf("gzip content: %w", err)
		s.markUnready(examID, err)
		return err
	}
	if err := gz.Close(); err != nil {
		err = fmt.Errorf("close gzip writer: %w", err)
		s.markUnready(examID, err)
		return err
	}

	// HIGH-2: re-check the generation under the publish lock immediately
	// before mutating state — a newer BeginRebuild that started while this
	// rebuild was reading rows must win the publication race.
	s.mu.Lock()
	if len(token) > 0 && s.generation[examID] != token[0] {
		s.mu.Unlock()
		return fmt.Errorf("%w: generation %d superseded by %d before publish",
			node_error.ErrExamContentNotReady, token[0], s.generation[examID])
	}
	s.cache[exam.ID] = &contentCache{content: content, etag: etag, gzipBytes: buf.Bytes(), rawBytes: raw}
	delete(s.unready, exam.ID)
	s.mu.Unlock()
	return nil
}

// ReloadAllCaches rebuilds every persisted exam cache after an out-of-process
// bundleload. The node server keeps cache state in memory, so a database-only
// bundle replacement must explicitly republish the current snapshot before it
// can serve students again.
func (s *ContentService) ReloadAllCaches(ctx context.Context) error {
	exams, err := s.repo.ListExams(ctx)
	if err != nil {
		return fmt.Errorf("list exams for cache reload: %w", err)
	}
	sort.Slice(exams, func(i, j int) bool { return exams[i].ID < exams[j].ID })

	persisted := make(map[string]struct{}, len(exams))
	tokens := make(map[string]uint64, len(exams))
	s.mu.Lock()
	for _, exam := range exams {
		persisted[exam.ID] = struct{}{}
	}
	// Mark both current and incoming IDs unready before the first rebuild. This
	// prevents a request from observing stale content during the refresh.
	for id := range s.cache {
		s.generation[id]++
		s.unready[id] = fmt.Errorf("%w: cache reload in progress", node_error.ErrExamContentNotReady)
	}
	for _, exam := range exams {
		s.generation[exam.ID]++
		tokens[exam.ID] = s.generation[exam.ID]
		s.unready[exam.ID] = fmt.Errorf("%w: cache reload in progress", node_error.ErrExamContentNotReady)
	}
	s.mu.Unlock()

	for _, exam := range exams {
		unlock := s.LockExam(exam.ID)
		rebuildErr := s.RebuildExam(ctx, exam.ID, tokens[exam.ID])
		unlock()
		if rebuildErr != nil {
			return fmt.Errorf("rebuild content cache for exam %s: %w", exam.ID, rebuildErr)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.cache {
		if _, ok := persisted[id]; !ok {
			delete(s.cache, id)
		}
	}
	for id := range s.unready {
		if _, ok := persisted[id]; !ok {
			delete(s.unready, id)
		}
	}
	return nil
}

func (s *ContentService) markUnready(examID string, cause error) {
	s.mu.Lock()
	s.unready[examID] = cause
	s.mu.Unlock()
}

// UnreadyExams returns every exam whose latest publication attempt failed or
// is still in flight — /readyz turns this into a 503 until retries succeed.
func (s *ContentService) UnreadyExams() map[string]error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]error, len(s.unready))
	for id, cause := range s.unready {
		out[id] = cause
	}
	return out
}

// CacheReadyExams returns the IDs of persisted exams whose content cache has
// been successfully built. Readiness requires this set to cover every exam
// in the database — and at least one exam to exist.
func (s *ContentService) CacheReadyExams() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.cache))
	for id := range s.cache {
		out = append(out, id)
	}
	return out
}

// DropFromCache removes an exam's cache entry (test/ops helper for simulating
// a DB-committed bundle whose cache publication never happened).
func (s *ContentService) DropFromCache(examID string) {
	s.mu.Lock()
	delete(s.cache, examID)
	s.mu.Unlock()
}

// GetExamContent returns the cached content for exactly the requested exam.
// A mismatched or unbuilt exam is an error — never another exam's content.
// An exam whose latest rebuild failed is refused with ErrExamContentNotReady:
// stale cache must never be served while the DB holds newer rows.
func (s *ContentService) GetExamContent(_ context.Context, examID string) (*inbound.ExamContent, string, []byte, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cause, bad := s.unready[examID]; bad {
		return nil, "", nil, nil, fmt.Errorf("%w: %v", node_error.ErrExamContentNotReady, cause)
	}
	cached, ok := s.cache[examID]
	if !ok {
		return nil, "", nil, nil, node_error.ErrExamNotLoaded
	}
	return cached.content, cached.etag, cached.gzipBytes, cached.rawBytes, nil
}
