package model

import "time"

type Exam struct {
	ID                    string    `gorm:"primaryKey;type:varchar(36)"`
	DeploymentID          string    `gorm:"type:varchar(36);not null"`
	Title                 string    `gorm:"type:varchar(255);not null"`
	Instruction           *string   `gorm:"type:text"`
	StartsAt              time.Time `gorm:"type:datetime;not null"`
	EndsAt                time.Time `gorm:"type:datetime;not null"`
	DurationMinutes       int       `gorm:"not null"`
	MaxAttempts           int       `gorm:"not null"`
	ShuffleQuestions      bool      `gorm:"type:tinyint(1);not null"`
	ShuffleOptions        bool      `gorm:"type:tinyint(1);not null"`
	ShowResultImmediately bool      `gorm:"type:tinyint(1);not null"`
	PassingScore          *float64  `gorm:"type:decimal(5,2)"`
	ResultSelectionPolicy string    `gorm:"type:varchar(30);not null"`
	MaxScore              float64   `gorm:"type:decimal(8,2);not null"`
	HasManualItems        bool      `gorm:"type:tinyint(1);not null"`
	AccessCodePrefix      string    `gorm:"type:varchar(10);not null"`
	BundleChecksum        string    `gorm:"type:varchar(80);not null"`
	LoadedAt              time.Time `gorm:"type:datetime;not null"`
}

func (Exam) TableName() string { return "exams" }

type Item struct {
	ID                    string  `gorm:"primaryKey;type:varchar(36)"`
	ExamID                string  `gorm:"type:varchar(36);not null;default:'';index:idx_items_exam,priority:1"`
	SectionID             string  `gorm:"type:varchar(36);not null"`
	SectionTitle          string  `gorm:"type:varchar(255);not null"`
	SectionSortOrder      int     `gorm:"not null;default:0;index:idx_items_exam,priority:2"`
	SortOrder             int     `gorm:"not null;default:0;index:idx_items_exam,priority:3"`
	QuestionType          string  `gorm:"type:varchar(40);not null"`
	PromptSnapshot        string  `gorm:"type:text;not null"`
	OptionsSnapshotJSON   *string `gorm:"type:json"`
	AnswerKeySnapshotJSON *string `gorm:"type:json"`
	RubricCriteriaJSON    *string `gorm:"type:json"`
	Points                float64 `gorm:"type:decimal(8,2);not null"`
	RequiresManualGrading bool    `gorm:"type:tinyint(1);not null"`
}

func (Item) TableName() string { return "items" }

type Participant struct {
	ID              string  `gorm:"primaryKey;type:varchar(36)"`
	StudentID       string  `gorm:"type:varchar(36);not null"`
	StudentName     string  `gorm:"type:varchar(255);not null"`
	AccessCode      string  `gorm:"type:varchar(20);not null;uniqueIndex:uniq_participants_access_code"`
	AttemptCount    int     `gorm:"not null;default:0"`
	LatestAttemptID *string `gorm:"type:varchar(36)"`
}

func (Participant) TableName() string { return "participants" }

type Attempt struct {
	ID              string     `gorm:"primaryKey;type:varchar(36)"`
	ParticipantID   string     `gorm:"type:varchar(36);not null;index:idx_attempts_exam,priority:2"`
	StudentID       string     `gorm:"type:varchar(36);not null"`
	ExamID          string     `gorm:"type:varchar(36);not null;default:'';index:idx_attempts_exam,priority:1"`
	AttemptNo       int        `gorm:"not null;index:idx_attempts_exam,priority:3"`
	Status          string     `gorm:"type:varchar(30);not null;index"`
	StartedAt       time.Time  `gorm:"type:datetime;not null"`
	DueAt           time.Time  `gorm:"type:datetime;not null"`
	SubmittedAt     *time.Time `gorm:"type:datetime"`
	AutoSubmittedAt *time.Time `gorm:"type:datetime"`
	Score           *float64   `gorm:"type:decimal(8,2)"`
	MaxScore        float64    `gorm:"type:decimal(8,2);not null"`
	GradingStatus   string     `gorm:"type:varchar(30);not null"`
	HarvestedAt     *time.Time `gorm:"type:datetime;index"`
}

func (Attempt) TableName() string { return "attempts" }

type Answer struct {
	ID            string    `gorm:"primaryKey;type:varchar(36)"`
	AttemptID     string    `gorm:"type:varchar(36);not null;uniqueIndex:uniq_answers_attempt_item,priority:1"`
	ItemID        string    `gorm:"type:varchar(36);not null;uniqueIndex:uniq_answers_attempt_item,priority:2"`
	AnswerJSON    *string   `gorm:"type:json"`
	AnswerText    *string   `gorm:"type:text"`
	Score         *float64  `gorm:"type:decimal(8,2)"`
	MaxScore      float64   `gorm:"type:decimal(8,2);not null"`
	GradingStatus string    `gorm:"type:varchar(30);not null"`
	LastSavedAt   time.Time `gorm:"type:datetime;not null"`
	ClientSeq     int64     `gorm:"not null;default:0"`
}

func (Answer) TableName() string { return "answers" }
