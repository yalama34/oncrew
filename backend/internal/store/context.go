package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) UpsertContext(ctx context.Context, rc domain.RunContext) error {
	if err := s.db.WithContext(ctx).Save(&rc).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to upsert context: %w", err)
	}
	return nil
}

func (s *Store) GetContext(ctx context.Context, runID uuid.UUID) (domain.RunContext, error) {
	var rc domain.RunContext
	err := s.db.WithContext(ctx).Where(&domain.RunContext{RunID: runID}).First(&rc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.RunContext{}, ErrNotFound
		}
		return domain.RunContext{}, fmt.Errorf("[STORE] Failed to get context: %w", err)
	}
	return rc, nil
}
