package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
)

func (s *Store) ListPreviewArtifacts(ctx context.Context, runID uuid.UUID) ([]domain.Artifact, error) {
	var artifacts []domain.Artifact
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run domain.Run
		if err := tx.Select("status").First(&run, "id = ?", runID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrNotFound
			}
			return err
		}
		if run.Status != domain.RunStatusFinished {
			return ErrRunNotFinished
		}
		return tx.Model(&domain.Artifact{}).
			Select("DISTINCT ON (artifacts.filename) artifacts.filename, artifacts.content").
			Joins("LEFT JOIN LATERAL (SELECT created_at FROM artifact_versions WHERE artifact_id = artifacts.id ORDER BY version DESC LIMIT 1) AS latest ON true").
			Where("artifacts.run_id = ?", runID).
			Order("artifacts.filename, latest.created_at DESC NULLS LAST, artifacts.id DESC").
			Find(&artifacts).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return artifacts, err
}
