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

var ErrRunNotFinished = errors.New("run is not finished")

func (s *Store) CreateRevision(ctx context.Context, runID uuid.UUID, text string) (domain.RunRevision, error) {
	var revision domain.RunRevision
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run domain.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		if run.Status != domain.RunStatusFinished {
			return ErrRunNotFinished
		}
		var active int64
		if err := tx.Model(&domain.RunRevision{}).Where("run_id = ? AND status IN ?", runID, []string{"running", "waiting"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrRunNotFinished
		}
		revision = domain.RunRevision{ID: uuid.New(), RunID: runID, Text: text, Status: domain.RunStatusRunning}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		return tx.Model(&run).Updates(map[string]interface{}{"status": domain.RunStatusRunning, "failure_reason": ""}).Error
	})
	if err != nil {
		return domain.RunRevision{}, fmt.Errorf("[STORE] Failed to create revision: %w", err)
	}
	return revision, nil
}

func (s *Store) GetRevision(ctx context.Context, id uuid.UUID) (domain.RunRevision, error) {
	var revision domain.RunRevision
	err := s.db.WithContext(ctx).First(&revision, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return revision, ErrNotFound
	}
	return revision, err
}

func (s *Store) ListRevisions(ctx context.Context, runID uuid.UUID) ([]domain.RunRevision, error) {
	var revisions []domain.RunRevision
	err := s.db.WithContext(ctx).Where("run_id = ?", runID).Order("created_at, id").Find(&revisions).Error
	return revisions, err
}

func (s *Store) ActiveRevision(ctx context.Context, runID uuid.UUID) (domain.RunRevision, error) {
	var revision domain.RunRevision
	err := s.db.WithContext(ctx).Where("run_id = ? AND status IN ?", runID, []string{"running", "waiting"}).Order("created_at DESC").First(&revision).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return revision, ErrNotFound
	}
	return revision, err
}

func (s *Store) UpdateRevisionStatus(ctx context.Context, id uuid.UUID, status domain.RunStatus, summary, reason string) error {
	return s.db.WithContext(ctx).Model(&domain.RunRevision{}).Where("id = ?", id).Updates(map[string]interface{}{"status": status, "summary": summary, "failure_reason": reason}).Error
}

func (s *Store) IncrementRevisionSteps(ctx context.Context, id uuid.UUID, limit int) (bool, error) {
	r := s.db.WithContext(ctx).Model(&domain.RunRevision{}).Where("id = ? AND lead_steps < ?", id, limit).Update("lead_steps", gorm.Expr("lead_steps + 1"))
	return r.RowsAffected == 1, r.Error
}

func (s *Store) MarkRevisionWork(ctx context.Context, runID uuid.UUID) error {
	return s.db.WithContext(ctx).Model(&domain.RunRevision{}).Where("run_id = ? AND status = ?", runID, domain.RunStatusRunning).Update("work_started", true).Error
}
