package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type attemptResetClockStub struct{ now time.Time }

func (c attemptResetClockStub) Now() time.Time { return c.now }

type attemptResetSequenceClock struct {
	values []time.Time
	index  int
}

func (c *attemptResetSequenceClock) Now() time.Time {
	if c.index >= len(c.values) {
		return c.values[len(c.values)-1]
	}
	value := c.values[c.index]
	c.index++
	return value
}

type attemptResetTxStub struct{ calls int }

func (tx *attemptResetTxStub) Atomic(ctx context.Context, fn func(context.Context) error) error {
	tx.calls++
	return fn(ctx)
}

type attemptResetRepositoryStub struct {
	exam             *entity.Exam
	participant      *entity.Participant
	attempt          *entity.Attempt
	latestAttemptErr error
	answers          []entity.Answer
	resetCalls       int
}

func (r *attemptResetRepositoryStub) FindExamByID(_ context.Context, examID string) (*entity.Exam, error) {
	if r.exam == nil || r.exam.ID != examID {
		return nil, node_error.ErrExamNotLoaded
	}
	return r.exam, nil
}

func (r *attemptResetRepositoryStub) FindParticipantByIDForUpdate(_ context.Context, participantID string) (*entity.Participant, error) {
	if r.participant == nil || r.participant.ID != participantID {
		return nil, node_error.ErrParticipantNotFound
	}
	return r.participant, nil
}

func (r *attemptResetRepositoryStub) FindAttemptByIDForUpdate(_ context.Context, attemptID string) (*entity.Attempt, error) {
	if r.attempt == nil || r.attempt.ID != attemptID {
		return nil, node_error.ErrAttemptNotFound
	}
	return r.attempt, nil
}

func (r *attemptResetRepositoryStub) FindLatestAttemptByParticipantAndExam(_ context.Context, participantID, examID string) (*entity.Attempt, error) {
	if r.latestAttemptErr != nil {
		return nil, r.latestAttemptErr
	}
	if r.attempt == nil || r.attempt.ParticipantID != participantID || r.attempt.ExamID != examID {
		return nil, node_error.ErrAttemptNotFound
	}
	return r.attempt, nil
}

func (r *attemptResetRepositoryStub) ResetAttempt(_ context.Context, attempt *entity.Attempt, _ entity.AttemptStatus, _ int64) error {
	r.resetCalls++
	r.attempt = attempt
	return nil
}

type attemptResetOperationRepositoryStub struct {
	operations map[string]*entity.AttemptResetOperation
}

func (r *attemptResetOperationRepositoryStub) FindAttemptResetOperationForUpdate(_ context.Context, requestID string) (*entity.AttemptResetOperation, error) {
	operation, ok := r.operations[requestID]
	if !ok {
		return nil, node_error.ErrAttemptResetOperationNotFound
	}
	return operation, nil
}

func (r *attemptResetOperationRepositoryStub) CreateAttemptResetOperation(_ context.Context, operation *entity.AttemptResetOperation) error {
	if _, exists := r.operations[operation.RequestID]; exists {
		return errors.New("duplicate reset operation")
	}
	r.operations[operation.RequestID] = operation
	return nil
}

func (r *attemptResetOperationRepositoryStub) UpdateAttemptResetOperation(_ context.Context, operation *entity.AttemptResetOperation) error {
	r.operations[operation.RequestID] = operation
	return nil
}

