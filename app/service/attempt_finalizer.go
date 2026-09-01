package service

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// finalizeAttempt serializes finalization with answer writes and other
// finalizers. The repository's conditional update remains the last guard, so
// this is safe even when a stale sweeper snapshot races a student submit.
func finalizeAttempt(
	ctx context.Context,
	repo outbound_repository.NodeRepository,
	txManager outbound.TxManager,
	attemptID string,
	auto bool,
) (*entity.Attempt, bool, error) {
	var result *entity.Attempt
	finalized := false
	err := txManager.Atomic(ctx, func(txCtx context.Context) error {
		var err error
		result, finalized, err = finalizeAttemptLocked(txCtx, repo, attemptID, auto)
		return err
	})
	return result, finalized, err
}

// finalizeAttemptLocked expects to run inside an existing transaction. It
// locks the attempt before reading answers, so the score and status describe a
// single consistent sitting.
func finalizeAttemptLocked(
	ctx context.Context,
	repo outbound_repository.NodeRepository,
	attemptID string,
	auto bool,
) (*entity.Attempt, bool, error) {
	attempt, err := repo.FindAttemptByIDForUpdate(ctx, attemptID)
	if err != nil {
		return nil, false, err
	}
	if attempt.Status != entity.AttemptInProgress {
		return attempt, false, nil
	}

	exam, err := repo.FindExamByID(ctx, attempt.ExamID)
	if err != nil {
		return nil, false, err
	}
	answers, err := repo.ListAnswersByAttempt(ctx, attempt.ID)
	if err != nil {
		return nil, false, err
	}
	total, gradingStatus := finalizeStatus(answers, exam.HasManualItems)
	now := time.Now().UTC()
	attempt.Score = &total
	attempt.GradingStatus = gradingStatus
	attempt.SubmittedAt = &now
	if auto {
		attempt.Status = entity.AttemptAutoSubmitted
		attempt.AutoSubmittedAt = &now
	} else {
		attempt.Status = entity.AttemptSubmitted
	}
	if err := repo.UpdateAttempt(ctx, attempt); err != nil {
		return nil, false, err
	}
	return attempt, true, nil
}
