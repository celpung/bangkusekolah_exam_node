package inbound

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

type IntegrityUsecase interface {
	RecordEvent(ctx context.Context, attemptID, participantID, eventType string, description *string, metadata map[string]interface{}) (*entity.IntegrityEvent, error)
}
