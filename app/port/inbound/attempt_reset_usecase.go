package inbound

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

const (
	AttemptResetPreviewOperation = "preview"
	AttemptResetOperation        = "reset"

	AttemptResetPending           = "pending"
	AttemptResetApplied           = "applied"
	AttemptResetRejected          = "rejected"
	AttemptResetExpired           = "expired"
	AttemptResetAlreadyInProgress = "already_in_progress"
)

// AttemptResetCommand is immutable after Central prepares it. The Node uses
// the expected fields as compare-and-set guards; it never recalculates DueAt.
type AttemptResetCommand struct {
	RequestID               string               `json:"request_id"`
	Operation               string               `json:"operation"`
	NodeID                  string               `json:"node_id"`
	DeploymentID            string               `json:"deployment_id"`
	ExamID                  string               `json:"exam_id"`
	StudentID               string               `json:"student_id"`
	ParticipantID           string               `json:"participant_id"`
	AttemptID               string               `json:"attempt_id"`
	ExpectedGeneration      int64                `json:"expected_generation"`
	ExpectedStatus          entity.AttemptStatus `json:"expected_status"`
	ExpectedDueAt           time.Time            `json:"expected_due_at"`
	ExpectedSubmittedAt     *time.Time           `json:"expected_submitted_at,omitempty"`
	ExpectedAutoSubmittedAt *time.Time           `json:"expected_auto_submitted_at,omitempty"`
	TargetGeneration        int64                `json:"target_generation"`
	Deadline                time.Time            `json:"deadline"`
}

type AttemptResetOutcome struct {
	RequestID       string               `json:"request_id"`
	Operation       string               `json:"operation"`
	Status          string               `json:"status"`
	Code            string               `json:"code,omitempty"`
	Message         string               `json:"message,omitempty"`
	NodeID          string               `json:"node_id"`
	DeploymentID    string               `json:"deployment_id"`
	ExamID          string               `json:"exam_id"`
	StudentID       string               `json:"student_id"`
	ParticipantID   string               `json:"participant_id"`
	AttemptID       string               `json:"attempt_id"`
	AttemptNo       int                  `json:"attempt_no"`
	ResetGeneration int64                `json:"reset_generation"`
	AttemptStatus   entity.AttemptStatus `json:"attempt_status"`
	StartedAt       time.Time            `json:"started_at"`
	DueAt           time.Time            `json:"due_at"`
	SubmittedAt     *time.Time           `json:"submitted_at,omitempty"`
	AutoSubmittedAt *time.Time           `json:"auto_submitted_at,omitempty"`
}

type AttemptResetUsecase interface {
	ApplyReset(ctx context.Context, command AttemptResetCommand) (*AttemptResetOutcome, error)
}

type AttemptResetPreviewUsecase interface {
	PreviewReset(ctx context.Context, command AttemptResetCommand) (*AttemptResetOutcome, error)
}
