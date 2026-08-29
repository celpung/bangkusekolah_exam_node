package central

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestBundleClient_PullBundle_DeploymentIDMustMatch verifies that the client
// rejects a bundle whose deployment_id does not match the requested deployment.
func TestBundleClient_PullBundle_DeploymentIDMustMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "ok",
			"data": inbound.ExamNodeBundle{
				// Return a bundle for a DIFFERENT deployment than requested.
				DeploymentID:  "wrong-deployment-id",
				BundleVersion: 1,
				Exam:          inbound.ExamNodeBundleExam{ID: "exam-1"},
			},
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "node-token", server.Client())
	_, err := client.PullBundle(t.Context(), "expected-deployment-id")
	if err == nil {
		t.Fatal("PullBundle: expected error when deployment_id mismatch, got nil")
	}
}

// TestBundleClient_PullBundle_ChecksumVersionFields verifies that the bundle
// response struct has bundle_checksum and bundle_version fields populated.
func TestBundleClient_PullBundle_ChecksumVersionFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "ok",
			"data": inbound.ExamNodeBundle{
				DeploymentID:  "dep-1",
				BundleVersion: 42,
				Checksum:      "sha256:abc123",
				Exam:          inbound.ExamNodeBundleExam{ID: "exam-1"},
				Sections:      []inbound.ExamNodeBundleSection{},
				Items:         []inbound.ExamNodeBundleItem{},
				Participants:  []inbound.ExamNodeBundleParticipant{},
			},
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "node-token", server.Client())
	bundle, err := client.PullBundle(t.Context(), "dep-1")
	if err != nil {
		t.Fatalf("PullBundle: unexpected error: %v", err)
	}
	if bundle.BundleVersion != 42 {
		t.Errorf("bundle.BundleVersion = %d, want 42", bundle.BundleVersion)
	}
	if bundle.Checksum != "sha256:abc123" {
		t.Errorf("bundle.Checksum = %q, want %q", bundle.Checksum, "sha256:abc123")
	}
}

// TestBundleClient_ListDeployments_RejectsEmptyData verifies that the client
// returns an error when the central response envelope contains null data
// (indicating no data payload).
func TestBundleClient_ListDeployments_RejectsEmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "ok",
			"data":    nil, // null data in envelope
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "node-token", server.Client())
	_, err := client.ListDeployments(t.Context())
	if err == nil {
		t.Fatal("ListDeployments: expected error for null data, got nil")
	}
}

// TestBundleClient_ListDeployments_DeterministicOrdering verifies that
// deployments are sorted deterministically by ExamID ASC, then ID ASC
// regardless of the order returned by the server.
func TestBundleClient_ListDeployments_DeterministicOrdering(t *testing.T) {
	// Server returns deployments in REVERSE expected order.
	inputDeployments := []Deployment{
		{ID: "dep-z", ExamID: "exam-z", ExamNodeID: "node-1", Status: "deployed"},
		{ID: "dep-a", ExamID: "exam-a", ExamNodeID: "node-1", Status: "deployed"},
		{ID: "dep-y", ExamID: "exam-y", ExamNodeID: "node-1", Status: "deployed"},
		{ID: "dep-b", ExamID: "exam-b", ExamNodeID: "node-1", Status: "deployed"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "ok",
			"data":    inputDeployments,
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "node-token", server.Client())

	// Run twice to confirm deterministic output independent of input order.
	var first, second []Deployment
	var err error

	first, err = client.ListDeployments(t.Context())
	if err != nil {
		t.Fatalf("ListDeployments (first call): %v", err)
	}

	second, err = client.ListDeployments(t.Context())
	if err != nil {
		t.Fatalf("ListDeployments (second call): %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("ListDeployments returned inconsistent lengths: %d vs %d", len(first), len(second))
	}

	for i := range first {
		if first[i] != second[i] {
			t.Errorf("ListDeployments returned inconsistent order at index %d: %+v vs %+v",
				i, first[i], second[i])
		}
	}

	// Verify sorted by ExamID ASC, then ID ASC.
	for i := 1; i < len(first); i++ {
		prev, curr := first[i-1], first[i]
		if curr.ExamID < prev.ExamID {
			t.Errorf("ListDeployments: ExamID not sorted ascending at index %d: %q before %q",
				i, curr.ExamID, prev.ExamID)
		}
		if curr.ExamID == prev.ExamID && curr.ID < prev.ID {
			t.Errorf("ListDeployments: ID not sorted ascending for ExamID %q at index %d: %q before %q",
				curr.ExamID, i, curr.ID, prev.ID)
		}
	}
}

// TestBundleClient_PullBundle_RejectsHttpError verifies that HTTP error
// status codes (401, 403, 500) result in a returned error.
func TestBundleClient_PullBundle_RejectsHttpError(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
	}{
		{name: "401 Unauthorized", statusCode: http.StatusUnauthorized},
		{name: "403 Forbidden", statusCode: http.StatusForbidden},
		{name: "500 Internal Server Error", statusCode: http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "error",
				})
			}))
			defer server.Close()

			client := newBundleClient(server.URL, "node-token", server.Client())
			_, err := client.PullBundle(t.Context(), "dep-1")
			if err == nil {
				t.Errorf("PullBundle: expected error for %s, got nil", tc.name)
			}
		})
	}
}
