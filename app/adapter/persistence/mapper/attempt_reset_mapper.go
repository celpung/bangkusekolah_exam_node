package mapper

import (
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/model"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

func ToAttemptResetOperationEntity(m *model.AttemptResetOperation) *entity.AttemptResetOperation {
	if m == nil {
		return nil
	}
	return &entity.AttemptResetOperation{
		RequestID: m.RequestID, Operation: m.Operation, CommandHash: m.CommandHash,
		Status: entity.AttemptResetOperationStatus(m.Status), Code: m.Code, Message: m.Message,
		NodeID: m.NodeID, DeploymentID: m.DeploymentID, ExamID: m.ExamID, StudentID: m.StudentID,
		ParticipantID: m.ParticipantID, AttemptID: m.AttemptID, ExpectedGeneration: m.ExpectedGeneration,
		TargetGeneration: m.TargetGeneration, ExpectedStatus: entity.AttemptStatus(m.ExpectedStatus), ExpectedDueAt: timeValue(m.ExpectedDueAt),
		ExpectedSubmittedAt: m.ExpectedSubmittedAt, ExpectedAutoSubmittedAt: m.ExpectedAutoSubmittedAt, Deadline: m.Deadline,
		AttemptNo: m.AttemptNo, AttemptStatus: entity.AttemptStatus(m.AttemptStatus), StartedAt: timeValue(m.StartedAt), DueAt: timeValue(m.DueAt),
		SubmittedAt: m.SubmittedAt, AutoSubmittedAt: m.AutoSubmittedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func ToAttemptResetOperationModel(e *entity.AttemptResetOperation) *model.AttemptResetOperation {
	if e == nil {
		return nil
	}
	return &model.AttemptResetOperation{
		RequestID: e.RequestID, Operation: e.Operation, CommandHash: e.CommandHash,
		Status: string(e.Status), Code: e.Code, Message: e.Message,
		NodeID: e.NodeID, DeploymentID: e.DeploymentID, ExamID: e.ExamID, StudentID: e.StudentID,
		ParticipantID: e.ParticipantID, AttemptID: e.AttemptID, ExpectedGeneration: e.ExpectedGeneration,
		TargetGeneration: e.TargetGeneration, ExpectedStatus: string(e.ExpectedStatus), ExpectedDueAt: timePtr(e.ExpectedDueAt),
		ExpectedSubmittedAt: e.ExpectedSubmittedAt, ExpectedAutoSubmittedAt: e.ExpectedAutoSubmittedAt, Deadline: e.Deadline,
		AttemptNo: e.AttemptNo, AttemptStatus: string(e.AttemptStatus), StartedAt: timePtr(e.StartedAt), DueAt: timePtr(e.DueAt),
		SubmittedAt: e.SubmittedAt, AutoSubmittedAt: e.AutoSubmittedAt, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
