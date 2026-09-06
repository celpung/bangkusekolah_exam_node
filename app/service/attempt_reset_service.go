package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type AttemptResetService struct {
	repo       outbound_repository.AttemptResetRepository
	operations outbound_repository.AttemptResetOperationRepository
	txManager  outbound.TxManager
	clock      AttemptClock
}

func NewAttemptResetService(
	repo outbound_repository.AttemptResetRepository,
	operations outbound_repository.AttemptResetOperationRepository,
	txManager outbound.TxManager,
) *AttemptResetService {
	return NewAttemptResetServiceWithClock(repo, operations, txManager, systemAttemptClock{})
}

func NewAttemptResetServiceWithClock(
	repo outbound_repository.AttemptResetRepository,
	operations outbound_repository.AttemptResetOperationRepository,
	txManager outbound.TxManager,
	clock AttemptClock,
) *AttemptResetService {
	if clock == nil {
		clock = systemAttemptClock{}
	}
	return &AttemptResetService{repo: repo, operations: operations, txManager: txManager, clock: clock}
}

func (s *AttemptResetService) ApplyReset(ctx context.Context, command inbound.AttemptResetCommand) (*inbound.AttemptResetOutcome, error) {
	if err := validateAttemptResetCommand(command); err != nil {
		return nil, err
	}
	commandHash, err := attemptResetCommandHash(command)
	if err != nil {
		return nil, err
	}

	var outcome *inbound.AttemptResetOutcome
	err = s.txManager.Atomic(ctx, func(txCtx context.Context) error {
		operation, err := s.operations.FindAttemptResetOperationForUpdate(txCtx, command.RequestID)
		if err != nil && !errors.Is(err, node_error.ErrAttemptResetOperationNotFound) {
			return err
		}
		if operation != nil {
			if operation.CommandHash != commandHash || !sameResetCommand(operation, command) {
				return node_error.ErrAttemptResetConflict
			}
			if operation.Status != entity.AttemptResetPending {
				outcome = attemptResetOperationOutcome(operation)
				return nil
			}
		} else {
			operation = newAttemptResetOperation(command, commandHash, s.clock.Now().UTC())
			if err := s.operations.CreateAttemptResetOperation(txCtx, operation); err != nil {
				return err
			}
		}

		now := s.clock.Now().UTC()
		if !now.Before(command.Deadline) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetExpired, "reset_deadline_expired", node_error.ErrAttemptResetExpired, &outcome)
		}
		exam, err := findAttemptResetExamForUpdate(txCtx, s.repo, command.ExamID)
		if err != nil {
			if errors.Is(err, node_error.ErrExamNotLoaded) {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "exam_not_loaded", node_error.ErrAttemptResetConflict, &outcome)
			}
			return err
		}
		if exam.DeploymentID != command.DeploymentID {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "deployment_mismatch", node_error.ErrAttemptResetConflict, &outcome)
		}
		if exam.FencedAt != nil {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "deployment_fenced", node_error.ErrAttemptResetFenced, &outcome)
		}
		if !now.Before(exam.EndsAt) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetExpired, "exam_window_closed", node_error.ErrAttemptResetExpired, &outcome)
		}

		participant, err := s.repo.FindParticipantByIDForUpdate(txCtx, command.ParticipantID)
		if err != nil {
			if errors.Is(err, node_error.ErrParticipantNotFound) {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "participant_not_found", node_error.ErrAttemptResetConflict, &outcome)
			}
			return err
		}
		attempt, err := s.repo.FindAttemptByIDForUpdate(txCtx, command.AttemptID)
		if err != nil {
			if errors.Is(err, node_error.ErrAttemptNotFound) {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "attempt_not_found", node_error.ErrAttemptNotFound, &outcome)
			}
			return err
		}
		if participant.ExamID != command.ExamID || participant.DeploymentID != command.DeploymentID || participant.StudentID != command.StudentID ||
			attempt.ParticipantID != participant.ID || attempt.ExamID != command.ExamID || attempt.StudentID != command.StudentID ||
			participant.LatestAttemptID == nil || *participant.LatestAttemptID != attempt.ID {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "target_conflict", node_error.ErrAttemptResetConflict, &outcome)
		}
		if attempt.ResetGeneration != command.ExpectedGeneration || !attempt.DueAt.Equal(command.ExpectedDueAt) ||
			!sameTimePointer(attempt.SubmittedAt, command.ExpectedSubmittedAt) ||
			!sameTimePointer(attempt.AutoSubmittedAt, command.ExpectedAutoSubmittedAt) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "target_changed", node_error.ErrAttemptResetConflict, &outcome)
		}
		if attempt.Status == entity.AttemptInProgress {
			if command.ExpectedStatus != entity.AttemptInProgress {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "target_changed", node_error.ErrAttemptResetConflict, &outcome)
			}
			return s.finishOutcome(txCtx, operation, attempt, inbound.AttemptResetAlreadyInProgress, "already_in_progress", "attempt is already in progress", &outcome)
		}
		if attempt.Status != command.ExpectedStatus {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "target_changed", node_error.ErrAttemptResetConflict, &outcome)
		}
		if !now.Before(attempt.DueAt) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetExpired, "attempt_deadline_expired", node_error.ErrAttemptResetExpired, &outcome)
		}
		if attempt.Status != entity.AttemptSubmitted && attempt.Status != entity.AttemptAutoSubmitted {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "status_not_resettable", node_error.ErrAttemptResetNotAllowed, &outcome)
		}
		if command.TargetGeneration != command.ExpectedGeneration+1 {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "generation_invalid", node_error.ErrAttemptResetConflict, &outcome)
		}
		writeNow := s.clock.Now().UTC()
		if !writeNow.Before(command.Deadline) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetExpired, "reset_deadline_expired", node_error.ErrAttemptResetExpired, &outcome)
		}
		if !writeNow.Before(attempt.DueAt) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetExpired, "attempt_deadline_expired", node_error.ErrAttemptResetExpired, &outcome)
		}
		if !writeNow.Before(exam.EndsAt) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetExpired, "exam_window_closed", node_error.ErrAttemptResetExpired, &outcome)
		}

		attempt.Status = entity.AttemptInProgress
		attempt.SubmittedAt = nil
		attempt.AutoSubmittedAt = nil
		attempt.Score = nil
		attempt.GradingStatus = entity.GradingPending
		attempt.ResetGeneration = command.TargetGeneration
		if err := s.repo.ResetAttempt(txCtx, attempt, command.ExpectedStatus, command.ExpectedGeneration); err != nil {
			if errors.Is(err, node_error.ErrAttemptResetConflict) {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "reset_conflict", node_error.ErrAttemptResetConflict, &outcome)
			}
			return err
		}
		return s.finishOutcome(txCtx, operation, attempt, inbound.AttemptResetApplied, "reset_applied", "attempt reset applied", &outcome)
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}

