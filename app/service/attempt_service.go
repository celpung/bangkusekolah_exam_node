package service

import (
	"context"
	"errors"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/grading"
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
	active, err := s.repo.FindActiveAttemptByParticipant(ctx, participantID)
	if err == nil {
		return active, nil
	}
	if !errors.Is(err, node_error.ErrAttemptNotFound) {
		return nil, err
	}
	if participant.AttemptCount >= exam.MaxAttempts {
		return nil, node_error.ErrMaxAttemptsReached
	}
	due := now.Add(time.Duration(exam.DurationMinutes) * time.Minute)
	if due.After(exam.EndsAt) {
		due = exam.EndsAt
	}
	var result *entity.Attempt
	err = s.txManager.Atomic(ctx, func(txCtx context.Context) error {
		locked, err := s.repo.FindParticipantByIDForUpdate(txCtx, participantID)
		if err != nil {
			return err
		}
		if active, err := s.repo.FindActiveAttemptByParticipant(txCtx, participantID); err == nil {
			result = active
			return nil
		} else if !errors.Is(err, node_error.ErrAttemptNotFound) {
			return err
		}
		if locked.AttemptCount >= exam.MaxAttempts {
			return node_error.ErrMaxAttemptsReached
		}
		attemptNo := locked.AttemptCount + 1
		attempt := &entity.Attempt{
			ParticipantID: participantID, StudentID: locked.StudentID,
			AttemptNo: attemptNo, Status: entity.AttemptInProgress,
			StartedAt: now, DueAt: due, MaxScore: exam.MaxScore, GradingStatus: entity.GradingPending,
		}
		attempt.ID = s.idGen.NewID()
		if err := s.repo.CreateAttempt(txCtx, attempt); err != nil {
			return err
		}
		locked.AttemptCount = attemptNo
		locked.LatestAttemptID = &attempt.ID
		if err := s.repo.UpdateParticipant(txCtx, locked); err != nil {
			return err
		}
		result = attempt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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

func (s *AttemptService) AutosaveAnswer(ctx context.Context, attemptID, itemID string, answerJSON map[string]interface{}, answerText *string, clientSeq int64, participantID string) (*entity.Answer, error) {
	attempt, err := s.repo.FindAttemptByID(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.ParticipantID != participantID {
		return nil, node_error.ErrForbidden
	}
	if attempt.Status != entity.AttemptInProgress {
		return nil, node_error.ErrAttemptLocked
	}
	if time.Now().After(attempt.DueAt) {
		return nil, node_error.ErrAttemptExpired
	}
	item, err := s.repo.FindItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}
	// Grade inline — cheap, and makes submit fast. Essay and other manual types
	// return (0, false) and are stored as manual_required.
	score, graded := grading.GradeObjectiveAnswer(*item, &entity.Answer{AnswerJSON: answerJSON})
	var scorePtr *float64
	gradingStatus := entity.GradingPending
	if graded {
		scorePtr = &score
		gradingStatus = entity.GradingAutoGraded
	} else if item.RequiresManualGrading {
		gradingStatus = entity.GradingManualRequired
	}
	answer := &entity.Answer{
		ID: s.idGen.NewID(), AttemptID: attemptID, ItemID: itemID,
		AnswerJSON: answerJSON, AnswerText: answerText,
		Score: scorePtr, MaxScore: item.Points, GradingStatus: gradingStatus,
		LastSavedAt: time.Now().UTC(), ClientSeq: clientSeq,
	}
	return s.repo.UpsertAnswer(ctx, answer)
}
