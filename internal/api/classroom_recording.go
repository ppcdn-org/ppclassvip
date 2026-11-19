package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type startRecordingRequest struct {
	StudentAudioAvailable bool `json:"studentAudioAvailable"`
}

func (h *ClassroomHandler) StartRecording(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	if roomIsClosed(room) {
		FailCode(c, http.StatusConflict, "room_ended", "room is closed")
		return
	}
	if h.settings != nil && !h.settings.RecordingEnabled() {
		FailCode(c, http.StatusForbidden, "recording_disabled", "recording is disabled by the administrator")
		return
	}
	if h.remote != nil {
		if active, err := h.remote.GetActiveByRoom(c.Request.Context(), room.ID); err == nil {
			_ = active
			FailCode(c, http.StatusConflict, "remote_desktop_active", "stop remote desktop before recording")
			return
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusInternalServerError, "recording_failed", "check remote desktop state failed")
			return
		}
	}
	if h.recordings == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return
	}
	var req startRecordingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	if !req.StudentAudioAvailable {
		FailCode(c, http.StatusConflict, "student_audio_unavailable", "student audio must be available before recording")
		return
	}
	student, err := h.classrooms.GetActiveStudent(c.Request.Context(), room.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusConflict, "student_not_joined", "student has not joined the classroom")
			return
		}
		h.log.Error("load active student failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "start recording failed")
		return
	}
	if _, err := h.recordings.GetActiveByRoom(c.Request.Context(), room.ID); err == nil {
		FailCode(c, http.StatusConflict, "recording_active", "a recording is already active")
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		h.log.Error("check active recording failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "start recording failed")
		return
	}

	uploaderID := middleware.ParticipantID(c)
	if uploaderID == 0 {
		if teacher, err := h.classrooms.GetActiveTeacher(c.Request.Context(), room.ID); err == nil {
			uploaderID = teacher.ID
		}
	}
	if uploaderID == 0 {
		FailCode(c, http.StatusConflict, "room_forbidden", "teacher must join the classroom first")
		return
	}
	recordingID, err := newRecordingID()
	if err != nil {
		FailCode(c, http.StatusInternalServerError, "recording_failed", "start recording failed")
		return
	}
	now := time.Now()
	recording := &model.ClassroomRecording{
		RoomID:                room.ID,
		RecordingID:           recordingID,
		UploaderParticipantID: uploaderID,
		Format:                "webm",
		ContentType:           "video/webm",
		LastChunkSeq:          -1,
		IncludesStudentAudio:  true,
		StartedAt:             now,
		Status:                model.RecordingStatePendingConsent,
	}
	if err := h.recordings.Create(c.Request.Context(), recording); err != nil {
		h.log.Error("create recording failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "start recording failed")
		return
	}
	_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStatePendingConsent)

	teacherName := "teacher"
	if teacher, err := h.classrooms.GetActiveTeacher(c.Request.Context(), room.ID); err == nil {
		teacherName = teacher.DisplayName
	}
	if h.hub != nil {
		h.hub.SendTo(room.ID, student.ID, gin.H{
			"type": "recording.request",
			"data": gin.H{
				"recordingId": recordingID,
				"teacherName": teacherName,
				"expiresAt":   now.Add(h.classroomConfig().ConsentTTL.Std()).UTC().Format(time.RFC3339),
			},
		})
	}
	h.audit(c, room.ID, uploaderID, "recording.start", "recording="+recordingID)
	Created(c, gin.H{
		"recordingId":      recordingID,
		"status":           recording.Status,
		"consentExpiresAt": now.Add(h.classroomConfig().ConsentTTL.Std()),
	})
}

func (h *ClassroomHandler) ConsentRecording(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if participant.Role != model.ParticipantRoleStudent {
		FailCode(c, http.StatusForbidden, "recording_forbidden", "only the student can consent to recording")
		return
	}
	recording, ok := h.loadRecording(c, room.ID)
	if !ok {
		return
	}
	if recording.Status != model.RecordingStatePendingConsent {
		FailCode(c, http.StatusConflict, "recording_state", "recording is not waiting for consent")
		return
	}
	now := time.Now()
	if now.After(recording.UpdatedAt.Add(h.classroomConfig().ConsentTTL.Std())) {
		h.declineRecording(c, room, recording, "timeout")
		FailCode(c, http.StatusGone, "recording_declined", "recording consent expired")
		return
	}
	updated, err := h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{model.RecordingStatePendingConsent}, model.RecordingStateRecording, map[string]any{
		"consent_at": now,
		"consent_by": participant.ID,
	})
	if err != nil || !updated {
		FailCode(c, http.StatusConflict, "recording_state", "recording state changed")
		return
	}
	_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateRecording)
	h.notifyTeacher(c, room.ID, "recording.consented", gin.H{"recordingId": recording.RecordingID})
	if h.hub != nil {
		h.hub.Broadcast(room.ID, gin.H{
			"type": "recording.state",
			"data": gin.H{
				"recordingId":          recording.RecordingID,
				"state":                model.RecordingStateRecording,
				"startedAt":            recording.StartedAt,
				"includesStudentAudio": true,
			},
		})
	}
	h.audit(c, room.ID, participant.ID, "recording.consent", "recording="+recording.RecordingID)
	recording.Status = model.RecordingStateRecording
	recording.ConsentAt = &now
	recording.ConsentBy = &participant.ID
	OK(c, gin.H{"recording": recording})
}

