package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeBundleRepo struct {
	outbound_repository.NodeRepository
	exam         *entity.Exam
	items        map[string]*entity.Item
	participants map[string]*entity.Participant
	loadCalls    int
}

func (f *fakeBundleRepo) CreateExam(_ context.Context, e *entity.Exam) error { f.exam = e; return nil }
func (f *fakeBundleRepo) CreateItems(_ context.Context, items []entity.Item) error {
	for i := range items {
		copied := items[i]
		f.items[items[i].ID] = &copied
	}
	return nil
}
func (f *fakeBundleRepo) CreateParticipants(_ context.Context, ps []entity.Participant) error {
	for i := range ps {
		copied := ps[i]
		f.participants[ps[i].ID] = &copied
	}
	return nil
}
func (f *fakeBundleRepo) FindExam(_ context.Context) (*entity.Exam, error) {
	if f.exam == nil {
		return nil, node_error.ErrExamNotLoaded
	}
	return f.exam, nil
}
func (f *fakeBundleRepo) ReplaceBundle(_ context.Context, exam *entity.Exam, items []entity.Item, participants []entity.Participant) error {
	f.loadCalls++
	f.exam = exam
	for i := range items {
		copied := items[i]
		f.items[items[i].ID] = &copied
	}
	for i := range participants {
		copied := participants[i]
		f.participants[participants[i].ID] = &copied
	}
	return nil
}
func (f *fakeBundleRepo) ListItemsByExam(_ context.Context) ([]entity.Item, error) {
	out := make([]entity.Item, 0, len(f.items))
	for _, it := range f.items {
		out = append(out, *it)
	}
	return out, nil
}
func (f *fakeBundleRepo) ListItemsByExamID(_ context.Context, _ string) ([]entity.Item, error) {
	out := make([]entity.Item, 0, len(f.items))
	for _, it := range f.items {
		out = append(out, *it)
	}
	return out, nil
}
func (f *fakeBundleRepo) FindExamByID(_ context.Context, id string) (*entity.Exam, error) {
	if f.exam == nil || f.exam.ID != id {
		return nil, node_error.ErrExamNotLoaded
	}
	return f.exam, nil
}
func (f *fakeBundleRepo) ListParticipants(_ context.Context) ([]entity.Participant, error) {
	out := make([]entity.Participant, 0, len(f.participants))
	for _, p := range f.participants {
		out = append(out, *p)
	}
	return out, nil
}

type fakeContentSvc struct{ rebuilt bool }

func (f *fakeContentSvc) RebuildExam(_ context.Context, _ string) error { f.rebuilt = true; return nil }

func fakeBundle() inbound.ExamNodeBundle {
	now := time.Now()
	instr := "Kerjakan dengan jujur"
	bundle := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-1",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-b1", Title: "UTS", Instruction: &instr,
			StartsAt: now.Add(time.Hour), EndsAt: now.Add(3 * time.Hour),
			DurationMinutes: 90, MaxAttempts: 1, ResultSelectionPolicy: entity.ResultSelectionBest,
		},
		Sections:     []inbound.ExamNodeBundleSection{{ID: "sec-1", Title: "Bagian A", SortOrder: 1}},
		Items:        []inbound.ExamNodeBundleItem{{ID: "item-b1", SectionID: "sec-1", QuestionType: entity.QuestionSingleChoice, PromptSnapshot: "2+2?", Points: 10, SortOrder: 1, AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"}}},
		Participants: []inbound.ExamNodeBundleParticipant{{ID: "part-b1", StudentID: "stu-1", StudentName: "Budi", AccessCode: "K7M2QX-3B9FTD"}},
		Checksum:     "",
	}
	bundle.Checksum = ComputeBundleChecksum(bundle)
	return bundle
}

