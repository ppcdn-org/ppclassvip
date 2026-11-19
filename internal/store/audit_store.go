package store

import (
	"context"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

type AuditStore struct {
	db *gorm.DB
}

func NewAuditStore(db *gorm.DB) *AuditStore {
	return &AuditStore{db: db}
}

func (s *AuditStore) Create(ctx context.Context, entry *model.ClassroomAuditLog) error {
	return s.db.WithContext(ctx).Create(entry).Error
}

func (s *AuditStore) ListByRoom(ctx context.Context, roomID uint64, limit int) ([]model.ClassroomAuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var entries []model.ClassroomAuditLog
	err := s.db.WithContext(ctx).
		Where("room_id = ?", roomID).
		Order("id DESC").
		Limit(limit).
		Find(&entries).Error
	return entries, err
}
