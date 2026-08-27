package handler

import (
	"errors"
	"net/http"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/dto"
	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type AuthHandler struct {
	authUC inbound.AuthUsecase
}

func NewAuthHandler(authUC inbound.AuthUsecase) *AuthHandler {
	return &AuthHandler{authUC: authUC}
}

// Login handles POST /api/v1/auth/exam-login. It decodes {code}, calls Login,
// and returns the frozen envelope. Invalid codes are 401, rate limits 429,
// everything else 5xx via HandleError indirection.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if !delivery_helper.DecodeJSON(w, r, &req) {
		return
	}
	result, err := h.authUC.Login(r.Context(), req.Code)
	if err != nil {
		switch {
		case errors.Is(err, node_error.ErrInvalidAccessCode), errors.Is(err, node_error.ErrUnauthorized):
			delivery_helper.Error(w, http.StatusUnauthorized, "invalid access code")
			return
		case errors.Is(err, node_error.ErrTooManyAttempts), errors.Is(err, node_error.ErrIntegrityFlood):
			delivery_helper.Error(w, http.StatusTooManyRequests, "too many login attempts")
			return
		case errors.Is(err, node_error.ErrExamNotLoaded):
			delivery_helper.Error(w, http.StatusServiceUnavailable, "exam not loaded")
			return
		default:
			delivery_helper.HandleError(w, err)
			// Ensure unhandled maps to 500 if not already
			return
		}
	}
	delivery_helper.Success(w, http.StatusOK, "login successful", result)
}
