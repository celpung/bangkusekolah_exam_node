package inbound

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

type ExamContent struct {
	Exam  *entity.Exam  `json:"exam"`
	Items []ExamItemDTO `json:"items"`
}

// ExamItemDTO renames the persistence fields to what the client renders.
// AnswerKeySnapshotJSON and RubricCriteria are never sent to the student —
// the node grades locally, so the keys stay in the DB.
type ExamItemDTO struct {
	ID                    string                   `json:"id"`
	SectionID             string                   `json:"section_id"`
	SectionTitle          string                   `json:"section_title"`
	SectionSortOrder      int                      `json:"section_sort_order"`
	SortOrder             int                      `json:"sort_order"`
	QuestionType          string                   `json:"question_type"`
	Prompt                string                   `json:"prompt"`
	Options               []map[string]interface{} `json:"options,omitempty"`
	Points                float64                  `json:"points"`
	RequiresManualGrading bool                     `json:"requires_manual_grading"`
}

type ContentUsecase interface {
	// GetExamContent returns the cached immutable content, its quoted ETag,
	// the pre-gzipped bytes, and the raw JSON bytes (for clients without gzip).
	GetExamContent(ctx context.Context) (content *ExamContent, etag string, gzipBytes []byte, rawBytes []byte, err error)
}
