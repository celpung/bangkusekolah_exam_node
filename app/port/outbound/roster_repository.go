package outbound

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

type RosterRepository interface {
	FindReceipt(ctx context.Context, eventID string) (*entity.RosterEventReceipt, error)
	InsertReceipt(ctx context.Context, receipt *entity.RosterEventReceipt) error
	FindExamForUpdate(ctx context.Context, examID string) (*entity.Exam, error)
	InsertParticipantIfAbsent(ctx context.Context, participant *entity.Participant) (*entity.Participant, bool, error)
	ListItemsByExamID(ctx context.Context, examID string) ([]entity.Item, error)
	ListParticipantsByExam(ctx context.Context, examID string) ([]entity.Participant, error)
	UpdateExamRosterState(ctx context.Context, examID string, revision int64, contentHash string) error
}

type RosterPreflightRepository interface {
	CountAppliedReceiptsByExam(ctx context.Context, examID string) (int64, error)
}
