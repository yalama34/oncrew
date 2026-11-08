package domain

import (
	"github.com/google/uuid"
	"time"
)

type ArtifactVersion struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey"`
	RunID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	ArtifactID uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_artifact_version"`
	TaskID     uuid.UUID  `gorm:"type:uuid;not null;index"`
	Filename   string     `gorm:"type:text;not null"`
	Language   string     `gorm:"type:text;not null"`
	Content    string     `gorm:"type:text;not null"`
	Version    int        `gorm:"not null;uniqueIndex:idx_artifact_version"`
	RevisionID *uuid.UUID `gorm:"type:uuid;index"`
	CreatedAt  time.Time
}
