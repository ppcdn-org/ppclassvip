package store

import (
	"context"
	"time"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

type MediaStatStore struct {
	db *gorm.DB
}

func NewMediaStatStore(db *gorm.DB) *MediaStatStore {
	return &MediaStatStore{db: db}
}

func (s *MediaStatStore) Create(ctx context.Context, stat *model.ClassroomMediaStat) error {
	return s.db.WithContext(ctx).Create(stat).Error
}

func (s *MediaStatStore) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	result := s.db.WithContext(ctx).
		Where("created_at < ?", before).
		Delete(&model.ClassroomMediaStat{})
	return result.RowsAffected, result.Error
}
