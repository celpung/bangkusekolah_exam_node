package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type fakeAuthUC struct {
	loginFn func(context.Context, string) (*inbound.LoginResult, error)
}

func (f *fakeAuthUC) Login(ctx context.Context, code string) (*inbound.LoginResult, error) {
	return f.loginFn(ctx, code)
}

func TestLoginReturnsTokenInResponseEnvelope(t *testing.T) {
	var gotCode string
	uc := &fakeAuthUC{loginFn: func(_ context.Context, code string) (*inbound.LoginResult, error) {
		gotCode = code
		return &inbound.LoginResult{ParticipantID: "part-1", StudentID: "stu-1", ExamID: "exam-1", Token: "jwt-token"}, nil
	}}
	reqBody, _ := json.Marshal(map[string]string{"code": "AAAAAA-000001"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/exam-login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	NewAuthHandler(uc).Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if gotCode != "AAAAAA-000001" {
		t.Fatalf("login code = %q, want normalized request code", gotCode)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"token":"jwt-token"`)) {
		t.Fatalf("login response does not contain token: %s", rec.Body.String())
	}
}

type fakeStartAttemptUC struct {
	inbound.AttemptUsecase
	startFn func(context.Context, string) (*entity.Attempt, error)
}

func (f *fakeStartAttemptUC) StartAttempt(ctx context.Context, participantID string) (*entity.Attempt, error) {
	return f.startFn(ctx, participantID)
}

func TestStartUsesJWTParticipantAndExamPath(t *testing.T) {
	var gotParticipant string
	uc := &fakeStartAttemptUC{startFn: func(_ context.Context, participantID string) (*entity.Attempt, error) {
		gotParticipant = participantID
		return &entity.Attempt{ID: "att-1", ParticipantID: participantID, ExamID: "exam-1"}, nil
	}}
	r := chi.NewRouter()
	r.Use(stubAuth("part-1", "exam-1"))
	r.Post("/api/v1/student/exams/{examId}/attempts", NewAttemptHandler(uc, &fakeIntegrityUC{}).Start)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/student/exams/exam-1/attempts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if gotParticipant != "part-1" {
		t.Fatalf("start participant = %q, want part-1", gotParticipant)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"ID":"att-1"`)) {
		t.Fatalf("start response missing attempt: %s", rec.Body.String())
	}
}

func TestStartRejectsExamPathDifferentFromJWT(t *testing.T) {
	uc := &fakeStartAttemptUC{startFn: func(_ context.Context, _ string) (*entity.Attempt, error) {
		t.Fatal("StartAttempt must not be called for a foreign exam path")
		return nil, nil
	}}
	r := chi.NewRouter()
	r.Use(stubAuth("part-1", "exam-1"))
	r.Post("/api/v1/student/exams/{examId}/attempts", NewAttemptHandler(uc, &fakeIntegrityUC{}).Start)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/student/exams/exam-2/attempts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign exam start status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}
