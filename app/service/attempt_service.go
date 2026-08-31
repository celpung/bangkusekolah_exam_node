package service

import (
	"context"
	"errors"
	"strings"
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

func (s *AttemptService) StartAttempt(ctx context.Context, participantID, examID string) (*entity.Attempt, error) {
	return s.startAttempt(ctx, participantID, examID, "")
}

func (s *AttemptService) StartAttemptWithDevice(ctx context.Context, participantID, examID, deviceID string) (*entity.Attempt, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, node_error.ErrAttemptDeviceIDInvalid
	}
	return s.startAttempt(ctx, participantID, examID, deviceID)
}

func (s *AttemptService) startAttempt(ctx context.Context, participantID, examID, deviceID string) (*entity.Attempt, error) {
	deviceID = strings.TrimSpace(deviceID)
	if len(deviceID) > 128 {
		return nil, node_error.ErrAttemptDeviceIDInvalid
	}
	exam, err := s.repo.FindExamByID(ctx, examID)
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
	if participant.ExamID != examID {
		return nil, node_error.ErrForbidden
	}
	active, err := s.repo.FindActiveAttemptByParticipantAndExam(ctx, participantID, examID)
	if err == nil {
		if active.DeviceID != "" && active.DeviceID != deviceID {
			return nil, node_error.ErrAttemptDeviceMismatch
		}
		if active.DeviceID == "" && deviceID != "" {
			if binder, ok := s.repo.(outbound_repository.AttemptDeviceBinder); ok {
				if err := binder.BindAttemptDevice(ctx, active.ID, deviceID); err != nil {
					return nil, err
				}
			} else {
				active.DeviceID = deviceID
				if err := s.repo.UpdateAttempt(ctx, active); err != nil {
					return nil, err
				}
			}
			active.DeviceID = deviceID
		}
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
		if locked.ExamID != examID {
			return node_error.ErrForbidden
		}
		if active, err := s.repo.FindActiveAttemptByParticipantAndExam(txCtx, participantID, examID); err == nil {
			if active.DeviceID != "" && active.DeviceID != deviceID {
				return node_error.ErrAttemptDeviceMismatch
			}
			if active.DeviceID == "" && deviceID != "" {
				if binder, ok := s.repo.(outbound_repository.AttemptDeviceBinder); ok {
					if err := binder.BindAttemptDevice(txCtx, active.ID, deviceID); err != nil {
						return err
					}
				} else {
					active.DeviceID = deviceID
					if err := s.repo.UpdateAttempt(txCtx, active); err != nil {
						return err
					}
				}
				active.DeviceID = deviceID
			}
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
			ExamID: exam.ID, DeviceID: deviceID, AttemptNo: attemptNo, Status: entity.AttemptInProgress,
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
	if item.ExamID != "" && item.ExamID != attempt.ExamID {
		return nil, node_error.ErrForbidden
	}
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

func (s *AttemptService) SubmitAttempt(ctx context.Context, attemptID, participantID string) (*entity.Attempt, error) {
	attempt, err := s.repo.FindAttemptByID(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.ParticipantID != participantID {
		return nil, node_error.ErrForbidden
	}
	if attempt.Status == entity.AttemptSubmitted || attempt.Status == entity.AttemptAutoSubmitted || attempt.Status == entity.AttemptGraded {
		return attempt, nil
	}
	if attempt.Status != entity.AttemptInProgress {
		return nil, node_error.ErrAttemptLocked
	}
	if time.Now().After(attempt.DueAt) {
		return nil, node_error.ErrAttemptExpired
	}
	exam, err := s.repo.FindExamByID(ctx, attempt.ExamID)
	if err != nil {
		return nil, err
	}
	var result *entity.Attempt
	err = s.txManager.Atomic(ctx, func(txCtx context.Context) error {
		locked, err := s.repo.FindAttemptByIDForUpdate(txCtx, attemptID)
		if err != nil {
			return err
		}
		if locked.Status != entity.AttemptInProgress {
			result = locked
			return nil
		}
		answers, err := s.repo.ListAnswersByAttempt(txCtx, attemptID)
		if err != nil {
			return err
		}
		total, gradingStatus := finalizeStatus(answers, exam.HasManualItems)
		now := time.Now().UTC()
		locked.Score = &total
		locked.GradingStatus = gradingStatus
		locked.Status = entity.AttemptSubmitted
		locked.SubmittedAt = &now
		if err := s.repo.UpdateAttempt(txCtx, locked); err != nil {
			return err
		}
		result = locked
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func finalizeStatus(answers []entity.Answer, hasManualItems bool) (float64, entity.GradingStatus) {
	total := 0.0
	hasManual := hasManualItems
	for _, ans := range answers {
		if ans.Score != nil {
			total += *ans.Score
		}
		if ans.GradingStatus == entity.GradingManualRequired {
			hasManual = true
		}
	}
	status := entity.GradingAutoGraded
	if hasManual {
		status = entity.GradingManualRequired
	}
	return total, status
}

func (s *AttemptService) GetResult(ctx context.Context, participantID, examID string) (*entity.Attempt, error) {
	attempt, err := s.repo.FindLatestAttemptByParticipantAndExam(ctx, participantID, examID)
	if err != nil {
		return nil, err
	}
	if !attempt.IsFinished() {
		return nil, node_error.ErrResultNotAvailable
	}
	return attempt, nil
}

var (
	_ inbound.AttemptUsecase = (*AttemptService)(nil)
)
