package central

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFenceClientListsAndAcknowledgesPendingFence(t *testing.T) {
	var gotAuth string
	var acked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/v1/exam-nodes/fences":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    []map[string]string{{"id": "dep-1", "exam_id": "exam-1", "status": "aborted"}},
			})
		case "/api/v1/exam-nodes/deployments/dep-1/fence-ack":
			acked = r.Method == http.MethodPost
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]bool{"ok": true}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewFenceClient(server.URL, "node-token")
	fences, err := client.ListPendingFences(context.Background())
	if err != nil {
		t.Fatalf("ListPendingFences: %v", err)
	}
	if len(fences) != 1 || fences[0].ID != "dep-1" {
		t.Fatalf("fences=%+v", fences)
	}
	if err := client.AcknowledgeFence(context.Background(), "dep-1"); err != nil {
		t.Fatalf("AcknowledgeFence: %v", err)
	}
	if gotAuth != "Bearer node-token" || !acked {
		t.Fatalf("auth=%q acked=%v", gotAuth, acked)
	}
}
