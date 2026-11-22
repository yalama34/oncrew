package domain

import (
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	TaskStatusTodo       TaskStatus = "todo"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusReview     TaskStatus = "review"
	TaskStatusDone       TaskStatus = "done"
	TaskStatusBlocked    TaskStatus = "blocked"
)

type Task struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primaryKey"`
	RunID               uuid.UUID  `gorm:"type:uuid;not null;index"`
	Status              TaskStatus `gorm:"type:text;not null;index"`
	Text                string     `gorm:"type:text;not null;index"`
	Details             string     `gorm:"type:text"`
	Summary             string     `gorm:"type:text"`
	Feedback            string     `gorm:"type:text; index"`
	ModelPhase          string     `gorm:"type:text"`
	ModelStartedAt      *time.Time
	ModelLastActivityAt *time.Time
	FailureReason       string     `gorm:"type:text"`
	MemoryStatus        string     `gorm:"type:text"`
	MemoryError         string     `gorm:"type:text"`
	MemoryVersion       int64      `gorm:"not null;default:0"`
	FilePlan            bool       `gorm:"not null;default:false"`
	AssigneeID          *uuid.UUID `gorm:"type:uuid;index"`
	Assignee            *Agent     `gorm:"foreignKey:AssigneeID"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type TaskFile struct {
	TaskID uuid.UUID `gorm:"type:uuid;primaryKey"`
	Path   string    `gorm:"type:text;primaryKey"`
	Read   bool      `gorm:"not null"`
	Write  bool      `gorm:"not null"`
}

type TaskAttempt struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	TaskID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Result    string    `gorm:"type:text;not null"`
	CreatedAt time.Time
}

type TaskDependency struct {
	TaskID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	DependsOnID uuid.UUID `gorm:"type:uuid;primaryKey"`
}

type TaskArtifactSource struct {
	TaskID     uuid.UUID `gorm:"type:uuid;primaryKey"`
	ArtifactID uuid.UUID `gorm:"type:uuid;primaryKey"`
	Version    int       `gorm:"not null"`
}
