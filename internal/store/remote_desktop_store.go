package store

import (
	"context"
	"time"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

type RemoteDesktopStore struct {
	db *gorm.DB
}

func NewRemoteDesktopStore(db *gorm.DB) *RemoteDesktopStore {
	return &RemoteDesktopStore{db: db}
}

func (s *RemoteDesktopStore) Create(ctx context.Context, session *model.RemoteDesktopSession) error {
	return s.db.WithContext(ctx).Create(session).Error
}

func (s *RemoteDesktopStore) GetByID(ctx context.Context, id uint64) (*model.RemoteDesktopSession, error) {
	var session model.RemoteDesktopSession
	if err := s.db.WithContext(ctx).First(&session, id).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *RemoteDesktopStore) Get(ctx context.Context, roomID uint64, sessionID string) (*model.RemoteDesktopSession, error) {
	var session model.RemoteDesktopSession
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND session_id = ?", roomID, sessionID).
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *RemoteDesktopStore) GetByAgentCodeHash(ctx context.Context, hash string) (*model.RemoteDesktopSession, error) {
	var session model.RemoteDesktopSession
	err := s.db.WithContext(ctx).
		Where("agent_code_hash = ?", hash).
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}
func (s *RemoteDesktopStore) GetActiveByRoom(ctx context.Context, roomID uint64) (*model.RemoteDesktopSession, error) {
	var session model.RemoteDesktopSession
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND status IN ?", roomID, []string{
			model.RemoteDesktopStatusPending,
			model.RemoteDesktopStatusActive,
		}).
		Order("id DESC").
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *RemoteDesktopStore) HasActiveForParticipant(ctx context.Context, roomID, participantID uint64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&model.RemoteDesktopSession{}).
		Where("room_id = ? AND student_participant_id = ? AND status = ?", roomID, participantID, model.RemoteDesktopStatusActive).
		Count(&count).Error
	return count > 0, err
}

func (s *RemoteDesktopStore) UpdateStatus(ctx context.Context, id uint64, from []string, status string, fields map[string]any) (bool, error) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["status"] = status
	result := s.db.WithContext(ctx).
		Model(&model.RemoteDesktopSession{}).
		Where("id = ? AND status IN ?", id, from).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *RemoteDesktopStore) Update(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).
		Model(&model.RemoteDesktopSession{}).
		Where("id = ?", id).
		Updates(fields).Error
}

func (s *RemoteDesktopStore) ListExpired(ctx context.Context, now time.Time, limit int) ([]model.RemoteDesktopSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var sessions []model.RemoteDesktopSession
	err := s.db.WithContext(ctx).
		Where("status IN ? AND expires_at IS NOT NULL AND expires_at < ?", []string{
			model.RemoteDesktopStatusPending,
			model.RemoteDesktopStatusActive,
		}, now).
		Order("id ASC").
		Limit(limit).
		Find(&sessions).Error
	return sessions, err
}

func (s *RemoteDesktopStore) ListActiveByRoom(ctx context.Context, roomID uint64) ([]model.RemoteDesktopSession, error) {
	var sessions []model.RemoteDesktopSession
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND status IN ?", roomID, []string{
			model.RemoteDesktopStatusPending,
			model.RemoteDesktopStatusActive,
		}).
		Find(&sessions).Error
	return sessions, err
}
