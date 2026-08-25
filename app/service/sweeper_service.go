package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type SweeperService struct {
	repo      outbound_repository.NodeRepository
	txManager outbound.TxManager
}

func NewSweeperService(repo outbound_repository.NodeRepository, txManager outbound.TxManager) *SweeperService {
	return &SweeperService{repo: repo, txManager: txManager}
}

// SweepExpiredAttempts finalizes every in_progress attempt past due_at.
// It is idempotent: a concurrently submitted attempt is skipped.
func (s *SweeperService) SweepExpiredAttempts(ctx context.Context) (int, error) {
	expired, err := s.repo.ListExpiredAttempts(ctx, time.Now())
	if err != nil {
		return 0, err
	}
	swept := 0
	for _, attempt := range expired {
		answers, err := s.repo.ListAnswersByAttempt(ctx, attempt.ID)
		if err != nil {
			slog.WarnContext(ctx, "sweeper: list answers failed", "attempt_id", attempt.ID, "error", err)
			continue
		}
		total, gradingStatus, _ := sumAnswerScores(answers)
		now := time.Now().UTC()
		attempt.Score = &total
		attempt.GradingStatus = gradingStatus
		attempt.Status = entity.AttemptAutoSubmitted
		attempt.AutoSubmittedAt = &now
		if err := s.txManager.Atomic(ctx, func(txCtx context.Context) error {
			return s.repo.UpdateAttempt(txCtx, &attempt)
		}); err != nil {
			slog.WarnContext(ctx, "sweeper: update failed", "attempt_id", attempt.ID, "error", err)
			continue
		}
		swept++
	}
	return swept, nil
}

// Start runs the sweep ticker until ctx is cancelled. Called from
// cmd/examnode/main.go alongside the harvest ticker; both share the same ctx.
func (s *SweeperService) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SweepExpiredAttempts(ctx); err != nil {
				slog.ErrorContext(ctx, "sweeper tick failed", "error", err)
			}
		}
	}
}
