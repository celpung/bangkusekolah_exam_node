package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_middleware "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

type reloadRouteRepo struct {
	outbound_repository.NodeRepository
}

func (r *reloadRouteRepo) ListExams(context.Context) ([]entity.Exam, error) {
	return []entity.Exam{{ID: "exam-1", Title: "Exam"}}, nil
}

func (r *reloadRouteRepo) FindExamByID(context.Context, string) (*entity.Exam, error) {
	return &entity.Exam{ID: "exam-1", Title: "Exam"}, nil
}

func (r *reloadRouteRepo) ListItemsByExamID(context.Context, string) ([]entity.Item, error) {
	return []entity.Item{{
		ID: "item-1", ExamID: "exam-1", SectionID: "section-1",
		SectionTitle: "Section", QuestionType: entity.QuestionSingleChoice,
		PromptSnapshot: "1+1?", Points: 1,
	}}, nil
}

func TestCacheReloadRouteRequiresNodeToken(t *testing.T) {
	contentSvc := service.NewContentService(&reloadRouteRepo{})
	internal := handler.NewInternalHandler(nil, contentSvc)

	r := chi.NewRouter()
	r.Route("/internal/v1", func(r chi.Router) {
		r.Use(node_middleware.NodeTokenAuth("test-node-token"))
		r.Post("/cache/reload", internal.ReloadCache)
	})

	for _, tc := range []struct {
		name   string
		header string
		want   int
	}{
		{name: "missing token", want: http.StatusUnauthorized},
		{name: "student token", header: "Bearer student-jwt", want: http.StatusUnauthorized},
		{name: "node token", header: "Bearer test-node-token", want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/cache/reload", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
