package store

import (
	"context"
	"time"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

type AttachmentStore struct {
	db *gorm.DB
}

func NewAttachmentStore(db *gorm.DB) *AttachmentStore {
	return &AttachmentStore{db: db}
}

func (s *AttachmentStore) Create(ctx context.Context, attachment *model.ClassroomAttachment) error {
	return s.db.WithContext(ctx).Create(attachment).Error
}

func (s *AttachmentStore) Get(ctx context.Context, roomID, id uint64) (*model.ClassroomAttachment, error) {
	var attachment model.ClassroomAttachment
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND id = ?", roomID, id).
		First(&attachment).Error
	if err != nil {
		return nil, err
	}
	return &attachment, nil
}

func (s *AttachmentStore) GetByID(ctx context.Context, id uint64) (*model.ClassroomAttachment, error) {
	var attachment model.ClassroomAttachment
	if err := s.db.WithContext(ctx).First(&attachment, id).Error; err != nil {
		return nil, err
	}
	return &attachment, nil
}

func (s *AttachmentStore) ListByRoom(ctx context.Context, roomID uint64) ([]model.ClassroomAttachment, error) {
	var attachments []model.ClassroomAttachment
	err := s.db.WithContext(ctx).Where("room_id = ?", roomID).Order("id DESC").Find(&attachments).Error
	return attachments, err
}

func (s *AttachmentStore) ListByIDs(ctx context.Context, roomID uint64, ids []uint64) ([]model.ClassroomAttachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var attachments []model.ClassroomAttachment
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND id IN ?", roomID, ids).
		Find(&attachments).Error
	return attachments, err
}

func (s *AttachmentStore) SumActiveSize(ctx context.Context, roomID uint64) (int64, error) {
	var total int64
	err := s.db.WithContext(ctx).
		Model(&model.ClassroomAttachment{}).
		Where("room_id = ? AND status = ?", roomID, model.AttachmentStatusStored).
		Select("COALESCE(SUM(size), 0)").
		Scan(&total).Error
	return total, err
}

func (s *AttachmentStore) MarkPurged(ctx context.Context, id uint64, at time.Time) error {
	return s.db.WithContext(ctx).
		Model(&model.ClassroomAttachment{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":    model.AttachmentStatusPurged,
			"purged_at": at,
		}).Error
}

func (s *AttachmentStore) ListExpired(ctx context.Context, roomBefore time.Time, limit int) ([]model.ClassroomAttachment, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var attachments []model.ClassroomAttachment
	err := s.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", model.AttachmentStatusStored, roomBefore).
		Order("id ASC").
		Limit(limit).
		Find(&attachments).Error
	return attachments, err
}
