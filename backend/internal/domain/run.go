package domain

import (
	"time"

	"github.com/google/uuid"
)

type RunStatus string

const (
	RunStatusPending  RunStatus = "pending"
	RunStatusRunning  RunStatus = "running"
	RunStatusWaiting  RunStatus = "waiting"
	RunStatusFinished RunStatus = "finished"
	RunStatusFailed   RunStatus = "failed"
)

type Run struct {
	ID            uuid.UUID `gorm:"type:uuid;not null;primaryKey"`
	Goal          string    `gorm:"type:text;not null;index"`
	SpecText      string    `gorm:"type:text"`
	SpecName      string    `gorm:"type:text"`
	SpecStatus    string    `gorm:"type:text"`
	SpecError     string    `gorm:"type:text"`
	FailureReason string    `gorm:"type:text"`
	LeadSteps     int       `gorm:"not null;default:0"`
	Status        RunStatus `gorm:"type:text;not null;index"`
	LeadID        uuid.UUID `gorm:"type:uuid;not null;index"`
	Lead          *Agent    `gorm:"foreignKey:LeadID"`
	Agents        []Agent   `gorm:"many2many:run_agents"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
