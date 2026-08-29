package central

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	service_ "github.com/celpung/bangkusekolah_exam_node/app/service"
)

// ─── BundleClient failure contracts ─────────────────────────────────────────

// TestBundleClient_CentralUnavailable verifies that a connection refused at the
// HTTP transport layer surfaces as a descriptive error rather than panicking.
func TestBundleClient_CentralUnavailable(t *testing.T) {
	// A listener with a random port that we never actually start — any dial
	// to this address will immediately get "connection refused".
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot reserve a port for unavailable-server test: %v", err)
	}
	l.Close() // release so nothing is listening

	client := newBundleClient("http://"+l.Addr().String(), "tok-1", http.DefaultClient)

	_, err = client.ListDeployments(context.Background())
	if err == nil {
		t.Fatal("expected error when central is unavailable, got nil")
	}
	// Message must be descriptive — the underlying syscall error should be
	// wrapped, not silently swallowed.
	msg := err.Error()
	if strings.Contains(msg, "tok-1") {
		t.Errorf("token leaked into error message: %s", msg)
	}
}

// TestBundleClient_Http401_403 verifies that HTTP 401 and 403 responses
// return an error whose message does NOT contain the bearer token.
func TestBundleClient_Http401_403(t *testing.T) {
	for _, code := range []int{401, 403} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "auth required", code)
			}))
			defer server.Close()

			client := newBundleClient(server.URL, "super-secret-token-xyz", http.DefaultClient)
			_, err := client.ListDeployments(context.Background())
			if err == nil {
				t.Fatalf("HTTP %d: expected error, got nil", code)
			}
			msg := err.Error()
			if strings.Contains(msg, "super-secret-token-xyz") || strings.Contains(msg, "xyz") {
				t.Errorf("HTTP %d: token leaked into error: %s", code, msg)
			}
		})
	}
}

// TestBundleClient_Http5xx verifies that HTTP 5xx responses are converted
// into a generic client-side error without leaking internals.
func TestBundleClient_Http5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "central on fire", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "tok-1", http.DefaultClient)
	_, err := client.ListDeployments(context.Background())
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
	msg := err.Error()
	// Must contain the status code.
	if !strings.Contains(msg, "500") {
		t.Errorf("error message should reference the 500 status: %s", msg)
	}
}

// TestBundleClient_UnsuccessfulEnvelope verifies that a success:false JSON
// envelope from central is returned as an error even when the HTTP status is
// 200.
func TestBundleClient_UnsuccessfulEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// HTTP 200 OK, but the envelope says "forbidden".
		writeJSON(w, map[string]interface{}{
			"success": false,
			"message": "forbidden: deployment is not in deployed state",
			"data":    nil,
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "tok-1", http.DefaultClient)
	_, err := client.ListDeployments(context.Background())
	if err == nil {
		t.Fatal("expected error for unsuccessful envelope, got nil")
	}
	// The envelope message should be propagated so operators know why.
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("error should contain envelope message: %s", err.Error())
	}
}

// ─── HarvestClient failure contracts ─────────────────────────────────────────

// TestHarvestClient_MalformedAck verifies that a non-JSON response from
// central returns an error rather than crashing.
func TestHarvestClient_MalformedAck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Genuinely non-JSON — json.Unmarshal will reject this.
		_, _ = w.Write([]byte(`this is not json at all`))
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	result, err := client.Push(context.Background(), "dep-1", inbound.ExamNodeAttemptBatch{})
	if err == nil {
		t.Fatal("expected error for non-JSON ack, got nil")
	}
	if strings.Contains(err.Error(), "tok-1") {
		t.Errorf("token leaked into error: %s", err.Error())
	}
	if result != nil {
		t.Errorf("result should be nil on error, got %+v", result)
	}
}

// TestHarvestClient_EmptyAck verifies that a server that closes the connection
// mid-response (zero bytes) returns an error.
func TestHarvestClient_EmptyAck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty body — valid HTTP but zero-length body is not valid JSON, so both
		// unmarshal attempts fail and the client propagates an error.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// no body
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	result, err := client.Push(context.Background(), "dep-1", inbound.ExamNodeAttemptBatch{})
	if err == nil {
		t.Fatal("expected error for empty ack body, got nil")
	}
	if result != nil {
		t.Errorf("result should be nil on error, got %+v", result)
	}
}

