package domain

import (
	"time"

	"github.com/google/uuid"
)

type RunQuestion struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	RunID      uuid.UUID `gorm:"type:uuid;not null;index"`
	Question   string    `gorm:"type:text;not null"`
	Answer     *string   `gorm:"type:text"`
	CreatedAt  time.Time
	AnsweredAt *time.Time
}
