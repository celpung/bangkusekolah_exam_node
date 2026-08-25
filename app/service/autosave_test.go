package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeAutosaveRepo struct {
	outbound_repository.NodeRepository
	attempts map[string]*entity.Attempt
	answers  map[string]*entity.Answer // key: attemptID:itemID
	items    map[string]*entity.Item
}

func (f *fakeAutosaveRepo) FindAttemptByID(_ context.Context, id string) (*entity.Attempt, error) {
	if a, ok := f.attempts[id]; ok {
		return a, nil
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeAutosaveRepo) FindItemByID(_ context.Context, id string) (*entity.Item, error) {
	if it, ok := f.items[id]; ok {
		return it, nil
	}
	return nil, node_error.ErrItemNotFound
}
func (f *fakeAutosaveRepo) UpsertAnswer(_ context.Context, ans *entity.Answer) (*entity.Answer, error) {
	key := ans.AttemptID + ":" + ans.ItemID
	if existing, ok := f.answers[key]; ok && ans.ClientSeq <= existing.ClientSeq {
		return nil, node_error.ErrStaleAnswerWrite
	}
	copied := *ans
	f.answers[key] = &copied
	return &copied, nil
}

func autosaveFixture() (*AttemptService, *fakeAutosaveRepo) {
	repo := &fakeAutosaveRepo{
		attempts: map[string]*entity.Attempt{
			"att-1": {ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(time.Hour), MaxScore: 10},
		},
		answers: map[string]*entity.Answer{},
		items: map[string]*entity.Item{
			"item-1":     {ID: "item-1", QuestionType: entity.QuestionSingleChoice, Points: 10, AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"}, RequiresManualGrading: false},
			"item-essay": {ID: "item-essay", QuestionType: entity.QuestionEssay, Points: 20, RequiresManualGrading: true},
		},
	}
	svc := &AttemptService{repo: repo, txManager: stubNodeTx{}, idGen: stubNodeID{id: "ans-new"}}
	return svc, repo
}

func TestAutosaveStoresScoreForObjectiveItem(t *testing.T) {
	svc, repo := autosaveFixture()
	ans, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-1", map[string]interface{}{"answer": "a"}, nil, 1, "part-1")
	if err != nil {
		t.Fatalf("AutosaveAnswer: %v", err)
	}
	if ans.Score == nil || *ans.Score != 10 || ans.GradingStatus != entity.GradingAutoGraded {
		t.Fatalf("objective autosave must grade inline: score=%v status=%q", ans.Score, ans.GradingStatus)
	}
	if ans.ClientSeq != 1 || repo.answers["att-1:item-1"].ClientSeq != 1 {
		t.Fatalf("client_seq not stored: %+v", ans)
	}
}

func TestAutosaveLeavesEssayManualRequired(t *testing.T) {
	svc, _ := autosaveFixture()
	ans, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-essay", map[string]interface{}{"answer": "long essay"}, ptrStr("essay body"), 1, "part-1")
	if err != nil {
		t.Fatalf("AutosaveAnswer: %v", err)
	}
	if ans.GradingStatus != entity.GradingManualRequired || ans.Score != nil {
		t.Fatalf("essay must be manual_required with no score: %+v", ans)
	}
}

func TestAutosaveDropsStaleClientSeq(t *testing.T) {
	svc, _ := autosaveFixture()
	if _, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-1", map[string]interface{}{"answer": "A"}, nil, 2, "part-1"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	_, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-1", map[string]interface{}{"answer": "B"}, nil, 1, "part-1")
	if !errors.Is(err, node_error.ErrStaleAnswerWrite) {
		t.Fatalf("stale seq must be ErrStaleAnswerWrite, got %v", err)
	}
	// seq=3 overwrites seq=2
	if _, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-1", map[string]interface{}{"answer": "A"}, nil, 3, "part-1"); err != nil {
		t.Fatalf("newer seq: %v", err)
	}
}

func TestAutosaveRejectsLockedAttempt(t *testing.T) {
	svc, repo := autosaveFixture()
	repo.attempts["att-1"].Status = entity.AttemptSubmitted
	_, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-1", map[string]interface{}{"answer": "A"}, nil, 1, "part-1")
	if !errors.Is(err, node_error.ErrAttemptLocked) {
		t.Fatalf("want ErrAttemptLocked, got %v", err)
	}
}

func TestAutosaveRejectsWrongOwner(t *testing.T) {
	svc, _ := autosaveFixture()
	_, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-1", map[string]interface{}{"answer": "A"}, nil, 1, "part-2")
	if !errors.Is(err, node_error.ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestAutosaveRejectsExpiredAttempt(t *testing.T) {
	svc, repo := autosaveFixture()
	repo.attempts["att-1"].DueAt = time.Now().Add(-time.Minute)
	_, err := svc.AutosaveAnswer(context.Background(), "att-1", "item-1", map[string]interface{}{"answer": "A"}, nil, 1, "part-1")
	if !errors.Is(err, node_error.ErrAttemptExpired) {
		t.Fatalf("want ErrAttemptExpired, got %v", err)
	}
}

func ptrStr(s string) *string { return &s }
