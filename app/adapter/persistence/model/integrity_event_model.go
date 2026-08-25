package model

import "time"

type IntegrityEvent struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)"`
	AttemptID    string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_integrity_attempt,priority:1"`
	StudentID    string    `gorm:"type:varchar(36);not null"`
	EventType    string    `gorm:"type:varchar(40);not null;uniqueIndex:idx_integrity_attempt,priority:2"`
	Description  *string   `gorm:"type:text"`
	MetadataJSON *string   `gorm:"type:json"`
	CreatedAt    time.Time `gorm:"type:datetime;not null;uniqueIndex:idx_integrity_attempt,priority:3"`
}

func (IntegrityEvent) TableName() string { return "integrity_events" }