func bundleFixture() (*BundleService, *fakeBundleRepo, *fakeContentSvc) {
	repo := &fakeBundleRepo{items: map[string]*entity.Item{}, participants: map[string]*entity.Participant{}}
	content := &fakeContentSvc{}
	svc := NewBundleService(repo, stubNodeTx{}, content)
	return svc, repo, content
}

func TestLoadBundleInsertsExamItemsAndParticipants(t *testing.T) {
	svc, repo, content := bundleFixture()
	bundle := fakeBundle()

	if err := svc.LoadBundle(context.Background(), bundle); err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if repo.exam == nil || repo.exam.ID != "exam-b1" || len(repo.items) != 1 || len(repo.participants) != 1 {
		t.Fatalf("bundle not inserted: exam=%+v items=%d participants=%d", repo.exam, len(repo.items), len(repo.participants))
	}
	if repo.exam.MaxScore != 10 || repo.exam.HasManualItems != false {
		t.Fatalf("precomputed fields: max_score=%v has_manual=%v", repo.exam.MaxScore, repo.exam.HasManualItems)
	}
	if repo.exam.AccessCodePrefix != "K7M2QX" {
		t.Fatalf("access prefix = %q, want K7M2QX", repo.exam.AccessCodePrefix)
	}
	if !content.rebuilt {
		t.Fatal("content cache must be rebuilt after load")
	}
}

func TestLoadBundleRejectsFileUploadItem(t *testing.T) {
	svc, _, _ := bundleFixture()
	bundle := fakeBundle()
	bundle.Items[0].QuestionType = "file_upload"
	bundle.Checksum = ComputeBundleChecksum(bundle)

	err := svc.LoadBundle(context.Background(), bundle)
	if !errors.Is(err, node_error.ErrBundleFileUpload) {
		t.Fatalf("file_upload: got %v, want ErrBundleFileUpload", err)
	}
}

func TestLoadBundleRejectsChecksumMismatch(t *testing.T) {
	svc, repo, _ := bundleFixture()
	bundle := fakeBundle()
	bundle.Checksum = "sha256:deadbeef"

	if err := svc.LoadBundle(context.Background(), bundle); !errors.Is(err, node_error.ErrBundleChecksum) {
		t.Fatalf("checksum mismatch: got %v, want ErrBundleChecksum", err)
	}
	if repo.exam != nil {
		t.Fatal("nothing must be written on checksum mismatch")
	}
}

// TestLoadBundleReplacesPreviousBundle pins multi-exam reload semantics:
// loading the same exam again replaces its rows instead of duplicating.
func TestLoadBundleReplacesPreviousBundle(t *testing.T) {
	svc, repo, _ := bundleFixture()
	ctx := context.Background()
	if err := svc.LoadBundle(ctx, fakeBundle()); err != nil {
		t.Fatalf("first load: %v", err)
	}
	if err := svc.LoadBundle(ctx, fakeBundle()); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if len(repo.items) != 1 || len(repo.participants) != 1 {
		t.Fatalf("reload duplicated rows: items=%d participants=%d", len(repo.items), len(repo.participants))
	}
}

func TestPreflightFailsWhenCountsMismatch(t *testing.T) {
	svc, repo, _ := bundleFixture()
	if err := svc.LoadBundle(context.Background(), fakeBundle()); err != nil {
		t.Fatalf("load: %v", err)
	}
	_ = repo
	// Simulate central saying "expected 2 items" — bundle has 1, so preflight fails.
	err := svc.Preflight(context.Background(), "exam-b1", 2, 1)
	if err == nil || !errors.Is(err, node_error.ErrPreflightFailed) {
		t.Fatalf("preflight with wrong item count should fail with ErrPreflightFailed, got %v", err)
	}
}

func TestPreflightPassesWithMatchingCounts(t *testing.T) {
	svc, _, _ := bundleFixture()
	if err := svc.LoadBundle(context.Background(), fakeBundle()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := svc.Preflight(context.Background(), "exam-b1", 1, 1); err != nil {
		t.Fatalf("preflight should pass, got %v", err)
	}
}
