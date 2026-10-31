package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) CreateAgent(ctx context.Context, agent *domain.Agent) error {
	if err := s.db.WithContext(ctx).Create(agent).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to create agent: %w", err)
	}
	return nil
}

func (s *Store) GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error) {
	var a domain.Agent
	err := s.db.WithContext(ctx).Where(&domain.Agent{ID: id}).First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Agent{}, ErrNotFound
		}
		return domain.Agent{}, fmt.Errorf("[STORE] Failed to get agent: %w", err)
	}
	return a, nil
}

func (s *Store) GetAgentByRole(ctx context.Context, role domain.RoleName) (domain.Agent, error) {
	var a domain.Agent
	err := s.db.WithContext(ctx).
		Where("role_name = ?", role).
		First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Agent{}, ErrNotFound
		}
		return domain.Agent{}, fmt.Errorf("[STORE] Failed to get agent by role: %w", err)
	}
	return a, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	var agents []domain.Agent
	if err := s.db.WithContext(ctx).Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("[STORE] Failed to list agents: %w", err)
	}
	return agents, nil
}

func (s *Store) ListAgentsByRun(ctx context.Context, runID uuid.UUID) ([]domain.Agent, error) {
	var run domain.Run
	if err := s.db.WithContext(ctx).Preload("Agents").First(&run, "id = ?", runID).Error; err != nil {
		return nil, fmt.Errorf("[STORE] Failed to list agents by run: %w", err)
	}
	return run.Agents, nil
}
