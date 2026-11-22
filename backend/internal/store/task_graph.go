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
var ErrUnplannedArtifact = errors.New("artifact was not declared for writing")
var ErrMissingTaskFile = errors.New("required task file missing")

func (s *Store) SaveTaskAttempt(ctx context.Context, taskID uuid.UUID, result string) error {
	return s.db.WithContext(ctx).Create(&domain.TaskAttempt{ID: uuid.New(), TaskID: taskID, Result: result}).Error
}

func (s *Store) SnapshotTaskFiles(ctx context.Context, task domain.Task) ([]domain.Artifact, error) {
	var artifacts []domain.Artifact
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", task.RunID.String()).Error; err != nil {
			return err
		}
		var files []domain.TaskFile
		if err := tx.Where("task_id = ?", task.ID).Find(&files).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", task.ID).Delete(&domain.TaskArtifactSource{}).Error; err != nil {
			return err
		}
		for _, file := range files {
			var artifact domain.Artifact
			err := tx.Where("run_id = ? AND filename = ?", task.RunID, file.Path).Order("COALESCE((SELECT MAX(created_at) FROM artifact_versions WHERE artifact_id = artifacts.id), 'epoch'::timestamptz) DESC").Order("id DESC").First(&artifact).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if file.Read {
					return fmt.Errorf("%w: %s", ErrMissingTaskFile, file.Path)
				}
				continue
			}
			if err != nil {
				return err
			}
			var latest domain.ArtifactVersion
			if err := tx.Where("artifact_id = ?", artifact.ID).Order("version DESC").First(&latest).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			version := latest.Version
			if version == 0 {
				version = 1
			}
			if err := tx.Create(&domain.TaskArtifactSource{TaskID: task.ID, ArtifactID: artifact.ID, Version: version}).Error; err != nil {
				return err
			}
			artifacts = append(artifacts, artifact)
		}
		return nil
	})
	return artifacts, err
}

