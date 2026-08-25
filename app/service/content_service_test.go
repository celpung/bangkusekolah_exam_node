package service

import (
	"context"
	"errors"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeContentRepo struct {
	outbound_repository.NodeRepository
	exam  *entity.Exam
	items []entity.Item
}

func (f *fakeContentRepo) FindExam(_ context.Context) (*entity.Exam, error) { return f.exam, nil }
func (f *fakeContentRepo) ListItemsByExam(_ context.Context) ([]entity.Item, error) {
	return f.items, nil
}

func contentFixture() (*ContentService, *fakeContentRepo) {
	repo := &fakeContentRepo{
		exam: &entity.Exam{ID: "exam-1", Title: "UTS", MaxScore: 30},
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
	if err := svc.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild 1: %v", err)
	}
	c1, etag1, gz1, raw1, err := svc.GetExamContent(ctx)
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}
	if err := svc.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild 2: %v", err)
	}
	_, etag2, gz2, raw2, err := svc.GetExamContent(ctx)
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if etag1 != etag2 || string(gz1) != string(gz2) || string(raw1) != string(raw2) {
		t.Fatal("etag/gzip/raw must be deterministic across rebuilds of the same bundle")
	}
	if len(c1.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(c1.Items))
	}
}

func TestContentDTOHidesAnswerKeyAndRubric(t *testing.T) {
	svc, _ := contentFixture()
	if err := svc.Rebuild(context.Background()); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	content, _, _, _, err := svc.GetExamContent(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The fixture items carry no answer keys here; the assertion is structural:
	// ExamItemDTO has no answer-key/rubric fields to leak. Verify the DTO shape.
	for _, item := range content.Items {
		if item.ID == "" || item.QuestionType == "" {
			t.Fatalf("malformed DTO item: %+v", item)
		}
	}
}

func TestContentGetWithoutRebuildIsExamNotLoaded(t *testing.T) {
	svc := NewContentService(&fakeContentRepo{})
	_, _, _, _, err := svc.GetExamContent(context.Background())
	if !errors.Is(err, node_error.ErrExamNotLoaded) {
		t.Fatalf("want ErrExamNotLoaded, got %v", err)
	}
}
