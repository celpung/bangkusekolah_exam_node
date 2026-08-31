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
	StartAttempt(ctx context.Context, participantID, examID string) (*entity.Attempt, error)
	GetAttemptState(ctx context.Context, participantID, attemptID string) (*AttemptState, error)
	// AutosaveAnswer writes one answer. clientSeq is monotonic per (attempt, item)
	// on the client; the server drops any write whose seq is not greater than the
	// stored one.
	AutosaveAnswer(ctx context.Context, attemptID, itemID string, answerJSON map[string]interface{}, answerText *string, clientSeq int64, participantID string) (*entity.Answer, error)
	SubmitAttempt(ctx context.Context, attemptID, participantID string) (*entity.Attempt, error)
	GetResult(ctx context.Context, participantID, examID string) (*entity.Attempt, error)
}
