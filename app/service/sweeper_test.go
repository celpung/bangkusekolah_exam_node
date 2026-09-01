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

type fakeSweeperRepo struct {
	outbound_repository.NodeRepository
	attempts map[string]*entity.Attempt
	answers  map[string][]entity.Answer
	exam     *entity.Exam
	failList map[string]bool
	failUpd  map[string]bool
}

func (f *fakeSweeperRepo) ListExpiredAttempts(_ context.Context, now time.Time) ([]entity.Attempt, error) {
	var out []entity.Attempt
	for _, a := range f.attempts {
		if a.Status == entity.AttemptInProgress && a.DueAt.Before(now) {
			out = append(out, *a)
		}
	}
	return out, nil
}
func (f *fakeSweeperRepo) ListAnswersByAttempt(_ context.Context, id string) ([]entity.Answer, error) {
	if f.failList[id] {
		return nil, errors.New("list answers failed")
	}
	return f.answers[id], nil
}
func (f *fakeSweeperRepo) FindAttemptByIDForUpdate(_ context.Context, id string) (*entity.Attempt, error) {
	if a, ok := f.attempts[id]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeSweeperRepo) UpdateAttempt(_ context.Context, a *entity.Attempt) error {
	if f.failUpd[a.ID] {
		return errors.New("update failed")
	}
	f.attempts[a.ID] = a
	return nil
}
func (f *fakeSweeperRepo) FindExamByID(_ context.Context, id string) (*entity.Exam, error) {
	if f.exam != nil && f.exam.ID == id {
		return f.exam, nil
	}
	return nil, node_error.ErrExamNotLoaded
}

func sweeperFixture() (*SweeperService, *fakeSweeperRepo) {
	repo := &fakeSweeperRepo{
		attempts: map[string]*entity.Attempt{},
		answers:  map[string][]entity.Answer{},
		exam:     &entity.Exam{ID: "exam-1", HasManualItems: false},
	}
	return NewSweeperService(repo, stubNodeTx{}), repo
}

