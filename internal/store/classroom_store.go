package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"ppclassvip/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInviteInvalid = errors.New("invite invalid")
	ErrInviteUsed    = errors.New("invite already used")
	ErrInviteExpired = errors.New("invite expired")
	ErrRoomNotReady  = errors.New("room not ready")
	ErrRoomClosed    = errors.New("room closed")
	ErrRoomFull      = errors.New("room full")
)

type ClassroomStore struct {
	db *gorm.DB
}

func NewClassroomStore(db *gorm.DB) *ClassroomStore {
	return &ClassroomStore{db: db}
}

func HashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func isClosedRoomState(state string) bool {
	switch state {
	case model.ClassroomStateEnded, model.ClassroomStateFailed, model.ClassroomStateExpired:
		return true
	default:
		return false
	}
}

func (s *ClassroomStore) CreateClassroom(ctx context.Context, classroom *model.Classroom) error {
	return s.db.WithContext(ctx).Create(classroom).Error
}

func (s *ClassroomStore) GetClassroom(ctx context.Context, id uint64) (*model.Classroom, error) {
	var classroom model.Classroom
	if err := s.db.WithContext(ctx).First(&classroom, id).Error; err != nil {
		return nil, err
	}
	return &classroom, nil
}

func (s *ClassroomStore) CountOpenByTeacher(ctx context.Context, teacherUserID uint64) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&model.Classroom{}).
		Where("teacher_user_id = ? AND state IN ?", teacherUserID, []string{
			model.ClassroomStateCreated,
			model.ClassroomStateWaiting,
			model.ClassroomStateActive,
		}).
		Count(&count).Error
	return count, err
}

func (s *ClassroomStore) ListByTeacher(ctx context.Context, teacherUserID uint64, limit int) ([]model.Classroom, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var rooms []model.Classroom
	err := s.db.WithContext(ctx).
		Where("teacher_user_id = ?", teacherUserID).
		Order("id DESC").
		Limit(limit).
		Find(&rooms).Error
	return rooms, err
}

func (s *ClassroomStore) CreateInvite(ctx context.Context, invite *model.ClassroomInvite) error {
	return s.db.WithContext(ctx).Create(invite).Error
}

// ReopenClassroom resets a closed classroom (ended/failed/expired) back to a
// fresh "created" state so the teacher can regenerate an invite and run the
// room again. Chat, attachments and recordings are kept; every participant is
// left as-is (already marked left by End) and is re-activated by the next
// join.
func (s *ClassroomStore) ReopenClassroom(ctx context.Context, roomID uint64) (*model.Classroom, error) {
	var room model.Classroom
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&room, roomID).Error; err != nil {
			return err
		}
		return reopenRoomTx(tx, &room)
	})
	if err != nil {
		return nil, err
	}
	return &room, nil
}

// reopenRoomTx clears the previous session's terminal state in place. It is a
// no-op for an already-open room, so callers can invoke it unconditionally.
func reopenRoomTx(tx *gorm.DB, room *model.Classroom) error {
	if !isClosedRoomState(room.State) {
		return nil
	}
	if err := tx.Model(room).Updates(map[string]any{
		"state":                       model.ClassroomStateCreated,
		"ended_at":                    nil,
		"end_reason":                  "",
		"started_at":                  nil,
		"screen_owner_participant_id": nil,
		"screen_leg":                  "",
		"recording_state":             model.RecordingStateOff,
	}).Error; err != nil {
		return err
	}
	room.State = model.ClassroomStateCreated
	room.EndedAt = nil
	room.EndReason = ""
	room.StartedAt = nil
	room.ScreenOwnerParticipantID = nil
	room.ScreenLeg = ""
	room.RecordingState = model.RecordingStateOff
	return nil
}

