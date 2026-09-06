package model

import "time"

type AttemptResetOperation struct {
	RequestID               string     `gorm:"primaryKey;type:varchar(100)"`
	Operation               string     `gorm:"type:varchar(20);not null"`
	CommandHash             string     `gorm:"type:char(64);not null"`
	Status                  string     `gorm:"type:varchar(30);not null;index"`
	Code                    string     `gorm:"type:varchar(60);not null;default:''"`
	Message                 string     `gorm:"type:varchar(255);not null;default:''"`
	NodeID                  string     `gorm:"type:varchar(100);not null;index"`
	DeploymentID            string     `gorm:"type:varchar(36);not null;index"`
	ExamID                  string     `gorm:"type:varchar(36);not null;index"`
	StudentID               string     `gorm:"type:varchar(36);not null;index"`
	ParticipantID           string     `gorm:"type:varchar(36);not null;index"`
	AttemptID               string     `gorm:"type:varchar(36);not null;index"`
	ExpectedGeneration      int64      `gorm:"not null;default:0"`
	TargetGeneration        int64      `gorm:"not null;default:0"`
	ExpectedStatus          string     `gorm:"type:varchar(30);not null"`
	ExpectedDueAt           *time.Time `gorm:"type:datetime"`
	ExpectedSubmittedAt     *time.Time `gorm:"type:datetime"`
	ExpectedAutoSubmittedAt *time.Time `gorm:"type:datetime"`
	Deadline                time.Time  `gorm:"type:datetime;not null"`
	AttemptNo               int        `gorm:"not null;default:0"`
	AttemptStatus           string     `gorm:"type:varchar(30);not null;default:''"`
	StartedAt               *time.Time `gorm:"type:datetime"`
	DueAt                   *time.Time `gorm:"type:datetime"`
	SubmittedAt             *time.Time `gorm:"type:datetime"`
	AutoSubmittedAt         *time.Time `gorm:"type:datetime"`
	CreatedAt               time.Time  `gorm:"type:datetime;not null"`
	UpdatedAt               time.Time  `gorm:"type:datetime;not null"`
}

func (AttemptResetOperation) TableName() string { return "attempt_reset_operations" }
