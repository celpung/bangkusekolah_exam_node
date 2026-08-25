package service

import (
	"context"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeSweeperRepo struct {
	outbound_repository.NodeRepository
	attempts map[string]*entity.Attempt
	answers  map[string][]entity.Answer
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
	return f.answers[id], nil
}
func (f *fakeSweeperRepo) UpdateAttempt(_ context.Context, a *entity.Attempt) error {
	f.attempts[a.ID] = a
	return nil
}

func TestSweeperFinalizesPastDueAttempts(t *testing.T) {
	score := 5.0
	repo := &fakeSweeperRepo{
		attempts: map[string]*entity.Attempt{
			"att-past":   {ID: "att-past", ParticipantID: "part-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(-time.Minute), MaxScore: 10},
			"att-future": {ID: "att-future", ParticipantID: "part-2", Status: entity.AttemptInProgress, DueAt: time.Now().Add(time.Hour), MaxScore: 10},
		},
		answers: map[string][]entity.Answer{
			"att-past": {{ID: "ans-1", AttemptID: "att-past", Score: &score, GradingStatus: entity.GradingAutoGraded}},
		},
	}
	sweeper := NewSweeperService(repo, stubNodeTx{})
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
	repo := &fakeSweeperRepo{attempts: map[string]*entity.Attempt{}}
	sweeper := NewSweeperService(repo, stubNodeTx{})
	n, err := sweeper.SweepExpiredAttempts(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("want 0, got %d err %v", n, err)
	}
}
