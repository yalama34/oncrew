package domain

import (
	"github.com/google/uuid"
	"time"
)

type RunRevision struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"`
	RunID         uuid.UUID `gorm:"type:uuid;not null;index"`
	Text          string    `gorm:"type:text;not null"`
	Status        RunStatus `gorm:"type:text;not null;index"`
	FailureReason string    `gorm:"type:text"`
	LeadSteps     int       `gorm:"not null;default:0"`
	WorkStarted   bool      `gorm:"not null;default:false"`
	Summary       string    `gorm:"type:text"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
