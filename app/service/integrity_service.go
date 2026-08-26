package service

import (
	"context"
	"errors"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

const (
	integrityRateLimit  = 10
	integrityRateWindow = time.Minute
	integrityDedupWin   = 5 * time.Second
)

type IntegrityService struct {
	repo      outbound_repository.NodeRepository
	txManager outbound.TxManager
	idGen     IDGenerator
}

func NewIntegrityService(repo outbound_repository.NodeRepository, txManager outbound.TxManager, idGen IDGenerator) *IntegrityService {
	return &IntegrityService{repo: repo, txManager: txManager, idGen: idGen}
}

// RecordEvent stores one integrity event. The rate limit (10/min per attempt)
// and dedup window (same event type within 5s) are enforced inside a single
// transaction that locks the attempt row, so concurrent requests serialize:
// two simultaneous events can never both pass the count check.
func (s *IntegrityService) RecordEvent(ctx context.Context, attemptID, participantID, eventType string, description *string, metadata map[string]interface{}) (*entity.IntegrityEvent, error) {
	var result *entity.IntegrityEvent
	err := s.txManager.Atomic(ctx, func(txCtx context.Context) error {
		attempt, err := s.repo.FindAttemptByIDForUpdate(txCtx, attemptID)
		if err != nil {
			return err
		}
		if attempt.ParticipantID != participantID {
			return node_error.ErrForbidden
		}
		now := time.Now().UTC()
		count, err := s.repo.CountIntegrityEventsSince(txCtx, attemptID, now.Add(-integrityRateWindow))
		if err != nil {
			return err
		}
		if count >= integrityRateLimit {
			return node_error.ErrIntegrityFlood
		}
		dup, err := s.repo.FindIntegrityEventSince(txCtx, attemptID, eventType, now.Add(-integrityDedupWin))
		if err != nil && !errors.Is(err, node_error.ErrAttemptNotFound) {
			return err
		}
		if dup != nil {
			result = dup // idempotent — do not store a duplicate
			return nil
		}
		event := &entity.IntegrityEvent{
			ID: s.idGen.NewID(), AttemptID: attemptID, StudentID: attempt.StudentID,
			EventType: eventType, Description: description, MetadataJSON: metadata, CreatedAt: now,
		}
		if err := s.repo.CreateIntegrityEvent(txCtx, event); err != nil {
			return err
		}
		result = event
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