func newAttemptResetFixture() (*attemptResetRepositoryStub, *attemptResetOperationRepositoryStub, *attemptResetTxStub, time.Time) {
	now := time.Date(2026, time.September, 6, 10, 0, 0, 0, time.UTC)
	submittedAt := now.Add(-5 * time.Minute)
	autoSubmittedAt := now.Add(-4 * time.Minute)
	score := 24.0
	return &attemptResetRepositoryStub{
		exam: &entity.Exam{
			ID:              "exam-1",
			DeploymentID:    "deployment-1",
			StartsAt:        now.Add(-time.Hour),
			EndsAt:          now.Add(time.Hour),
			DurationMinutes: 60,
			FencedAt:        nil,
		},
		participant: &entity.Participant{
			ID:              "participant-1",
			ExamID:          "exam-1",
			DeploymentID:    "deployment-1",
			StudentID:       "student-1",
			AttemptCount:    1,
			LatestAttemptID: stringPointer("attempt-1"),
		},
		attempt: &entity.Attempt{
			ID:              "attempt-1",
			ParticipantID:   "participant-1",
			StudentID:       "student-1",
			ExamID:          "exam-1",
			DeviceID:        "device-1",
			AttemptNo:       1,
			Status:          entity.AttemptSubmitted,
			StartedAt:       now.Add(-30 * time.Minute),
			DueAt:           now.Add(30 * time.Minute),
			SubmittedAt:     &submittedAt,
			AutoSubmittedAt: &autoSubmittedAt,
			Score:           &score,
			MaxScore:        40,
			GradingStatus:   entity.GradingAutoGraded,
			ResetGeneration: 3,
		},
		answers: []entity.Answer{{
			ID:         "answer-1",
			AttemptID:  "attempt-1",
			ItemID:     "item-1",
			AnswerJSON: map[string]interface{}{"value": "A"},
			Score:      &score,
			ClientSeq:  9,
		}},
	}, &attemptResetOperationRepositoryStub{operations: make(map[string]*entity.AttemptResetOperation)}, &attemptResetTxStub{}, now
}

func stringPointer(value string) *string { return &value }

func TestAttemptResetPreservesAnswersAndOriginalDeadline(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	original := *repo.attempt
	originalDueAt := original.DueAt
	originalStartedAt := original.StartedAt
	originalAnswers := repo.answers
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})

	outcome, err := service.ApplyReset(context.Background(), inbound.AttemptResetCommand{
		RequestID:               "reset-1",
		Operation:               inbound.AttemptResetOperation,
		NodeID:                  "node-1",
		DeploymentID:            "deployment-1",
		ExamID:                  "exam-1",
		StudentID:               "student-1",
		ParticipantID:           "participant-1",
		AttemptID:               "attempt-1",
		ExpectedGeneration:      3,
		ExpectedStatus:          entity.AttemptSubmitted,
		ExpectedDueAt:           originalDueAt,
		ExpectedSubmittedAt:     original.SubmittedAt,
		ExpectedAutoSubmittedAt: original.AutoSubmittedAt,
		TargetGeneration:        4,
		Deadline:                now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ApplyReset() error = %v", err)
	}
	if outcome == nil || outcome.Status != inbound.AttemptResetApplied {
		t.Fatalf("outcome = %+v, want applied", outcome)
	}
	if repo.resetCalls != 1 || tx.calls != 1 {
		t.Fatalf("reset/transaction calls = %d/%d, want 1/1", repo.resetCalls, tx.calls)
	}
	if repo.attempt.ID != original.ID || repo.attempt.AttemptNo != original.AttemptNo || repo.attempt.ResetGeneration != 4 {
		t.Fatalf("identity/generation changed incorrectly: %+v", repo.attempt)
	}
	if repo.attempt.StartedAt != originalStartedAt || repo.attempt.DueAt != originalDueAt {
		t.Fatalf("working window changed: started=%v due=%v, want started=%v due=%v", repo.attempt.StartedAt, repo.attempt.DueAt, originalStartedAt, originalDueAt)
	}
	if repo.attempt.DeviceID != original.DeviceID || len(repo.answers) != len(originalAnswers) || repo.answers[0].ID != "answer-1" || repo.answers[0].ClientSeq != 9 {
		t.Fatalf("saved attempt data was not preserved: attempt=%+v answers=%+v", repo.attempt, repo.answers)
	}
	if repo.attempt.Status != entity.AttemptInProgress || repo.attempt.SubmittedAt != nil || repo.attempt.AutoSubmittedAt != nil || repo.attempt.Score != nil || repo.attempt.GradingStatus != entity.GradingPending {
		t.Fatalf("reset fields = %+v", repo.attempt)
	}
}

