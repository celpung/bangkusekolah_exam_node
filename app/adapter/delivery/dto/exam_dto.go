package dto

import (
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

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

type AttemptStateResponse struct {
	AttemptResponse
	Answers    []AttemptAnswerResponse `json:"answers"`
	ServerTime time.Time               `json:"server_time"`
}

type AttemptAnswerResponse struct {
	ItemID      string                 `json:"item_id"`
	AnswerJSON  map[string]interface{} `json:"answer_json"`
	AnswerText  *string                `json:"answer_text"`
	ClientSeq   int64                  `json:"client_seq"`
	LastSavedAt time.Time              `json:"last_saved_at"`
}

func NewAttemptStateResponse(state *inbound.AttemptState) AttemptStateResponse {
	response := AttemptStateResponse{ServerTime: state.ServerTime}
	if state.Attempt != nil {
		response.AttemptResponse = AttemptResponse{
			ID:              state.Attempt.ID,
			ExamID:          state.Attempt.ExamID,
			AttemptNo:       state.Attempt.AttemptNo,
			Status:          string(state.Attempt.Status),
			StartedAt:       state.Attempt.StartedAt,
			DueAt:           state.Attempt.DueAt,
			SubmittedAt:     state.Attempt.SubmittedAt,
			AutoSubmittedAt: state.Attempt.AutoSubmittedAt,
			Score:           state.Attempt.Score,
			MaxScore:        state.Attempt.MaxScore,
			GradingStatus:   string(state.Attempt.GradingStatus),
		}
	}
	response.Answers = make([]AttemptAnswerResponse, 0, len(state.Answers))
	for _, answer := range state.Answers {
		response.Answers = append(response.Answers, AttemptAnswerResponse{
			ItemID:      answer.ItemID,
			AnswerJSON:  answer.AnswerJSON,
			AnswerText:  answer.AnswerText,
			ClientSeq:   answer.ClientSeq,
			LastSavedAt: answer.LastSavedAt,
		})
	}
	return response
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
