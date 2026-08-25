package inbound

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

type AttemptState struct {
	Attempt    *entity.Attempt `json:"attempt"`
	Answers    []entity.Answer `json:"answers"`
	ServerTime time.Time       `json:"server_time"`
}

type AttemptUsecase interface {
	StartAttempt(ctx context.Context, participantID string) (*entity.Attempt, error)
	GetAttemptState(ctx context.Context, participantID, attemptID string) (*AttemptState, error)
}