func (s *Store) CreateTaskGraph(ctx context.Context, tasks []domain.Task, dependencies []domain.TaskDependency, sources []domain.TaskArtifactSource, files ...[]domain.TaskFile) error {
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
		if len(sources) > 0 {
			if err := tx.Create(&sources).Error; err != nil {
				return err
			}
		}
		if len(files) > 0 && len(files[0]) > 0 {
			if err := tx.Create(&files[0]).Error; err != nil {
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

func (s *Store) ListTaskFilesByRun(ctx context.Context, runID uuid.UUID) ([]domain.TaskFile, error) {
	var files []domain.TaskFile
	err := s.db.WithContext(ctx).Joins("JOIN tasks ON tasks.id = task_files.task_id").Where("tasks.run_id = ?", runID).Find(&files).Error
	return files, err
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
		var revision domain.RunRevision
		err := tx.Where("run_id = ? AND status = ?", task.RunID, domain.RunStatusRunning).Order("created_at DESC").First(&revision).Error
		var revisionID *uuid.UUID
		if err == nil {
			revisionID = &revision.ID
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", task.RunID.String()).Error; err != nil {
			return err
		}
		if task.FilePlan {
			var snapshots []domain.TaskArtifactSource
			if err := tx.Where("task_id = ?", task.ID).Find(&snapshots).Error; err != nil {
				return err
			}
			for _, snapshot := range snapshots {
				var source domain.Artifact
				if err := tx.Where("id = ? AND run_id = ?", snapshot.ArtifactID, task.RunID).First(&source).Error; err != nil {
					return err
				}
				var current domain.Artifact
				if err := tx.Where("run_id = ? AND filename = ?", task.RunID, source.Filename).Order("COALESCE((SELECT MAX(created_at) FROM artifact_versions WHERE artifact_id = artifacts.id), 'epoch'::timestamptz) DESC").Order("id DESC").First(&current).Error; err != nil {
					return err
				}
				if current.ID != source.ID {
					return fmt.Errorf("%w: %s changed since task started", ErrArtifactConflict, source.Filename)
				}
				var latest domain.ArtifactVersion
				if err := tx.Where("artifact_id = ?", snapshot.ArtifactID).Order("version DESC").First(&latest).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				version := latest.Version
				if version == 0 {
					version = 1
				}
				if version != snapshot.Version {
					return fmt.Errorf("%w: source %s changed since task started", ErrArtifactConflict, snapshot.ArtifactID)
				}
			}
		}
		for i := range artifacts {
			artifact := &artifacts[i]
			if task.FilePlan {
				var n int64
				if err := tx.Model(&domain.TaskFile{}).Where("task_id = ? AND path = ? AND write = ?", task.ID, artifact.Filename, true).Count(&n).Error; err != nil {
					return err
				}
				if n == 0 {
					return fmt.Errorf("%w: %s", ErrUnplannedArtifact, artifact.Filename)
				}
			}
			var existing domain.Artifact
			var source domain.TaskArtifactSource
			sourceErr := tx.Joins("JOIN artifacts ON artifacts.id = task_artifact_sources.artifact_id").Where("task_artifact_sources.task_id = ? AND artifacts.filename = ?", task.ID, artifact.Filename).First(&source).Error
			if sourceErr == nil {
				if task.FilePlan {
					var current domain.Artifact
					if err := tx.Where("run_id = ? AND filename = ?", task.RunID, artifact.Filename).Order("COALESCE((SELECT MAX(created_at) FROM artifact_versions WHERE artifact_id = artifacts.id), 'epoch'::timestamptz) DESC").Order("id DESC").First(&current).Error; err != nil {
						return err
					}
					if current.ID != source.ArtifactID {
						return fmt.Errorf("%w: %s changed since task started", ErrArtifactConflict, artifact.Filename)
					}
				}
				var latest domain.ArtifactVersion
				if err := tx.Where("artifact_id = ?", source.ArtifactID).Order("version DESC").First(&latest).Error; err != nil && err != gorm.ErrRecordNotFound {
					return err
				}
				currentVersion := 1
				if latest.ID != uuid.Nil {
					currentVersion = latest.Version
				}
				if currentVersion != source.Version {
					return fmt.Errorf("%w: %s changed since task assignment", ErrArtifactConflict, artifact.Filename)
				}
				if err := tx.First(&existing, "id = ?", source.ArtifactID).Error; err != nil {
					return err
				}
				err = nil
			} else if sourceErr != gorm.ErrRecordNotFound {
				return sourceErr
			} else {
				err = tx.Where("task_id = ? AND filename = ?", task.ID, artifact.Filename).First(&existing).Error
				if task.FilePlan {
					var count int64
					if qerr := tx.Model(&domain.Artifact{}).Where("run_id = ? AND filename = ?", task.RunID, artifact.Filename).Count(&count).Error; qerr != nil {
						return qerr
					}
					if count > 0 {
						return fmt.Errorf("%w: %s appeared since task started", ErrArtifactConflict, artifact.Filename)
					}
				}
			}
			var conflicts int64
			conflictQuery := tx.Model(&domain.Artifact{}).Where("run_id = ? AND filename = ? AND task_id <> ?", task.RunID, artifact.Filename, task.ID).Where("task_id NOT IN (SELECT depends_on_id FROM task_dependencies WHERE task_id = ?)", task.ID)
			if source.ArtifactID != uuid.Nil {
				conflictQuery = conflictQuery.Where("id <> ?", source.ArtifactID)
			}
			if !task.FilePlan {
				if err := conflictQuery.Count(&conflicts).Error; err != nil {
					return err
				}
			}
			if conflicts > 0 {
				return fmt.Errorf("%w: %s", ErrArtifactConflict, artifact.Filename)
			}
			if err == gorm.ErrRecordNotFound {
				if err = tx.Create(artifact).Error; err != nil {
					return err
				}
				version := domain.ArtifactVersion{ID: uuid.New(), RunID: artifact.RunID, ArtifactID: artifact.ID, TaskID: artifact.TaskID, Filename: artifact.Filename, Language: artifact.Language, Content: artifact.Content, Version: 1, RevisionID: revisionID}
				if err = tx.Create(&version).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if existing.Content != artifact.Content || existing.Language != artifact.Language {
				var count int64
				if err = tx.Model(&domain.ArtifactVersion{}).Where("artifact_id = ?", existing.ID).Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					initial := domain.ArtifactVersion{ID: uuid.New(), RunID: existing.RunID, ArtifactID: existing.ID, TaskID: existing.TaskID, Filename: existing.Filename, Language: existing.Language, Content: existing.Content, Version: 1}
					if err = tx.Create(&initial).Error; err != nil {
						return err
					}
					count = 1
				}
				version := domain.ArtifactVersion{ID: uuid.New(), RunID: artifact.RunID, ArtifactID: existing.ID, TaskID: task.ID, Filename: artifact.Filename, Language: artifact.Language, Content: artifact.Content, Version: int(count) + 1, RevisionID: revisionID}
				if err = tx.Create(&version).Error; err != nil {
					return err
				}
				if err = tx.Model(&existing).Updates(map[string]interface{}{"content": artifact.Content, "language": artifact.Language}).Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(&domain.Task{}).Where("id = ? AND status = ?", task.ID, domain.TaskStatusInProgress).Updates(map[string]interface{}{"summary": summary, "status": status, "model_phase": ""}).Error
	})
}
