package helper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
)

func TestHandleErrorIncludesStableCodeForExamNotOpen(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleError(rec, node_error.ErrExamNotOpen)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "request rejected" {
		t.Fatalf("error = %v, want request rejected", body["error"])
	}
	if body["code"] != "exam_not_open" {
		t.Fatalf("code = %v, want exam_not_open", body["code"])
	}
}

func TestHandleErrorIncludesStableCodeForExpiredAttempt(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleError(rec, node_error.ErrAttemptExpired)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != "attempt_expired" {
		t.Fatalf("code = %v, want attempt_expired", body["code"])
	}
}
