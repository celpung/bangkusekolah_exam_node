package model

import "time"

type RosterEventReceipt struct {
	EventID       string    `gorm:"primaryKey;type:varchar(36)"`
	DeploymentID  string    `gorm:"not null;type:varchar(36);uniqueIndex:uniq_roster_receipt_deployment_revision,priority:1;index"`
	ExamID        string    `gorm:"not null;type:varchar(36);index"`
	Revision      int64     `gorm:"not null;uniqueIndex:uniq_roster_receipt_deployment_revision,priority:2"`
	ParticipantID string    `gorm:"not null;type:varchar(36);index"`
	PayloadHash   string    `gorm:"not null;type:char(64)"`
	Status        string    `gorm:"not null;type:varchar(20);index"`
	OutcomeCode   string    `gorm:"not null;type:varchar(60);default:''"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"not null;autoUpdateTime"`
}

func (RosterEventReceipt) TableName() string { return "roster_event_receipts" }
