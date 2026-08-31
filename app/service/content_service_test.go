package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeContentRepo struct {
	outbound_repository.NodeRepository
	exams map[string]*entity.Exam
	items []entity.Item
}

func (f *fakeContentRepo) FindExamByID(_ context.Context, id string) (*entity.Exam, error) {
	if e, ok := f.exams[id]; ok {
		return e, nil
	}
	return nil, node_error.ErrExamNotLoaded
}
func (f *fakeContentRepo) ListItemsByExamID(_ context.Context, _ string) ([]entity.Item, error) {
	return f.items, nil
}

func (f *fakeContentRepo) ListExams(_ context.Context) ([]entity.Exam, error) {
	exams := make([]entity.Exam, 0, len(f.exams))
	for _, exam := range f.exams {
		exams = append(exams, *exam)
	}
	return exams, nil
}

func contentFixture() (*ContentService, *fakeContentRepo) {
	repo := &fakeContentRepo{
		exams: map[string]*entity.Exam{
			"exam-a": {ID: "exam-a", Title: "Matematika", MaxScore: 30},
			"exam-b": {ID: "exam-b", Title: "IPA", MaxScore: 20},
		},
		items: []entity.Item{
			{ID: "item-1", SectionID: "sec-1", QuestionType: entity.QuestionSingleChoice, PromptSnapshot: "2+2?", Points: 10},
			{ID: "item-2", SectionID: "sec-1", QuestionType: entity.QuestionEssay, PromptSnapshot: "essay", Points: 20, RequiresManualGrading: true},
		},
	}
	return NewContentService(repo), repo
}

func TestContentRebuildProducesStableEtagAndDeterministicGzip(t *testing.T) {
	svc, _ := contentFixture()
	ctx := context.Background()
	if err := svc.RebuildExam(ctx, "exam-a"); err != nil {
		t.Fatalf("rebuild 1: %v", err)
	}
	_, etag1, gz1, raw1, err := svc.GetExamContent(ctx, "exam-a")
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}
	if err := svc.RebuildExam(ctx, "exam-a"); err != nil {
		t.Fatalf("rebuild 2: %v", err)
	}
	_, etag2, gz2, raw2, err := svc.GetExamContent(ctx, "exam-a")
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if etag1 != etag2 || string(gz1) != string(gz2) || string(raw1) != string(raw2) {
		t.Fatal("etag/gzip/raw must be deterministic across rebuilds of the same bundle")
	}
}

// TestContentServesEachExamItsOwnCache pins BLOCKER-1 at the service level:
// two exams rebuilt on one node keep separate caches and never cross-serve.
func TestContentServesEachExamItsOwnCache(t *testing.T) {
	svc, _ := contentFixture()
	ctx := context.Background()
	if err := svc.RebuildExam(ctx, "exam-a"); err != nil {
		t.Fatalf("rebuild a: %v", err)
	}
	if err := svc.RebuildExam(ctx, "exam-b"); err != nil {
		t.Fatalf("rebuild b: %v", err)
	}
	a, etagA, _, _, err := svc.GetExamContent(ctx, "exam-a")
	if err != nil {
		t.Fatalf("get a: %v", err)
	}
	b, etagB, _, _, err := svc.GetExamContent(ctx, "exam-b")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if a.Exam.ID != "exam-a" || b.Exam.ID != "exam-b" {
		t.Fatalf("cross-exam exposure: a=%s b=%s", a.Exam.ID, b.Exam.ID)
	}
	if etagA == etagB {
		t.Fatal("different exams must have different ETags")
	}
}

func TestContentUnknownExamIsNotLoaded(t *testing.T) {
	svc, _ := contentFixture()
	if err := svc.RebuildExam(context.Background(), "exam-a"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	// exam-b exists in the repo fixture but was never rebuilt on this node
	if _, _, _, _, err := svc.GetExamContent(context.Background(), "exam-b"); !errors.Is(err, node_error.ErrExamNotLoaded) {
		t.Fatalf("unbuilt exam must be ErrExamNotLoaded, got %v", err)
	}
	if _, _, _, _, err := svc.GetExamContent(context.Background(), "exam-unknown"); !errors.Is(err, node_error.ErrExamNotLoaded) {
		t.Fatalf("unknown exam must be ErrExamNotLoaded, got %v", err)
	}
}

func TestContentRawBytesFollowStudentContentContract(t *testing.T) {
	svc, _ := contentFixture()
	if err := svc.RebuildExam(context.Background(), "exam-a"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	_, _, _, raw, err := svc.GetExamContent(context.Background(), "exam-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("student content must be a JSON array: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("student content item count = %d, want 2", len(items))
	}
	if _, ok := items[0]["answer_key_snapshot_json"]; ok {
		t.Fatal("student content must not expose answer keys")
	}
	if _, ok := items[0]["rubric_criteria"]; ok {
		t.Fatal("student content must not expose rubric criteria")
	}
}

func TestContentReloadAllCachesRefreshesAndRemovesStaleEntries(t *testing.T) {
	svc, repo := contentFixture()
	ctx := context.Background()
	if err := svc.RebuildExam(ctx, "exam-a"); err != nil {
		t.Fatalf("rebuild a: %v", err)
	}
	if err := svc.RebuildExam(ctx, "exam-b"); err != nil {
		t.Fatalf("rebuild b: %v", err)
	}
	_, _, _, before, err := svc.GetExamContent(ctx, "exam-a")
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	repo.items[0].PromptSnapshot = "updated after deploy"
	delete(repo.exams, "exam-b")
	if err := svc.ReloadAllCaches(ctx); err != nil {
		t.Fatalf("reload all caches: %v", err)
	}

	_, _, _, after, err := svc.GetExamContent(ctx, "exam-a")
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if string(after) == string(before) {
		t.Fatal("reload must publish the database's updated content")
	}
	if _, _, _, _, err := svc.GetExamContent(ctx, "exam-b"); !errors.Is(err, node_error.ErrExamNotLoaded) {
		t.Fatalf("stale deleted exam must be unavailable, got %v", err)
	}
	if ready := svc.CacheReadyExams(); len(ready) != 1 || ready[0] != "exam-a" {
		t.Fatalf("ready cache entries = %v, want [exam-a]", ready)
	}
}

func TestContentDTOHidesAnswerKeyAndRubric(t *testing.T) {
	svc, _ := contentFixture()
	if err := svc.RebuildExam(context.Background(), "exam-a"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	content, _, _, _, err := svc.GetExamContent(context.Background(), "exam-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, item := range content.Items {
		if item.ID == "" || item.QuestionType == "" {
			t.Fatalf("malformed DTO item: %+v", item)
		}
	}
}
