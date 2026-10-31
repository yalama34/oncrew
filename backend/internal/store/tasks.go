package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) CreateTask(ctx context.Context, task *domain.Task) error {
	if err := s.db.WithContext(ctx).Create(task).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to create task: %w", err)
	}
	return nil
}

func (s *Store) GetTask(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	var t domain.Task
	err := s.db.WithContext(ctx).Where(&domain.Task{ID: id}).First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Task{}, ErrNotFound
		}
		return domain.Task{}, fmt.Errorf("[STORE] Failed to get task: %w", err)
	}
	return t, nil
}

func (s *Store) UpdateTaskStatus(ctx context.Context, id uuid.UUID, status domain.TaskStatus) error {
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ?", id).Update("status", status).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to update task status: %w", err)
	}
	return nil
}

func (s *Store) StartTaskModel(ctx context.Context, id uuid.UUID, started time.Time) error {
	return s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ?", id).Updates(map[string]interface{}{"status": domain.TaskStatusInProgress, "model_phase": "waiting", "model_started_at": started, "model_last_activity_at": nil, "failure_reason": ""}).Error
}

func (s *Store) UpdateTaskModelActivity(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ? AND status = ?", id, domain.TaskStatusInProgress).Updates(map[string]interface{}{"model_phase": "receiving", "model_last_activity_at": at}).Error
}

func (s *Store) FailTask(ctx context.Context, id uuid.UUID, reason string) error {
	return s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ?", id).Updates(map[string]interface{}{"status": domain.TaskStatusBlocked, "model_phase": "", "failure_reason": reason}).Error
}

func (s *Store) SetTaskFeedback(ctx context.Context, id uuid.UUID, feedback string) error {
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ?", id).Update("feedback", feedback).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to set task feedback: %w", err)
	}
	return nil
}

func (s *Store) SetTaskResult(ctx context.Context, id uuid.UUID, summary string, status domain.TaskStatus) error {
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ?", id).Updates(map[string]interface{}{"summary": summary, "status": status, "model_phase": ""}).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to save task result: %w", err)
	}
	return nil
}

func (s *Store) AssignTask(ctx context.Context, id uuid.UUID, assignee *domain.Agent) error {
	var t domain.Task
	if err := s.db.WithContext(ctx).Where(&domain.Task{ID: id}).First(&t).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to get task for assign: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&t).Update("assignee_id", assignee.ID).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to assign task: %w", err)
	}
	return nil
}

func (s *Store) ListTasksByRun(ctx context.Context, runID uuid.UUID) ([]domain.Task, error) {
	var tasks []domain.Task
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("[STORE] Failed to list tasks by run: %w", err)
	}
	return tasks, nil
}
