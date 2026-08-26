package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	nodecentral "github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeHarvestRepo struct {
	outbound_repository.NodeRepository
	attempts map[string]*entity.Attempt
	answers  map[string][]entity.Answer
	events   map[string][]entity.IntegrityEvent
	pushLog  []string // failure log entries
}

func (f *fakeHarvestRepo) ListUnpushedAttempts(_ context.Context) ([]entity.Attempt, error) {
	var out []entity.Attempt
	for _, a := range f.attempts {
		if a.HarvestedAt == nil && (a.Status == entity.AttemptSubmitted || a.Status == entity.AttemptAutoSubmitted) {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakeHarvestRepo) ListAnswersByAttempt(_ context.Context, id string) ([]entity.Answer, error) {
	return f.answers[id], nil
}

func (f *fakeHarvestRepo) ListIntegrityEventsByAttempt(_ context.Context, id string) ([]entity.IntegrityEvent, error) {
	return f.events[id], nil
}

func (f *fakeHarvestRepo) MarkAttemptsHarvested(_ context.Context, ids []string, at time.Time) error {
	for _, id := range ids {
		if a, ok := f.attempts[id]; ok {
			a.HarvestedAt = &at
		}
	}
	return nil
}

func (f *fakeHarvestRepo) LogHarvestFailure(_ context.Context, attemptID, errMsg string) error {
	f.pushLog = append(f.pushLog, attemptID+": "+errMsg)
	return nil
}

// testPusher wraps a real HarvestClient pointed at the httptest server via
// CentralBaseURL — no direct network in tests beyond loopback.
type testPusher struct {
	delegate harvestPusherFunc
}

type harvestPusherFunc func(ctx context.Context, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error)

func (t harvestPusherFunc) Push(ctx context.Context, b inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	return t(ctx, b)
}

// newTestHarvestClient constructs the real central.HarvestClient pointed at
// the httptest server URL — loopback only, no external network.
func newTestHarvestClient(baseURL, token, deploymentID string) *nodecentral.HarvestClient {
	return nodecentral.NewHarvestClient(&config.Config{
		CentralBaseURL:   baseURL,
		CentralNodeToken: token,
		DeploymentID:     deploymentID,
	})
}

func (t testPusher) Push(ctx context.Context, b inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	return t.delegate(ctx, b)
}

func centralMock(t *testing.T, wantToken string, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantToken {
			t.Errorf("Authorization = %q, want Bearer %q", got, wantToken)
		}
		handler(w, r)
	}))
}

func TestDrainOncePushesFinishedAttemptsAndMarksOnAck(t *testing.T) {
	score := 10.0
	submitted := time.Now()
	repo := &fakeHarvestRepo{
		attempts: map[string]*entity.Attempt{
			"att-1": {ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1", Status: entity.AttemptSubmitted,
				AttemptNo: 1, StartedAt: submitted.Add(-90 * time.Minute), DueAt: submitted, SubmittedAt: ptrTime(submitted)},
		},
		answers: map[string][]entity.Answer{"att-1": {{ID: "ans-1", AttemptID: "att-1", ItemID: "item-1", Score: &score, GradingStatus: entity.GradingAutoGraded}}},
		events:  map[string][]entity.IntegrityEvent{"att-1": {{ID: "ev-1", AttemptID: "att-1", EventType: "focus_lost"}}},
	}
	server := centralMock(t, "node-token", func(w http.ResponseWriter, r *http.Request) {
		var batch inbound.ExamNodeAttemptBatch
		_ = json.NewDecoder(r.Body).Decode(&batch)
		if len(batch.Attempts) != 1 || batch.Attempts[0].ID != "att-1" ||
			len(batch.Attempts[0].Answers) != 1 || len(batch.Attempts[0].IntegrityEvents) != 1 {
			t.Errorf("central got batch = %+v", batch)
		}
		_ = json.NewEncoder(w).Encode(inbound.ExamNodeIngestResult{AcceptedAttemptIDs: []string{"att-1"}, Failures: map[string]string{}})
	})
	defer server.Close()

	client := newTestHarvestClient(server.URL, "node-token", "dep-1")
	svc := NewHarvestService(repo, client)
	n, err := svc.DrainOnce(context.Background())
	if err != nil {
		t.Fatalf("DrainOnce: %v", err)
	}
	if n != 1 || repo.attempts["att-1"].HarvestedAt == nil {
		t.Fatalf("drain: n=%d harvested_at=%v", n, repo.attempts["att-1"].HarvestedAt)
	}
}