func TestAttemptResetRechecksDeadlineBeforeConditionalWrite(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	clock := &attemptResetSequenceClock{values: []time.Time{now, now, now.Add(2 * time.Minute)}}
	service := NewAttemptResetServiceWithClock(repo, operations, tx, clock)

	outcome, err := service.ApplyReset(context.Background(), inbound.AttemptResetCommand{
		RequestID: "reset-lock-wait", Operation: inbound.AttemptResetOperation, NodeID: "node-1", DeploymentID: "deployment-1", ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", AttemptID: "attempt-1",
		ExpectedGeneration: 3, ExpectedStatus: entity.AttemptSubmitted, ExpectedDueAt: repo.attempt.DueAt, ExpectedSubmittedAt: repo.attempt.SubmittedAt, ExpectedAutoSubmittedAt: repo.attempt.AutoSubmittedAt,
		TargetGeneration: 4, Deadline: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ApplyReset() error = %v, want durable expiry outcome", err)
	}
	if outcome == nil || outcome.Status != inbound.AttemptResetExpired || operations.operations["reset-lock-wait"].Code != "reset_deadline_expired" {
		t.Fatalf("outcome = %+v, operation = %+v, want expired", outcome, operations.operations["reset-lock-wait"])
	}
	if repo.resetCalls != 0 || repo.attempt.Status != entity.AttemptSubmitted || repo.attempt.ResetGeneration != 3 {
		t.Fatalf("late deadline check mutated attempt: reset_calls=%d attempt=%+v", repo.resetCalls, repo.attempt)
	}
}

func TestAttemptResetDuplicateReturnsSavedOutcomeAfterResubmit(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})
	command := inbound.AttemptResetCommand{
		RequestID: "reset-duplicate", Operation: inbound.AttemptResetOperation, NodeID: "node-1", DeploymentID: "deployment-1", ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", AttemptID: "attempt-1",
		ExpectedGeneration: 3, ExpectedStatus: entity.AttemptSubmitted, ExpectedDueAt: repo.attempt.DueAt, ExpectedSubmittedAt: repo.attempt.SubmittedAt, ExpectedAutoSubmittedAt: repo.attempt.AutoSubmittedAt, TargetGeneration: 4, Deadline: now.Add(time.Minute),
	}
	first, err := service.ApplyReset(context.Background(), command)
	if err != nil {
		t.Fatalf("first ApplyReset() error = %v", err)
	}
	repo.attempt.Status = entity.AttemptSubmitted
	second, err := service.ApplyReset(context.Background(), command)
	if err != nil {
		t.Fatalf("duplicate ApplyReset() error = %v", err)
	}
	if second == nil || second.Status != first.Status || second.ResetGeneration != first.ResetGeneration {
		t.Fatalf("duplicate outcome = %+v, first = %+v", second, first)
	}
	if repo.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", repo.resetCalls)
	}
}

func TestAttemptResetAlreadyInProgressIsReplayable(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	repo.attempt.Status = entity.AttemptInProgress
	repo.attempt.SubmittedAt = nil
	repo.attempt.AutoSubmittedAt = nil
	repo.attempt.Score = nil
	repo.attempt.GradingStatus = entity.GradingPending
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})
	command := inbound.AttemptResetCommand{
		RequestID: "reset-already-in-progress", Operation: inbound.AttemptResetOperation, NodeID: "node-1", DeploymentID: "deployment-1", ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", AttemptID: "attempt-1",
		ExpectedGeneration: 3, ExpectedStatus: entity.AttemptInProgress, ExpectedDueAt: repo.attempt.DueAt, TargetGeneration: 4, Deadline: now.Add(time.Minute),
	}

	first, err := service.ApplyReset(context.Background(), command)
	if err != nil {
		t.Fatalf("first ApplyReset() error = %v", err)
	}
	second, err := service.ApplyReset(context.Background(), command)
	if err != nil {
		t.Fatalf("duplicate ApplyReset() error = %v", err)
	}
	if first == nil || second == nil || first.Status != inbound.AttemptResetAlreadyInProgress || second.Status != first.Status || second.ResetGeneration != first.ResetGeneration {
		t.Fatalf("already-in-progress outcomes = first=%+v second=%+v", first, second)
	}
	if repo.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0 for an existing in-progress attempt", repo.resetCalls)
	}
}

