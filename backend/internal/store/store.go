package store

import (
	"errors"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("[STORE] Not found")

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) Store {
	return Store{db: db}
}
