package settings

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"ppclassvip/internal/config"
	"ppclassvip/internal/model"
	"ppclassvip/internal/store"
)

var ErrInvalidSetting = errors.New("invalid setting")

type Service struct {
	mu     sync.RWMutex
	values model.PlatformSetting
	store  *store.SettingsStore
	base   config.ClassroomConfig
}

func defaults(cfg config.ClassroomConfig) model.PlatformSetting {
	return model.PlatformSetting{
		ID:                      1,
		MaxFileBytes:            cfg.MaxFileBytes,
		MaxImageBytes:           cfg.MaxImageBytes,
		RoomQuotaBytes:          cfg.RoomQuotaBytes,
		RecordingEnabled:        true,
		ConsentTTLSeconds:       int(cfg.ConsentTTL.Std() / time.Second),
		IdleTTLSeconds:          int(cfg.IdleTTL.Std() / time.Second),
		AttachmentRetentionDays: int(cfg.AttachmentRetention.Std() / (24 * time.Hour)),
		MessageRatePerMinute:    cfg.MessageRatePerMinute,
		UploadRatePerMinute:     cfg.UploadRatePerMinute,
		InviteRatePerMinute:     cfg.InviteRatePerMinute,
		MaxRoomsPerTeacher:      3,
	}
}

func NewService(ctx context.Context, cfg config.ClassroomConfig, settingsStore *store.SettingsStore) (*Service, error) {
	service := &Service{values: defaults(cfg), store: settingsStore, base: cfg}
	if settingsStore == nil {
		return service, nil
	}
	stored, err := settingsStore.Get(ctx)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		service.values = normalize(*stored, cfg)
		return service, nil
	}
	if err := settingsStore.Save(ctx, &service.values); err != nil {
		return nil, err
	}
	return service, nil
}

func NewStatic(cfg config.ClassroomConfig) *Service {
	return &Service{values: defaults(cfg), base: cfg}
}

func (s *Service) Values() model.PlatformSetting {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values
}

func (s *Service) Classroom() config.ClassroomConfig {
	v := s.Values()
	return config.ClassroomConfig{
		InviteTTL:            s.base.InviteTTL,
		RoomTokenTTL:         s.base.RoomTokenTTL,
		ConsentTTL:           config.Duration(time.Duration(v.ConsentTTLSeconds) * time.Second),
		IdleTTL:              config.Duration(time.Duration(v.IdleTTLSeconds) * time.Second),
		MaxFileBytes:         v.MaxFileBytes,
		MaxImageBytes:        v.MaxImageBytes,
		RoomQuotaBytes:       v.RoomQuotaBytes,
		AttachmentRetention:  config.Duration(time.Duration(v.AttachmentRetentionDays) * 24 * time.Hour),
		MessageRatePerMinute: v.MessageRatePerMinute,
		UploadRatePerMinute:  v.UploadRatePerMinute,
		InviteRatePerMinute:  v.InviteRatePerMinute,
	}
}

func (s *Service) RecordingEnabled() bool {
	return s.Values().RecordingEnabled
}

func (s *Service) MaxRoomsPerTeacher() int {
	return s.Values().MaxRoomsPerTeacher
}

func (s *Service) Update(ctx context.Context, input model.PlatformSetting, userID uint64) (model.PlatformSetting, error) {
	if err := validate(input); err != nil {
		return model.PlatformSetting{}, err
	}
	input.ID = 1
	input.UpdatedBy = userID
	input.UpdatedAt = time.Now()
	normalized := normalize(input, s.Classroom())
	normalized.UpdatedBy = userID
	normalized.UpdatedAt = input.UpdatedAt
	if s.store != nil {
		if err := s.store.Save(ctx, &normalized); err != nil {
			return model.PlatformSetting{}, err
		}
	}
	s.mu.Lock()
	s.values = normalized
	s.mu.Unlock()
	return normalized, nil
}

func validate(v model.PlatformSetting) error {
	if v.MaxFileBytes <= 0 || v.MaxImageBytes <= 0 {
		return fmt.Errorf("%w: size limits must be positive", ErrInvalidSetting)
	}
	if v.RoomQuotaBytes < v.MaxFileBytes {
		return fmt.Errorf("%w: room quota must be at least max file size", ErrInvalidSetting)
	}
	if v.ConsentTTLSeconds < 10 || v.ConsentTTLSeconds > 600 {
		return fmt.Errorf("%w: consent TTL must be between 10 and 600 seconds", ErrInvalidSetting)
	}
	if v.IdleTTLSeconds < 60 {
		return fmt.Errorf("%w: idle TTL must be at least 60 seconds", ErrInvalidSetting)
	}
	if v.AttachmentRetentionDays < 1 {
		return fmt.Errorf("%w: retention must be at least 1 day", ErrInvalidSetting)
	}
	if v.MessageRatePerMinute < 1 || v.UploadRatePerMinute < 1 || v.InviteRatePerMinute < 1 {
		return fmt.Errorf("%w: rate limits must be positive", ErrInvalidSetting)
	}
	if v.MaxRoomsPerTeacher < 1 || v.MaxRoomsPerTeacher > 100 {
		return fmt.Errorf("%w: max rooms per teacher must be between 1 and 100", ErrInvalidSetting)
	}
	return nil
}

func normalize(v model.PlatformSetting, cfg config.ClassroomConfig) model.PlatformSetting {
	d := defaults(cfg)
	v.ID = 1
	if v.MaxFileBytes <= 0 {
		v.MaxFileBytes = d.MaxFileBytes
	}
	if v.MaxImageBytes <= 0 {
		v.MaxImageBytes = d.MaxImageBytes
	}
	if v.RoomQuotaBytes <= 0 {
		v.RoomQuotaBytes = d.RoomQuotaBytes
	}
	if v.ConsentTTLSeconds <= 0 {
		v.ConsentTTLSeconds = d.ConsentTTLSeconds
	}
	if v.IdleTTLSeconds <= 0 {
		v.IdleTTLSeconds = d.IdleTTLSeconds
	}
	if v.AttachmentRetentionDays <= 0 {
		v.AttachmentRetentionDays = d.AttachmentRetentionDays
	}
	if v.MessageRatePerMinute <= 0 {
		v.MessageRatePerMinute = d.MessageRatePerMinute
	}
	if v.UploadRatePerMinute <= 0 {
		v.UploadRatePerMinute = d.UploadRatePerMinute
	}
	if v.InviteRatePerMinute <= 0 {
		v.InviteRatePerMinute = d.InviteRatePerMinute
	}
	if v.MaxRoomsPerTeacher <= 0 {
		v.MaxRoomsPerTeacher = d.MaxRoomsPerTeacher
	}
	return v
}
