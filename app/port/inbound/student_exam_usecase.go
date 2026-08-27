package inbound

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

// StudentExamUsecase is the token-scoped exam list.
type StudentExamUsecase interface {
	ListExams(ctx context.Context, participantID string) ([]entity.Exam, error)
}
