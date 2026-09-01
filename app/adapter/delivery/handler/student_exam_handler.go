package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/dto"
	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type StudentExamHandler struct {
	studentUC inbound.StudentExamUsecase
	attemptUC inbound.AttemptUsecase
}

func NewStudentExamHandler(studentUC inbound.StudentExamUsecase, attemptUC inbound.AttemptUsecase) *StudentExamHandler {
	return &StudentExamHandler{studentUC: studentUC, attemptUC: attemptUC}
}

// ListExams handles GET /api/v1/student/exams — scoped to JWT participant.
func (h *StudentExamHandler) ListExams(w http.ResponseWriter, r *http.Request) {
	pid, ok := middleware.ParticipantIDFromContext(r.Context())
	if !ok {
		delivery_helper.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	exams, err := h.studentUC.ListExams(r.Context(), pid)
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	resp := make([]dto.StudentExamResponse, 0, len(exams))
	for _, e := range exams {
		resp = append(resp, toStudentExamDTO(e))
	}
	delivery_helper.Success(w, http.StatusOK, "Exams retrieved successfully", resp)
}

// StartAttempt handles POST /api/v1/student/exams/{examId}/attempts.
// It rejects cross-exam requests where path examId != JWT claim before calling the service.
func (h *StudentExamHandler) StartAttempt(w http.ResponseWriter, r *http.Request) {
	pid, ok := middleware.ParticipantIDFromContext(r.Context())
	if !ok {
		delivery_helper.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	examID := chi.URLParam(r, "examId")
	if tokenExamID, ok := middleware.ExamIDFromContext(r.Context()); ok && tokenExamID != examID {
		delivery_helper.Error(w, http.StatusForbidden, "exam does not belong to your token")
		return
	}
	var req dto.StartAttemptRequest
	if r.ContentLength != 0 {
		if !delivery_helper.DecodeJSON(w, r, &req) {
			return
		}
	}
	attempt, err := h.attemptUC.StartAttemptWithDevice(r.Context(), pid, examID, req.DeviceID)
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

func toStudentExamDTO(e entity.Exam) dto.StudentExamResponse {
	return dto.StudentExamResponse{
		ID: e.ID, Title: e.Title, StartsAt: e.StartsAt, EndsAt: e.EndsAt,
		DurationMinutes: e.DurationMinutes, MaxAttempts: e.MaxAttempts,
	}
}
