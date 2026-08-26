package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// HarvestPusher abstracts the central client so tests inject an httptest
// transport instead of a real network call.
type HarvestPusher interface {
	Push(ctx context.Context, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error)
}

type HarvestService struct {
	repo   outbound_repository.NodeRepository
	client HarvestPusher
}

func NewHarvestService(repo outbound_repository.NodeRepository, client HarvestPusher) *HarvestService {
	return &HarvestService{repo: repo, client: client}
}

// DrainOnce collects finished attempts not yet acked, pushes them, and marks
// the acked ones. Idempotent: a second drain with the same attempts sends
// nothing. Push failures are logged to harvest_log and retried next tick —
// never block a student on harvest failure.
func (s *HarvestService) DrainOnce(ctx context.Context) (int, error) {
	attempts, err := s.repo.ListUnpushedAttempts(ctx)
	if err != nil {
		return 0, err
	}
	if len(attempts) == 0 {
		return 0, nil
	}

	batch := inbound.ExamNodeAttemptBatch{Attempts: make([]inbound.ExamNodeAttemptPayload, 0, len(attempts))}
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
			ID: att.ID, ParticipantID: att.ParticipantID, StudentID: att.StudentID,
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
	}

	result, err := s.client.Push(ctx, batch)
	if err != nil {
		slog.ErrorContext(ctx, "harvest push failed", "error", err, "batch_size", len(batch.Attempts))
		_ = s.repo.LogHarvestFailure(ctx, batch.Attempts[0].ID, err.Error())
		return 0, err
	}
	if len(result.AcceptedAttemptIDs) > 0 {
		now := time.Now().UTC()
		if err := s.repo.MarkAttemptsHarvested(ctx, result.AcceptedAttemptIDs, now); err != nil {
			return 0, err
		}
	}
	for id, msg := range result.Failures {
		slog.WarnContext(ctx, "harvest attempt rejected", "attempt_id", id, "reason", msg)
	}
	return len(result.AcceptedAttemptIDs), nil
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
