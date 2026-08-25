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
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// fakes for this file only — full AttemptUsecase + IntegrityUsecase
type fakeSittingAttemptUC struct {
	autosaveFn  func(ctx context.Context, attemptID, itemID string, answerJSON map[string]interface{}, answerText *string, clientSeq int64, participantID string) (*entity.Answer, error)
	submitFn    func(ctx context.Context, attemptID, participantID string) (*entity.Attempt, error)
	getResultFn func(ctx context.Context, participantID, examID string) (*entity.Attempt, error)
}

func (f *fakeSittingAttemptUC) StartAttempt(_ context.Context, _ string) (*entity.Attempt, error) {
	return nil, nil
}
func (f *fakeSittingAttemptUC) GetAttemptState(_ context.Context, _, _ string) (*inbound.AttemptState, error) {
	return nil, nil
}
func (f *fakeSittingAttemptUC) AutosaveAnswer(ctx context.Context, aid, iid string, aj map[string]interface{}, at *string, seq int64, pid string) (*entity.Answer, error) {
	return f.autosaveFn(ctx, aid, iid, aj, at, seq, pid)
}
func (f *fakeSittingAttemptUC) SubmitAttempt(ctx context.Context, aid, pid string) (*entity.Attempt, error) {
	return f.submitFn(ctx, aid, pid)
}
func (f *fakeSittingAttemptUC) GetResult(ctx context.Context, pid, eid string) (*entity.Attempt, error) {
	return f.getResultFn(ctx, pid, eid)
}

type fakeIntegrityUC struct {
	recordFn func(ctx context.Context, aid, pid, etype string, desc *string, meta map[string]interface{}) (*entity.IntegrityEvent, error)
}

func (f *fakeIntegrityUC) RecordEvent(ctx context.Context, aid, pid, etype string, desc *string, meta map[string]interface{}) (*entity.IntegrityEvent, error) {
	return f.recordFn(ctx, aid, pid, etype, desc, meta)
}

func newSittingRouter(attemptUC inbound.AttemptUsecase, integrityUC inbound.IntegrityUsecase) chi.Router {
	h := NewAttemptHandler(attemptUC, integrityUC)
	r := chi.NewRouter()
	r.Use(stubAuth("part-1", "exam-1"))
	r.Put("/api/v1/student/exam-attempts/{attemptId}/answers/{itemId}", h.Autosave)
	r.Post("/api/v1/student/exam-attempts/{attemptId}/submit", h.Submit)
	r.Get("/api/v1/student/exams/{examId}/result", h.GetResult)
	r.Post("/api/v1/student/exam-attempts/{attemptId}/integrity-events", h.RecordIntegrity)
	return r
}

func TestAutosaveReturnsLastSavedAt(t *testing.T) {
	uc := &fakeSittingAttemptUC{autosaveFn: func(_ context.Context, _, _ string, _ map[string]interface{}, _ *string, _ int64, _ string) (*entity.Answer, error) {
		return &entity.Answer{ID: "ans-1", GradingStatus: entity.GradingAutoGraded}, nil
	}}
	router := newSittingRouter(uc, &fakeIntegrityUC{})

	body, _ := json.Marshal(map[string]interface{}{"answer_json": map[string]interface{}{"answer": "A"}, "client_seq": 3})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/student/exam-attempts/att-1/answers/item-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("autosave: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("last_saved_at")) {
		t.Fatalf("response missing last_saved_at: %q", rec.Body.String())
	}
}

func TestAutosaveStaleClientSeqIs200WithNoChange(t *testing.T) {
	uc := &fakeSittingAttemptUC{autosaveFn: func(_ context.Context, _, _ string, _ map[string]interface{}, _ *string, _ int64, _ string) (*entity.Answer, error) {
		return nil, node_error.ErrStaleAnswerWrite
	}}
	router := newSittingRouter(uc, &fakeIntegrityUC{})

	body, _ := json.Marshal(map[string]interface{}{"answer_json": map[string]interface{}{"answer": "A"}, "client_seq": 1})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/student/exam-attempts/att-1/answers/item-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Stale is not 409 — the client already has the newer content; retry is a no-op.
	if rec.Code != http.StatusOK {
		t.Fatalf("stale seq: code=%d, want 200", rec.Code)
	}
}

