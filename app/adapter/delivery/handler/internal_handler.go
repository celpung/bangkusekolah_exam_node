package handler

import (
	"net/http"

	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

type InternalHandler struct {
	bundleSvc *service.BundleService
}

func NewInternalHandler(bundleSvc *service.BundleService) *InternalHandler {
	return &InternalHandler{bundleSvc: bundleSvc}
}

// PushBundle accepts one exam bundle pushed by central. The route sits behind
// NodeTokenAuth, not the student JWT middleware.
func (h *InternalHandler) PushBundle(w http.ResponseWriter, r *http.Request) {
	var bundle inbound.ExamNodeBundle
	if !delivery_helper.DecodeJSON(w, r, &bundle) {
		return
	}
	if err := h.bundleSvc.LoadBundle(r.Context(), bundle); err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "bundle loaded", map[string]string{"checksum": bundle.Checksum})
}
