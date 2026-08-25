package outbound_repository

import (
	"context"

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
	FindItemByID(ctx context.Context, itemID string) (*entity.Item, error)
}
