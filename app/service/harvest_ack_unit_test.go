package service

import (
	"context"
	"strings"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// ackTestRepo is a minimal repo for ack-protocol unit tests.
type ackTestRepo struct {
	fakeHarvestRepo
}

func newAckFixture(t *testing.T) (*fakeHarvestRepo, *multiDeploymentClient, *HarvestService) {
	t.Helper()
	repo := &fakeHarvestRepo{
		exams:    map[string]*entity.Exam{"exam-x": {ID: "exam-x", DeploymentID: "dep-x"}},
		attempts: map[string]*entity.Attempt{"att-1": {ID: "att-1", ExamID: "exam-x", Status: entity.AttemptSubmitted}},
	}
	client := &multiDeploymentClient{ack: func(string, inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult {
		return inbound.ExamNodeIngestResult{}
	}}
	return repo, client, NewHarvestService(repo, client)
}

func scriptedAck(res inbound.ExamNodeIngestResult) func(string, inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult {
	return func(string, inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult { return res }
}

// Case 1+2: same accepted ID twice -> deduplicated; unknown ID ignored.
func TestDrainOnceDedupesAndIgnoresUnknownAcks(t *testing.T) {
	repo, _, svc := newAckFixture(t)
	client := &multiDeploymentClient{ack: scriptedAck(inbound.ExamNodeIngestResult{
		AcceptedAttemptIDs: []string{"att-1", "att-1", "ghost-id"},
	})}
	if err := svc.rebuildWith(t, repo, client); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if repo.attempts["att-1"].HarvestedAt == nil {
		t.Fatal("att-1 should be marked exactly once")
	}
}

// Case 3: ID in both accepted and failures -> treated as failure, unharvested.
func TestDrainOnceContradictoryAckStaysUnharvested(t *testing.T) {
	repo, _, svc := newAckFixture(t)
	client := &multiDeploymentClient{ack: scriptedAck(inbound.ExamNodeIngestResult{
		AcceptedAttemptIDs: []string{"att-1"},
		Failures:           map[string]string{"att-1": "contradictory"},
	})}
	err := svc.rebuildWith(t, repo, client)
	if err != nil {
		t.Fatalf("contradictory ack must not abort drain: %v", err)
	}
	if len(repo.pushLog) != 1 || !strings.Contains(repo.pushLog[0], "contradictory") {
		t.Fatalf("rejection not logged to harvest_log: %v", repo.pushLog)
	}
}

// Case 4: nil result with nil error -> error, never a panic.
func TestDrainOnceNilResultIsError(t *testing.T) {
	repo, _, _ := newAckFixture(t)
	svc := NewHarvestService(repo, nilResultPusher{})
	if _, err := svc.DrainOnce(context.Background()); err == nil {
		t.Fatal("nil acknowledgement must surface as an error")
	}
}

type nilResultPusher struct{}

func (nilResultPusher) Push(context.Context, string, inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	return nil, nil
}

// rebuildWith runs a drain through the given client against the fixture repo.
func (svc *HarvestService) rebuildWith(t *testing.T, repo *fakeHarvestRepo, client *multiDeploymentClient) error {
	t.Helper()
	svc2 := NewHarvestService(repo, client)
	_, err := svc2.DrainOnce(context.Background())
	return err
}