func TestAutosaveRejectsLockedAttempt(t *testing.T) {
	uc := &fakeSittingAttemptUC{autosaveFn: func(_ context.Context, _, _ string, _ map[string]interface{}, _ *string, _ int64, _ string) (*entity.Answer, error) {
		return nil, node_error.ErrAttemptLocked
	}}
	router := newSittingRouter(uc, &fakeIntegrityUC{})

	body, _ := json.Marshal(map[string]interface{}{"answer_json": map[string]interface{}{"answer": "A"}, "client_seq": 1})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/student/exam-attempts/att-1/answers/item-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("locked attempt: code=%d want 403", rec.Code)
	}
}

func TestSubmitReturnsGradingStatus(t *testing.T) {
	uc := &fakeSittingAttemptUC{submitFn: func(_ context.Context, _, _ string) (*entity.Attempt, error) {
		score := 10.0
		return &entity.Attempt{ID: "att-1", Status: entity.AttemptSubmitted, Score: &score, MaxScore: 30, GradingStatus: entity.GradingManualRequired}, nil
	}}
	router := newSittingRouter(uc, &fakeIntegrityUC{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/student/exam-attempts/att-1/submit", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("manual_required")) {
		t.Fatalf("submit: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGetResultObjectiveOnlyShowsScore(t *testing.T) {
	score := 25.0
	uc := &fakeSittingAttemptUC{getResultFn: func(_ context.Context, _, _ string) (*entity.Attempt, error) {
		return &entity.Attempt{ID: "att-1", Status: entity.AttemptGraded, Score: &score, MaxScore: 30, GradingStatus: entity.GradingAutoGraded}, nil
	}}
	router := newSittingRouter(uc, &fakeIntegrityUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-1/result", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("25")) {
		t.Fatalf("result: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGetResultNotAvailableIsBadRequest(t *testing.T) {
	uc := &fakeSittingAttemptUC{getResultFn: func(_ context.Context, _, _ string) (*entity.Attempt, error) {
		return nil, node_error.ErrResultNotAvailable
	}}
	router := newSittingRouter(uc, &fakeIntegrityUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-1/result", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("result not available: code=%d want 400", rec.Code)
	}
}

func TestIntegrityRateLimited(t *testing.T) {
	calls := 0
	uc := &fakeIntegrityUC{recordFn: func(_ context.Context, _, _, _ string, _ *string, _ map[string]interface{}) (*entity.IntegrityEvent, error) {
		calls++
		if calls > 10 {
			return nil, node_error.ErrIntegrityFlood
		}
		return &entity.IntegrityEvent{ID: "ev-1"}, nil
	}}
	router := newSittingRouter(&fakeSittingAttemptUC{}, uc)

	for i := 0; i < 11; i++ {
		body, _ := json.Marshal(map[string]interface{}{"event_type": "focus_lost"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/student/exam-attempts/att-1/integrity-events", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if i < 10 && rec.Code != http.StatusOK {
			t.Fatalf("event %d: code=%d, want 200", i, rec.Code)
		}
		if i == 10 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("11th event: code=%d, want 429", rec.Code)
		}
	}
}

func TestRecordIntegrityAcceptsMetadata(t *testing.T) {
	var gotMeta map[string]interface{}
	uc := &fakeIntegrityUC{recordFn: func(_ context.Context, _, _, _ string, _ *string, meta map[string]interface{}) (*entity.IntegrityEvent, error) {
		gotMeta = meta
		return &entity.IntegrityEvent{ID: "ev-2"}, nil
	}}
	router := newSittingRouter(&fakeSittingAttemptUC{}, uc)

	body, _ := json.Marshal(map[string]interface{}{
		"event_type":    "tab_switch",
		"metadata_json": map[string]interface{}{"duration_ms": 1500},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/student/exam-attempts/att-1/integrity-events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("integrity: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if gotMeta == nil || gotMeta["duration_ms"] != float64(1500) {
		t.Fatalf("metadata not passed through: %v", gotMeta)
	}
}