func TestAttemptResetRejectsExpiredDeadlineWithoutMutation(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})
	original := *repo.attempt
	result, err := service.ApplyReset(context.Background(), inbound.AttemptResetCommand{
		RequestID: "reset-expired", Operation: inbound.AttemptResetOperation, NodeID: "node-1", DeploymentID: "deployment-1", ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", AttemptID: "attempt-1",
		ExpectedGeneration: 3, ExpectedStatus: entity.AttemptSubmitted, ExpectedDueAt: original.DueAt, ExpectedSubmittedAt: original.SubmittedAt, ExpectedAutoSubmittedAt: original.AutoSubmittedAt, TargetGeneration: 4, Deadline: now.Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("expired ApplyReset() error = %v", err)
	}
	if result == nil || result.Status != inbound.AttemptResetExpired {
		t.Fatalf("expired outcome = %+v, want expired", result)
	}
	if repo.resetCalls != 0 || repo.attempt.Status != original.Status || repo.attempt.ResetGeneration != original.ResetGeneration {
		t.Fatalf("expired command mutated attempt: calls=%d attempt=%+v", repo.resetCalls, repo.attempt)
	}
}

func TestAttemptResetMissingTargetReturnsDurableRejection(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	repo.attempt = nil
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})
	result, err := service.ApplyReset(context.Background(), inbound.AttemptResetCommand{
		RequestID: "reset-missing-target", Operation: inbound.AttemptResetOperation, NodeID: "node-1", DeploymentID: "deployment-1", ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", AttemptID: "attempt-1",
		ExpectedGeneration: 3, ExpectedStatus: entity.AttemptSubmitted, ExpectedDueAt: now.Add(30 * time.Minute), TargetGeneration: 4, Deadline: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ApplyReset() error = %v", err)
	}
	if result == nil || result.Status != inbound.AttemptResetRejected || result.Code != "attempt_not_found" {
		t.Fatalf("missing-target outcome = %+v, want durable rejection", result)
	}
	if operation := operations.operations["reset-missing-target"]; operation == nil || operation.Status != entity.AttemptResetRejected {
		t.Fatalf("stored operation = %+v, want rejected", operation)
	}
}

func TestAttemptResetPreviewIsIdempotentAfterMetadataIsRecorded(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})
	command := inbound.AttemptResetCommand{
		RequestID: "preview-1", Operation: inbound.AttemptResetPreviewOperation, NodeID: "node-1", DeploymentID: "deployment-1",
		ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", Deadline: now.Add(time.Minute),
	}
	first, err := service.PreviewReset(context.Background(), command)
	if err != nil {
		t.Fatalf("first PreviewReset() error = %v", err)
	}
	second, err := service.PreviewReset(context.Background(), command)
	if err != nil {
		t.Fatalf("duplicate PreviewReset() error = %v", err)
	}
	if first == nil || second == nil || first.AttemptID != "attempt-1" || second.AttemptID != first.AttemptID || second.ResetGeneration != first.ResetGeneration || !second.DueAt.Equal(first.DueAt) {
		t.Fatalf("preview outcomes = first=%+v second=%+v", first, second)
	}
	if tx.calls != 2 {
		t.Fatalf("transaction calls = %d, want 2", tx.calls)
	}
}

