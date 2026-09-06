package outbound_repository

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

type DeploymentFenceRepository interface {
	MarkDeploymentFenced(ctx context.Context, deploymentID string, at time.Time) error
	IsDeploymentFenced(ctx context.Context, examID string, deploymentID string) (bool, error)
}

// AttemptResetRepository is deliberately separate from NodeRepository. The
// reset write has a different allowed-column set and must not weaken the
// conditional in ordinary UpdateAttempt used by submit and the sweeper.
type AttemptResetRepository interface {
	FindExamByID(ctx context.Context, examID string) (*entity.Exam, error)
	FindParticipantByIDForUpdate(ctx context.Context, participantID string) (*entity.Participant, error)
	FindAttemptByIDForUpdate(ctx context.Context, attemptID string) (*entity.Attempt, error)
	FindLatestAttemptByParticipantAndExam(ctx context.Context, participantID, examID string) (*entity.Attempt, error)
	ResetAttempt(ctx context.Context, attempt *entity.Attempt, expectedStatus entity.AttemptStatus, expectedGeneration int64) error
}

// AttemptResetExamLocker is an optional capability used by production reset
// execution to serialize with the local fence writer. Legacy test doubles can
// fall back to the ordinary read without changing the student flow.
type AttemptResetExamLocker interface {
	FindExamByIDForUpdate(ctx context.Context, examID string) (*entity.Exam, error)
}

type AttemptResetOperationRepository interface {
	FindAttemptResetOperationForUpdate(ctx context.Context, requestID string) (*entity.AttemptResetOperation, error)
	CreateAttemptResetOperation(ctx context.Context, operation *entity.AttemptResetOperation) error
	UpdateAttemptResetOperation(ctx context.Context, operation *entity.AttemptResetOperation) error
}

// GenerationHarvestMarker protects the harvest cursor from an acknowledgement
// for a snapshot that was taken before a reset. It is optional at the port
// boundary so small legacy test doubles can continue using MarkAttemptsHarvested.
type GenerationHarvestMarker interface {
	MarkAttemptsHarvestedForGeneration(ctx context.Context, generations map[string]int64, at time.Time) (int, error)
}

type NodeRepository interface {
	FindExamByID(ctx context.Context, examID string) (*entity.Exam, error)
	FindParticipantByID(ctx context.Context, participantID string) (*entity.Participant, error)
	FindParticipantByIDForUpdate(ctx context.Context, participantID string) (*entity.Participant, error)
	FindActiveAttemptByParticipant(ctx context.Context, participantID string) (*entity.Attempt, error)
	FindActiveAttemptByParticipantAndExam(ctx context.Context, participantID, examID string) (*entity.Attempt, error)
	FindAttemptByID(ctx context.Context, attemptID string) (*entity.Attempt, error)
	ListAnswersByAttempt(ctx context.Context, attemptID string) ([]entity.Answer, error)
	CreateAttempt(ctx context.Context, attempt *entity.Attempt) error
	UpdateParticipant(ctx context.Context, p *entity.Participant) error
	CountAttemptsByParticipant(ctx context.Context, participantID string) (int, error)
	// UpsertAnswer is a single INSERT ... ON DUPLICATE KEY UPDATE statement keyed
	// by (attempt_id, item_id). It returns ErrStaleAnswerWrite if the stored
	// client_seq is >= the incoming one.
	UpsertAnswer(ctx context.Context, answer *entity.Answer) (*entity.Answer, error)
	FindItemByID(ctx context.Context, itemID string) (*entity.Item, error)
	UpdateAttempt(ctx context.Context, attempt *entity.Attempt) error
	ListExpiredAttempts(ctx context.Context, now time.Time) ([]entity.Attempt, error)
	FindAttemptByIDForUpdate(ctx context.Context, attemptID string) (*entity.Attempt, error)
	FindParticipantByAccessCode(ctx context.Context, code string) (*entity.Participant, error)
	ListItemsByExam(ctx context.Context) ([]entity.Item, error)
	ListItemsByExamID(ctx context.Context, examID string) ([]entity.Item, error)
	ListExams(ctx context.Context) ([]entity.Exam, error)
	FindLatestAttemptByParticipant(ctx context.Context, participantID string) (*entity.Attempt, error)
	FindLatestAttemptByParticipantAndExam(ctx context.Context, participantID, examID string) (*entity.Attempt, error)
	CreateIntegrityEvent(ctx context.Context, event *entity.IntegrityEvent) error
	CountIntegrityEventsSince(ctx context.Context, attemptID string, since time.Time) (int64, error)
	FindIntegrityEventSince(ctx context.Context, attemptID, eventType string, since time.Time) (*entity.IntegrityEvent, error)
	CreateExam(ctx context.Context, exam *entity.Exam) error
	ReplaceBundle(ctx context.Context, exam *entity.Exam, items []entity.Item, participants []entity.Participant) error
	ListParticipants(ctx context.Context) ([]entity.Participant, error)
	ListParticipantsByExam(ctx context.Context, examID string) ([]entity.Participant, error)

	// Harvest cursor operations (Task 20).
	ListUnpushedAttempts(ctx context.Context) ([]entity.Attempt, error)
	MarkAttemptsHarvested(ctx context.Context, ids []string, at time.Time) (int, error)
	LogHarvestFailure(ctx context.Context, attemptID, deploymentID string, attemptsCount int, errMsg string) error
	ListIntegrityEventsByAttempt(ctx context.Context, attemptID string) ([]entity.IntegrityEvent, error)
}

// AttemptDeviceBinder atomically claims an unbound in-progress attempt for
// one app installation. It is optional so small in-memory test doubles can
// continue to use UpdateAttempt.
type AttemptDeviceBinder interface {
	BindAttemptDevice(ctx context.Context, attemptID, deviceID string) error
}
