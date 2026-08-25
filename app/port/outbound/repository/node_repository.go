package outbound_repository

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

type NodeRepository interface {
	FindExam(ctx context.Context) (*entity.Exam, error)
	FindParticipantByID(ctx context.Context, participantID string) (*entity.Participant, error)
	FindParticipantByIDForUpdate(ctx context.Context, participantID string) (*entity.Participant, error)
	FindActiveAttemptByParticipant(ctx context.Context, participantID string) (*entity.Attempt, error)
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
}
