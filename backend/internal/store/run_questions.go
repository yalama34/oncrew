package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRunNotWaiting = errors.New("[STORE] Run is not waiting for this question")

func (s *Store) CreateRunQuestion(ctx context.Context, runID uuid.UUID, question string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.Run{}).Where("id = ? AND status = ?", runID, domain.RunStatusRunning).Update("status", domain.RunStatusWaiting)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRunNotWaiting
		}
		return tx.Create(&domain.RunQuestion{ID: uuid.New(), RunID: runID, Question: question}).Error
	})
}

func (s *Store) ListRunQuestions(ctx context.Context, runID uuid.UUID) ([]domain.RunQuestion, error) {
	var questions []domain.RunQuestion
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Order("created_at ASC, id ASC").Find(&questions).Error; err != nil {
		return nil, fmt.Errorf("[STORE] Failed to list run questions: %w", err)
	}
	return questions, nil
}

func (s *Store) AnswerRunQuestion(ctx context.Context, runID, questionID uuid.UUID, answer string) (domain.Run, error) {
	var run domain.Run
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", runID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if run.Status != domain.RunStatusWaiting {
			return ErrRunNotWaiting
		}
		result := tx.Model(&domain.RunQuestion{}).Where("id = ? AND run_id = ? AND answer IS NULL", questionID, runID).Updates(map[string]interface{}{"answer": answer, "answered_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRunNotWaiting
		}
		return tx.Model(&run).Update("status", domain.RunStatusRunning).Error
	})
	return run, err
}
