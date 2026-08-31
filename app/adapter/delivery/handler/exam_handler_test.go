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

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/middleware"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type fakeExamCache struct {
	content *inbound.ExamContent
	etag    string
	gzip    []byte
	raw     []byte
}

type fakeContentUC struct {
	exams map[string]fakeExamCache
	err   error
}

func (f *fakeContentUC) GetExamContent(_ context.Context, examID string) (*inbound.ExamContent, string, []byte, []byte, error) {
	if f.err != nil {
		return nil, "", nil, nil, f.err
	}
	cached, ok := f.exams[examID]
	if !ok {
		return nil, "", nil, nil, node_error.ErrExamNotLoaded
	}
	return cached.content, cached.etag, cached.gzip, cached.raw, nil
}

type fakeAttemptUC struct {
	state *inbound.AttemptState
	err   error
}

func (f *fakeAttemptUC) StartAttempt(_ context.Context, _, _ string) (*entity.Attempt, error) {
	return nil, nil
}
func (f *fakeAttemptUC) StartAttemptWithDevice(_ context.Context, _, _, _ string) (*entity.Attempt, error) {
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
func (f *fakeAttemptUC) GetResult(_ context.Context, _, _ string) (*entity.Attempt, error) {
	return nil, nil
}

// stubAuth stands in for the Task 16 JWT middleware: it injects a fixed
// participant id and exam id so these tests never need a real token or DB.
func stubAuth(pid, examID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), middleware.CtxParticipantID, pid)
			ctx = context.WithValue(ctx, middleware.CtxExamID, examID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newContentRouter(contentUC inbound.ContentUsecase, attemptUC inbound.AttemptUsecase) chi.Router {
	r := chi.NewRouter()
	r.Use(stubAuth("part-1", "exam-a"))
	r.Get("/api/v1/student/exams/{examId}/content", NewExamHandler(contentUC).GetContent)
	r.Get("/api/v1/student/exam-attempts/{attemptId}", NewAttemptHandler(attemptUC, &fakeIntegrityUC{}).GetState)
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

func singleExamCache(id string) map[string]fakeExamCache {
	content := &inbound.ExamContent{Exam: &entity.Exam{ID: id, Title: "UTS"}}
	raw := mustJSON(content)
	return map[string]fakeExamCache{
		id: {content: content, etag: `"abc123"`, gzip: mustGzip(raw), raw: raw},
	}
}

func TestGetExamContentReturnsETagAndGzip(t *testing.T) {
	caches := singleExamCache("exam-a")
	router := newContentRouter(&fakeContentUC{exams: caches}, &fakeAttemptUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-a/content", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != `"abc123"` {
		t.Fatalf("content: code=%d etag=%q", rec.Code, rec.Header().Get("ETag"))
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("missing Vary: Accept-Encoding, got %q", rec.Header().Get("Vary"))
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("want gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	decoded, _ := io.ReadAll(gr)
	if string(decoded) != string(caches["exam-a"].raw) {
		t.Fatalf("gzip body mismatch")
	}
}

func TestGetExamContentFallsBackToRawWithoutGzip(t *testing.T) {
	router := newContentRouter(&fakeContentUC{exams: singleExamCache("exam-a")}, &fakeAttemptUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-a/content", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatalf("no-gzip client: code=%d encoding=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if !bytes.Equal(rec.Body.Bytes(), cachesRaw(singleExamCache("exam-a"))) {
		t.Fatalf("raw fallback body mismatch")
	}
}

func cachesRaw(caches map[string]fakeExamCache) []byte { return caches["exam-a"].raw }

func TestGetExamContentReturns304OnMatchingETag(t *testing.T) {
	router := newContentRouter(&fakeContentUC{exams: singleExamCache("exam-a")}, &fakeAttemptUC{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-a/content", nil)
	req.Header.Set("If-None-Match", `"abc123"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("If-None-Match: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("304 missing Vary: Accept-Encoding, got %q", rec.Header().Get("Vary"))
	}
}

// TestGetExamContentServesEachExamItsOwnContent pins BLOCKER-1: two exams
// cached on the same node must never cross-serve content.
func TestGetExamContentServesEachExamItsOwnContent(t *testing.T) {
	examA := &inbound.ExamContent{Exam: &entity.Exam{ID: "exam-a", Title: "Matematika"}}
	examB := &inbound.ExamContent{Exam: &entity.Exam{ID: "exam-b", Title: "IPA"}}
	rawA, rawB := mustJSON(examA), mustJSON(examB)
	caches := map[string]fakeExamCache{
		"exam-a": {content: examA, etag: `"etag-a"`, gzip: mustGzip(rawA), raw: rawA},
		"exam-b": {content: examB, etag: `"etag-b"`, gzip: mustGzip(rawB), raw: rawB},
	}
	fetch := func(examID string) *httptest.ResponseRecorder {
		r := chi.NewRouter()
		r.Use(stubAuth("part-1", examID))
		r.Get("/api/v1/student/exams/{examId}/content", NewExamHandler(&fakeContentUC{exams: caches}).GetContent)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/"+examID+"/content", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	recA, recB := fetch("exam-a"), fetch("exam-b")

	if recA.Header().Get("ETag") != `"etag-a"` || recB.Header().Get("ETag") != `"etag-b"` {
		t.Fatalf("etags: a=%q b=%q", recA.Header().Get("ETag"), recB.Header().Get("ETag"))
	}
	// bodies are gzipped; compare the decompressed exam IDs
	bodyA := gunzip(t, recA.Body.Bytes())
	bodyB := gunzip(t, recB.Body.Bytes())
	if !bytes.Contains(bodyA, []byte(`"exam-a"`)) || bytes.Contains(bodyA, []byte(`"exam-b"`)) {
		t.Fatalf("exam A content cross-served: %s", bodyA)
	}
	if !bytes.Contains(bodyB, []byte(`"exam-b"`)) || bytes.Contains(bodyB, []byte(`"exam-a"`)) {
		t.Fatalf("exam B content cross-served: %s", bodyB)
	}
}

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, _ := io.ReadAll(gr)
	return out
}

// TestGetExamContentRejectsForeignExam pins the JWT/path exam match: a token
// issued for exam-a cannot read exam-b content on the same node.
func TestGetExamContentRejectsForeignExam(t *testing.T) {
	caches := map[string]fakeExamCache{
		"exam-a": {},
		"exam-b": {},
	}
	router := newContentRouter(&fakeContentUC{exams: caches}, &fakeAttemptUC{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-b/content", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign exam: code=%d want 403", rec.Code)
	}
}

func TestGetExamContentIsByteIdenticalForTwoParticipants(t *testing.T) {
	router := newContentRouter(&fakeContentUC{exams: singleExamCache("exam-a")}, &fakeAttemptUC{})

	doReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-a/content", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exams/exam-a/content", nil)
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

func TestGetAttemptStateUsesFrozenSnakeCaseResponse(t *testing.T) {
	now := time.Date(2026, 8, 29, 8, 10, 0, 0, time.UTC)
	state := &inbound.AttemptState{
		Attempt: &entity.Attempt{
			ID: "att-1", ExamID: "exam-a", AttemptNo: 1,
			Status: entity.AttemptInProgress, MaxScore: 10,
			StartedAt: now, DueAt: now.Add(time.Hour),
		},
		Answers: []entity.Answer{{
			ID: "ans-1", ItemID: "item-1", AnswerJSON: map[string]interface{}{"answer": "B"},
			ClientSeq: 7, LastSavedAt: now,
		}},
		ServerTime: now,
	}
	router := newContentRouter(&fakeContentUC{}, &fakeAttemptUC{state: state})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exam-attempts/att-1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode state response: %v body=%q", err, rec.Body.String())
	}
	if _, ok := envelope.Data["Attempt"]; ok {
		t.Fatal("state response must not serialize the domain Attempt field")
	}
	if envelope.Data["id"] != "att-1" || envelope.Data["exam_id"] != "exam-a" {
		t.Fatalf("state identity is not snake_case: %#v", envelope.Data)
	}
	answers, ok := envelope.Data["answers"].([]interface{})
	if !ok || len(answers) != 1 {
		t.Fatalf("state answers malformed: %#v", envelope.Data["answers"])
	}
	answer, ok := answers[0].(map[string]interface{})
	if !ok || answer["item_id"] != "item-1" {
		t.Fatalf("answer is not snake_case: %#v", answers[0])
	}
	if _, ok := answer["ItemID"]; ok {
		t.Fatal("answer response must not serialize domain field names")
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