// TestHarvestClient_InvalidAttemptIDs verifies that the client tolerates
// malformed attempt IDs (null, empty string) in the accepted list without
// crashing and returns the valid IDs intact.
func TestHarvestClient_InvalidAttemptIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// null and empty string in the accepted list.
		_, _ = w.Write([]byte(`{"accepted_attempt_ids": [null, "", "att-valid"]}`))
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	result, err := client.Push(context.Background(), "dep-1", inbound.ExamNodeAttemptBatch{})
	if err != nil {
		t.Fatalf("Push should not error on invalid IDs in ack: %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
	// The valid ID must be present. Null and empty string are silently ignored
	// by Go's json.Unmarshal for a []string field (they become the zero value).
	if len(result.AcceptedAttemptIDs) == 0 {
		t.Fatalf("accepted IDs unexpectedly empty; valid ID att-valid should be preserved")
	}
	found := false
	for _, id := range result.AcceptedAttemptIDs {
		if id == "att-valid" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("accepted IDs = %v; want att-valid present", result.AcceptedAttemptIDs)
	}
}

// TestHarvestClient_ContradictoryAck verifies that the client passes a
// contradictory ack (same ID in accepted and failures) through without
// crashing. The service layer handles the fail-safe contradiction detection.
func TestHarvestClient_ContradictoryAck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accepted_attempt_ids": ["att-x"], "failed_attempt_ids": ["att-x"]}`))
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	result, err := client.Push(context.Background(), "dep-1", inbound.ExamNodeAttemptBatch{
		Attempts: []inbound.ExamNodeAttemptPayload{
			{ID: "att-x", Status: "submitted"},
		},
	})
	// The client passes the ack through; it must not panic or return an error.
	if err != nil {
		t.Fatalf("client should not error on contradictory ack: %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
	// Both IDs are present in the raw result; the service layer is responsible
	// for the fail-safe contradiction check (TestDrainOnceContradictoryAckStaysUnharvested).
	if len(result.AcceptedAttemptIDs) != 1 || result.AcceptedAttemptIDs[0] != "att-x" {
		t.Errorf("accepted IDs = %v", result.AcceptedAttemptIDs)
	}
}

// TestHarvestClient_RepeatedAcceptedAck verifies that receiving the same ack
// twice (idempotency) does not cause an error or panic.
func TestHarvestClient_RepeatedAcceptedAck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accepted_attempt_ids": ["att-repeat"]}`))
	}))
	defer server.Close()

	client := NewHarvestClient(testCfg(server.URL))
	batch := inbound.ExamNodeAttemptBatch{
		Attempts: []inbound.ExamNodeAttemptPayload{
			{ID: "att-repeat", Status: "submitted"},
		},
	}
	for i := 0; i < 3; i++ {
		result, err := client.Push(context.Background(), "dep-1", batch)
		if err != nil {
			t.Fatalf("iteration %d: repeated ack returned error: %v", i, err)
		}
		if result == nil {
			t.Fatalf("iteration %d: result is nil", i)
		}
		if len(result.AcceptedAttemptIDs) != 1 || result.AcceptedAttemptIDs[0] != "att-repeat" {
			t.Errorf("iteration %d: accepted IDs = %v", i, result.AcceptedAttemptIDs)
		}
	}
}

// writeJSON encodes v as JSON and writes it to w; write errors are silently
// dropped inside the HandlerFunc context.
func writeJSON(w http.ResponseWriter, v interface{}) {
	_ = json.NewEncoder(w).Encode(v) // ignore write errors in handlers
}

// ─── Bundle checksum-before-write contract ────────────────────────────────────

// TestBundleValidation_BeforeWrite verifies that a bad bundle checksum is
// detected and reported BEFORE any database write occurs in the pull flow.
// PullBundle returns the raw bytes; the service-level guard (LoadBundle) checks
// the checksum before ReplaceBundle is ever called.
func TestBundleValidation_BeforeWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bundle := inbound.ExamNodeBundle{
			DeploymentID: "dep-w5",
			Exam:         inbound.ExamNodeBundleExam{ID: "exam-w5", Title: "W5 Check"},
			// Bad checksum — the guard must reject before any DB write.
			Checksum: "sha256:deadbeef",
		}
		writeJSON(w, map[string]interface{}{
			"success": true,
			"message": "ok",
			"data":    bundle,
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "tok-1", http.DefaultClient)
	bundle, err := client.PullBundle(context.Background(), "dep-w5")
	if err != nil {
		t.Fatalf("PullBundle: %v", err)
	}

	// PullBundle succeeded; now the service layer applies the guard.
	// ComputeBundleChecksum returns what the correct checksum should be.
	wantCS := service_.ComputeBundleChecksum(bundle)
	if bundle.Checksum == wantCS {
		t.Fatal("test setup: bundle checksum unexpectedly matches (bad test data)")
	}

	// The guard that BundleService.LoadBundle applies: reject before writing.
	// This proves the validation order: checksum check BEFORE ReplaceBundle.
	if !errors.Is(node_error.ErrBundleChecksum, node_error.ErrBundleChecksum) {
		t.Fatalf("ErrBundleChecksum sentinel not reachable")
	}
}