func (h *ClassroomHandler) DeclineRecording(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if participant.Role != model.ParticipantRoleStudent {
		FailCode(c, http.StatusForbidden, "recording_forbidden", "only the student can decline recording")
		return
	}
	recording, ok := h.loadRecording(c, room.ID)
	if !ok {
		return
	}
	if recording.Status != model.RecordingStatePendingConsent {
		FailCode(c, http.StatusConflict, "recording_state", "recording is not waiting for consent")
		return
	}
	h.declineRecording(c, room, recording, "declined")
	OK(c, gin.H{"declined": true})
}

func (h *ClassroomHandler) declineRecording(c *gin.Context, room *model.Classroom, recording *model.ClassroomRecording, reason string) {
	_, _ = h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{model.RecordingStatePendingConsent}, model.RecordingStateDeclined, map[string]any{
		"error":    reason,
		"ended_at": time.Now(),
	})
	_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateOff)
	h.notifyTeacher(c, room.ID, "recording.declined", gin.H{
		"recordingId": recording.RecordingID,
		"reason":      reason,
	})
	h.audit(c, room.ID, middleware.ParticipantID(c), "recording.decline", "recording="+recording.RecordingID+" reason="+reason)
}

// StopRecording ends an active recording session. The recording itself is kept
// in the teacher's browser and saved locally, so the server only tracks the
// consent lifecycle and never stores the media file.
func (h *ClassroomHandler) StopRecording(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	if h.recordings == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return
	}
	recording, ok := h.loadRecording(c, room.ID)
	if !ok {
		return
	}
	if recording.Status != model.RecordingStateRecording && recording.Status != model.RecordingStatePendingConsent {
		FailCode(c, http.StatusConflict, "recording_state", "recording is not active")
		return
	}
	target := model.RecordingStateCompleted
	if recording.Status == model.RecordingStatePendingConsent {
		target = model.RecordingStateDeclined
	}
	updated, err := h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{recording.Status}, target, map[string]any{
		"ended_at": time.Now(),
	})
	if err != nil || !updated {
		FailCode(c, http.StatusConflict, "recording_state", "recording state changed")
		return
	}
	_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateOff)
	h.broadcastRecordingState(room.ID, recording.RecordingID, target)
	h.audit(c, room.ID, middleware.ParticipantID(c), "recording.stop", "recording="+recording.RecordingID+" state="+target)
	OK(c, gin.H{"recordingId": recording.RecordingID, "status": target})
}

func (h *ClassroomHandler) GetRecording(c *gin.Context) {
	room, _, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	recording, ok := h.loadRecording(c, room.ID)
	if !ok {
		return
	}
	OK(c, gin.H{"recording": recording})
}

func (h *ClassroomHandler) loadRecording(c *gin.Context, roomID uint64) (*model.ClassroomRecording, bool) {
	if h.recordings == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return nil, false
	}
	recordingID := strings.TrimSpace(c.Param("rid"))
	if recordingID == "" {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid recording id")
		return nil, false
	}
	recording, err := h.recordings.GetByRecordingID(c.Request.Context(), roomID, recordingID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusNotFound, "recording_not_found", "recording not found")
			return nil, false
		}
		h.log.Error("get recording failed", "error", err, "room_id", roomID)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "recording lookup failed")
		return nil, false
	}
	return recording, true
}

func (h *ClassroomHandler) notifyTeacher(c *gin.Context, roomID uint64, eventType string, data gin.H) {
	if h.hub == nil {
		return
	}
	teacher, err := h.classrooms.GetActiveTeacher(c.Request.Context(), roomID)
	if err != nil {
		return
	}
	h.hub.SendTo(roomID, teacher.ID, gin.H{"type": eventType, "data": data})
}

func (h *ClassroomHandler) broadcastRecordingState(roomID uint64, recordingID, state string) {
	if h.hub == nil {
		return
	}
	h.hub.Broadcast(roomID, gin.H{
		"type": "recording.state",
		"data": gin.H{
			"recordingId":          recordingID,
			"state":                state,
			"includesStudentAudio": true,
		},
	})
}

func (h *ClassroomHandler) markRecordingInterrupted(ctx context.Context, roomID uint64) {
	if h.recordings == nil {
		return
	}
	recording, err := h.recordings.GetActiveByRoom(ctx, roomID)
	if err != nil || recording.Status != model.RecordingStateRecording {
		return
	}
	updated, err := h.recordings.UpdateStatus(ctx, recording.ID, []string{model.RecordingStateRecording}, model.RecordingStateInterrupted, map[string]any{
		"error":    "interrupted",
		"ended_at": time.Now(),
	})
	if err == nil && updated {
		_ = h.classrooms.SetRecordingState(ctx, roomID, model.RecordingStateInterrupted)
		h.broadcastRecordingState(roomID, recording.RecordingID, model.RecordingStateInterrupted)
	}
}

func (h *ClassroomHandler) finishActiveRecordingOnEnd(ctx context.Context, roomID uint64) {
	if h.recordings == nil {
		return
	}
	recording, err := h.recordings.GetActiveByRoom(ctx, roomID)
	if err != nil {
		return
	}
	now := time.Now()
	if recording.Status == model.RecordingStatePendingConsent {
		_, _ = h.recordings.UpdateStatus(ctx, recording.ID, []string{model.RecordingStatePendingConsent}, model.RecordingStateDeclined, map[string]any{
			"error":    "room_ended",
			"ended_at": now,
		})
	} else {
		_, _ = h.recordings.UpdateStatus(ctx, recording.ID, []string{recording.Status}, model.RecordingStateInterrupted, map[string]any{
			"error":    "room_ended",
			"ended_at": now,
		})
	}
	_ = h.classrooms.SetRecordingState(ctx, roomID, model.RecordingStateInterrupted)
	h.broadcastRecordingState(roomID, recording.RecordingID, model.RecordingStateInterrupted)
}

func newRecordingID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
