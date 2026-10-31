package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) CreateArtifact(ctx context.Context, artifact *domain.Artifact) error {
	if err := s.db.WithContext(ctx).Create(artifact).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to create artifact: %w", err)
	}
	return nil
}

func (s *Store) GetArtifact(ctx context.Context, filename string) (domain.Artifact, error) {
	var a domain.Artifact
	err := s.db.WithContext(ctx).Where("filename = ?", filename).First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Artifact{}, ErrNotFound
		}
		return domain.Artifact{}, fmt.Errorf("[STORE] Failed to get artifact: %w", err)
	}
	return a, nil
}

func (s *Store) GetArtifactById(ctx context.Context, id uuid.UUID) (domain.Artifact, error) {
	var a domain.Artifact
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Artifact{}, ErrNotFound
		}
		return domain.Artifact{}, fmt.Errorf("[STORE] Failed to get artifact: %w", err)
	}
	return a, nil
}

func (s *Store) ListArtifactsByRun(ctx context.Context, runID uuid.UUID) ([]domain.Artifact, error) {
	var aList []domain.Artifact
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Find(&aList).Error; err != nil {
		return []domain.Artifact{}, fmt.Errorf("[STORE] Failed to list artifacts by run: %w", err)
	}
	return aList, nil

}

func (s *Store) ListArtifactsByTask(ctx context.Context, taskID uuid.UUID) ([]domain.Artifact, error) {
	var artifacts []domain.Artifact
	if err := s.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id").Find(&artifacts).Error; err != nil {
		return nil, fmt.Errorf("[STORE] Failed to list task artifacts: %w", err)
	}
	return artifacts, nil
}

func (s *Store) SaveTaskArtifact(ctx context.Context, artifact *domain.Artifact) error {
	var existing domain.Artifact
	err := s.db.WithContext(ctx).Where("task_id = ? AND filename = ?", artifact.TaskID, artifact.Filename).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.CreateArtifact(ctx, artifact)
	}
	if err != nil {
		return fmt.Errorf("[STORE] Failed to find task artifact: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{"content": artifact.Content, "language": artifact.Language}).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to update task artifact: %w", err)
	}
	return nil
}

func (s *Store) UpdateArtifact(ctx context.Context, filename, content string) error {
	if err := s.db.WithContext(ctx).Model(&domain.Artifact{}).Where("filename = ?", filename).Update("content", content).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to update artifact: %w", err)
	}
	return nil
}
