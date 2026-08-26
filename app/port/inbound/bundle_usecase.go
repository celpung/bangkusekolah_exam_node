package inbound

import (
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

// The bundle types mirror central's inbound.ExamNodeBundle field-for-field and
// JSON-tag-for-tag so the checksum recomputation matches byte-for-byte.

type ExamNodeBundleExam struct {
	ID                    string                           `json:"id"`
	Title                 string                           `json:"title"`
	Instruction           *string                          `json:"instruction"`
	StartsAt              time.Time                        `json:"starts_at"`
	EndsAt                time.Time                        `json:"ends_at"`
	DurationMinutes       int                              `json:"duration_minutes"`
	MaxAttempts           int                              `json:"max_attempts"`
	ShuffleQuestions      bool                             `json:"shuffle_questions"`
	ShuffleOptions        bool                             `json:"shuffle_options"`
	ShowResultImmediately bool                             `json:"show_result_immediately"`
	PassingScore          *float64                         `json:"passing_score"`
	ResultSelectionPolicy entity.ExamResultSelectionPolicy `json:"result_selection_policy"`
}

type ExamNodeBundleSection struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Instruction *string `json:"instruction"`
	SortOrder   int     `json:"sort_order"`
}

type ExamNodeBundleRubricCriterion struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	MaxPoints float64 `json:"max_points"`
	SortOrder int     `json:"sort_order"`
}

type ExamNodeBundleItem struct {
	ID                    string                          `json:"id"`
	SectionID             string                          `json:"section_id"`
	QuestionType          entity.ExamQuestionType         `json:"question_type"`
	PromptSnapshot        string                          `json:"prompt_snapshot"`
	OptionsSnapshotJSON   []map[string]interface{}        `json:"options_snapshot_json"`
	AnswerKeySnapshotJSON map[string]interface{}          `json:"answer_key_snapshot_json"`
	Points                float64                         `json:"points"`
	SortOrder             int                             `json:"sort_order"`
	RequiresManualGrading bool                            `json:"requires_manual_grading"`
	RubricCriteria        []ExamNodeBundleRubricCriterion `json:"rubric_criteria"`
}

type ExamNodeBundleParticipant struct {
	ID          string `json:"id"`
	StudentID   string `json:"student_id"`
	StudentName string `json:"student_name"`
	AccessCode  string `json:"access_code"`
}

// ExamNodeBundle is everything a node needs to run one exam with no network.
// Checksum covers the canonical JSON of every other field.
type ExamNodeBundle struct {
	BundleVersion int                         `json:"bundle_version"`
	DeploymentID  string                      `json:"deployment_id"`
	Exam          ExamNodeBundleExam          `json:"exam"`
	Sections      []ExamNodeBundleSection     `json:"sections"`
	Items         []ExamNodeBundleItem        `json:"items"`
	Participants  []ExamNodeBundleParticipant `json:"participants"`
	Checksum      string                      `json:"checksum"`
}
