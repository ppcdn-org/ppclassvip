package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	RoleAdmin   = "admin"
	RoleTeacher = "teacher"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type User struct {
	ID           uint64         `gorm:"primaryKey" json:"id"`
	Email        string         `gorm:"size:255;uniqueIndex;not null" json:"email"`
	PasswordHash string         `gorm:"size:255;not null" json:"-"`
	Nickname     string         `gorm:"size:64;not null" json:"nickname"`
	Role         string         `gorm:"size:32;not null;index" json:"role"`
	Status       string         `gorm:"size:32;not null;default:active;index" json:"status"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string {
	return "users"
}
