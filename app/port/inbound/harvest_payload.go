package inbound

import (
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

// Harvest payload types mirror central's ingest contract field-for-field so
// the same JSON decodes on both sides without a conversion layer.

type ExamNodeAnswerPayload struct {
	ID            string                 `json:"id"`
	ItemID        string                 `json:"item_id"`
	AnswerJSON    map[string]interface{} `json:"answer_json"`
	AnswerText    *string                `json:"answer_text"`
	Score         *float64               `json:"score"`
	GradingStatus entity.GradingStatus   `json:"grading_status"`
	LastSavedAt   time.Time              `json:"last_saved_at"`
}

type ExamNodeIntegrityEventPayload struct {
	ID           string                 `json:"id"`
	EventType    string                 `json:"event_type"`
	Description  *string                `json:"description"`
	MetadataJSON map[string]interface{} `json:"metadata_json"`
	CreatedAt    time.Time              `json:"created_at"`
}

type ExamNodeAttemptPayload struct {
	ID              string                          `json:"id"`
	ParticipantID   string                          `json:"participant_id"`
	StudentID       string                          `json:"student_id"`
	AttemptNo       int                             `json:"attempt_no"`
	Status          entity.AttemptStatus            `json:"status"`
	StartedAt       time.Time                       `json:"started_at"`
	DueAt           time.Time                       `json:"due_at"`
	SubmittedAt     *time.Time                      `json:"submitted_at"`
	AutoSubmittedAt *time.Time                      `json:"auto_submitted_at"`
	Answers         []ExamNodeAnswerPayload         `json:"answers"`
	IntegrityEvents []ExamNodeIntegrityEventPayload `json:"integrity_events"`
}

type ExamNodeAttemptBatch struct {
	Attempts []ExamNodeAttemptPayload `json:"attempts"`
}

// ExamNodeIngestResult acknowledges each attempt individually so the node
// retries per attempt rather than per batch.
type ExamNodeIngestResult struct {
	AcceptedAttemptIDs []string          `json:"accepted_attempt_ids"`
	Failures           map[string]string `json:"failures"`
}
