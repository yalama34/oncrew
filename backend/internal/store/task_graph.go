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

var ErrArtifactConflict = errors.New("artifact filename conflict")

func (s *Store) CreateTaskGraph(ctx context.Context, tasks []domain.Task, dependencies []domain.TaskDependency) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(tasks) > 0 {
			if err := tx.Create(&tasks).Error; err != nil {
				return err
			}
		}
		if len(dependencies) > 0 {
			if err := tx.Create(&dependencies).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListTaskDependencies(ctx context.Context, runID uuid.UUID) ([]domain.TaskDependency, error) {
	var dependencies []domain.TaskDependency
	err := s.db.WithContext(ctx).Model(&domain.TaskDependency{}).Joins("JOIN tasks ON tasks.id = task_dependencies.task_id").Where("tasks.run_id = ?", runID).Find(&dependencies).Error
	if err != nil {
		return nil, fmt.Errorf("[STORE] Failed to list task dependencies: %w", err)
	}
	return dependencies, nil
}

func (s *Store) ClaimTask(ctx context.Context, id uuid.UUID) (bool, error) {
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ? AND status = ?", id, domain.TaskStatusTodo).Updates(map[string]interface{}{"status": domain.TaskStatusInProgress, "model_phase": "waiting", "model_started_at": now, "model_last_activity_at": nil, "failure_reason": ""})
	if result.Error != nil {
		return false, fmt.Errorf("[STORE] Failed to claim task: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (s *Store) SaveTaskOutput(ctx context.Context, task domain.Task, artifacts []domain.Artifact, summary string, status domain.TaskStatus) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", task.RunID.String()).Error; err != nil {
			return err
		}
		for i := range artifacts {
			artifact := &artifacts[i]
			var conflicts int64
			err := tx.Model(&domain.Artifact{}).Where("run_id = ? AND filename = ? AND task_id <> ?", task.RunID, artifact.Filename, task.ID).Where("task_id NOT IN (SELECT depends_on_id FROM task_dependencies WHERE task_id = ?)", task.ID).Count(&conflicts).Error
			if err != nil {
				return err
			}
			if conflicts > 0 {
				return fmt.Errorf("%w: %s", ErrArtifactConflict, artifact.Filename)
			}
			var existing domain.Artifact
			err = tx.Where("task_id = ? AND filename = ?", task.ID, artifact.Filename).First(&existing).Error
			if err == gorm.ErrRecordNotFound {
				if err = tx.Create(artifact).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err = tx.Model(&existing).Updates(map[string]interface{}{"content": artifact.Content, "language": artifact.Language}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&domain.Task{}).Where("id = ? AND status = ?", task.ID, domain.TaskStatusInProgress).Updates(map[string]interface{}{"summary": summary, "status": status, "model_phase": ""}).Error
	})
}
