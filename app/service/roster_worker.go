package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

const (
	maxRosterEventsPerPoll = 50
	minRosterBackoff       = 5 * time.Second
	maxRosterBackoff       = 60 * time.Second
)

type RosterWorker struct {
	client    outbound.RosterClient
	processor inbound.RosterUsecase
	mu        sync.Mutex
}

func NewRosterWorker(client outbound.RosterClient, processor inbound.RosterUsecase) *RosterWorker {
	return &RosterWorker{client: client, processor: processor}
}

func (w *RosterWorker) PollOnce(ctx context.Context) error {
	if w == nil || w.client == nil || w.processor == nil {
		return fmt.Errorf("roster worker is not configured")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	events, err := w.client.PullPending(ctx)
	if err != nil {
		return err
	}
	if len(events) > maxRosterEventsPerPoll {
		events = events[:maxRosterEventsPerPoll]
	}
	groups := make(map[string][]inbound.RosterEvent)
	for _, event := range events {
		groups[event.DeploymentID] = append(groups[event.DeploymentID], event)
	}
	deployments := make([]string, 0, len(groups))
	for deploymentID := range groups {
		deployments = append(deployments, deploymentID)
	}
	sort.Strings(deployments)
	processed := make(map[string]bool, len(events))
	var firstErr error
	for _, deploymentID := range deployments {
		group := groups[deploymentID]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Revision != group[j].Revision {
				return group[i].Revision < group[j].Revision
			}
			return group[i].EventID < group[j].EventID
		})
		if err := w.processDeployment(ctx, deploymentID, group, processed); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("process roster deployment %s: %w", deploymentID, err)
		}
	}
	return firstErr
}

func (w *RosterWorker) processDeployment(ctx context.Context, deploymentID string, events []inbound.RosterEvent, processed map[string]bool) error {
	var firstErr error
	for _, event := range events {
		if processed[event.EventID] {
			continue
		}
		outcome, err := w.processor.Apply(ctx, event)
		if errors.Is(err, node_error.ErrRosterRevisionGap) {
			if replayErr := w.replayDeployment(ctx, deploymentID, processed); replayErr != nil {
				return replayErr
			}
			processed[event.EventID] = true
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("apply roster event %s: %w", event.EventID, err)
			}
			continue
		}
		if outcome == nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("apply roster event %s returned no outcome", event.EventID)
			}
			continue
		}
		if shouldAcknowledgeRosterEvent(event) {
			if ackErr := w.client.Acknowledge(ctx, *outcome); ackErr != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("acknowledge roster event %s: %w", event.EventID, ackErr)
				}
			}
		}
		processed[event.EventID] = true
	}
	return firstErr
}

func (w *RosterWorker) replayDeployment(ctx context.Context, deploymentID string, processed map[string]bool) error {
	var afterRevision int64
	for {
		events, err := w.client.Replay(ctx, deploymentID, afterRevision)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		sort.SliceStable(events, func(i, j int) bool {
			if events[i].Revision != events[j].Revision {
				return events[i].Revision < events[j].Revision
			}
			return events[i].EventID < events[j].EventID
		})
		lastRevision := afterRevision
		for _, event := range events {
			if !processed[event.EventID] {
				outcome, err := w.processor.Apply(ctx, event)
				if err != nil {
					return fmt.Errorf("apply replay roster event %s: %w", event.EventID, err)
				}
				if outcome == nil {
					return fmt.Errorf("apply replay roster event %s returned no outcome", event.EventID)
				}
				if shouldAcknowledgeRosterEvent(event) {
					if err := w.client.Acknowledge(ctx, *outcome); err != nil {
						return fmt.Errorf("acknowledge replay roster event %s: %w", event.EventID, err)
					}
				}
				processed[event.EventID] = true
			}
			if event.Revision > lastRevision {
				lastRevision = event.Revision
			}
		}
		if lastRevision == afterRevision || len(events) < maxRosterEventsPerPoll {
			return nil
		}
		afterRevision = lastRevision
	}
}

func shouldAcknowledgeRosterEvent(event inbound.RosterEvent) bool {
	return event.Status == string(entity.RosterEventPending) || event.Status == string(entity.RosterEventApplied)
}

func (w *RosterWorker) Start(ctx context.Context, interval time.Duration) {
	if w == nil || interval <= 0 {
		return
	}
	if err := w.client.AnnounceCapabilities(ctx); err != nil {
		// A central running the pre-roster binary is a supported rollout state;
		// it must not prevent students from using the node's local sitting flow.
		slog.WarnContext(ctx, "roster capability handshake unavailable", "error", err)
	}
	backoff := minRosterBackoff
	for {
		err := w.PollOnce(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "roster worker poll failed", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		delay := interval
		if err != nil {
			delay = rosterBackoffWithJitter(backoff)
			backoff *= 2
			if backoff > maxRosterBackoff {
				backoff = maxRosterBackoff
			}
		} else {
			backoff = minRosterBackoff
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func rosterBackoffWithJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return minRosterBackoff
	}
	factor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(base) * factor)
}
