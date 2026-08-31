package service

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type StudentExamService struct {
	repo outbound_repository.NodeRepository
}

func NewStudentExamService(repo outbound_repository.NodeRepository) *StudentExamService {
	return &StudentExamService{repo: repo}
}

// ListExams returns the exams this participant is enrolled in. On a multi-exam
// node it is scoped to the JWT's participant — the repo lookup is by participant
// ID, never a global list. If participant not found it returns that error.
func (s *StudentExamService) ListExams(ctx context.Context, participantID string) ([]entity.Exam, error) {
	participant, err := s.repo.FindParticipantByID(ctx, participantID)
	if err != nil {
		return nil, err
	}
	exam, err := s.repo.FindExamByID(ctx, participant.ExamID)
	if err != nil {
		return nil, err
	}
	return []entity.Exam{*exam}, nil
}
