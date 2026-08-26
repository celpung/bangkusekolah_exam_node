package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

// TestGetResultRejectsForeignExam pins BLOCKER-1: a token issued for exam-a
// cannot read exam-b's result on the same node.
func TestGetResultRejectsForeignExam(t *testing.T) {
	score := 20.0
	uc := &fakeSittingAttemptUC{getResultFn: func(_ context.Context, _, examID string) (*entity.Attempt, error) {
		return &entity.Attempt{ID: "att-a", Status: entity.AttemptGraded, Score: &score, MaxScore: 30}, nil
	}}
	r := chi.NewRouter()
	r.Use(stubAuth("part-1", "exam-a"))
	h := NewAttemptHandler(uc, &fakeIntegrityUC{})
	r.Get("/api/v1/student/exams/{examId}/result", h.GetResult)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-b/result", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign exam result: code=%d want 403", rec.Code)
	}
}

// TestGetResultScopedToOwnExam proves the happy path still works when the
// token and path agree.
func TestGetResultScopedToOwnExam(t *testing.T) {
	score := 20.0
	var gotExam string
	uc := &fakeSittingAttemptUC{getResultFn: func(_ context.Context, _, examID string) (*entity.Attempt, error) {
		gotExam = examID
		return &entity.Attempt{ID: "att-a", Status: entity.AttemptGraded, Score: &score, MaxScore: 30}, nil
	}}
	r := chi.NewRouter()
	r.Use(stubAuth("part-1", "exam-a"))
	h := NewAttemptHandler(uc, &fakeIntegrityUC{})
	r.Get("/api/v1/student/exams/{examId}/result", h.GetResult)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-a/result", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || gotExam != "exam-a" {
		t.Fatalf("own exam result: code=%d gotExam=%q", rec.Code, gotExam)
	}
}
