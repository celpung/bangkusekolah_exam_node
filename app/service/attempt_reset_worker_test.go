package service

import (
	"context"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type attemptResetWorkerClientStub struct {
	commands []inbound.AttemptResetCommand
	outcomes []inbound.AttemptResetOutcome
}

func (c *attemptResetWorkerClientStub) PullAttemptResetCommands(context.Context) ([]inbound.AttemptResetCommand, error) {
	return c.commands, nil
}

func (c *attemptResetWorkerClientStub) ReportAttemptResetOutcome(_ context.Context, outcome inbound.AttemptResetOutcome) error {
	c.outcomes = append(c.outcomes, outcome)
	return nil
}

type attemptResetWorkerProcessorStub struct{}

func (attemptResetWorkerProcessorStub) PreviewReset(_ context.Context, command inbound.AttemptResetCommand) (*inbound.AttemptResetOutcome, error) {
	return &inbound.AttemptResetOutcome{RequestID: command.RequestID, Operation: command.Operation, Status: inbound.AttemptResetApplied}, nil
}

func (attemptResetWorkerProcessorStub) ApplyReset(_ context.Context, command inbound.AttemptResetCommand) (*inbound.AttemptResetOutcome, error) {
	return &inbound.AttemptResetOutcome{RequestID: command.RequestID, Operation: command.Operation, Status: inbound.AttemptResetApplied}, nil
}

func TestAttemptResetWorkerProcessesPreviewAndResetCommands(t *testing.T) {
	client := &attemptResetWorkerClientStub{commands: []inbound.AttemptResetCommand{
		{RequestID: "preview-1", Operation: inbound.AttemptResetPreviewOperation, Deadline: time.Now().UTC().Add(time.Minute)},
		{RequestID: "reset-1", Operation: inbound.AttemptResetOperation, Deadline: time.Now().UTC().Add(time.Minute)},
	}}
	worker := NewAttemptResetWorker(client, attemptResetWorkerProcessorStub{})
	if err := worker.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce() error = %v", err)
	}
	if len(client.outcomes) != 2 || client.outcomes[0].RequestID != "preview-1" || client.outcomes[1].RequestID != "reset-1" {
		t.Fatalf("outcomes = %+v, want both commands reported", client.outcomes)
	}
}
