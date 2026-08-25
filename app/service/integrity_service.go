package service

import (
	"context"
	"errors"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

const (
	integrityRateLimit  = 10
	integrityRateWindow = time.Minute
	integrityDedupWin   = 5 * time.Second
)

type IntegrityService struct {
	repo  outbound_repository.NodeRepository
	idGen IDGenerator
}

func NewIntegrityService(repo outbound_repository.NodeRepository, idGen IDGenerator) *IntegrityService {
	return &IntegrityService{repo: repo, idGen: idGen}
}

// RecordEvent stores one integrity event with a per-attempt rate limit
// (10/min) and a dedup window (same event type within 5s is an idempotent
// no-op from clients firing focus_lost on every blur).
func (s *IntegrityService) RecordEvent(ctx context.Context, attemptID, participantID, eventType string, description *string, metadata map[string]interface{}) (*entity.IntegrityEvent, error) {
	attempt, err := s.repo.FindAttemptByID(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.ParticipantID != participantID {
		return nil, node_error.ErrForbidden
	}
	now := time.Now().UTC()
	count, err := s.repo.CountIntegrityEventsSince(ctx, attemptID, now.Add(-integrityRateWindow))
	if err != nil {
		return nil, err
	}
	if count >= integrityRateLimit {
		return nil, node_error.ErrIntegrityFlood
	}
	dup, err := s.repo.FindIntegrityEventSince(ctx, attemptID, eventType, now.Add(-integrityDedupWin))
	if err != nil && !errors.Is(err, node_error.ErrAttemptNotFound) {
		return nil, err
	}
	if dup != nil {
		return dup, nil // idempotent — do not store a duplicate
	}
	event := &entity.IntegrityEvent{
		ID: s.idGen.NewID(), AttemptID: attemptID, StudentID: attempt.StudentID,
		EventType: eventType, Description: description, MetadataJSON: metadata, CreatedAt: now,
	}
	if err := s.repo.CreateIntegrityEvent(ctx, event); err != nil {
		return nil, err
	}
	return event, nil
}
