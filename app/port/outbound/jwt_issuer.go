package outbound

import "context"

type JWTClaims struct {
	ParticipantID string `json:"pid"`
	StudentID     string `json:"sid"`
	ExamID        string `json:"exam_id"`
	DeploymentID  string `json:"deployment_id"`
	ExpiresAt     int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
}

type JWTIssuer interface {
	Issue(ctx context.Context, participantID, studentID, examID, deploymentID string) (string, error)
	Parse(ctx context.Context, token string) (*JWTClaims, error)
}
