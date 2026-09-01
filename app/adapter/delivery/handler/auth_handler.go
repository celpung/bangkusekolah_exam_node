package handler

import (
	"errors"
	"net"
	"net/http"
	"strings"

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
	var result *inbound.LoginResult
	var err error
	if rateLimited, ok := h.authUC.(inbound.RateLimitedAuthUsecase); ok {
		result, err = rateLimited.LoginWithKey(r.Context(), req.Code, clientRateKey(r))
	} else {
		result, err = h.authUC.Login(r.Context(), req.Code)
	}
	if err != nil {
		switch {
		case errors.Is(err, node_error.ErrInvalidAccessCode), errors.Is(err, node_error.ErrUnauthorized):
			delivery_helper.ErrorWithCode(w, http.StatusUnauthorized, "invalid_access_code", "invalid access code")
			return
		case errors.Is(err, node_error.ErrTooManyAttempts), errors.Is(err, node_error.ErrIntegrityFlood):
			delivery_helper.ErrorWithCode(w, http.StatusTooManyRequests, "too_many_login_attempts", "too many login attempts")
			return
		case errors.Is(err, node_error.ErrExamNotLoaded):
			delivery_helper.ErrorWithCode(w, http.StatusServiceUnavailable, "exam_not_loaded", "exam not loaded")
			return
		default:
			delivery_helper.HandleError(w, err)
			return
		}
	}
	delivery_helper.Success(w, http.StatusOK, "login successful", result)
}

func clientRateKey(r *http.Request) string {
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil && host != "" {
		return host
	}
	if remote != "" {
		return remote
	}
	return "unknown"
}
