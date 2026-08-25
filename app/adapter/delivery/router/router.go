package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

// NewStudentRouter mounts the student sitting flow under the JWT middleware.
// The node serves exactly one exam, so content is keyed only by the cached
// bundle; attempt routes carry the attemptId path param.
func NewStudentRouter(issuer outbound.JWTIssuer, contentUC inbound.ContentUsecase, attemptUC inbound.AttemptUsecase) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1/student", func(r chi.Router) {
		r.Use(middleware.AuthMiddleware(issuer))
		r.Get("/exams/{examId}/content", handler.NewExamHandler(contentUC).GetContent)
		r.Get("/exam-attempts/{attemptId}", handler.NewAttemptHandler(attemptUC).GetState)
		// Task 18 adds autosave, submit, result, integrity here.
	})
	return r
}

var _ = delivery_helper.Success // keep helper import until Task 18 handlers land
