package handler

import (
	"net/http"

	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

type InternalHandler struct {
	bundleSvc  *service.BundleService
	contentSvc *service.ContentService
}

func NewInternalHandler(bundleSvc *service.BundleService, contentServices ...*service.ContentService) *InternalHandler {
	var contentSvc *service.ContentService
	if len(contentServices) > 0 {
		contentSvc = contentServices[0]
	}
	return &InternalHandler{bundleSvc: bundleSvc, contentSvc: contentSvc}
}

func (h *InternalHandler) ReloadCache(w http.ResponseWriter, r *http.Request) {
	if h.contentSvc == nil {
		delivery_helper.Error(w, http.StatusNotImplemented, "cache reload is unavailable")
		return
	}
	if err := h.contentSvc.ReloadAllCaches(r.Context()); err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "cache reloaded", nil)
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
