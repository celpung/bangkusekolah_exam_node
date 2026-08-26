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

	// Resolve each attempt to its exam's deployment ID, batching the exam
	// lookups by unique ExamID to avoid an N+1 query pattern. An orphaned
	// attempt (missing/corrupt exam row) is logged to harvest_log and left
	// unharvested — it must never block valid deployments from draining.
	var resolveFailures []string
	byDeployment := map[string][]entity.Attempt{}
	examCache := map[string]*entity.Exam{}
	for _, att := range attempts {
		exam, cached := examCache[att.ExamID]
		if !cached {
			e, derr := s.repo.FindExamByID(ctx, att.ExamID)
			if derr != nil || e == nil {
				msg := "exam row missing or unloadable"
				if derr != nil {
					msg = derr.Error()
				}
				errMsg := fmt.Sprintf("resolve deployment for attempt %s (exam %s): %s", att.ID, att.ExamID, msg)
				slog.ErrorContext(ctx, "harvest: orphaned attempt skipped", "attempt_id", att.ID, "error", errMsg)
				resolveFailures = append(resolveFailures, errMsg)
				if logErr := s.repo.LogHarvestFailure(ctx, att.ID, "", 1, errMsg); logErr != nil {
					slog.ErrorContext(ctx, "harvest_log write failed", "error", logErr)
				}
				continue
			}
			exam = e
			examCache[att.ExamID] = e
		}
		if strings.TrimSpace(exam.DeploymentID) == "" {
			errMsg := fmt.Sprintf("attempt %s: exam %s has no deployment_id", att.ID, att.ExamID)
			slog.ErrorContext(ctx, "harvest: orphaned attempt skipped", "attempt_id", att.ID, "error", errMsg)
			resolveFailures = append(resolveFailures, errMsg)
			if logErr := s.repo.LogHarvestFailure(ctx, att.ID, "", 1, errMsg); logErr != nil {
				slog.ErrorContext(ctx, "harvest_log write failed", "error", logErr)
			}
			continue
		}
		byDeployment[exam.DeploymentID] = append(byDeployment[exam.DeploymentID], att)
	}

	total := 0
	var drainErrors []string
	// Deterministic order for stable runbook output.
	for _, depID := range sortedKeys(byDeployment) {
		group := byDeployment[depID]
		for len(group) > 0 {
			n, perr := s.drainDeployment(ctx, depID, group)
			if perr != nil {
				slog.ErrorContext(ctx, "harvest: deployment drain failed, continuing",
					"deployment", depID, "error", perr)
				drainErrors = append(drainErrors, fmt.Sprintf("dep %s: %s", depID, perr.Error()))
				break // skip remaining batches of this deployment, try next
			}
			total += n
			if n < len(group) {
				break // nothing more was accepted from this group
			}
			group = group[n:]
		}
	}

	// Aggregate all diagnostic errors: orphans + per-deployment failures.
	var allErrors []string
	allErrors = append(allErrors, resolveFailures...)
	allErrors = append(allErrors, drainErrors...)
	if len(allErrors) > 0 {
		return total, fmt.Errorf("harvest completed with %d accepted and %d issue(s): %s",
			total, len(allErrors), strings.Join(allErrors, "; "))
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
	if result == nil {
		// HIGH-2: a nil result with nil error is a broken pusher — never
		// dereference it.
		err := fmt.Errorf("harvest push: nil acknowledgement from central")
		if logErr := s.repo.LogHarvestFailure(ctx, batch.Attempts[0].ID, deploymentID, len(batch.Attempts), err.Error()); logErr != nil {
			slog.ErrorContext(ctx, "harvest_log write failed", "error", logErr)
		}
		return 0, err
	}

	now := time.Now().UTC()
	// BLOCKER-2/HIGH-2 ack validation:
	//   - deduplicate repeated accepted IDs;
	//   - reject an ID appearing in BOTH accepted and failures (protocol
	//     error, logged and left unharvested);
	//   - only IDs actually sent in this request are eligible;
	//   - accepted IDs stay scoped to this deployment's batch.
	var accepted []string
	seenAccepted := map[string]struct{}{}
	for _, id := range result.AcceptedAttemptIDs {
		if _, wasSent := sent[id]; !wasSent {
			slog.WarnContext(ctx, "central acknowledged an unsent attempt — protocol error ignored", "attempt_id", id)
			continue
		}
		if _, dup := seenAccepted[id]; dup {
			slog.WarnContext(ctx, "central acknowledged the same attempt twice — deduplicated", "attempt_id", id)
			continue
		}
		if reason, rejected := result.Failures[id]; rejected {
			slog.WarnContext(ctx, "central returned contradictory accept+failure — treating as failure",
				"attempt_id", id, "reason", reason)
			continue
		}
		seenAccepted[id] = struct{}{}
		accepted = append(accepted, id)
		delete(sent, id)
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
