package domain

import (
	"github.com/google/uuid"
)

type Agent struct {
	ID      uuid.UUID `gorm:"primaryKey"`
	Name    string    `gorm:"type:text"`
	Model   string    `gorm:"type:text"`
	BaseURL string    `gorm:"type:text"`
	APIKey  string    `gorm:"type:text"`
	BotKey  string    `gorm:"type:text"`
	Role    Role      `gorm:"embedded;embeddedPrefix:role_"`
}

type RoleName string

const (
	Teamlead RoleName = "teamlead"
	Frontend RoleName = "frontend"
	Backend  RoleName = "backend"
	DevOps   RoleName = "devops"
)

type Role struct {
	Name         RoleName `gorm:"type:text"`
	SystemPrompt string   `gorm:"type:text"`
}