func (s *ClassroomStore) JoinByInvite(ctx context.Context, tokenHash, nickname string) (*model.Classroom, *model.ClassroomParticipant, error) {
	var room model.Classroom
	var participant model.ClassroomParticipant

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var invite model.ClassroomInvite
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", tokenHash).
			First(&invite).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInviteInvalid
			}
			return err
		}

		now := time.Now()
		if invite.UsedAt != nil {
			return ErrInviteUsed
		}
		if !now.Before(invite.ExpiresAt) {
			return ErrInviteExpired
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&room, invite.RoomID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInviteInvalid
			}
			return err
		}
		switch room.State {
		case model.ClassroomStateEnded, model.ClassroomStateFailed, model.ClassroomStateExpired:
			return ErrRoomClosed
		case model.ClassroomStateCreated:
			return ErrRoomNotReady
		}

		role := invite.Role
		if role == "" {
			role = model.ParticipantRoleStudent
		}
		if role == model.ParticipantRoleStudent {
			var studentCount int64
			if err := tx.Model(&model.ClassroomParticipant{}).
				Where("room_id = ? AND role = ? AND left_at IS NULL", room.ID, model.ParticipantRoleStudent).
				Count(&studentCount).Error; err != nil {
				return err
			}
			if studentCount > 0 {
				return ErrRoomFull
			}
		}
		participant = model.ClassroomParticipant{
			RoomID:      room.ID,
			DisplayName: nickname,
			Role:        role,
			Guest:       true,
			JoinedAt:    now,
			LastSeenAt:  now,
		}
		if err := tx.Create(&participant).Error; err != nil {
			return err
		}
		if err := tx.Model(&invite).Update("used_at", now).Error; err != nil {
			return err
		}

		if room.State == model.ClassroomStateWaiting {
			room.State = model.ClassroomStateActive
			room.StartedAt = &now
			if err := tx.Model(&room).Updates(map[string]any{
				"state":      room.State,
				"started_at": now,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &room, &participant, nil
}

func (s *ClassroomStore) UpsertTeacherParticipant(ctx context.Context, roomID, teacherUserID uint64, nickname string) (*model.Classroom, *model.ClassroomParticipant, error) {
	var room model.Classroom
	var participant model.ClassroomParticipant

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&room, roomID).Error; err != nil {
			return err
		}
		// Entering a closed classroom reopens it (same room, new session), so
		// the teacher can run an already-ended room again.
		if err := reopenRoomTx(tx, &room); err != nil {
			return err
		}

		now := time.Now()
		err := tx.Where("room_id = ? AND user_id = ?", roomID, teacherUserID).First(&participant).Error
		switch {
		case err == nil:
			if err := tx.Model(&participant).Updates(map[string]any{
				"display_name": nickname,
				"last_seen_at": now,
				"left_at":      nil,
			}).Error; err != nil {
				return err
			}
			participant.DisplayName = nickname
			participant.LastSeenAt = now
			participant.LeftAt = nil
		case errors.Is(err, gorm.ErrRecordNotFound):
			uid := teacherUserID
			participant = model.ClassroomParticipant{
				RoomID:      roomID,
				UserID:      &uid,
				DisplayName: nickname,
				Role:        model.ParticipantRoleTeacher,
				Guest:       false,
				JoinedAt:    now,
				LastSeenAt:  now,
			}
			if err := tx.Create(&participant).Error; err != nil {
				return err
			}
		default:
			return err
		}

		if room.State == model.ClassroomStateCreated {
			room.State = model.ClassroomStateWaiting
			if err := tx.Model(&room).Update("state", room.State).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &room, &participant, nil
}

func (s *ClassroomStore) ListParticipants(ctx context.Context, roomID uint64) ([]model.ClassroomParticipant, error) {
	var participants []model.ClassroomParticipant
	err := s.db.WithContext(ctx).
		Where("room_id = ?", roomID).
		Order("id ASC").
		Find(&participants).Error
	return participants, err
}

func (s *ClassroomStore) EndClassroom(ctx context.Context, roomID uint64) (*model.Classroom, error) {
	return s.EndClassroomWithReason(ctx, roomID, model.EndReasonTeacherEnd)
}

func (s *ClassroomStore) EndClassroomWithReason(ctx context.Context, roomID uint64, reason string) (*model.Classroom, error) {
	var room model.Classroom
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&room, roomID).Error; err != nil {
			return err
		}
		if room.State == model.ClassroomStateEnded {
			return nil
		}
		now := time.Now()
		room.State = model.ClassroomStateEnded
		room.EndedAt = &now
		room.EndReason = reason
		if err := tx.Model(&room).Updates(map[string]any{
			"state":                       room.State,
			"ended_at":                    now,
			"end_reason":                  reason,
			"screen_owner_participant_id": nil,
			"screen_leg":                  "",
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ClassroomParticipant{}).
			Where("room_id = ? AND left_at IS NULL", roomID).
			Update("left_at", now).Error
	})
	if err != nil {
		return nil, err
	}
	return &room, nil
}

