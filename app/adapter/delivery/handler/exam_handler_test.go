package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type fakeContentUC struct {
	content *inbound.ExamContent
	etag    string
	gzip    []byte
	raw     []byte
	err     error
}

func (f *fakeContentUC) GetExamContent(_ context.Context) (*inbound.ExamContent, string, []byte, []byte, error) {
	return f.content, f.etag, f.gzip, f.raw, f.err
}

type fakeAttemptUC struct {
	state *inbound.AttemptState
	err   error
}

func (f *fakeAttemptUC) StartAttempt(_ context.Context, _ string) (*entity.Attempt, error) {
	return nil, nil
}
func (f *fakeAttemptUC) GetAttemptState(_ context.Context, _, _ string) (*inbound.AttemptState, error) {
	return f.state, f.err
}
func (f *fakeAttemptUC) AutosaveAnswer(_ context.Context, _, _ string, _ map[string]interface{}, _ *string, _ int64, _ string) (*entity.Answer, error) {
	return nil, nil
}
func (f *fakeAttemptUC) SubmitAttempt(_ context.Context, _, _ string) (*entity.Attempt, error) {
	return nil, nil
}

// stubAuth injects a fixed participant id, standing in for the Task 16 JWT
// middleware so these tests never need a real token or DB.
func stubAuth(pid string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxKeyPID("participant_id"), pid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type ctxKeyPID string

func newContentRouter(contentUC inbound.ContentUsecase, attemptUC inbound.AttemptUsecase) chi.Router {
	r := chi.NewRouter()
	r.Use(stubAuth("part-1"))
	r.Get("/api/v1/student/exams/{examId}/content", NewExamHandler(contentUC).GetContent)
	r.Get("/api/v1/student/exam-attempts/{attemptId}", NewAttemptHandler(attemptUC).GetState)
	return r
}

func mustJSON(v interface{}) []byte { b, _ := json.Marshal(v); return b }
func mustGzip(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}

func TestGetExamContentReturnsETagAndGzip(t *testing.T) {
	content := &inbound.ExamContent{Exam: &entity.Exam{ID: "exam-1", Title: "UTS"}, Items: []inbound.ExamItemDTO{{ID: "item-1", QuestionType: "single_choice", Prompt: "2+2?"}}}
	raw := mustJSON(content)
	gz := mustGzip(raw)
	etag := `"abc123"`
	router := newContentRouter(&fakeContentUC{content: content, etag: etag, gzip: gz, raw: raw}, &fakeAttemptUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-1/content", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != etag {
		t.Fatalf("content: code=%d etag=%q", rec.Code, rec.Header().Get("ETag"))
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("want gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	decoded, _ := io.ReadAll(gr)
	if string(decoded) != string(raw) {
		t.Fatalf("gzip body mismatch")
	}
}

func TestGetExamContentFallsBackToRawWithoutGzip(t *testing.T) {
	content := &inbound.ExamContent{Exam: &entity.Exam{ID: "exam-1"}}
	raw := mustJSON(content)
	router := newContentRouter(&fakeContentUC{content: content, etag: `"e1"`, gzip: []byte("gz"), raw: raw}, &fakeAttemptUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-1/content", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatalf("no-gzip client: code=%d encoding=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if !bytes.Equal(rec.Body.Bytes(), raw) {
		t.Fatalf("raw fallback body mismatch")
	}
}

func TestGetExamContentReturns304OnMatchingETag(t *testing.T) {
	content := &inbound.ExamContent{Exam: &entity.Exam{ID: "exam-1"}}
	raw := mustJSON(content)
	router := newContentRouter(&fakeContentUC{content: content, etag: `"abc123"`, gzip: []byte("gz"), raw: raw}, &fakeAttemptUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-1/content", nil)
	req.Header.Set("If-None-Match", `"abc123"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("If-None-Match: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGetExamContentIsByteIdenticalForTwoParticipants(t *testing.T) {
	content := &inbound.ExamContent{Exam: &entity.Exam{ID: "exam-1"}}
	gz := mustGzip(mustJSON(content))
	raw := mustJSON(content)
	uc := &fakeContentUC{content: content, etag: `"abc123"`, gzip: gz, raw: raw}
	router := newContentRouter(uc, &fakeAttemptUC{})

	doReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-1/content", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	rec1, rec2 := doReq(), doReq()

	if !bytes.Equal(rec1.Body.Bytes(), rec2.Body.Bytes()) || rec1.Header().Get("ETag") != rec2.Header().Get("ETag") {
		t.Fatal("content must be byte-identical for two participants (immutable, cached)")
	}
}

func TestGetExamContentMapsNotLoadedToBadRequest(t *testing.T) {
	router := newContentRouter(&fakeContentUC{err: node_error.ErrExamNotLoaded}, &fakeAttemptUC{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-1/content", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("not loaded: code=%d want 400", rec.Code)
	}
}

func TestGetAttemptStateReturnsAttemptPlusAnswersAndServerTime(t *testing.T) {
	now := time.Now()
	state := &inbound.AttemptState{
		Attempt: &entity.Attempt{ID: "att-1", ParticipantID: "part-1"},
		Answers: []entity.Answer{{ID: "ans-1"}}, ServerTime: now,
	}
	router := newContentRouter(&fakeContentUC{}, &fakeAttemptUC{state: state})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exam-attempts/att-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("server_time")) {
		t.Fatalf("response missing server_time: %q", rec.Body.String())
	}
}

func TestGetAttemptStateRejectsForeignAttemptAsForbidden(t *testing.T) {
	router := newContentRouter(&fakeContentUC{}, &fakeAttemptUC{err: node_error.ErrForbidden})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exam-attempts/att-other", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign attempt: code=%d want 403", rec.Code)
	}
}
