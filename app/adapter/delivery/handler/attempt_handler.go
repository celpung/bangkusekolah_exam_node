package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/dto"
	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type AttemptHandler struct {
	attemptUC   inbound.AttemptUsecase
	integrityUC inbound.IntegrityUsecase
}

func NewAttemptHandler(uc inbound.AttemptUsecase, integrityUC inbound.IntegrityUsecase) *AttemptHandler {
	return &AttemptHandler{attemptUC: uc, integrityUC: integrityUC}
}

// Start creates or returns the participant's active attempt for the exam in
// the JWT. The service remains the authority for the exam window and attempt
// limit; the handler only enforces URL/JWT exam scoping.
func (h *AttemptHandler) Start(w http.ResponseWriter, r *http.Request) {
	pid, _ := middleware.ParticipantIDFromContext(r.Context())
	examID := chi.URLParam(r, "examId")
	if tokenExamID, ok := middleware.ExamIDFromContext(r.Context()); !ok || tokenExamID != examID {
		delivery_helper.Error(w, http.StatusForbidden, "exam does not belong to your token")
		return
	}
	attempt, err := h.attemptUC.StartAttempt(r.Context(), pid, examID)
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	resp := dto.AttemptResponse{
		ID: attempt.ID, ExamID: attempt.ExamID, AttemptNo: attempt.AttemptNo,
		Status: string(attempt.Status), StartedAt: attempt.StartedAt, DueAt: attempt.DueAt,
		SubmittedAt: attempt.SubmittedAt, AutoSubmittedAt: attempt.AutoSubmittedAt,
		Score: attempt.Score, MaxScore: attempt.MaxScore, GradingStatus: string(attempt.GradingStatus),
	}
	delivery_helper.Success(w, http.StatusOK, "attempt started", resp)
}

// GetState returns the caller's attempt with answers and server_time. The
// participant id comes from the JWT context, so a student can only read their
// own attempt — the service enforces ownership again as defense in depth.
func (h *AttemptHandler) GetState(w http.ResponseWriter, r *http.Request) {
	pid, _ := middleware.ParticipantIDFromContext(r.Context())
	attemptID := chi.URLParam(r, "attemptId")
	state, err := h.attemptUC.GetAttemptState(r.Context(), pid, attemptID)
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "attempt retrieved", dto.NewAttemptStateResponse(state))
}

// Autosave writes one answer. A stale client_seq is a 200 no-op — the client
// already holds newer content, and a 409 would invite retrying the stale
// payload, which is exactly what the sequence number prevents.
func (h *AttemptHandler) Autosave(w http.ResponseWriter, r *http.Request) {
	pid, _ := middleware.ParticipantIDFromContext(r.Context())
	attemptID := chi.URLParam(r, "attemptId")
	itemID := chi.URLParam(r, "itemId")
	var req dto.AutosaveRequest
	if !delivery_helper.DecodeJSON(w, r, &req) {
		return
	}
	ans, err := h.attemptUC.AutosaveAnswer(r.Context(), attemptID, itemID, req.AnswerJSON, req.AnswerText, req.ClientSeq, pid)
	if err != nil {
		if errors.Is(err, node_error.ErrStaleAnswerWrite) {
			delivery_helper.Success(w, http.StatusOK, "answer already up to date", map[string]interface{}{"last_saved_at": time.Now().UTC()})
			return
		}
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "answer saved", map[string]interface{}{"last_saved_at": ans.LastSavedAt})
}

func (h *AttemptHandler) Submit(w http.ResponseWriter, r *http.Request) {
	pid, _ := middleware.ParticipantIDFromContext(r.Context())
	attemptID := chi.URLParam(r, "attemptId")
	att, err := h.attemptUC.SubmitAttempt(r.Context(), attemptID, pid)
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "attempt submitted", map[string]interface{}{
		"grading_status": string(att.GradingStatus), "score": att.Score, "max_score": att.MaxScore,
	})
}

// GetResult serves the participant's latest finished attempt for the
// requested exam. Mixed exams return manual_required with the auto-graded
// subtotal (decision 10); the JWT exam_id must match the path exam.
func (h *AttemptHandler) GetResult(w http.ResponseWriter, r *http.Request) {
	examID := chi.URLParam(r, "examId")
	if tokenExamID, ok := middleware.ExamIDFromContext(r.Context()); ok && tokenExamID != examID {
		delivery_helper.Error(w, http.StatusForbidden, "exam does not belong to your token")
		return
	}
	pid, _ := middleware.ParticipantIDFromContext(r.Context())
	att, err := h.attemptUC.GetResult(r.Context(), pid, examID)
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	resp := dto.ResultResponse{
		AttemptID: att.ID, Status: string(att.Status), Score: att.Score,
		MaxScore: att.MaxScore, GradingStatus: string(att.GradingStatus),
	}
	delivery_helper.Success(w, http.StatusOK, "result retrieved", resp)
}

func (h *AttemptHandler) RecordIntegrity(w http.ResponseWriter, r *http.Request) {
	pid, _ := middleware.ParticipantIDFromContext(r.Context())
	attemptID := chi.URLParam(r, "attemptId")
	var req dto.IntegrityEventRequest
	if !delivery_helper.DecodeJSON(w, r, &req) {
		return
	}
	ev, err := h.integrityUC.RecordEvent(r.Context(), attemptID, pid, req.EventType, req.Description, req.Metadata)
	if err != nil {
		if errors.Is(err, node_error.ErrIntegrityFlood) {
			delivery_helper.Error(w, http.StatusTooManyRequests, "too many integrity events")
			return
		}
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "integrity event recorded", ev)
}
