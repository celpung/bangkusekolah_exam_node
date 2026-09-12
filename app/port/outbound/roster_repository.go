package outbound

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

type RosterRepository interface {
	ListExams(ctx context.Context) ([]entity.Exam, error)
	FindReceipt(ctx context.Context, eventID string) (*entity.RosterEventReceipt, error)
	InsertReceipt(ctx context.Context, receipt *entity.RosterEventReceipt) error
	FindExamForUpdate(ctx context.Context, examID string) (*entity.Exam, error)
	InsertParticipantIfAbsent(ctx context.Context, participant *entity.Participant) (*entity.Participant, bool, error)
	ListItemsByExamID(ctx context.Context, examID string) ([]entity.Item, error)
	ListParticipantsByExam(ctx context.Context, examID string) ([]entity.Participant, error)
	UpdateExamRosterState(ctx context.Context, examID string, revision int64, contentHash string) error
}

type RosterPreflightRepository interface {
	CountProcessedRosterReceipts(ctx context.Context, deploymentID string, revision int64) (int64, error)
}
