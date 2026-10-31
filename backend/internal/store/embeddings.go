package store

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/yalama34/orchestrator/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DocumentChunk struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	RunID      uuid.UUID `gorm:"type:uuid;not null"`
	DocumentID uuid.UUID `gorm:"type:uuid;not null"`
	Heading    string    `gorm:"type:text;not null"`
	Content    string    `gorm:"type:text;not null"`
	Model      string    `gorm:"type:text;not null"`
	Embedding  Vector    `gorm:"type:vector;not null"`
}

type Vector []float64

func (v Vector) Value() (driver.Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (v *Vector) Scan(value interface{}) error {
	var data []byte
	switch value := value.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return fmt.Errorf("[STORE] Invalid vector type %T", value)
	}
	return json.Unmarshal(data, v)
}

func (s *Store) CreateDocumentChunk(ctx context.Context, chunk *DocumentChunk) error {
	if chunk == nil || chunk.ID == uuid.Nil || chunk.RunID == uuid.Nil || chunk.DocumentID == uuid.Nil || chunk.Content == "" || chunk.Model == "" || len(chunk.Embedding) == 0 {
		return fmt.Errorf("[STORE] Invalid document chunk")
	}

	if err := s.db.WithContext(ctx).Create(chunk).Error; err != nil {
		return fmt.Errorf("[STORE] Failed to create document chunk: %w", err)
	}
	return nil
}

func (s *Store) SaveDocumentChunks(ctx context.Context, runID uuid.UUID, chunks []DocumentChunk) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", runID).Delete(&DocumentChunk{}).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(chunks, 8).Error; err != nil {
			return err
		}
		return tx.Model(&domain.Run{}).Where("id = ?", runID).Updates(map[string]interface{}{"spec_status": "ready", "spec_error": ""}).Error
	})
}

func (s *Store) FindDocumentChunks(ctx context.Context, runID uuid.UUID, model string, embedding Vector, limit int) ([]DocumentChunk, error) {
	if len(embedding) == 0 {
		return nil, fmt.Errorf("[STORE] Empty query embedding")
	}
	for _, value := range embedding {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("[STORE] Invalid query embedding")
		}
	}
	var chunks []DocumentChunk
	err := s.db.WithContext(ctx).Where("run_id = ? AND model = ? AND vector_dims(embedding) = ?", runID, model, len(embedding)).Order(clause.OrderBy{Expression: clause.Expr{SQL: "embedding <=> ?::vector", Vars: []interface{}{embedding}}}).Limit(limit).Find(&chunks).Error
	if err != nil {
		return nil, fmt.Errorf("[STORE] Failed to search document chunks: %w", err)
	}
	return chunks, nil
}
