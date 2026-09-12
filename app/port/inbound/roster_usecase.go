package inbound

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

const RosterProtocolVersion = 1

type RosterEvent struct {
	EventID       string    `json:"event_id"`
	DeploymentID  string    `json:"deployment_id"`
	ExamID        string    `json:"exam_id"`
	ParticipantID string    `json:"participant_id"`
	StudentID     string    `json:"student_id"`
	StudentName   string    `json:"student_name"`
	AccessCode    string    `json:"access_code"`
	Revision      int64     `json:"revision"`
	Deadline      time.Time `json:"deadline"`
	CreatedAt     time.Time `json:"created_at"`
	Status        string    `json:"status"`
	OutcomeCode   string    `json:"outcome_code"`
}

type RosterOutcome struct {
	EventID      string `json:"event_id"`
	DeploymentID string `json:"deployment_id"`
	Revision     int64  `json:"revision"`
	Status       string `json:"status"`
	Code         string `json:"code"`
}

type RosterUsecase interface {
	ListRosterExams(ctx context.Context) ([]entity.Exam, error)
	Apply(ctx context.Context, event RosterEvent) (*RosterOutcome, error)
}
