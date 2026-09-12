package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type rosterWorkerClientStub struct {
	pending       []inbound.RosterEvent
	replay        []inbound.RosterEvent
	outcomes      []inbound.RosterOutcome
	ackErrors     int
	announceError error
}

func (c *rosterWorkerClientStub) PullPending(context.Context) ([]inbound.RosterEvent, error) {
	return append([]inbound.RosterEvent(nil), c.pending...), nil
}

func (c *rosterWorkerClientStub) Replay(context.Context, string, int64) ([]inbound.RosterEvent, error) {
	events := append([]inbound.RosterEvent(nil), c.replay...)
	c.replay = nil
	return events, nil
}

func (c *rosterWorkerClientStub) Acknowledge(_ context.Context, outcome inbound.RosterOutcome) error {
	if c.ackErrors > 0 {
		c.ackErrors--
		return errors.New("ack unavailable")
	}
	c.outcomes = append(c.outcomes, outcome)
	return nil
}

func (c *rosterWorkerClientStub) AnnounceCapabilities(context.Context) error { return c.announceError }

type rosterWorkerProcessorStub struct {
	mu           sync.Mutex
	applyCalls   int
	participants map[string]bool
	gapOnce      bool
}

func (p *rosterWorkerProcessorStub) Apply(_ context.Context, event inbound.RosterEvent) (*inbound.RosterOutcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.applyCalls++
	if p.gapOnce && event.Revision == 2 {
		p.gapOnce = false
		return nil, node_error.ErrRosterRevisionGap
	}
	if p.participants == nil {
		p.participants = map[string]bool{}
	}
	p.participants[event.ParticipantID] = true
	return &inbound.RosterOutcome{EventID: event.EventID, DeploymentID: event.DeploymentID, Revision: event.Revision, Status: string(entity.RosterEventApplied)}, nil
}

func TestRosterWorkerRetriesACKWithoutCreatingParticipantTwice(t *testing.T) {
	client := &rosterWorkerClientStub{pending: []inbound.RosterEvent{workerRosterEvent("event-1", "deployment-1", 1)}, ackErrors: 1}
	processor := &rosterWorkerProcessorStub{}
	worker := NewRosterWorker(client, processor)
	if err := worker.PollOnce(context.Background()); err == nil {
		t.Fatal("first poll should report ACK failure")
	}
	if err := worker.PollOnce(context.Background()); err != nil {
		t.Fatalf("retry poll: %v", err)
	}
	if processor.applyCalls != 2 || len(processor.participants) != 1 || len(client.outcomes) != 1 {
		t.Fatalf("apply calls/participants/acks = %d/%d/%d, want 2/1/1", processor.applyCalls, len(processor.participants), len(client.outcomes))
	}
}

func TestRosterWorkerReplaysRevisionGapAndContinuesOtherDeployments(t *testing.T) {
	client := &rosterWorkerClientStub{
		pending: []inbound.RosterEvent{
			workerRosterEvent("event-a2", "deployment-a", 2),
			workerRosterEvent("event-b1", "deployment-b", 1),
		},
		replay: []inbound.RosterEvent{
			workerRosterEvent("event-a1", "deployment-a", 1),
			workerRosterEvent("event-a2", "deployment-a", 2),
		},
	}
	processor := &rosterWorkerProcessorStub{gapOnce: true}
	worker := NewRosterWorker(client, processor)
	if err := worker.PollOnce(context.Background()); err != nil {
		t.Fatalf("poll should continue after replay: %v", err)
	}
	if len(client.outcomes) != 3 {
		t.Fatalf("ack count = %d, want 3 (a1, a2, b1)", len(client.outcomes))
	}
	if !processor.participants["participant-event-a1"] || !processor.participants["participant-event-a2"] || !processor.participants["participant-event-b1"] {
		t.Fatalf("participants = %+v", processor.participants)
	}
}

func TestRosterWorkerDoesNotACKTerminalHistoryEvents(t *testing.T) {
	terminal := workerRosterEvent("event-a1", "deployment-a", 1)
	terminal.Status = string(entity.RosterEventCancelled)
	client := &rosterWorkerClientStub{
		pending: []inbound.RosterEvent{workerRosterEvent("event-a2", "deployment-a", 2)},
		replay:  []inbound.RosterEvent{terminal, workerRosterEvent("event-a2", "deployment-a", 2)},
	}
	processor := &rosterWorkerProcessorStub{gapOnce: true}
	worker := NewRosterWorker(client, processor)

	if err := worker.PollOnce(context.Background()); err != nil {
		t.Fatalf("poll should recover terminal history and continue: %v", err)
	}
	if len(client.outcomes) != 1 || client.outcomes[0].EventID != "event-a2" {
		t.Fatalf("acknowledged outcomes = %+v, want only event-a2", client.outcomes)
	}
}

func TestRosterWorkerCapabilityFailureDoesNotBlockPolling(t *testing.T) {
	client := &rosterWorkerClientStub{announceError: errors.New("old central")}
	processor := &rosterWorkerProcessorStub{}
	worker := NewRosterWorker(client, processor)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		worker.Start(ctx, time.Millisecond)
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
}

func workerRosterEvent(eventID, deploymentID string, revision int64) inbound.RosterEvent {
	return inbound.RosterEvent{
		EventID: eventID, DeploymentID: deploymentID, ExamID: "exam-" + deploymentID,
		ParticipantID: "participant-" + eventID, StudentID: "student-" + eventID,
		StudentName: "Student " + eventID, AccessCode: "ABCDEF-" + eventID,
		Revision: revision, Deadline: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC(),
		Status: string(entity.RosterEventPending),
	}
}
