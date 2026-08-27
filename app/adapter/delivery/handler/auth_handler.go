package handler

import (
	"net/http"

	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type AuthHandler struct {
	authUC inbound.AuthUsecase
}

func NewAuthHandler(authUC inbound.AuthUsecase) *AuthHandler {
	return &AuthHandler{authUC: authUC}
}

// Login exchanges the paperless exam access code for the node JWT used by the
// student sitting endpoints.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !delivery_helper.DecodeJSON(w, r, &req) {
		return
	}
	result, err := h.authUC.Login(r.Context(), req.Code)
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "exam login successful", result)
}
