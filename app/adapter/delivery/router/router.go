package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_middleware "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

// NewStudentRouter mounts the student sitting flow under the JWT middleware.
// Content is keyed by exam (multi-exam per VPS); attempts and integrity are
// scoped by the attemptId path param plus the JWT participant id.
func NewStudentRouter(issuer outbound.JWTIssuer, contentUC inbound.ContentUsecase, attemptUC inbound.AttemptUsecase, integrityUC inbound.IntegrityUsecase) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1/student", func(r chi.Router) {
		r.Use(node_middleware.AuthMiddleware(issuer))
		r.Get("/exams/{examId}/content", handler.NewExamHandler(contentUC).GetContent)
		r.Get("/exam-attempts/{attemptId}", handler.NewAttemptHandler(attemptUC, integrityUC).GetState)
		r.Put("/exam-attempts/{attemptId}/answers/{itemId}", handler.NewAttemptHandler(attemptUC, integrityUC).Autosave)
		r.Post("/exam-attempts/{attemptId}/submit", handler.NewAttemptHandler(attemptUC, integrityUC).Submit)
		r.Get("/exams/{examId}/result", handler.NewAttemptHandler(attemptUC, integrityUC).GetResult)
		r.Post("/exam-attempts/{attemptId}/integrity-events", handler.NewAttemptHandler(attemptUC, integrityUC).RecordIntegrity)
	})
	return r
}
