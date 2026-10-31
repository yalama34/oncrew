package domain

import (
	"github.com/google/uuid"
)

type Artifact struct {
	ID       uuid.UUID `gorm:"type:uuid;not null;primaryKey"`
	RunID    uuid.UUID `gorm:"type:uuid;not null;index"`
	TaskID   uuid.UUID `gorm:"type:uuid;not null;index"`
	Filename string    `gorm:"type:text;not null;index"`
	Language string    `gorm:"type:text;not null;index"`
	Content  string    `gorm:"type:text;not null"`
}