// DeleteClassroom permanently removes a classroom and every row that belongs
// to it (participants, invites, messages, attachments, recordings, audit logs,
// media stats and remote-desktop sessions). The API layer only calls this for
// closed rooms.
func (s *ClassroomStore) DeleteClassroom(ctx context.Context, roomID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, table := range []any{
			&model.ClassroomParticipant{},
			&model.ClassroomInvite{},
			&model.ClassroomAttachment{},
			&model.ClassroomRecording{},
			&model.ClassroomAuditLog{},
			&model.ClassroomMediaStat{},
			&model.RemoteDesktopSession{},
		} {
			if err := tx.Where("room_id = ?", roomID).Delete(table).Error; err != nil {
				return err
			}
		}
		// Messages use gorm.DeletedAt; the room is gone for good, so hard-delete
		// them instead of leaving orphaned soft-deleted rows behind.
		if err := tx.Unscoped().Where("room_id = ?", roomID).Delete(&model.ClassroomMessage{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.Classroom{}, roomID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *ClassroomStore) UpdateRoomFields(ctx context.Context, roomID uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).
		Model(&model.Classroom{}).
		Where("id = ?", roomID).
		Updates(fields).Error
}

func (s *ClassroomStore) SetRecordingState(ctx context.Context, roomID uint64, state string) error {
	return s.UpdateRoomFields(ctx, roomID, map[string]any{"recording_state": state})
}

func (s *ClassroomStore) ClaimScreen(ctx context.Context, roomID, participantID uint64, leg string) (bool, error) {
	result := s.db.WithContext(ctx).
		Model(&model.Classroom{}).
		Where("id = ? AND (screen_owner_participant_id IS NULL OR screen_owner_participant_id = ?)", roomID, participantID).
		Updates(map[string]any{
			"screen_owner_participant_id": participantID,
			"screen_leg":                  leg,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *ClassroomStore) ReleaseScreen(ctx context.Context, roomID, participantID uint64) (bool, error) {
	result := s.db.WithContext(ctx).
		Model(&model.Classroom{}).
		Where("id = ? AND screen_owner_participant_id = ?", roomID, participantID).
		Updates(map[string]any{
			"screen_owner_participant_id": nil,
			"screen_leg":                  "",
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *ClassroomStore) LeaveParticipant(ctx context.Context, roomID, participantID uint64) error {
	now := time.Now()
	return s.db.WithContext(ctx).
		Model(&model.ClassroomParticipant{}).
		Where("room_id = ? AND id = ? AND left_at IS NULL", roomID, participantID).
		Updates(map[string]any{
			"last_seen_at": now,
			"left_at":      now,
		}).Error
}

func (s *ClassroomStore) GetActiveStudent(ctx context.Context, roomID uint64) (*model.ClassroomParticipant, error) {
	return s.getActiveParticipantByRole(ctx, roomID, model.ParticipantRoleStudent)
}

func (s *ClassroomStore) GetActiveTeacher(ctx context.Context, roomID uint64) (*model.ClassroomParticipant, error) {
	return s.getActiveParticipantByRole(ctx, roomID, model.ParticipantRoleTeacher)
}

func (s *ClassroomStore) getActiveParticipantByRole(ctx context.Context, roomID uint64, role string) (*model.ClassroomParticipant, error) {
	var participant model.ClassroomParticipant
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND role = ? AND left_at IS NULL", roomID, role).
		Order("id DESC").
		First(&participant).Error
	if err != nil {
		return nil, err
	}
	return &participant, nil
}

func (s *ClassroomStore) SetParticipantMuted(ctx context.Context, roomID, participantID uint64, muted bool) error {
	return s.db.WithContext(ctx).
		Model(&model.ClassroomParticipant{}).
		Where("room_id = ? AND id = ?", roomID, participantID).
		Update("muted", muted).Error
}

func (s *ClassroomStore) ActiveParticipantCount(ctx context.Context, roomID uint64) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&model.ClassroomParticipant{}).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Count(&count).Error
	return count, err
}

func (s *ClassroomStore) LatestParticipantSeen(ctx context.Context, roomID uint64) (time.Time, bool, error) {
	var latest time.Time
	err := s.db.WithContext(ctx).
		Model(&model.ClassroomParticipant{}).
		Where("room_id = ?", roomID).
		Select("MAX(last_seen_at)").
		Scan(&latest).Error
	if err != nil {
		return time.Time{}, false, err
	}
	if latest.IsZero() {
		return time.Time{}, false, nil
	}
	return latest, true, nil
}

func (s *ClassroomStore) DeleteExpiredInvites(ctx context.Context, before time.Time) (int64, error) {
	result := s.db.WithContext(ctx).
		Where("expires_at < ?", before).
		Delete(&model.ClassroomInvite{})
	return result.RowsAffected, result.Error
}

func (s *ClassroomStore) ListStaleRooms(ctx context.Context, before time.Time, limit int) ([]model.Classroom, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rooms []model.Classroom
	err := s.db.WithContext(ctx).
		Where("state IN ? AND updated_at < ?", []string{
			model.ClassroomStateCreated,
			model.ClassroomStateWaiting,
			model.ClassroomStateActive,
		}, before).
		Order("id ASC").
		Limit(limit).
		Find(&rooms).Error
	return rooms, err
}
func (s *ClassroomStore) CreateMessage(ctx context.Context, message *model.ClassroomMessage) error {
	return s.db.WithContext(ctx).Create(message).Error
}

func (s *ClassroomStore) GetMessageByClientID(ctx context.Context, roomID, participantID uint64, clientMsgID string) (*model.ClassroomMessage, error) {
	var message model.ClassroomMessage
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND sender_participant_id = ? AND client_msg_id = ?", roomID, participantID, clientMsgID).
		First(&message).Error
	if err != nil {
		return nil, err
	}
	return &message, nil
}

func (s *ClassroomStore) ListMessages(ctx context.Context, roomID, afterID, beforeID uint64, limit int) ([]model.ClassroomMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := s.db.WithContext(ctx).Where("room_id = ?", roomID)
	var messages []model.ClassroomMessage

	if afterID > 0 {
		err := query.Where("id > ?", afterID).Order("id ASC").Limit(limit).Find(&messages).Error
		return messages, err
	}

	if beforeID > 0 {
		query = query.Where("id < ?", beforeID)
	}
	if err := query.Order("id DESC").Limit(limit).Find(&messages).Error; err != nil {
		return nil, err
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}
func (s *ClassroomStore) GetParticipant(ctx context.Context, roomID, participantID uint64) (*model.ClassroomParticipant, error) {
	var participant model.ClassroomParticipant
	err := s.db.WithContext(ctx).
		Where("room_id = ? AND id = ?", roomID, participantID).
		First(&participant).Error
	if err != nil {
		return nil, err
	}
	return &participant, nil
}

func (s *ClassroomStore) SetParticipantOnline(ctx context.Context, roomID, participantID uint64) error {
	now := time.Now()
	return s.db.WithContext(ctx).
		Model(&model.ClassroomParticipant{}).
		Where("room_id = ? AND id = ?", roomID, participantID).
		Updates(map[string]any{
			"last_seen_at": now,
			"left_at":      nil,
		}).Error
}

func (s *ClassroomStore) SetParticipantOffline(ctx context.Context, roomID, participantID uint64) error {
	now := time.Now()
	return s.db.WithContext(ctx).
		Model(&model.ClassroomParticipant{}).
		Where("room_id = ? AND id = ?", roomID, participantID).
		Updates(map[string]any{
			"last_seen_at": now,
			"left_at":      now,
		}).Error
}
