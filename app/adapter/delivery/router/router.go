package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_middleware "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

// NewRouter assembles the full node HTTP surface: the student sitting flow
// under the JWT middleware and internal central→node routes under the shared
// node token.
func NewRouter(
	issuer outbound.JWTIssuer,
	nodeToken string,
	contentUC inbound.ContentUsecase,
	attemptUC inbound.AttemptUsecase,
	integrityUC inbound.IntegrityUsecase,
	internalHandler *handler.InternalHandler,
	harvestHandler *handler.HarvestHandler,
	readiness http.Handler,
) http.Handler {
	r := chi.NewRouter()

	r.Mount("/", readiness)

	r.Route("/api/v1/student", func(r chi.Router) {
		r.Use(node_middleware.AuthMiddleware(issuer))
		examH := handler.NewExamHandler(contentUC)
		attemptH := handler.NewAttemptHandler(attemptUC, integrityUC)
		r.Get("/exams/{examId}/content", examH.GetContent)
		r.Get("/exams/{examId}/result", attemptH.GetResult)
		r.Get("/exam-attempts/{attemptId}", attemptH.GetState)
		r.Put("/exam-attempts/{attemptId}/answers/{itemId}", attemptH.Autosave)
		r.Post("/exam-attempts/{attemptId}/submit", attemptH.Submit)
		r.Post("/exam-attempts/{attemptId}/integrity-events", attemptH.RecordIntegrity)
	})

	r.Route("/internal/v1", func(r chi.Router) {
		r.Use(node_middleware.NodeTokenAuth(nodeToken))
		r.Post("/bundle", internalHandler.PushBundle)
		r.Post("/harvest/force", harvestHandler.Force)
	})

	return r
}