func TestSweeperFinalizesPastDueAttempts(t *testing.T) {
	score := 5.0
	sweeper, repo := sweeperFixture()
	repo.attempts = map[string]*entity.Attempt{
		"att-past":   {ID: "att-past", ParticipantID: "part-1", ExamID: "exam-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(-time.Minute), MaxScore: 10},
		"att-future": {ID: "att-future", ParticipantID: "part-2", ExamID: "exam-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(time.Hour), MaxScore: 10},
	}
	repo.answers = map[string][]entity.Answer{
		"att-past": {{ID: "ans-1", AttemptID: "att-past", Score: &score, GradingStatus: entity.GradingAutoGraded}},
	}
	n, err := sweeper.SweepExpiredAttempts(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 || repo.attempts["att-past"].Status != entity.AttemptAutoSubmitted || repo.attempts["att-past"].AutoSubmittedAt == nil {
		t.Fatalf("sweeper did not finalize past-due: n=%d att=%+v", n, repo.attempts["att-past"])
	}
	if got := *repo.attempts["att-past"].Score; got != 5 {
		t.Fatalf("score = %v, want 5", got)
	}
	if repo.attempts["att-future"].Status != entity.AttemptInProgress {
		t.Fatalf("sweeper swept a future attempt")
	}
}

func TestSweeperIsNoopWhenNoneExpired(t *testing.T) {
	sweeper, _ := sweeperFixture()
	n, err := sweeper.SweepExpiredAttempts(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("want 0, got %d err %v", n, err)
	}
}

// TestSweeperManualExamWithoutAnswerRow pins BLOCKER-3 for the sweeper path.
func TestSweeperManualExamWithoutAnswerRow(t *testing.T) {
	sweeper, repo := sweeperFixture()
	repo.exam.HasManualItems = true
	repo.attempts["att-manual"] = &entity.Attempt{ID: "att-manual", ParticipantID: "part-1", ExamID: "exam-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(-time.Minute), MaxScore: 10}
	n, err := sweeper.SweepExpiredAttempts(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("sweep: n=%d err=%v", n, err)
	}
	att := repo.attempts["att-manual"]
	if att.GradingStatus != entity.GradingManualRequired {
		t.Fatalf("manual exam without answer row must be manual_required, got %q", att.GradingStatus)
	}
	if att.Score == nil || *att.Score != 0 {
		t.Fatalf("score = %v, want 0", att.Score)
	}
}

// TestSweeperReturnsErrorWhenFinalizeFails pins BLOCKER-2: a failed attempt
// must surface as a non-nil SweepError with the unfinished count.
func TestSweeperReturnsErrorWhenFinalizeFails(t *testing.T) {
	score := 5.0
	sweeper, repo := sweeperFixture()
	repo.attempts = map[string]*entity.Attempt{
		"att-ok":  {ID: "att-ok", ParticipantID: "part-1", ExamID: "exam-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(-time.Minute), MaxScore: 10},
		"att-bad": {ID: "att-bad", ParticipantID: "part-2", ExamID: "exam-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(-time.Minute), MaxScore: 10},
	}
	repo.answers = map[string][]entity.Answer{
		"att-ok":  {{ID: "ans-1", AttemptID: "att-ok", Score: &score, GradingStatus: entity.GradingAutoGraded}},
		"att-bad": {{ID: "ans-2", AttemptID: "att-bad", Score: &score, GradingStatus: entity.GradingAutoGraded}},
	}
	repo.failUpd = map[string]bool{"att-bad": true}
	n, err := sweeper.SweepExpiredAttempts(context.Background())
	if err == nil {
		t.Fatalf("sweep failure must return error")
	}
	var sweepErr *SweepError
	if !errors.As(err, &sweepErr) {
		t.Fatalf("error must be *SweepError, got %T", err)
	}
	if sweepErr.Failed != 1 || sweepErr.AttemptID != "att-bad" {
		t.Fatalf("SweepError = %+v", sweepErr)
	}
	if n != 1 {
		t.Fatalf("swept = %d, want 1 (only att-ok)", n)
	}
	if repo.attempts["att-bad"].Status != entity.AttemptInProgress {
		t.Fatalf("failed attempt must stay in_progress for retry")
	}
}

// TestSubmitVsSweeperLostRaceIsIdempotent pins the benign race: submit wins,
// the sweeper's stale finalize hits ErrAttemptLocked, and SweepExpiredAttempts
// treats it as a successful no-op — no SweepError, status stays submitted.
func TestSubmitVsSweeperLostRaceIsIdempotent(t *testing.T) {
	subSvc, subRepo := submitFixture()
	sweeper := &SweeperService{repo: subRepo, txManager: stubNodeTx{}}
	subRepo.exam.HasManualItems = false

	// student submits first
	submitted, err := subSvc.SubmitAttempt(context.Background(), "att-1", "part-1")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.Status != entity.AttemptSubmitted {
		t.Fatalf("expected submitted, got %q", submitted.Status)
	}

	// simulate the sweeper's stale expired list: it still holds the attempt as
	// in_progress because the list was computed before the submit landed
	subRepo.attempts["att-stale"] = &entity.Attempt{
		ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1", ExamID: "exam-1",
		Status: entity.AttemptInProgress, DueAt: time.Now().Add(-time.Minute), MaxScore: 30,
	}
	subRepo.answers["att-1"] = []entity.Answer{}

	n, err := sweeper.SweepExpiredAttempts(context.Background())
	if err != nil {
		t.Fatalf("benign race must not surface a sweep failure, got %v", err)
	}
	if n != 0 {
		t.Fatalf("swept = %d, want 0 (attempt already finalized by submit)", n)
	}
	final := subRepo.attempts["att-1"]
	if final.Status != entity.AttemptSubmitted {
		t.Fatalf("final status must remain submitted, got %q", final.Status)
	}
}
