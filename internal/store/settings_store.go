package store

import (
	"context"
	"errors"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

type SettingsStore struct {
	db *gorm.DB
}

func NewSettingsStore(db *gorm.DB) *SettingsStore {
	return &SettingsStore{db: db}
}

func (s *SettingsStore) Get(ctx context.Context) (*model.PlatformSetting, error) {
	var setting model.PlatformSetting
	if err := s.db.WithContext(ctx).First(&setting, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &setting, nil
}

func (s *SettingsStore) Save(ctx context.Context, setting *model.PlatformSetting) error {
	setting.ID = 1
	return s.db.WithContext(ctx).Save(setting).Error
}