func TestAttemptResetPreviewMissingAttemptReturnsDurableRejection(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	repo.attempt = nil
	repo.latestAttemptErr = node_error.ErrResultNotAvailable
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})

	result, err := service.PreviewReset(context.Background(), inbound.AttemptResetCommand{
		RequestID: "preview-missing-target", Operation: inbound.AttemptResetPreviewOperation, NodeID: "node-1", DeploymentID: "deployment-1",
		ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", Deadline: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PreviewReset() error = %v", err)
	}
	if result == nil || result.Status != inbound.AttemptResetRejected || result.Code != "attempt_not_found" {
		t.Fatalf("missing-attempt outcome = %+v, want durable rejection", result)
	}
	if operation := operations.operations["preview-missing-target"]; operation == nil || operation.Status != entity.AttemptResetRejected {
		t.Fatalf("stored operation = %+v, want rejected", operation)
	}
}

func TestAttemptResetPreviewRejectsNonLatestAttempt(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	repo.participant.LatestAttemptID = stringPointer("another-attempt")
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})

	result, err := service.PreviewReset(context.Background(), inbound.AttemptResetCommand{
		RequestID: "preview-stale-target", Operation: inbound.AttemptResetPreviewOperation, NodeID: "node-1", DeploymentID: "deployment-1",
		ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", Deadline: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("PreviewReset() error = %v", err)
	}
	if result == nil || result.Status != inbound.AttemptResetRejected || result.Code != "target_conflict" {
		t.Fatalf("stale-preview outcome = %+v, want target conflict", result)
	}
}

func TestAttemptResetRejectsInvalidTargetGeneration(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})

	_, err := service.ApplyReset(context.Background(), inbound.AttemptResetCommand{
		RequestID: "reset-invalid-generation", Operation: inbound.AttemptResetOperation, NodeID: "node-1", DeploymentID: "deployment-1",
		ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", AttemptID: "attempt-1", ExpectedGeneration: 3,
		ExpectedStatus: entity.AttemptSubmitted, ExpectedDueAt: repo.attempt.DueAt, ExpectedSubmittedAt: repo.attempt.SubmittedAt,
		ExpectedAutoSubmittedAt: repo.attempt.AutoSubmittedAt, TargetGeneration: 3, Deadline: now.Add(time.Minute),
	})
	if !errors.Is(err, node_error.ErrAttemptResetConflict) {
		t.Fatalf("ApplyReset() error = %v, want generation conflict", err)
	}
	if tx.calls != 0 || len(operations.operations) != 0 {
		t.Fatalf("invalid command changed state: tx calls=%d operations=%+v", tx.calls, operations.operations)
	}
}

func TestAttemptResetOperationsRejectMismatchedOperation(t *testing.T) {
	repo, operations, tx, now := newAttemptResetFixture()
	service := NewAttemptResetServiceWithClock(repo, operations, tx, attemptResetClockStub{now: now})

	resetCommand := inbound.AttemptResetCommand{
		RequestID: "wrong-reset-operation", Operation: inbound.AttemptResetPreviewOperation, NodeID: "node-1", DeploymentID: "deployment-1",
		ExamID: "exam-1", StudentID: "student-1", ParticipantID: "participant-1", AttemptID: "attempt-1",
		ExpectedGeneration: 3, ExpectedStatus: entity.AttemptSubmitted, ExpectedDueAt: repo.attempt.DueAt,
		TargetGeneration: 4, Deadline: now.Add(time.Minute),
	}
	if _, err := service.ApplyReset(context.Background(), resetCommand); !errors.Is(err, node_error.ErrAttemptResetConflict) {
		t.Fatalf("ApplyReset with preview operation error = %v, want ErrAttemptResetConflict", err)
	}

	previewCommand := resetCommand
	previewCommand.RequestID = "wrong-preview-operation"
	previewCommand.Operation = inbound.AttemptResetOperation
	if _, err := service.PreviewReset(context.Background(), previewCommand); !errors.Is(err, node_error.ErrAttemptResetConflict) {
		t.Fatalf("PreviewReset with reset operation error = %v, want ErrAttemptResetConflict", err)
	}
}

var _ outbound.TxManager = (*attemptResetTxStub)(nil)
var _ outbound_repository.AttemptResetRepository = (*attemptResetRepositoryStub)(nil)
var _ outbound_repository.AttemptResetOperationRepository = (*attemptResetOperationRepositoryStub)(nil)
