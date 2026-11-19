package store

import (
	"context"
	"strings"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
)

type AdminStore struct {
	db *gorm.DB
}

func NewAdminStore(db *gorm.DB) *AdminStore {
	return &AdminStore{db: db}
}

type AdminStats struct {
	Users           int64 `json:"users"`
	Teachers        int64 `json:"teachers"`
	Admins          int64 `json:"admins"`
	Rooms           int64 `json:"rooms"`
	OpenRooms       int64 `json:"openRooms"`
	EndedRooms      int64 `json:"endedRooms"`
	Recordings      int64 `json:"recordings"`
	Attachments     int64 `json:"attachments"`
	RecordingBytes  int64 `json:"recordingBytes"`
	AttachmentBytes int64 `json:"attachmentBytes"`
}

func (s *AdminStore) Stats(ctx context.Context) (AdminStats, error) {
	var stats AdminStats
	db := s.db.WithContext(ctx)
	if err := db.Model(&model.User{}).Count(&stats.Users).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.User{}).Where("role = ?", model.RoleTeacher).Count(&stats.Teachers).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.User{}).Where("role = ?", model.RoleAdmin).Count(&stats.Admins).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.Classroom{}).Count(&stats.Rooms).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.Classroom{}).
		Where("state IN ?", []string{model.ClassroomStateCreated, model.ClassroomStateWaiting, model.ClassroomStateActive}).
		Count(&stats.OpenRooms).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.Classroom{}).Where("state = ?", model.ClassroomStateEnded).Count(&stats.EndedRooms).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.ClassroomRecording{}).Count(&stats.Recordings).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.ClassroomAttachment{}).Count(&stats.Attachments).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.ClassroomRecording{}).
		Where("status = ?", model.RecordingStateCompleted).
		Select("COALESCE(SUM(size_bytes), 0)").Scan(&stats.RecordingBytes).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&model.ClassroomAttachment{}).
		Where("status = ?", model.AttachmentStatusStored).
		Select("COALESCE(SUM(size), 0)").Scan(&stats.AttachmentBytes).Error; err != nil {
		return stats, err
	}
	return stats, nil
}

func pagination(limit, offset int) (int, int) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *AdminStore) ListUsers(ctx context.Context, query, role, status string, limit, offset int) ([]model.User, int64, error) {
	limit, offset = pagination(limit, offset)
	db := s.db.WithContext(ctx).Model(&model.User{})
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + q + "%"
		db = db.Where("email LIKE ? OR nickname LIKE ?", like, like)
	}
	if role != "" {
		db = db.Where("role = ?", role)
	}
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []model.User
	err := db.Order("id DESC").Limit(limit).Offset(offset).Find(&users).Error
	return users, total, err
}

func (s *AdminStore) ListRooms(ctx context.Context, query, state string, teacherID uint64, limit, offset int) ([]model.Classroom, int64, error) {
	limit, offset = pagination(limit, offset)
	db := s.db.WithContext(ctx).Model(&model.Classroom{})
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + q + "%"
		db = db.Where("title LIKE ? OR room_code LIKE ?", like, like)
	}
	if state != "" {
		db = db.Where("state = ?", state)
	}
	if teacherID != 0 {
		db = db.Where("teacher_user_id = ?", teacherID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rooms []model.Classroom
	err := db.Order("id DESC").Limit(limit).Offset(offset).Find(&rooms).Error
	return rooms, total, err
}

func (s *AdminStore) ListRecordings(ctx context.Context, roomID uint64, status string, limit, offset int) ([]model.ClassroomRecording, int64, error) {
	limit, offset = pagination(limit, offset)
	db := s.db.WithContext(ctx).Model(&model.ClassroomRecording{})
	if roomID != 0 {
		db = db.Where("room_id = ?", roomID)
	}
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var recordings []model.ClassroomRecording
	err := db.Order("id DESC").Limit(limit).Offset(offset).Find(&recordings).Error
	return recordings, total, err
}

func (s *AdminStore) ListAttachments(ctx context.Context, roomID uint64, status string, limit, offset int) ([]model.ClassroomAttachment, int64, error) {
	limit, offset = pagination(limit, offset)
	db := s.db.WithContext(ctx).Model(&model.ClassroomAttachment{})
	if roomID != 0 {
		db = db.Where("room_id = ?", roomID)
	}
	if status != "" {
		db = db.Where("status = ?", status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var attachments []model.ClassroomAttachment
	err := db.Order("id DESC").Limit(limit).Offset(offset).Find(&attachments).Error
	return attachments, total, err
}

func (s *AdminStore) ListAudit(ctx context.Context, roomID uint64, action string, limit, offset int) ([]model.ClassroomAuditLog, int64, error) {
	limit, offset = pagination(limit, offset)
	db := s.db.WithContext(ctx).Model(&model.ClassroomAuditLog{})
	if roomID != 0 {
		db = db.Where("room_id = ?", roomID)
	}
	if a := strings.TrimSpace(action); a != "" {
		db = db.Where("action LIKE ?", "%"+a+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var entries []model.ClassroomAuditLog
	err := db.Order("id DESC").Limit(limit).Offset(offset).Find(&entries).Error
	return entries, total, err
}
