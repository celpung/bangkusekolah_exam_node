package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type AttemptHandler struct{ attemptUC inbound.AttemptUsecase }

func NewAttemptHandler(uc inbound.AttemptUsecase) *AttemptHandler {
	return &AttemptHandler{attemptUC: uc}
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
	delivery_helper.Success(w, http.StatusOK, "attempt retrieved", state)
}
