package service

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type IDGenerator interface{ NewID() string }

type AttemptService struct {
	repo      outbound_repository.NodeRepository
	txManager outbound.TxManager
	idGen     IDGenerator
}

func NewAttemptService(repo outbound_repository.NodeRepository, txManager outbound.TxManager, idGen IDGenerator) *AttemptService {
	return &AttemptService{repo: repo, txManager: txManager, idGen: idGen}
}

func (s *AttemptService) StartAttempt(ctx context.Context, participantID string) (*entity.Attempt, error) {
	exam, err := s.repo.FindExam(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if now.Before(exam.StartsAt) || now.After(exam.EndsAt) {
		return nil, node_error.ErrExamNotOpen
	}
	participant, err := s.repo.FindParticipantByID(ctx, participantID)
	if err != nil {
		return nil, err
	}
	if active, err := s.repo.FindActiveAttemptByParticipant(ctx, participantID); err == nil {
		return active, nil
	}
	if participant.AttemptCount >= exam.MaxAttempts {
		return nil, node_error.ErrMaxAttemptsReached
	}
	due := now.Add(time.Duration(exam.DurationMinutes) * time.Minute)
	if due.After(exam.EndsAt) {
		due = exam.EndsAt
	}
	attemptNo := participant.AttemptCount + 1
	attempt := &entity.Attempt{
		ParticipantID: participantID, StudentID: participant.StudentID,
		AttemptNo: attemptNo, Status: entity.AttemptInProgress,
		StartedAt: now, DueAt: due, MaxScore: exam.MaxScore, GradingStatus: entity.GradingPending,
	}
	attempt.ID = s.idGen.NewID()
	if err := s.txManager.Atomic(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateAttempt(ctx, attempt); err != nil {
			return err
		}
		participant.AttemptCount = attemptNo
		participant.LatestAttemptID = &attempt.ID
		return s.repo.UpdateParticipant(ctx, participant)
	}); err != nil {
		return nil, err
	}
	return attempt, nil
}

func (s *AttemptService) GetAttemptState(ctx context.Context, participantID, attemptID string) (*inbound.AttemptState, error) {
	attempt, err := s.repo.FindAttemptByID(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.ParticipantID != participantID {
		return nil, node_error.ErrForbidden
	}
	answers, err := s.repo.ListAnswersByAttempt(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	return &inbound.AttemptState{Attempt: attempt, Answers: answers, ServerTime: time.Now().UTC()}, nil
}
