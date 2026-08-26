package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// ContentService caches immutable exam content keyed by exam ID. One VPS
// hosts multiple exams (typically 3-10), so every lookup and rebuild is
// scoped to a single exam — there is no global cache to cross-serve.
//
// Readiness contract: RebuildExam failure during a live bundle push marks the
// exam unready while the DB keeps the new rows; IsExamReady reports that
// state and /readyz fails until a retry succeeds.
type ContentService struct {
	repo    outbound_repository.NodeRepository
	mu      sync.RWMutex
	cache   map[string]*contentCache // key: exam ID
	unready map[string]error         // exams whose last rebuild failed
}

type contentCache struct {
	content   *inbound.ExamContent
	etag      string
	gzipBytes []byte
	rawBytes  []byte
}

func NewContentService(repo outbound_repository.NodeRepository) *ContentService {
	return &ContentService{repo: repo, cache: map[string]*contentCache{}, unready: map[string]error{}}
}

// RebuildExam must be called by BundleService after loading one exam's bundle
// (Task 19) and by startup rehydration. It is the only writer; GetExamContent
// serves from the snapshot. A failure marks the exam unready until a retry
// succeeds.
func (s *ContentService) RebuildExam(ctx context.Context, examID string) error {
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
	raw, err := json.Marshal(content)
	if err != nil {
		err = fmt.Errorf("marshal content: %w", err)
		s.markUnready(examID, err)
		return err
	}
	sum := sha256.Sum256(raw)
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

	s.mu.Lock()
	s.cache[exam.ID] = &contentCache{content: content, etag: etag, gzipBytes: buf.Bytes(), rawBytes: raw}
	delete(s.unready, exam.ID)
	s.mu.Unlock()
	return nil
}

func (s *ContentService) markUnready(examID string, cause error) {
	s.mu.Lock()
	s.unready[examID] = cause
	s.mu.Unlock()
}

// UnreadyExams returns every exam whose latest rebuild failed with its cause —
// /readyz turns this into a 503 until retries succeed.
func (s *ContentService) UnreadyExams() map[string]error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]error, len(s.unready))
	for id, cause := range s.unready {
		out[id] = cause
	}
	return out
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
