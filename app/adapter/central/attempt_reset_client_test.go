package central

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

func TestAttemptResetClientPullsCommandsAndReportsOutcome(t *testing.T) {
	const token = "node-token"
	var reported inbound.AttemptResetOutcome
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/exam-nodes/attempt-reset-commands":
			command := inbound.AttemptResetCommand{RequestID: "preview-1", Operation: inbound.AttemptResetPreviewOperation, Deadline: time.Now().UTC().Add(time.Minute)}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": []inbound.AttemptResetCommand{command}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/outcome"):
			if err := json.NewDecoder(r.Body).Decode(&reported); err != nil {
				t.Fatalf("decode outcome: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]string{"status": "accepted"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newAttemptResetClient(server.URL, token, server.Client())
	commands, err := client.PullAttemptResetCommands(context.Background())
	if err != nil {
		t.Fatalf("PullAttemptResetCommands() error = %v", err)
	}
	if len(commands) != 1 || commands[0].RequestID != "preview-1" {
		t.Fatalf("commands = %+v", commands)
	}
	want := inbound.AttemptResetOutcome{RequestID: "reset-1", Operation: inbound.AttemptResetOperation, Status: inbound.AttemptResetApplied}
	if err := client.ReportAttemptResetOutcome(context.Background(), want); err != nil {
		t.Fatalf("ReportAttemptResetOutcome() error = %v", err)
	}
	if reported.RequestID != want.RequestID || reported.Status != want.Status {
		t.Fatalf("reported = %+v, want %+v", reported, want)
	}
}

func TestAttemptResetClientAnnouncesSupportedProtocol(t *testing.T) {
	const token = "node-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/exam-nodes/capabilities" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode capability body: %v", err)
		}
		if body["attempt_reset_protocol_version"] != float64(attemptResetProtocolVersion) || body["attempt_reset_supported"] != true {
			t.Fatalf("capability body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"attempt_reset_protocol_version": attemptResetProtocolVersion,
				"attempt_reset_supported":        true,
			},
		})
	}))
	defer server.Close()

	client := newAttemptResetClient(server.URL, token, server.Client())
	if err := client.AnnounceAttemptResetCapabilities(context.Background()); err != nil {
		t.Fatalf("AnnounceAttemptResetCapabilities() error = %v", err)
	}
}
