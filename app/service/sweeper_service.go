package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
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

// SweepError reports how many expired attempts could not be finalized. The
// caller (harvest) must refuse the final drain while Failed > 0 — the node is
// destroyed after harvest, so a silently unfinished attempt loses student data.
type SweepError struct {
	Failed    int
	AttemptID string
	Cause     error
}

func (e *SweepError) Error() string {
	if e.AttemptID != "" {
		return "sweeper: attempt " + e.AttemptID + " not finalized: " + e.Cause.Error()
	}
	return "sweeper: " + e.Cause.Error()
}

func (e *SweepError) Unwrap() error { return e.Cause }

// SweepExpiredAttempts finalizes every in_progress attempt past due_at.
// Scoped per-exam: each attempt's exam is loaded by ID so multi-exam nodes
// do not share hasManualItems across exams.
func (s *SweeperService) SweepExpiredAttempts(ctx context.Context) (int, error) {
	expired, err := s.repo.ListExpiredAttempts(ctx, time.Now())
	if err != nil {
		return 0, &SweepError{Cause: err}
	}
	swept := 0
	var firstFailure *SweepError
	failed := 0
	for _, attempt := range expired {
		exam, err := s.repo.FindExamByID(ctx, attempt.ExamID)
		if err != nil {
			// If exam not found, treat as failure — do not silently drop.
			slog.WarnContext(ctx, "sweeper: exam not found for attempt", "attempt_id", attempt.ID, "exam_id", attempt.ExamID, "error", err)
			failed++
			if firstFailure == nil {
				firstFailure = &SweepError{Failed: 1, AttemptID: attempt.ID, Cause: err}
			}
			continue
		}
		if err := s.finalizeOne(ctx, exam.HasManualItems, attempt); err != nil {
			// A concurrent submit already finalized this attempt — a safe,
			// idempotent race, not a failure. Skip it without an error.
			if errors.Is(err, node_error.ErrAttemptLocked) {
				continue
			}
			slog.WarnContext(ctx, "sweeper: attempt not finalized", "attempt_id", attempt.ID, "error", err)
			failed++
			if firstFailure == nil {
				firstFailure = &SweepError{Failed: 1, AttemptID: attempt.ID, Cause: err}
			}
			continue
		}
		swept++
	}
	if firstFailure != nil {
		firstFailure.Failed = failed
		return swept, firstFailure
	}
	return swept, nil
}

func (s *SweeperService) finalizeOne(ctx context.Context, hasManualItems bool, attempt entity.Attempt) error {
	answers, err := s.repo.ListAnswersByAttempt(ctx, attempt.ID)
	if err != nil {
		return err
	}
	total, gradingStatus := finalizeStatus(answers, hasManualItems)
	now := time.Now().UTC()
	attempt.Score = &total
	attempt.GradingStatus = gradingStatus
	attempt.Status = entity.AttemptAutoSubmitted
	attempt.AutoSubmittedAt = &now
	return s.txManager.Atomic(ctx, func(txCtx context.Context) error {
		return s.repo.UpdateAttempt(txCtx, &attempt)
	})
}

// Start runs the sweep ticker until ctx is cancelled. Called from
// cmd/examnode/main.go alongside the harvest ticker; both share the same ctx.
// A failed tick is logged but never stops the loop; failures are surfaced by
// the synchronous pre-harvest sweep.
func (s *SweeperService) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SweepExpiredAttempts(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "sweeper tick failed", "error", err)
			}
		}
	}
}