// PreviewReset reads the latest local attempt for the selected participant.
// It is deliberately a separate operation from ApplyReset: preview never
// changes an attempt and returns metadata only so Central can prepare a
// compare-and-set reset command for an attempt that may not yet exist there.
func (s *AttemptResetService) PreviewReset(ctx context.Context, command inbound.AttemptResetCommand) (*inbound.AttemptResetOutcome, error) {
	if err := validateAttemptResetPreviewCommand(command); err != nil {
		return nil, err
	}
	commandHash, err := attemptResetCommandHash(command)
	if err != nil {
		return nil, err
	}
	var outcome *inbound.AttemptResetOutcome
	err = s.txManager.Atomic(ctx, func(txCtx context.Context) error {
		operation, err := s.operations.FindAttemptResetOperationForUpdate(txCtx, command.RequestID)
		if err != nil && !errors.Is(err, node_error.ErrAttemptResetOperationNotFound) {
			return err
		}
		if operation != nil {
			if operation.CommandHash != commandHash || !sameResetCommand(operation, command) {
				return node_error.ErrAttemptResetConflict
			}
			if operation.Status != entity.AttemptResetPending {
				outcome = attemptResetOperationOutcome(operation)
				return nil
			}
		} else {
			operation = newAttemptResetOperation(command, commandHash, s.clock.Now().UTC())
			if err := s.operations.CreateAttemptResetOperation(txCtx, operation); err != nil {
				return err
			}
		}

		now := s.clock.Now().UTC()
		if !now.Before(command.Deadline) {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetExpired, "preview_deadline_expired", node_error.ErrAttemptResetExpired, &outcome)
		}
		exam, err := findAttemptResetExamForUpdate(txCtx, s.repo, command.ExamID)
		if err != nil {
			if errors.Is(err, node_error.ErrExamNotLoaded) {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "exam_not_loaded", node_error.ErrAttemptResetConflict, &outcome)
			}
			return err
		}
		if exam.DeploymentID != command.DeploymentID {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "deployment_mismatch", node_error.ErrAttemptResetConflict, &outcome)
		}
		if exam.FencedAt != nil {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "deployment_fenced", node_error.ErrAttemptResetFenced, &outcome)
		}
		participant, err := s.repo.FindParticipantByIDForUpdate(txCtx, command.ParticipantID)
		if err != nil {
			if errors.Is(err, node_error.ErrParticipantNotFound) {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "participant_not_found", node_error.ErrAttemptResetConflict, &outcome)
			}
			return err
		}
		if participant.ExamID != command.ExamID || participant.DeploymentID != command.DeploymentID || participant.StudentID != command.StudentID {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "target_conflict", node_error.ErrAttemptResetConflict, &outcome)
		}
		attempt, err := s.repo.FindLatestAttemptByParticipantAndExam(txCtx, participant.ID, command.ExamID)
		if err != nil {
			if errors.Is(err, node_error.ErrAttemptNotFound) || errors.Is(err, node_error.ErrResultNotAvailable) {
				return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "attempt_not_found", node_error.ErrAttemptNotFound, &outcome)
			}
			return err
		}
		if participant.LatestAttemptID == nil || *participant.LatestAttemptID != attempt.ID {
			return s.finishRejectedReset(txCtx, operation, inbound.AttemptResetRejected, "target_conflict", node_error.ErrAttemptResetConflict, &outcome)
		}
		operation.AttemptID = attempt.ID
		operation.AttemptNo = attempt.AttemptNo
		operation.AttemptStatus = attempt.Status
		operation.TargetGeneration = attempt.ResetGeneration
		operation.StartedAt = attempt.StartedAt
		operation.DueAt = attempt.DueAt
		operation.SubmittedAt = attempt.SubmittedAt
		operation.AutoSubmittedAt = attempt.AutoSubmittedAt
		operation.Status = entity.AttemptResetApplied
		operation.Code = "preview_ready"
		operation.Message = "attempt metadata preview is ready"
		operation.UpdatedAt = now
		outcome = attemptResetOperationOutcome(operation)
		return s.operations.UpdateAttemptResetOperation(txCtx, operation)
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}

