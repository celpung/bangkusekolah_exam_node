package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type ExamHandler struct{ contentUC inbound.ContentUsecase }

func NewExamHandler(uc inbound.ContentUsecase) *ExamHandler { return &ExamHandler{contentUC: uc} }

// GetContent serves the requested exam's immutable cached content with an
// ETag and pre-gzipped bytes — no DB, no marshal on the hot path. The exam is
// scoped by the path param and validated against the JWT's exam_id, so one
// VPS can host multiple exams without cross-exam exposure.
func (h *ExamHandler) GetContent(w http.ResponseWriter, r *http.Request) {
	examID := chi.URLParam(r, "examId")
	if tokenExamID, ok := middleware.ExamIDFromContext(r.Context()); ok && tokenExamID != examID {
		delivery_helper.Error(w, http.StatusForbidden, "exam does not belong to your token")
		return
	}
	content, etag, gzipBytes, rawBytes, err := h.contentUC.GetExamContent(r.Context(), examID)
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Vary", "Accept-Encoding")
	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(gzipBytes) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(gzipBytes)
		return
	}
	// Fallback without gzip — still served from memory, already marshaled.
	_ = content
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(rawBytes)
}
