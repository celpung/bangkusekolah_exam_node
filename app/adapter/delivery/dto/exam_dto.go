package dto

type AutosaveRequest struct {
	AnswerJSON map[string]interface{} `json:"answer_json"`
	AnswerText *string                `json:"answer_text,omitempty"`
	ClientSeq  int64                  `json:"client_seq"`
}

type IntegrityEventRequest struct {
	EventType   string                 `json:"event_type"`
	Description *string                `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata_json,omitempty"`
}

type ResultResponse struct {
	AttemptID     string   `json:"attempt_id"`
	Status        string   `json:"status"`
	Score         *float64 `json:"score,omitempty"`
	MaxScore      float64  `json:"max_score"`
	GradingStatus string   `json:"grading_status"`
	// awaiting_grading: when grading_status is manual_required, score is the
	// auto-graded subtotal and the final result is not yet available.
}
