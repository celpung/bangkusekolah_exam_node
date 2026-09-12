package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

var _ inbound.RosterUsecase = (*RosterService)(nil)

type RosterService struct {
	repo       outbound.RosterRepository
	txManager  outbound.TxManager
	idGen      IDGenerator
	contentSvc interface{ LockExam(string) func() }
}

func NewRosterService(repo outbound.RosterRepository, txManager outbound.TxManager, idGen IDGenerator, contentSvc interface{ LockExam(string) func() }) *RosterService {
	return &RosterService{repo: repo, txManager: txManager, idGen: idGen, contentSvc: contentSvc}
}

func (s *RosterService) Apply(ctx context.Context, event inbound.RosterEvent) (*inbound.RosterOutcome, error) {
	if err := validateRosterEvent(event); err != nil {
		return nil, err
	}
	payloadHash := rosterPayloadHash(event)
	unlock := func() {}
	if s.contentSvc != nil {
		unlock = s.contentSvc.LockExam(event.ExamID)
	}
	defer unlock()

	var outcome *inbound.RosterOutcome
	operation := func(txCtx context.Context) error {
		exam, err := s.repo.FindExamForUpdate(txCtx, event.ExamID)
		if err != nil {
			return err
		}
		if receipt, err := s.repo.FindReceipt(txCtx, event.EventID); err == nil {
			if receipt.PayloadHash != payloadHash {
				return node_error.ErrRosterPayloadConflict
			}
			outcome = rosterOutcomeFromReceipt(receipt)
			return nil
		} else if !errors.Is(err, node_error.ErrRosterReceiptNotFound) {
			return err
		}

		if event.DeploymentID != exam.DeploymentID {
			outcome, err = s.rejectEvent(txCtx, event, payloadHash, node_error.ErrRosterDeploymentMismatch.Error())
			return err
		}
		if event.Revision != exam.RosterRevision+1 {
			return node_error.ErrRosterRevisionGap
		}
		if event.Status == string(entity.RosterEventCancelled) || event.Status == string(entity.RosterEventRejected) || event.Status == string(entity.RosterEventExpired) {
			code := event.OutcomeCode
			if code == "" {
				code = event.Status
			}
			outcome, err = s.rejectAndAdvance(txCtx, event, payloadHash, code, exam)
			return err
		}
		now := time.Now().UTC()
		if event.Status == string(entity.RosterEventPending) {
			if exam.FencedAt != nil {
				outcome, err = s.rejectAndAdvance(txCtx, event, payloadHash, node_error.ErrRosterExamFenced.Error(), exam)
				return err
			}
			if !now.Before(event.Deadline) || !now.Before(exam.EndsAt) {
				outcome, err = s.rejectAndAdvance(txCtx, event, payloadHash, node_error.ErrRosterExpired.Error(), exam)
				return err
			}
		}
		if event.Status != string(entity.RosterEventPending) && event.Status != string(entity.RosterEventApplied) {
			return node_error.ErrRosterInvalid
		}

		participant := &entity.Participant{
			ID: event.ParticipantID, ExamID: event.ExamID, StudentID: event.StudentID,
			StudentName: event.StudentName, AccessCode: event.AccessCode, RosterRevision: event.Revision,
		}
		existing, inserted, err := s.repo.InsertParticipantIfAbsent(txCtx, participant)
		if err != nil {
			if errors.Is(err, node_error.ErrRosterCodeConflict) {
				outcome, err = s.rejectAndAdvance(txCtx, event, payloadHash, node_error.ErrRosterCodeConflict.Error(), exam)
				return err
			}
			return err
		}
		if !inserted && (existing.ID != event.ParticipantID || existing.StudentID != event.StudentID || existing.AccessCode != event.AccessCode || existing.StudentName != event.StudentName) {
			outcome, err = s.rejectAndAdvance(txCtx, event, payloadHash, "participant_identity_conflict", exam)
			return err
		}
		items, err := s.repo.ListItemsByExamID(txCtx, event.ExamID)
		if err != nil {
			return err
		}
		participants, err := s.repo.ListParticipantsByExam(txCtx, event.ExamID)
		if err != nil {
			return err
		}
		exam.ContentHash = contentHash(items, participants, exam)
		if err := s.repo.UpdateExamRosterState(txCtx, exam.ID, event.Revision, exam.ContentHash); err != nil {
			return err
		}
		receipt := &entity.RosterEventReceipt{
			EventID: event.EventID, DeploymentID: event.DeploymentID, ExamID: event.ExamID,
			Revision: event.Revision, ParticipantID: event.ParticipantID, PayloadHash: payloadHash,
			Status: entity.RosterEventApplied, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.InsertReceipt(txCtx, receipt); err != nil {
			return err
		}
		outcome = &inbound.RosterOutcome{EventID: event.EventID, DeploymentID: event.DeploymentID, Revision: event.Revision, Status: string(entity.RosterEventApplied)}
		return nil
	}
	if s.txManager == nil {
		if err := operation(ctx); err != nil {
			return nil, err
		}
	} else if err := s.txManager.Atomic(ctx, operation); err != nil {
		return nil, err
	}
	return outcome, nil
}

func (s *RosterService) rejectAndAdvance(ctx context.Context, event inbound.RosterEvent, payloadHash, code string, exam *entity.Exam) (*inbound.RosterOutcome, error) {
	if err := s.repo.UpdateExamRosterState(ctx, exam.ID, event.Revision, exam.ContentHash); err != nil {
		return nil, err
	}
	return s.rejectEvent(ctx, event, payloadHash, code)
}

func (s *RosterService) rejectEvent(ctx context.Context, event inbound.RosterEvent, payloadHash, code string) (*inbound.RosterOutcome, error) {
	now := time.Now().UTC()
	receipt := &entity.RosterEventReceipt{
		EventID: event.EventID, DeploymentID: event.DeploymentID, ExamID: event.ExamID,
		Revision: event.Revision, ParticipantID: event.ParticipantID, PayloadHash: payloadHash,
		Status: entity.RosterEventRejected, OutcomeCode: code, CreatedAt: now, UpdatedAt: now,
	}
	if event.Status == string(entity.RosterEventCancelled) {
		receipt.Status = entity.RosterEventCancelled
	}
	if event.Status == string(entity.RosterEventExpired) {
		receipt.Status = entity.RosterEventExpired
	}
	if err := s.repo.InsertReceipt(ctx, receipt); err != nil {
		return nil, err
	}
	return rosterOutcomeFromReceipt(receipt), nil
}

func rosterOutcomeFromReceipt(receipt *entity.RosterEventReceipt) *inbound.RosterOutcome {
	status := string(entity.RosterEventRejected)
	if receipt.Status == entity.RosterEventApplied {
		status = string(entity.RosterEventApplied)
	}
	return &inbound.RosterOutcome{EventID: receipt.EventID, DeploymentID: receipt.DeploymentID, Revision: receipt.Revision, Status: status, Code: receipt.OutcomeCode}
}

func validateRosterEvent(event inbound.RosterEvent) error {
	if event.EventID == "" || event.DeploymentID == "" || event.ExamID == "" || event.ParticipantID == "" || event.StudentID == "" || event.AccessCode == "" || event.Revision < 1 || event.Deadline.IsZero() {
		return node_error.ErrRosterInvalid
	}
	switch entity.RosterEventStatus(event.Status) {
	case entity.RosterEventPending, entity.RosterEventApplied, entity.RosterEventRejected, entity.RosterEventExpired, entity.RosterEventCancelled:
		return nil
	default:
		return node_error.ErrRosterInvalid
	}
}

func rosterPayloadHash(event inbound.RosterEvent) string {
	view := struct {
		EventID       string    `json:"event_id"`
		DeploymentID  string    `json:"deployment_id"`
		ExamID        string    `json:"exam_id"`
		ParticipantID string    `json:"participant_id"`
		StudentID     string    `json:"student_id"`
		StudentName   string    `json:"student_name"`
		AccessCode    string    `json:"access_code"`
		Revision      int64     `json:"revision"`
		Deadline      time.Time `json:"deadline"`
		CreatedAt     time.Time `json:"created_at"`
	}{event.EventID, event.DeploymentID, event.ExamID, event.ParticipantID, event.StudentID, event.StudentName, event.AccessCode, event.Revision, event.Deadline.UTC(), event.CreatedAt.UTC()}
	encoded, _ := json.Marshal(view)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
