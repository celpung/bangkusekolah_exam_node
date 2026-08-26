package mapper

import (
	"encoding/json"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/model"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

func ToExamEntity(m *model.Exam) *entity.Exam {
	if m == nil {
		return nil
	}
	return &entity.Exam{
		ID: m.ID, DeploymentID: m.DeploymentID, Title: m.Title, Instruction: m.Instruction,
		StartsAt: m.StartsAt, EndsAt: m.EndsAt, DurationMinutes: m.DurationMinutes, MaxAttempts: m.MaxAttempts,
		ShuffleQuestions: m.ShuffleQuestions, ShuffleOptions: m.ShuffleOptions, ShowResultImmediately: m.ShowResultImmediately,
		PassingScore: m.PassingScore, ResultSelectionPolicy: m.ResultSelectionPolicy, MaxScore: m.MaxScore,
		HasManualItems: m.HasManualItems, AccessCodePrefix: m.AccessCodePrefix, BundleChecksum: m.BundleChecksum, LoadedAt: m.LoadedAt,
	}
}

func ToParticipantEntity(m *model.Participant) *entity.Participant {
	if m == nil {
		return nil
	}
	return &entity.Participant{ID: m.ID, StudentID: m.StudentID, StudentName: m.StudentName, AccessCode: m.AccessCode, AttemptCount: m.AttemptCount, LatestAttemptID: m.LatestAttemptID}
}

func ToAttemptEntity(m *model.Attempt) *entity.Attempt {
	if m == nil {
		return nil
	}
	return &entity.Attempt{ID: m.ID, ParticipantID: m.ParticipantID, StudentID: m.StudentID, ExamID: m.ExamID, AttemptNo: m.AttemptNo, Status: entity.AttemptStatus(m.Status), StartedAt: m.StartedAt, DueAt: m.DueAt, SubmittedAt: m.SubmittedAt, AutoSubmittedAt: m.AutoSubmittedAt, Score: m.Score, MaxScore: m.MaxScore, GradingStatus: entity.GradingStatus(m.GradingStatus), HarvestedAt: m.HarvestedAt}
}

func ToAttemptModel(e *entity.Attempt) *model.Attempt {
	if e == nil {
		return nil
	}
	return &model.Attempt{ID: e.ID, ParticipantID: e.ParticipantID, StudentID: e.StudentID, ExamID: e.ExamID, AttemptNo: e.AttemptNo, Status: string(e.Status), StartedAt: e.StartedAt, DueAt: e.DueAt, SubmittedAt: e.SubmittedAt, AutoSubmittedAt: e.AutoSubmittedAt, Score: e.Score, MaxScore: e.MaxScore, GradingStatus: string(e.GradingStatus), HarvestedAt: e.HarvestedAt}
}

func ToAnswerEntity(m *model.Answer) *entity.Answer {
	if m == nil {
		return nil
	}
	var aj map[string]interface{}
	if m.AnswerJSON != nil {
		_ = json.Unmarshal([]byte(*m.AnswerJSON), &aj)
	}
	return &entity.Answer{ID: m.ID, AttemptID: m.AttemptID, ItemID: m.ItemID, AnswerJSON: aj, AnswerText: m.AnswerText, Score: m.Score, MaxScore: m.MaxScore, GradingStatus: entity.GradingStatus(m.GradingStatus), LastSavedAt: m.LastSavedAt, ClientSeq: m.ClientSeq}
}

func ToAnswerModel(e *entity.Answer) *model.Answer {
	if e == nil {
		return nil
	}
	var aj *string
	if e.AnswerJSON != nil {
		if b, err := json.Marshal(e.AnswerJSON); err == nil {
			s := string(b)
			aj = &s
		}
	}
	return &model.Answer{ID: e.ID, AttemptID: e.AttemptID, ItemID: e.ItemID, AnswerJSON: aj, AnswerText: e.AnswerText, Score: e.Score, MaxScore: e.MaxScore, GradingStatus: string(e.GradingStatus), LastSavedAt: e.LastSavedAt, ClientSeq: e.ClientSeq}
}

func ToIntegrityEventEntity(m *model.IntegrityEvent) *entity.IntegrityEvent {
	if m == nil {
		return nil
	}
	var meta map[string]interface{}
	if m.MetadataJSON != nil {
		_ = json.Unmarshal([]byte(*m.MetadataJSON), &meta)
	}
	var desc *string
	return &entity.IntegrityEvent{ID: m.ID, AttemptID: m.AttemptID, StudentID: m.StudentID, EventType: m.EventType, Description: desc, MetadataJSON: meta, CreatedAt: m.CreatedAt}
}

func ToIntegrityEventModel(e *entity.IntegrityEvent) *model.IntegrityEvent {
	if e == nil {
		return nil
	}
	var meta *string
	if e.MetadataJSON != nil {
		if b, err := json.Marshal(e.MetadataJSON); err == nil {
			s := string(b)
			meta = &s
		}
	}
	return &model.IntegrityEvent{ID: e.ID, AttemptID: e.AttemptID, StudentID: e.StudentID, EventType: e.EventType, Description: e.Description, MetadataJSON: meta, CreatedAt: e.CreatedAt}
}

func ToItemEntity(m *model.Item) *entity.Item {
	if m == nil {
		return nil
	}
	var opts []map[string]interface{}
	var key map[string]interface{}
	var rubrics []entity.RubricCriterion
	if m.OptionsSnapshotJSON != nil {
		_ = json.Unmarshal([]byte(*m.OptionsSnapshotJSON), &opts)
	}
	if m.AnswerKeySnapshotJSON != nil {
		_ = json.Unmarshal([]byte(*m.AnswerKeySnapshotJSON), &key)
	}
	if m.RubricCriteriaJSON != nil {
		_ = json.Unmarshal([]byte(*m.RubricCriteriaJSON), &rubrics)
	}
	return &entity.Item{ID: m.ID, ExamID: m.ExamID, SectionID: m.SectionID, SectionTitle: m.SectionTitle, SectionSortOrder: m.SectionSortOrder, SortOrder: m.SortOrder, QuestionType: entity.QuestionType(m.QuestionType), PromptSnapshot: m.PromptSnapshot, OptionsSnapshotJSON: opts, AnswerKeySnapshotJSON: key, RubricCriteria: rubrics, Points: m.Points, RequiresManualGrading: m.RequiresManualGrading}
}
