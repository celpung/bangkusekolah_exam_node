package central

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

func testCfg(baseURL string) *config.Config {
	return &config.Config{
		CentralBaseURL:   baseURL,
		CentralNodeToken: "tok-1",
		DeploymentID:     "dep-1",
	}
}

func TestHarvestClientSendsAuthAndDecodesAck(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var batch inbound.ExamNodeAttemptBatch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&batch)
		_ = json.NewEncoder(w).Encode(inbound.ExamNodeIngestResult{AcceptedAttemptIDs: []string{"att-9"}})
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	result, err := client.Push(context.Background(), "dep-1", inbound.ExamNodeAttemptBatch{
		Attempts: []inbound.ExamNodeAttemptPayload{{ID: "att-9", Status: "submitted"}},
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if gotPath != "/api/v1/exam-nodes/deployments/dep-1/attempts" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok-1" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if len(batch.Attempts) != 1 || batch.Attempts[0].ID != "att-9" {
		t.Errorf("batch received = %+v", batch)
	}
	if len(result.AcceptedAttemptIDs) != 1 || result.AcceptedAttemptIDs[0] != "att-9" {
		t.Errorf("ack = %+v", result)
	}
}

func TestHarvestClientFailsOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server exploded", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	if _, err := client.Push(context.Background(), "dep-1", inbound.ExamNodeAttemptBatch{}); err == nil {
		t.Fatal("expected non-OK status to surface as an error")
	}
}

var _ = time.Second
