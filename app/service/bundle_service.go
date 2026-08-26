package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// ContentRebuilder lets BundleService refresh the content cache after a load
// without importing the concrete ContentService.
type ContentRebuilder interface {
	RebuildExam(ctx context.Context, examID string) error
}

type BundleService struct {
	repo       outbound_repository.NodeRepository
	txManager  outbound.TxManager
	contentSvc ContentRebuilder
}

func NewBundleService(repo outbound_repository.NodeRepository, txManager outbound.TxManager, contentSvc ContentRebuilder) *BundleService {
	return &BundleService{repo: repo, txManager: txManager, contentSvc: contentSvc}
}

// ComputeBundleChecksum is byte-for-byte the same algorithm as central's
// examNodeBundleChecksum: canonical JSON of the bundle with the checksum field
// cleared, SHA-256, "sha256:" prefix. encoding/json emits struct fields in
// declaration order and map keys sorted, so both sides compute identical
// values from the same bytes.
func ComputeBundleChecksum(bundle inbound.ExamNodeBundle) string {
	sum := sha256.Sum256(canonicalBundleBytes(bundle))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func canonicalBundleBytes(bundle inbound.ExamNodeBundle) []byte {
	bundle.Checksum = ""
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return nil
	}
	return encoded
}

// LoadBundle validates and stores one exam's bundle atomically. Loading the
// same exam again replaces its rows — a half-loaded bundle (exam without
// items) would be a worse failure than refusing to load.
func (s *BundleService) LoadBundle(ctx context.Context, bundle inbound.ExamNodeBundle) error {
	for _, item := range bundle.Items {
		if item.QuestionType == "file_upload" {
			return fmt.Errorf("%w: item %s", node_error.ErrBundleFileUpload, item.ID)
		}
	}
	if want := ComputeBundleChecksum(bundle); bundle.Checksum != want {
		return fmt.Errorf("%w: got %q, want %q", node_error.ErrBundleChecksum, bundle.Checksum, want)
	}
	// Precompute max_score and has_manual_items so submit never reads items
	// inside its transaction.
	var maxScore float64
	hasManual := false
	for _, item := range bundle.Items {
		maxScore += item.Points
		if item.RequiresManualGrading {
			hasManual = true
		}
	}
	accessPrefix := ""
	if len(bundle.Participants) > 0 && len(bundle.Participants[0].AccessCode) >= 6 {
		accessPrefix = bundle.Participants[0].AccessCode[:6]
	}
	exam := &entity.Exam{
		ID: bundle.Exam.ID, DeploymentID: bundle.DeploymentID, Title: bundle.Exam.Title,
		Instruction: bundle.Exam.Instruction, StartsAt: bundle.Exam.StartsAt, EndsAt: bundle.Exam.EndsAt,
		DurationMinutes: bundle.Exam.DurationMinutes, MaxAttempts: bundle.Exam.MaxAttempts,
		ShuffleQuestions:      bundle.Exam.ShuffleQuestions,
		ShuffleOptions:        bundle.Exam.ShuffleOptions,
		ShowResultImmediately: bundle.Exam.ShowResultImmediately,
		PassingScore:          bundle.Exam.PassingScore,
		ResultSelectionPolicy: string(bundle.Exam.ResultSelectionPolicy),
		MaxScore:              maxScore, HasManualItems: hasManual,
		AccessCodePrefix: accessPrefix, BundleChecksum: bundle.Checksum,
	}
	items := make([]entity.Item, len(bundle.Items))
	for i, it := range bundle.Items {
		items[i] = entity.Item{
			ID: it.ID, ExamID: bundle.Exam.ID,
			SectionID: it.SectionID, SectionTitle: sectionTitle(bundle.Sections, it.SectionID),
			SectionSortOrder: sectionOrder(bundle.Sections, it.SectionID), SortOrder: it.SortOrder,
			QuestionType:        entity.QuestionType(it.QuestionType),
			PromptSnapshot:      it.PromptSnapshot,
			OptionsSnapshotJSON: it.OptionsSnapshotJSON, AnswerKeySnapshotJSON: it.AnswerKeySnapshotJSON,
			Points: it.Points, RequiresManualGrading: it.RequiresManualGrading,
			RubricCriteria: toRubricCriteria(it.RubricCriteria),
		}
	}
	participants := make([]entity.Participant, len(bundle.Participants))
	for i, p := range bundle.Participants {
		participants[i] = entity.Participant{ID: p.ID, ExamID: bundle.Exam.ID, StudentID: p.StudentID, StudentName: p.StudentName, AccessCode: p.AccessCode}
	}
	exam.ContentHash = contentHash(items, participants)
	if err := s.repo.ReplaceBundle(ctx, exam, items, participants); err != nil {
		return err
	}
	// Rebuild the content cache from what is now durably stored. If this
	// fails, the DB is correct but the cache is stale — GetExamContent keeps
	// serving the previous snapshot (or ErrExamNotLoaded for a new exam), and
	// Preflight refuses to pass until RebuildExam succeeds. The load can be
	// safely retried: ReplaceBundle is idempotent.
	return s.contentSvc.RebuildExam(ctx, exam.ID)
}

