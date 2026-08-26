package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// maxBatchAttempts caps one push so a long outage cannot exceed the 10s
// client timeout; the next tick (or force) drains the remainder.
const maxBatchAttempts = 500

// HarvestPusher abstracts the central client so tests inject an httptest
// transport instead of a real network call. deploymentID is per-call: a node
// hosts multiple exams/deployments, and each batch is scoped to exactly one.
type HarvestPusher interface {
	Push(ctx context.Context, deploymentID string, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error)
}

type HarvestService struct {
	repo   outbound_repository.NodeRepository
	client HarvestPusher

	// drainMu serializes drains process-wide: the ticker and the internal
	// force route must not push duplicate batches concurrently.
	drainMu sync.Mutex
}

func NewHarvestService(repo outbound_repository.NodeRepository, client HarvestPusher) *HarvestService {
	return &HarvestService{repo: repo, client: client}
}

// DrainOnce collects finished unharvested attempts, groups them by their
// exam's deployment, pushes one batch per deployment, and marks acked
// attempts. Idempotent: a second drain with no new attempts sends nothing.
func (s *HarvestService) DrainOnce(ctx context.Context) (int, error) {
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	return s.drainLocked(ctx)
}

func (s *HarvestService) drainLocked(ctx context.Context) (int, error) {
	attempts, err := s.repo.ListUnpushedAttempts(ctx)
	if err != nil {
		return 0, err
	}
	if len(attempts) == 0 {
		return 0, nil
	}

	// Resolve each attempt to its exam's deployment ID.
	byDeployment := map[string][]entity.Attempt{}
	for _, att := range attempts {
		depID, err := s.deploymentForAttempt(ctx, att)
		if err != nil {
			return 0, err
		}
		byDeployment[depID] = append(byDeployment[depID], att)
	}

	total := 0
	// Deterministic order for stable runbook output.
	for _, depID := range sortedKeys(byDeployment) {
		group := byDeployment[depID]
		for len(group) > 0 {
			n, err := s.drainDeployment(ctx, depID, group)
			if err != nil {
				return total, err
			}
			total += n
			if n < len(group) {
				break // nothing more was accepted from this group
			}
			group = group[n:]
		}
	}
	return total, nil
}

// drainDeployment pushes up to maxBatchAttempts attempts of one deployment
// and returns how many were accepted.
func (s *HarvestService) drainDeployment(ctx context.Context, deploymentID string, attempts []entity.Attempt) (int, error) {
	if len(attempts) > maxBatchAttempts {
		attempts = attempts[:maxBatchAttempts]
	}

	batch := inbound.ExamNodeAttemptBatch{Attempts: make([]inbound.ExamNodeAttemptPayload, 0, len(attempts))}
	sent := make(map[string]struct{}, len(attempts))
	for _, att := range attempts {
		answers, err := s.repo.ListAnswersByAttempt(ctx, att.ID)
		if err != nil {
			return 0, err
		}
		events, err := s.repo.ListIntegrityEventsByAttempt(ctx, att.ID)
		if err != nil {
			return 0, err
		}
		payload := inbound.ExamNodeAttemptPayload{
			ID: att.ID, DeploymentID: deploymentID, ParticipantID: att.ParticipantID, StudentID: att.StudentID,
			AttemptNo: att.AttemptNo, Status: entity.AttemptStatus(att.Status),
			StartedAt: att.StartedAt, DueAt: att.DueAt,
			SubmittedAt: att.SubmittedAt, AutoSubmittedAt: att.AutoSubmittedAt,
		}
		for _, ans := range answers {
			payload.Answers = append(payload.Answers, inbound.ExamNodeAnswerPayload{
				ID: ans.ID, ItemID: ans.ItemID, AnswerJSON: ans.AnswerJSON,
				AnswerText: ans.AnswerText, Score: ans.Score,
				GradingStatus: ans.GradingStatus, LastSavedAt: ans.LastSavedAt,
			})
		}
		for _, ev := range events {
			payload.IntegrityEvents = append(payload.IntegrityEvents, inbound.ExamNodeIntegrityEventPayload{
				ID: ev.ID, EventType: ev.EventType, Description: ev.Description,
				MetadataJSON: ev.MetadataJSON, CreatedAt: ev.CreatedAt,
			})
		}
		batch.Attempts = append(batch.Attempts, payload)
		sent[att.ID] = struct{}{}
	}

	result, err := s.client.Push(ctx, deploymentID, batch)
	if err != nil {
		slog.ErrorContext(ctx, "harvest push failed", "deployment", deploymentID, "error", err, "batch_size", len(batch.Attempts))
		if logErr := s.repo.LogHarvestFailure(ctx, batch.Attempts[0].ID, deploymentID, len(batch.Attempts), err.Error()); logErr != nil {
			slog.ErrorContext(ctx, "harvest_log write failed", "error", logErr)
		}
		return 0, err
	}

	now := time.Now().UTC()
	// BLOCKER-2 guard: only mark IDs that were actually sent in this request.
	var accepted []string
	for _, id := range result.AcceptedAttemptIDs {
		if _, wasSent := sent[id]; wasSent {
			accepted = append(accepted, id)
			delete(sent, id)
		} else {
			slog.WarnContext(ctx, "central acknowledged an unsent attempt — protocol error ignored", "attempt_id", id)
		}
	}
	if len(accepted) > 0 {
		marked, err := s.repo.MarkAttemptsHarvested(ctx, accepted, now)
		if err != nil {
			return 0, err
		}
		if marked != len(accepted) {
			return 0, fmt.Errorf("harvest ack mismatch: marked %d of %d accepted attempts", marked, len(accepted))
		}
	}

	// HIGH-3: persist per-attempt rejections to harvest_log; rejected
	// attempts stay unharvested for retry.
	for id, msg := range result.Failures {
		slog.WarnContext(ctx, "harvest attempt rejected", "attempt_id", id, "reason", msg)
		if _, wasSent := sent[id]; wasSent {
			delete(sent, id)
			if logErr := s.repo.LogHarvestFailure(ctx, id, deploymentID, 1, msg); logErr != nil {
				slog.ErrorContext(ctx, "harvest_log write failed", "error", logErr)
			}
		}
	}

	return len(accepted), nil
}

// deploymentForAttempt resolves attempt -> exam -> deployment_id. Attempts
// without a loadable exam are skipped with a warning rather than failing the
// whole drain.
func (s *HarvestService) deploymentForAttempt(ctx context.Context, att entity.Attempt) (string, error) {
	exam, err := s.repo.FindExamByID(ctx, att.ExamID)
	if err != nil {
		return "", fmt.Errorf("resolve deployment for attempt %s (exam %s): %w", att.ID, att.ExamID, err)
	}
	if strings.TrimSpace(exam.DeploymentID) == "" {
		return "", fmt.Errorf("exam %s has no deployment_id", att.ExamID)
	}
	return exam.DeploymentID, nil
}

func sortedKeys(m map[string][]entity.Attempt) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Start runs the harvest ticker every interval until ctx is cancelled.
func (s *HarvestService) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.DrainOnce(ctx); err != nil {
				slog.ErrorContext(ctx, "harvest tick failed", "error", err)
			}
		}
	}
}