func validateAttemptResetCommand(command inbound.AttemptResetCommand) error {
	if command.Operation != inbound.AttemptResetOperation {
		return node_error.ErrAttemptResetConflict
	}
	fields := map[string]string{
		"request_id":     command.RequestID,
		"node_id":        command.NodeID,
		"deployment_id":  command.DeploymentID,
		"exam_id":        command.ExamID,
		"student_id":     command.StudentID,
		"participant_id": command.ParticipantID,
		"attempt_id":     command.AttemptID,
	}
	for field, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if command.ExpectedDueAt.IsZero() {
		return errors.New("expected_due_at is required")
	}
	if command.Deadline.IsZero() {
		return errors.New("deadline is required")
	}
	if command.TargetGeneration != command.ExpectedGeneration+1 {
		return node_error.ErrAttemptResetConflict
	}
	if command.ExpectedStatus != entity.AttemptSubmitted && command.ExpectedStatus != entity.AttemptAutoSubmitted && command.ExpectedStatus != entity.AttemptInProgress {
		return node_error.ErrAttemptResetNotAllowed
	}
	return nil
}

func validateAttemptResetPreviewCommand(command inbound.AttemptResetCommand) error {
	if command.Operation != inbound.AttemptResetPreviewOperation {
		return node_error.ErrAttemptResetConflict
	}
	fields := map[string]string{
		"request_id":     command.RequestID,
		"node_id":        command.NodeID,
		"deployment_id":  command.DeploymentID,
		"exam_id":        command.ExamID,
		"student_id":     command.StudentID,
		"participant_id": command.ParticipantID,
	}
	for field, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if command.Deadline.IsZero() {
		return errors.New("deadline is required")
	}
	return nil
}

func findAttemptResetExamForUpdate(ctx context.Context, repo outbound_repository.AttemptResetRepository, examID string) (*entity.Exam, error) {
	if locker, ok := repo.(outbound_repository.AttemptResetExamLocker); ok {
		return locker.FindExamByIDForUpdate(ctx, examID)
	}
	return repo.FindExamByID(ctx, examID)
}