// Preflight re-checks one exam's stored bundle against central's expected
// numbers: item and participant counts scoped to this exam, plus the bundle
// checksum recomputed from what is actually stored. Disk and clock checks
// live in cmd/preflight — they need os.Stat and a network time source.
func (s *BundleService) Preflight(ctx context.Context, examID string, expectedItemCount, expectedParticipantCount int) error {
	exam, err := s.repo.FindExamByID(ctx, examID)
	if err != nil {
		return err
	}
	if exam == nil || exam.BundleChecksum == "" {
		return fmt.Errorf("%w: exam %s has no loaded bundle", node_error.ErrPreflightFailed, examID)
	}
	items, err := s.repo.ListItemsByExamID(ctx, examID)
	if err != nil {
		return err
	}
	participants, err := s.repo.ListParticipantsByExam(ctx, examID)
	if err != nil {
		return err
	}
	if len(items) != expectedItemCount || len(participants) != expectedParticipantCount {
		return fmt.Errorf("%w: exam %s items %d/%d participants %d/%d",
			node_error.ErrPreflightFailed, examID, len(items), expectedItemCount, len(participants), expectedParticipantCount)
	}
	// Content-hash re-verification: sections are not stored as rows, so the
	// full bundle cannot be reconstructed from the DB byte-identically.
	// LoadBundle records a hash of the stored item/participant set; preflight
	// recomputes it and fails if the rows were hand-edited since.
	recomputed := contentHash(items, participants)
	if recomputed != exam.ContentHash {
		return fmt.Errorf("%w: exam %s stored content hash %q does not match load-time %q",
			node_error.ErrPreflightFailed, examID, recomputed, exam.ContentHash)
	}
	return nil
}

func contentHash(items []entity.Item, participants []entity.Participant) string {
	type row struct {
		ID     string  `json:"id"`
		ExamID string  `json:"exam_id"`
		Points float64 `json:"points"`
	}
	out := struct {
		Items        []row `json:"items"`
		Participants []struct {
			ID         string `json:"id"`
			ExamID     string `json:"exam_id"`
			StudentID  string `json:"student_id"`
			AccessCode string `json:"access_code"`
		} `json:"participants"`
	}{}
	for _, it := range items {
		out.Items = append(out.Items, row{ID: it.ID, ExamID: it.ExamID, Points: it.Points})
	}
	for _, p := range participants {
		out.Participants = append(out.Participants, struct {
			ID         string `json:"id"`
			ExamID     string `json:"exam_id"`
			StudentID  string `json:"student_id"`
			AccessCode string `json:"access_code"`
		}{ID: p.ID, ExamID: p.ExamID, StudentID: p.StudentID, AccessCode: p.AccessCode})
	}
	b, _ := json.Marshal(out)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sectionTitle(sections []inbound.ExamNodeBundleSection, id string) string {
	for _, s := range sections {
		if s.ID == id {
			return s.Title
		}
	}
	return ""
}

func sectionOrder(sections []inbound.ExamNodeBundleSection, id string) int {
	for _, s := range sections {
		if s.ID == id {
			return s.SortOrder
		}
	}
	return 0
}

func toRubricCriteria(in []inbound.ExamNodeBundleRubricCriterion) []entity.RubricCriterion {
	out := make([]entity.RubricCriterion, len(in))
	for i, c := range in {
		out[i] = entity.RubricCriterion{ID: c.ID, Title: c.Title, MaxPoints: c.MaxPoints, SortOrder: c.SortOrder}
	}
	return out
}
