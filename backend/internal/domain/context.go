package domain

import (
	"time"

	"github.com/google/uuid"
)

type RunContext struct {
	RunID     uuid.UUID `gorm:"type:uuid;not null;primaryKey"`
	Content   string    `gorm:"type:text;not null;index"`
	UpdatedAt time.Time
	UpdatedBy *uuid.UUID `gorm:"type:uuid"`
}