func attemptResetCommandHash(command inbound.AttemptResetCommand) (string, error) {
	encoded, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Errorf("hash reset command: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func newAttemptResetOperation(command inbound.AttemptResetCommand, commandHash string, now time.Time) *entity.AttemptResetOperation {
	return &entity.AttemptResetOperation{
		RequestID: command.RequestID, Operation: command.Operation, CommandHash: commandHash, Status: entity.AttemptResetPending,
		NodeID: command.NodeID, DeploymentID: command.DeploymentID, ExamID: command.ExamID, StudentID: command.StudentID,
		ParticipantID: command.ParticipantID, AttemptID: command.AttemptID, ExpectedGeneration: command.ExpectedGeneration,
		TargetGeneration: command.TargetGeneration, ExpectedStatus: command.ExpectedStatus, ExpectedDueAt: command.ExpectedDueAt,
		ExpectedSubmittedAt: command.ExpectedSubmittedAt, ExpectedAutoSubmittedAt: command.ExpectedAutoSubmittedAt, Deadline: command.Deadline,
		CreatedAt: now, UpdatedAt: now,
	}
}

func sameResetCommand(operation *entity.AttemptResetOperation, command inbound.AttemptResetCommand) bool {
	if operation.RequestID != command.RequestID || operation.Operation != command.Operation || operation.NodeID != command.NodeID ||
		operation.DeploymentID != command.DeploymentID || operation.ExamID != command.ExamID || operation.StudentID != command.StudentID ||
		operation.ParticipantID != command.ParticipantID || operation.ExpectedGeneration != command.ExpectedGeneration ||
		operation.ExpectedStatus != command.ExpectedStatus || !operation.ExpectedDueAt.Equal(command.ExpectedDueAt) ||
		!sameTimePointer(operation.ExpectedSubmittedAt, command.ExpectedSubmittedAt) || !sameTimePointer(operation.ExpectedAutoSubmittedAt, command.ExpectedAutoSubmittedAt) ||
		!operation.Deadline.Equal(command.Deadline) {
		return false
	}
	if operation.Operation == inbound.AttemptResetPreviewOperation {
		// Preview outcomes fill AttemptID and TargetGeneration after the
		// immutable command has been stored; those fields are not request input.
		return command.AttemptID == ""
	}
	return operation.AttemptID == command.AttemptID && operation.TargetGeneration == command.TargetGeneration
}

func (s *AttemptResetService) finishRejectedReset(ctx context.Context, operation *entity.AttemptResetOperation, status, code string, resultErr error, target **inbound.AttemptResetOutcome) error {
	operation.Status = entity.AttemptResetOperationStatus(status)
	operation.Code = code
	operation.Message = resultErr.Error()
	operation.UpdatedAt = s.clock.Now().UTC()
	*target = attemptResetOperationOutcome(operation)
	if err := s.operations.UpdateAttemptResetOperation(ctx, operation); err != nil {
		return err
	}
	return nil
}

func (s *AttemptResetService) finishOutcome(ctx context.Context, operation *entity.AttemptResetOperation, attempt *entity.Attempt, status, code, message string, target **inbound.AttemptResetOutcome) error {
	operation.Status = entity.AttemptResetOperationStatus(status)
	operation.Code = code
	operation.Message = message
	operation.AttemptID = attempt.ID
	operation.AttemptNo = attempt.AttemptNo
	operation.AttemptStatus = attempt.Status
	if status == inbound.AttemptResetApplied {
		operation.TargetGeneration = attempt.ResetGeneration
	}
	operation.AttemptNo = attempt.AttemptNo
	operation.StartedAt = attempt.StartedAt
	operation.DueAt = attempt.DueAt
	operation.SubmittedAt = attempt.SubmittedAt
	operation.AutoSubmittedAt = attempt.AutoSubmittedAt
	operation.UpdatedAt = s.clock.Now().UTC()
	*target = attemptResetOperationOutcome(operation)
	return s.operations.UpdateAttemptResetOperation(ctx, operation)
}

func attemptResetOperationOutcome(operation *entity.AttemptResetOperation) *inbound.AttemptResetOutcome {
	resetGeneration := operation.TargetGeneration
	if operation.Status == entity.AttemptResetAlreadyInProgress {
		// TargetGeneration remains the immutable generation requested by the
		// command. An already-live attempt has not advanced, so report the
		// generation that was observed instead.
		resetGeneration = operation.ExpectedGeneration
	}
	return &inbound.AttemptResetOutcome{
		RequestID: operation.RequestID, Operation: operation.Operation, Status: string(operation.Status), Code: operation.Code, Message: operation.Message,
		NodeID: operation.NodeID, DeploymentID: operation.DeploymentID, ExamID: operation.ExamID, StudentID: operation.StudentID,
		ParticipantID: operation.ParticipantID, AttemptID: operation.AttemptID, AttemptNo: operation.AttemptNo, ResetGeneration: resetGeneration,
		AttemptStatus: operation.AttemptStatus, StartedAt: operation.StartedAt, DueAt: operation.DueAt, SubmittedAt: operation.SubmittedAt, AutoSubmittedAt: operation.AutoSubmittedAt,
	}
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
