package central

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

func TestHarvestClientDecodesStandardCentralEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Attempts ingested",
			"data": inbound.ExamNodeIngestResult{
				AcceptedAttemptIDs: []string{"att-envelope"},
				Failures:           map[string]string{},
			},
		})
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	result, err := client.Push(context.Background(), "dep-1", inbound.ExamNodeAttemptBatch{})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(result.AcceptedAttemptIDs) != 1 || result.AcceptedAttemptIDs[0] != "att-envelope" {
		t.Fatalf("ack = %+v, want accepted att-envelope", result)
	}
}
