package model

import "time"

// HarvestLog is the append-only audit trail of push attempts: which batches
// went out, when, and which failed. The runbook reads it to answer "which
// attempts were pushed when and which failed".
type HarvestLog struct {
	ID            int64      `gorm:"primaryKey;autoIncrement"`
	AttemptID     string     `gorm:"type:varchar(36);not null;index:idx_harvest_attempt"`
	DeploymentID  string     `gorm:"type:varchar(36);not null;default:''"`
	PushedAt      time.Time  `gorm:"type:datetime;not null"`
	AckedAt       *time.Time `gorm:"type:datetime"`
	AttemptsCount int        `gorm:"not null;default:0"`
	LastError     *string    `gorm:"type:text"`
}

func (HarvestLog) TableName() string { return "harvest_log" }
