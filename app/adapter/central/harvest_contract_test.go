package central

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestHarvestPayloadStrictDecode pins BLOCKER-1 (review v4): the node's
// JSON payload must be accepted by central's DisallowUnknownFields decoder.
// This test marshals the real node payload and decodes it with a strict
// decoder, proving no unknown fields leak into the contract.
func TestHarvestPayloadStrictDecode(t *testing.T) {
	batch := inbound.ExamNodeAttemptBatch{
		Attempts: []inbound.ExamNodeAttemptPayload{{
			ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1",
			AttemptNo: 1, Status: "submitted",
			Answers: []inbound.ExamNodeAnswerPayload{{
				ID: "ans-1", ItemID: "item-1", Score: ptrFloat(10),
			}},
			IntegrityEvents: []inbound.ExamNodeIntegrityEventPayload{{
				ID: "ev-1", EventType: "focus_lost",
			}},
		}},
	}

	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Decode with strict decoder — same as central's request.go.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded inbound.ExamNodeAttemptBatch
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("strict decode rejected node payload: %v\npayload: %s", err, string(raw))
	}
	if len(decoded.Attempts) != 1 || decoded.Attempts[0].ID != "att-1" {
		t.Errorf("decoded payload mismatch: %+v", decoded)
	}
}

// TestHarvestPayloadNoDeploymentIDInJSON asserts the wire format has no
// deployment_id field — the URL provides the scope.
func TestHarvestPayloadNoDeploymentIDInJSON(t *testing.T) {
	batch := inbound.ExamNodeAttemptBatch{
		Attempts: []inbound.ExamNodeAttemptPayload{{
			ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1",
		}},
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "deployment_id") {
		t.Errorf("payload contains deployment_id — must be URL-scoped only:\n%s", string(raw))
	}
}

// TestHarvestClientTwoDeploymentsStrictContract pins the full contract:
// two grouped pushes through the real HarvestClient, decoded by a strict
// central-style handler that rejects unknown fields.
func TestHarvestClientTwoDeploymentsStrictContract(t *testing.T) {
	var gotPaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)

		// Strict decode — same as central's request.go.
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var batch inbound.ExamNodeAttemptBatch
		if err := decoder.Decode(&batch); err != nil {
			http.Error(w, "strict decode failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		res := inbound.ExamNodeIngestResult{}
		for _, a := range batch.Attempts {
			res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, a.ID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	client := NewHarvestClient(&config.Config{
		CentralBaseURL:   server.URL,
		CentralNodeToken: "tok",
	})

	// Push two batches — one per deployment.
	for _, tc := range []struct {
		depID     string
		attemptID string
	}{
		{"dep-A", "att-A1"},
		{"dep-B", "att-B1"},
	} {
		batch := inbound.ExamNodeAttemptBatch{
			Attempts: []inbound.ExamNodeAttemptPayload{{
				ID: tc.attemptID, ParticipantID: "part-1", StudentID: "stu-1",
				Status: "submitted",
			}},
		}
		result, err := client.Push(context.Background(), tc.depID, batch)
		if err != nil {
			t.Fatalf("push %s: %v", tc.depID, err)
		}
		if len(result.AcceptedAttemptIDs) != 1 || result.AcceptedAttemptIDs[0] != tc.attemptID {
			t.Fatalf("ack %s: %+v", tc.depID, result)
		}
	}

	if len(gotPaths) != 2 {
		t.Fatalf("got %d requests, want 2", len(gotPaths))
	}
	wantA := "/api/v1/exam-nodes/deployments/dep-A/attempts"
	wantB := "/api/v1/exam-nodes/deployments/dep-B/attempts"
	if gotPaths[0] != wantA || gotPaths[1] != wantB {
		t.Errorf("paths = %v, want [%s %s]", gotPaths, wantA, wantB)
	}
}

func ptrFloat(f float64) *float64 { return &f }
