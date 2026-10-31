package postgres

import (
	"fmt"

	"github.com/yalama34/orchestrator/internal/domain"
	"github.com/yalama34/orchestrator/internal/store"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func CreateTables(db *gorm.DB) error {
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		return fmt.Errorf("[DB] Failed to enable pgvector: %w", err)
	}
	if err := db.Exec("DROP INDEX IF EXISTS idx_artifacts_content").Error; err != nil {
		return fmt.Errorf("[DB] Failed to drop artifact content index: %w", err)
	}
	if err := db.AutoMigrate(
		&domain.Agent{},
		&domain.Run{},
		&domain.RunContext{},
		&domain.RunQuestion{},
		&domain.Task{},
		&domain.TaskDependency{},
		&domain.Artifact{},
		&store.DocumentChunk{},
		&store.MemoryChunk{},
	); err != nil {
		return fmt.Errorf("[DB] Failed AutoMigrate: %w", err)
	}
	return nil
}

func CreateDB() (*gorm.DB, error) {
	cfg, err := Load()
	if err != nil {
		return &gorm.DB{}, fmt.Errorf("[DB] Failed to load config: %w", err)
	}
	dsn := cfg.DSN()

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("[DB] Failed to open: %w", err)
	}
	if err := CreateTables(db); err != nil {
		return nil, fmt.Errorf("[DB] Failed to create tables: %w", err)
	}
	return db, nil
}
