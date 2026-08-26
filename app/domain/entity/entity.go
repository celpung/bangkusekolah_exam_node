package entity

import "time"

type QuestionType string

// ExamQuestionType mirrors central's question type name so bundle JSON decodes
// without a conversion layer.
type ExamQuestionType = QuestionType

// ExamResultSelectionPolicy mirrors central's policy type so the bundle JSON
// decodes without a conversion layer.
type ExamResultSelectionPolicy string

const (
	ResultSelectionBest   ExamResultSelectionPolicy = "best"
	ResultSelectionLatest ExamResultSelectionPolicy = "latest"
)

const (
	QuestionSingleChoice   QuestionType = "single_choice"
	QuestionMultipleChoice QuestionType = "multiple_choice"
	QuestionTrueFalse      QuestionType = "true_false"
	QuestionShortAnswer    QuestionType = "short_answer"
	QuestionEssay          QuestionType = "essay"
	QuestionMatching       QuestionType = "matching"
	QuestionOrdering       QuestionType = "ordering"
)

type AttemptStatus string

const (
	AttemptInProgress    AttemptStatus = "in_progress"
	AttemptSubmitted     AttemptStatus = "submitted"
	AttemptAutoSubmitted AttemptStatus = "auto_submitted"
	AttemptGraded        AttemptStatus = "graded"
)

type GradingStatus string

const (
	GradingPending        GradingStatus = "pending"
	GradingAutoGraded     GradingStatus = "auto_graded"
	GradingManualRequired GradingStatus = "manual_required"
)

type Exam struct {
	ID                    string
	DeploymentID          string
	Title                 string
	Instruction           *string
	StartsAt              time.Time
	EndsAt                time.Time
	DurationMinutes       int
	MaxAttempts           int
	ShuffleQuestions      bool
	ShuffleOptions        bool
	ShowResultImmediately bool
	PassingScore          *float64
	ResultSelectionPolicy string
	MaxScore              float64
	HasManualItems        bool
	AccessCodePrefix      string
	BundleChecksum        string
	LoadedAt              time.Time
}

type RubricCriterion struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	MaxPoints float64 `json:"max_points"`
	SortOrder int     `json:"sort_order"`
}

type Item struct {
	ID                    string
	ExamID                string
	SectionID             string
	SectionTitle          string
	SectionSortOrder      int
	SortOrder             int
	QuestionType          QuestionType
	PromptSnapshot        string
	OptionsSnapshotJSON   []map[string]interface{}
	AnswerKeySnapshotJSON map[string]interface{}
	RubricCriteria        []RubricCriterion
	Points                float64
	RequiresManualGrading bool
}

type Participant struct {
	ID              string
	StudentID       string
	StudentName     string
	AccessCode      string
	AttemptCount    int
	LatestAttemptID *string
}

type Attempt struct {
	ID              string
	ParticipantID   string
	StudentID       string
	ExamID          string
	AttemptNo       int
	Status          AttemptStatus
	StartedAt       time.Time
	DueAt           time.Time
	SubmittedAt     *time.Time
	AutoSubmittedAt *time.Time
	Score           *float64
	MaxScore        float64
	GradingStatus   GradingStatus
	HarvestedAt     *time.Time
}

func (a *Attempt) IsFinished() bool {
	return a.Status == AttemptSubmitted || a.Status == AttemptAutoSubmitted || a.Status == AttemptGraded
}

type Answer struct {
	ID            string
	AttemptID     string
	ItemID        string
	AnswerJSON    map[string]interface{}
	AnswerText    *string
	Score         *float64
	MaxScore      float64
	GradingStatus GradingStatus
	LastSavedAt   time.Time
	ClientSeq     int64
}

type IntegrityEvent struct {
	ID           string
	AttemptID    string
	StudentID    string
	EventType    string
	Description  *string
	MetadataJSON map[string]interface{}
	CreatedAt    time.Time
}
