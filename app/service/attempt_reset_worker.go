package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

const maxAttemptResetCommandsPerPoll = 50

type AttemptResetProcessor interface {
	inbound.AttemptResetUsecase
	inbound.AttemptResetPreviewUsecase
}

type AttemptResetWorker struct {
	client    outbound.AttemptResetClient
	processor AttemptResetProcessor
}

func NewAttemptResetWorker(client outbound.AttemptResetClient, processor AttemptResetProcessor) *AttemptResetWorker {
	return &AttemptResetWorker{client: client, processor: processor}
}

func (w *AttemptResetWorker) PollOnce(ctx context.Context) error {
	if w == nil || w.client == nil || w.processor == nil {
		return fmt.Errorf("attempt reset worker is not configured")
	}
	commands, err := w.client.PullAttemptResetCommands(ctx)
	if err != nil {
		return err
	}
	if len(commands) > maxAttemptResetCommandsPerPoll {
		commands = commands[:maxAttemptResetCommandsPerPoll]
	}
	var firstErr error
	for _, command := range commands {
		var outcome *inbound.AttemptResetOutcome
		switch command.Operation {
		case inbound.AttemptResetPreviewOperation:
			outcome, err = w.processor.PreviewReset(ctx, command)
		case inbound.AttemptResetOperation:
			outcome, err = w.processor.ApplyReset(ctx, command)
		default:
			err = fmt.Errorf("unsupported attempt reset operation %q", command.Operation)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("process reset command %s: %w", command.RequestID, err)
			}
			continue
		}
		if outcome == nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("process reset command %s: empty outcome", command.RequestID)
			}
			continue
		}
		if err := w.client.ReportAttemptResetOutcome(ctx, *outcome); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("report reset command %s: %w", command.RequestID, err)
			}
		}
	}
	return firstErr
}

func (w *AttemptResetWorker) Start(ctx context.Context, interval time.Duration) {
	if w == nil || interval <= 0 {
		return
	}
	if capabilityClient, ok := w.client.(outbound.AttemptResetCapabilityClient); ok {
		if !w.waitForCapabilities(ctx, capabilityClient, interval) {
			return
		}
	}
	w.poll(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *AttemptResetWorker) waitForCapabilities(ctx context.Context, client outbound.AttemptResetCapabilityClient, retryInterval time.Duration) bool {
	for {
		if err := client.AnnounceAttemptResetCapabilities(ctx); err == nil {
			return true
		} else {
			slog.WarnContext(ctx, "attempt reset capability handshake failed", "error", err)
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false
		case <-timer.C:
		}
	}
}

func (w *AttemptResetWorker) poll(ctx context.Context) {
	if err := w.PollOnce(ctx); err != nil {
		slog.ErrorContext(ctx, "attempt reset worker poll failed", "error", err)
	}
}
