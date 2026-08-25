package inbound

import "context"

type LoginResult struct {
	ParticipantID string `json:"participant_id"`
	StudentID     string `json:"student_id"`
	ExamID        string `json:"exam_id"`
	Token         string `json:"token"`
	ExpiresAt     int64  `json:"expires_at"`
}

type AuthUsecase interface {
	Login(ctx context.Context, code string) (*LoginResult, error)
	// Login is the only auth entry. The node has no password reset, no refresh,
	// no logout — the JWT lives 90m and the exam is at most 3 hours.
}
