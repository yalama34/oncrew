package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) CreateRun(ctx context.Context, run *domain.Run) error {
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to create run: %w", err)
	}
	return nil
}

func (s *Store) GetRun(ctx context.Context, id uuid.UUID) (domain.Run, error) {
	var r domain.Run
	err := s.db.WithContext(ctx).Preload("Agents").Where(&domain.Run{ID: id}).First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Run{}, ErrNotFound
		}
		return domain.Run{}, fmt.Errorf("[STORE] Failed to get run: %w", err)
	}
	return r, nil
}

func (s *Store) GetAllRuns(ctx context.Context) ([]domain.Run, error) {
	var runs []domain.Run
	if err := s.db.Model(&domain.Run{}).Preload("Agents").Find(&runs).Error; err != nil {
		return []domain.Run{}, fmt.Errorf("[STORE] Failed to get all runs: %w", err)
	}
	return runs, nil
}

func (s *Store) UpdateRunStatus(ctx context.Context, id uuid.UUID, status domain.RunStatus) error {
	if err := s.db.WithContext(ctx).Model(&domain.Run{}).Where("id = ?", id).Update("status", status).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to update run status: %w", err)
	}
	return nil
}

func (s *Store) UpdateSpecStatus(ctx context.Context, id uuid.UUID, status, message string) error {
	return s.db.WithContext(ctx).Model(&domain.Run{}).Where("id = ?", id).Updates(map[string]interface{}{"spec_status": status, "spec_error": message}).Error
}

func (s *Store) FailRun(ctx context.Context, id uuid.UUID, reason string) error {
	return s.db.WithContext(ctx).Model(&domain.Run{}).Where("id = ?", id).Updates(map[string]interface{}{"status": domain.RunStatusFailed, "failure_reason": reason}).Error
}

func (s *Store) IncrementLeadSteps(ctx context.Context, id uuid.UUID, limit int) (bool, error) {
	result := s.db.WithContext(ctx).Model(&domain.Run{}).Where("id = ? AND lead_steps < ?", id, limit).Update("lead_steps", gorm.Expr("lead_steps + 1"))
	return result.RowsAffected == 1, result.Error
}

func (s *Store) SetRunAgents(ctx context.Context, id uuid.UUID, agents []domain.Agent) error {
	var r domain.Run
	if err := s.db.WithContext(ctx).Where(&domain.Run{ID: id}).First(&r).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to get run for set agents: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&r).Association("Agents").Replace(agents); err != nil {
		return fmt.Errorf("[STORE] Failed to set run agents: %w", err)
	}
	return nil
}
