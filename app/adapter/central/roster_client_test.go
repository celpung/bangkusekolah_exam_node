package central

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

func TestRosterClientUsesAuthenticatedPullReplayAndAckContracts(t *testing.T) {
	var paths []string
	var ack struct {
		DeploymentID string `json:"deployment_id"`
		Revision     int64  `json:"revision"`
		Status       string `json:"status"`
		Code         string `json:"code"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer node-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/ack") {
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&ack); err != nil {
				t.Fatalf("decode ack: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/exam-nodes/capabilities" {
			_, _ = w.Write([]byte(`{"success":true,"data":{"roster_protocol_version":1,"roster_supported":true}}`))
			return
		}
		payload := inbound.RosterEvent{EventID: "event-1", DeploymentID: "deployment-1", ExamID: "exam-1", ParticipantID: "participant-1", StudentID: "student-1", StudentName: "Student", AccessCode: "ABCDEF-GHIJKM", Revision: 1, Deadline: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC(), Status: string(entity.RosterEventPending)}
		_, _ = w.Write([]byte(`{"success":true,"data":[` + mustJSON(payload) + `]}`))
	}))
	defer server.Close()

	client := newRosterClient(server.URL, "node-token", server.Client())
	if _, err := client.PullPending(context.Background()); err != nil {
		t.Fatalf("PullPending: %v", err)
	}
	if _, err := client.Replay(context.Background(), "deployment/1", 7); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if err := client.Acknowledge(context.Background(), inbound.RosterOutcome{EventID: "event-1", DeploymentID: "deployment-1", Revision: 1, Status: string(entity.RosterEventApplied)}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if err := client.AnnounceCapabilities(context.Background()); err != nil {
		t.Fatalf("AnnounceCapabilities: %v", err)
	}
	if ack.DeploymentID != "deployment-1" || ack.Revision != 1 || ack.Status != string(entity.RosterEventApplied) || ack.Code != "" {
		t.Fatalf("ack = %+v", ack)
	}
	if len(paths) != 4 {
		t.Fatalf("requests = %v", paths)
	}
	if paths[0] != "GET /api/v1/exam-nodes/roster-events?limit=50" {
		t.Fatalf("pull path = %q", paths[0])
	}
	replayURL, err := url.Parse(strings.TrimPrefix(paths[1], "GET "))
	if err != nil || replayURL.Query().Get("after_revision") != "7" || replayURL.Query().Get("limit") != "50" || replayURL.EscapedPath() != "/api/v1/exam-nodes/deployments/deployment%2F1/roster-events" {
		t.Fatalf("replay path = %q", paths[1])
	}
}

func TestRosterClientRejectsHTTPAndMalformedResponses(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
	}{
		{name: "http error", body: `{"success":false}`, code: http.StatusBadGateway},
		{name: "malformed", body: `{`, code: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := newRosterClient(server.URL, "node-token", server.Client())
			if _, err := client.PullPending(context.Background()); err == nil {
				t.Fatal("PullPending should fail")
			}
		})
	}
}

func TestRosterClientIncludesCentralErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid roster acknowledgement","code":"roster_ack_conflict"}`))
	}))
	defer server.Close()

	client := newRosterClient(server.URL, "node-token", server.Client())
	_, err := client.PullPending(context.Background())
	if err == nil || !strings.Contains(err.Error(), "roster_ack_conflict") || !strings.Contains(err.Error(), "invalid roster acknowledgement") {
		t.Fatalf("PullPending error = %v, want central error details", err)
	}
}

func TestRosterClientRequiresDeploymentIDForReplay(t *testing.T) {
	client := newRosterClient("http://central.test", "token", nil)
	if _, err := client.Replay(context.Background(), "", 0); !errors.Is(err, errRosterClientInvalidRequest) {
		t.Fatalf("Replay empty deployment error = %v", err)
	}
}

func mustJSON(value interface{}) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