func TestDrainOnceIsIdempotent(t *testing.T) {
	repo := &fakeHarvestRepo{
		attempts: map[string]*entity.Attempt{
			"att-1": {ID: "att-1", Status: entity.AttemptSubmitted, HarvestedAt: ptrTime(time.Now())},
		},
	}
	calls := 0
	server := centralMock(t, "node-token", func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(inbound.ExamNodeIngestResult{})
	})
	defer server.Close()

	client := newTestHarvestClient(server.URL, "node-token", "dep-1")
	svc := NewHarvestService(repo, client)
	n, err := svc.DrainOnce(context.Background())
	if err != nil {
		t.Fatalf("DrainOnce: %v", err)
	}
	if n != 0 || calls != 0 {
		t.Fatalf("idempotent drain: n=%d calls=%d, want 0/0", n, calls)
	}
}

func TestDrainOncePushesNothingWhenNoFinishedAttempts(t *testing.T) {
	repo := &fakeHarvestRepo{
		attempts: map[string]*entity.Attempt{
			"att-inprog": {ID: "att-inprog", Status: entity.AttemptInProgress},
		},
	}
	server := centralMock(t, "node-token", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("central should not be called when no finished attempts")
	})
	defer server.Close()

	client := newTestHarvestClient(server.URL, "node-token", "dep-1")
	svc := NewHarvestService(repo, client)
	n, err := svc.DrainOnce(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("empty drain: n=%d err=%v", n, err)
	}
}

func TestDrainOnceSendsIntegrityEventsAlongsideAttempts(t *testing.T) {
	repo := &fakeHarvestRepo{
		attempts: map[string]*entity.Attempt{"att-1": {ID: "att-1", Status: entity.AttemptSubmitted}},
		events:   map[string][]entity.IntegrityEvent{"att-1": {{ID: "ev-1", EventType: "tab_switch"}, {ID: "ev-2", EventType: "focus_lost"}}},
	}
	server := centralMock(t, "node-token", func(w http.ResponseWriter, r *http.Request) {
		var batch inbound.ExamNodeAttemptBatch
		_ = json.NewDecoder(r.Body).Decode(&batch)
		if len(batch.Attempts[0].IntegrityEvents) != 2 {
			t.Errorf("events = %d, want 2", len(batch.Attempts[0].IntegrityEvents))
		}
		_ = json.NewEncoder(w).Encode(inbound.ExamNodeIngestResult{AcceptedAttemptIDs: []string{"att-1"}})
	})
	defer server.Close()

	client := newTestHarvestClient(server.URL, "node-token", "dep-1")
	svc := NewHarvestService(repo, client)
	if _, err := svc.DrainOnce(context.Background()); err != nil {
		t.Fatalf("DrainOnce: %v", err)
	}
}

func TestDrainOnceLogsFailureAndReturnsError(t *testing.T) {
	repo := &fakeHarvestRepo{
		attempts: map[string]*entity.Attempt{"att-1": {ID: "att-1", Status: entity.AttemptSubmitted}},
	}
	client := testPusher{delegate: func(context.Context, inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
		return nil, context.DeadlineExceeded
	}}
	svc := NewHarvestService(repo, client)
	if _, err := svc.DrainOnce(context.Background()); err == nil {
		t.Fatal("expected push failure to surface")
	}
	if len(repo.pushLog) != 1 || !strings.Contains(repo.pushLog[0], "att-1") {
		t.Fatalf("harvest_log entry missing: %v", repo.pushLog)
	}
}
