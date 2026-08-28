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
// authUC and studentUC are optional variadic to preserve backward compatibility
// with existing handler tests that call the 7-arg form: if supplied they are
// wired as POST /api/v1/auth/exam-login and GET/POST student exam routes.
func NewRouter(
	issuer outbound.JWTIssuer,
	nodeToken string,
	contentUC inbound.ContentUsecase,
	attemptUC inbound.AttemptUsecase,
	integrityUC inbound.IntegrityUsecase,
	internalHandler *handler.InternalHandler,
	harvestHandler *handler.HarvestHandler,
	readiness http.Handler,
	extra ...interface{},
) http.Handler {
	var authUC inbound.AuthUsecase
	var studentUC inbound.StudentExamUsecase
	var fenceRepo outbound_repository.DeploymentFenceRepository
	if len(extra) > 0 {
		if v, ok := extra[0].(inbound.AuthUsecase); ok {
			authUC = v
		}
	}
	if len(extra) > 1 {
		if v, ok := extra[1].(inbound.StudentExamUsecase); ok {
			studentUC = v
		}
	}
	if len(extra) > 2 {
		fenceRepo, _ = extra[2].(outbound_repository.DeploymentFenceRepository)
	}

	r := chi.NewRouter()

	r.Mount("/", readiness)

	if authUC != nil {
		authH := handler.NewAuthHandler(authUC)
		r.Post("/api/v1/auth/exam-login", authH.Login)
	}

	r.Route("/api/v1/student", func(r chi.Router) {
		if fenceRepo != nil {
			r.Use(node_middleware.AuthMiddleware(issuer, fenceRepo))
		} else {
			r.Use(node_middleware.AuthMiddleware(issuer))
		}
		// Student exam list/start are token-scoped
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
		r.Post("/harvest/force", harvestHandler.Force)
	})

	return r
}
