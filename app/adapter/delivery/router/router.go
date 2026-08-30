package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_middleware "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// NewRouter assembles the full node HTTP surface: the student sitting flow
// under the JWT middleware, the public auth route outside it, and internal
// central→node routes under the shared node token.
//
// fenceRepos is optional for backwards-compatible handler tests; production
// wiring always supplies the node repository so fenced deployments are rejected
// before any student request reaches a usecase.
func NewRouter(
	issuer outbound.JWTIssuer,
	nodeToken string,
	authUC inbound.AuthUsecase,
	studentUC inbound.StudentExamUsecase,
	contentUC inbound.ContentUsecase,
	attemptUC inbound.AttemptUsecase,
	integrityUC inbound.IntegrityUsecase,
	internalHandler *handler.InternalHandler,
	harvestHandler *handler.HarvestHandler,
	readiness http.Handler,
	fenceRepos ...outbound_repository.DeploymentFenceRepository,
) http.Handler {
	r := chi.NewRouter()

	r.Mount("/", readiness)

	if authUC != nil {
		authH := handler.NewAuthHandler(authUC)
		r.Post("/api/v1/auth/exam-login", authH.Login)
	}

	r.Route("/api/v1/student", func(r chi.Router) {
		if len(fenceRepos) > 0 && fenceRepos[0] != nil {
			r.Use(node_middleware.AuthMiddleware(issuer, fenceRepos[0]))
		} else {
			r.Use(node_middleware.AuthMiddleware(issuer))
		}
		if studentUC != nil && attemptUC != nil {
			studentH := handler.NewStudentExamHandler(studentUC, attemptUC)
			r.Get("/exams", studentH.ListExams)
			r.Post("/exams/{examId}/attempts", studentH.StartAttempt)
		}
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
		r.Post("/cache/reload", internalHandler.ReloadCache)
		r.Post("/harvest/force", harvestHandler.Force)
	})

	return r
}
