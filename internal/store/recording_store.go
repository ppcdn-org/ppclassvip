package store

import (
	"context"
	"errors"
	"time"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

var (
	ErrRecordingNotFound = errors.New("recording not found")
	ErrRecordingState    = errors.New("recording state conflict")
)

type RecordingStore struct {
	db *gorm.DB
}

func NewRecordingStore(db *gorm.DB) *RecordingStore {
	return &RecordingStore{db: db}
}

func (s *RecordingStore) Create(ctx context.Context, recording *model.ClassroomRecording) error {
	return s.db.WithContext(ctx).Create(recording).Error
}

func (s *RecordingStore) Get(ctx context.Context, roomID, id uint64) (*model.ClassroomRecording, error) {
	var recording model.ClassroomRecording
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND id = ?", roomID, id).
		First(&recording).Error
	if err != nil {
		return nil, err
	}
	return &recording, nil
}

func (s *RecordingStore) GetByID(ctx context.Context, id uint64) (*model.ClassroomRecording, error) {
	var recording model.ClassroomRecording
	if err := s.db.WithContext(ctx).First(&recording, id).Error; err != nil {
		return nil, err
	}
	return &recording, nil
}

func (s *RecordingStore) GetByRecordingID(ctx context.Context, roomID uint64, recordingID string) (*model.ClassroomRecording, error) {
	var recording model.ClassroomRecording
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND recording_id = ?", roomID, recordingID).
		First(&recording).Error
	if err != nil {
		return nil, err
	}
	return &recording, nil
}

func (s *RecordingStore) GetActiveByRoom(ctx context.Context, roomID uint64) (*model.ClassroomRecording, error) {
	var recording model.ClassroomRecording
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND status IN ?", roomID, []string{
			model.RecordingStatePendingConsent,
			model.RecordingStateRecording,
			model.RecordingStateFinalizing,
		}).
		Order("id DESC").
		First(&recording).Error
	if err != nil {
		return nil, err
	}
	return &recording, nil
}

func (s *RecordingStore) ListByRoom(ctx context.Context, roomID uint64) ([]model.ClassroomRecording, error) {
	var recordings []model.ClassroomRecording
	err := s.db.WithContext(ctx).
		Where("room_id = ?", roomID).
		Order("id DESC").
		Find(&recordings).Error
	return recordings, err
}

func (s *RecordingStore) Update(ctx context.Context, id uint64, fields map[string]any) error {
	return s.db.WithContext(ctx).
		Model(&model.ClassroomRecording{}).
		Where("id = ?", id).
		Updates(fields).Error
}

func (s *RecordingStore) UpdateStatus(ctx context.Context, id uint64, from []string, status string, fields map[string]any) (bool, error) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["status"] = status
	result := s.db.WithContext(ctx).
		Model(&model.ClassroomRecording{}).
		Where("id = ? AND status IN ?", id, from).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *RecordingStore) ListStalePending(ctx context.Context, before time.Time, limit int) ([]model.ClassroomRecording, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var recordings []model.ClassroomRecording
	err := s.db.WithContext(ctx).
		Where("status = ? AND updated_at < ?", model.RecordingStatePendingConsent, before).
		Order("id ASC").
		Limit(limit).
		Find(&recordings).Error
	return recordings, err
}

func (s *RecordingStore) ListStaleRecording(ctx context.Context, before time.Time, limit int) ([]model.ClassroomRecording, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var recordings []model.ClassroomRecording
	err := s.db.WithContext(ctx).
		Where("status IN ? AND updated_at < ?", []string{
			model.RecordingStateRecording,
			model.RecordingStateFinalizing,
		}, before).
		Order("id ASC").
		Limit(limit).
		Find(&recordings).Error
	return recordings, err
}
