package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type testPusher struct {
	delegate harvestPusherFunc
}

type harvestPusherFunc func(ctx context.Context, deploymentID string, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error)

func (t harvestPusherFunc) Push(ctx context.Context, deploymentID string, b inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	return t(ctx, deploymentID, b)
}

func (t testPusher) Push(ctx context.Context, deploymentID string, b inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	return t.delegate(ctx, deploymentID, b)
}

func TestDrainOncePushesFinishedAttemptsAndMarksOnAck(t *testing.T) {
	score := 10.0
	submitted := time.Now()
	repo := &fakeHarvestRepo{
		exams: map[string]*entity.Exam{"exam-x": {ID: "exam-x", DeploymentID: "dep-1"}},
		attempts: map[string]*entity.Attempt{
			"att-1": {ID: "att-1", ExamID: "exam-x", ParticipantID: "part-1", StudentID: "stu-1", Status: entity.AttemptSubmitted,
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
		exams: map[string]*entity.Exam{"exam-x": {ID: "exam-x", DeploymentID: "dep-1"}},
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
		exams: map[string]*entity.Exam{"exam-x": {ID: "exam-x", DeploymentID: "dep-1"}},
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
		exams:    map[string]*entity.Exam{"exam-x": {ID: "exam-x", DeploymentID: "dep-1"}},
		attempts: map[string]*entity.Attempt{"att-1": {ID: "att-1", ExamID: "exam-x", Status: entity.AttemptSubmitted}},
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
		exams:    map[string]*entity.Exam{"exam-x": {ID: "exam-x", DeploymentID: "dep-1"}},
		attempts: map[string]*entity.Attempt{"att-1": {ID: "att-1", ExamID: "exam-x", Status: entity.AttemptSubmitted}},
	}
	client := testPusher{delegate: func(_ context.Context, _ string, _ inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
		return nil, context.DeadlineExceeded
	}}
	svc := NewHarvestService(repo, client)
	if _, err := svc.DrainOnce(context.Background()); err == nil {
		t.Fatal("expected push failure to surface")
	}
	if len(repo.pushLog) != 1 || !strings.Contains(repo.pushLog[0], "att-1") || !strings.Contains(repo.pushLog[0], "dep-1") {
		t.Fatalf("harvest_log entry missing: %v", repo.pushLog)
	}
}
