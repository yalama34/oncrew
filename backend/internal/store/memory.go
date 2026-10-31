package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMemoryStale = errors.New("[STORE] Memory version is stale")
var ErrMemoryNotRetryable = errors.New("[STORE] Task memory cannot be retried")

type MemoryChunk struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey"`
	RunID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	TaskID     uuid.UUID  `gorm:"type:uuid;not null;index"`
	ArtifactID *uuid.UUID `gorm:"type:uuid;index"`
	TaskTitle  string     `gorm:"type:text;not null"`
	Source     string     `gorm:"type:text;not null"`
	Filename   string     `gorm:"type:text"`
	Content    string     `gorm:"type:text;not null"`
	Model      string     `gorm:"type:text;not null"`
	Version    int64      `gorm:"not null"`
	Embedding  Vector     `gorm:"type:vector;not null"`
}

func (s *Store) AcceptTask(ctx context.Context, id uuid.UUID) (domain.Task, bool, error) {
	var task domain.Task
	accepted := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", id).Error; err != nil {
			return err
		}
		if task.Status == domain.TaskStatusDone {
			return nil
		}
		if task.Status != domain.TaskStatusReview {
			return fmt.Errorf("[STORE] Task %s is not in review", id)
		}
		task.Status = domain.TaskStatusDone
		task.MemoryVersion++
		task.MemoryStatus = "pending"
		task.MemoryError = ""
		if err := tx.Model(&task).Updates(map[string]interface{}{"status": task.Status, "memory_version": task.MemoryVersion, "memory_status": task.MemoryStatus, "memory_error": ""}).Error; err != nil {
			return err
		}
		accepted = true
		return nil
	})
	return task, accepted, err
}

func (s *Store) RetryTaskMemory(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	var task domain.Task
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if task.Status != domain.TaskStatusDone || task.MemoryStatus == "pending" {
			return ErrMemoryNotRetryable
		}
		task.MemoryVersion++
		task.MemoryStatus = "pending"
		task.MemoryError = ""
		return tx.Model(&task).Updates(map[string]interface{}{"memory_version": task.MemoryVersion, "memory_status": task.MemoryStatus, "memory_error": ""}).Error
	})
	return task, err
}

func (s *Store) PendingTaskMemories(ctx context.Context) ([]domain.Task, error) {
	var tasks []domain.Task
	err := s.db.WithContext(ctx).Where("status = ? AND memory_status = ?", domain.TaskStatusDone, "pending").Find(&tasks).Error
	return tasks, err
}

func (s *Store) SaveTaskMemory(ctx context.Context, taskID uuid.UUID, version int64, chunks []MemoryChunk) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task domain.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != domain.TaskStatusDone || task.MemoryVersion != version || task.MemoryStatus != "pending" {
			return ErrMemoryStale
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&MemoryChunk{}).Error; err != nil {
			return err
		}
		if len(chunks) > 0 {
			if err := tx.CreateInBatches(chunks, 8).Error; err != nil {
				return err
			}
		}
		return tx.Model(&task).Updates(map[string]interface{}{"memory_status": "ready", "memory_error": ""}).Error
	})
}

func (s *Store) FailTaskMemory(ctx context.Context, taskID uuid.UUID, version int64, message string) error {
	return s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ? AND status = ? AND memory_version = ? AND memory_status = ?", taskID, domain.TaskStatusDone, version, "pending").Updates(map[string]interface{}{"memory_status": "failed", "memory_error": message}).Error
}

func (s *Store) memoryQuery(ctx context.Context, runID uuid.UUID, model string, exclude []uuid.UUID) *gorm.DB {
	query := s.db.WithContext(ctx).Model(&MemoryChunk{}).Joins("JOIN tasks ON tasks.id = memory_chunks.task_id").Where("memory_chunks.run_id = ? AND tasks.run_id = ? AND tasks.status = ? AND tasks.memory_status = ? AND tasks.memory_version = memory_chunks.version AND memory_chunks.model = ?", runID, runID, domain.TaskStatusDone, "ready", model)
	if len(exclude) > 0 {
		query = query.Where("memory_chunks.task_id NOT IN ?", exclude)
	}
	return query
}

func (s *Store) HasTaskMemory(ctx context.Context, runID uuid.UUID, model string, exclude []uuid.UUID) (bool, error) {
	var count int64
	err := s.memoryQuery(ctx, runID, model, exclude).Count(&count).Error
	return count > 0, err
}

func (s *Store) FindTaskMemory(ctx context.Context, runID uuid.UUID, model string, exclude []uuid.UUID, embedding Vector, limit int) ([]MemoryChunk, error) {
	var chunks []MemoryChunk
	err := s.memoryQuery(ctx, runID, model, exclude).Select("memory_chunks.*").Where("vector_dims(memory_chunks.embedding) = ?", len(embedding)).Order(clause.OrderBy{Expression: clause.Expr{SQL: "memory_chunks.embedding <=> ?::vector", Vars: []interface{}{embedding}}}).Limit(limit).Find(&chunks).Error
	if err != nil {
		return nil, fmt.Errorf("[STORE] Failed to search task memory: %w", err)
	}
	return chunks, nil
}
