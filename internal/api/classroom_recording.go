package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const recordingChunkIntervalMS = 5000

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
	if h.recordings == nil || h.storage == nil {
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
		"recordingId":       recordingID,
		"status":            recording.Status,
		"chunkSize":         recordingChunkIntervalMS,
		"maxChunkBytes":     h.classroomConfig().MaxChunkBytes,
		"maxRecordingBytes": h.classroomConfig().MaxRecordingBytes,
		"consentExpiresAt":  now.Add(h.classroomConfig().ConsentTTL.Std()),
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

func (h *ClassroomHandler) UploadRecordingChunk(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	if h.recordings == nil || h.storage == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return
	}
	recording, ok := h.loadRecording(c, room.ID)
	if !ok {
		return
	}
	seq, err := strconv.ParseInt(c.Param("seq"), 10, 64)
	if err != nil || seq < 0 {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid chunk sequence")
		return
	}

	lock := h.recordingLock(recording.RecordingID)
	lock.Lock()
	defer lock.Unlock()

	recording, err = h.recordings.GetByRecordingID(c.Request.Context(), room.ID, recording.RecordingID)
	if err != nil {
		FailCode(c, http.StatusNotFound, "recording_not_found", "recording not found")
		return
	}
	if recording.Status != model.RecordingStateRecording {
		FailCode(c, http.StatusConflict, "recording_state", "recording is not accepting chunks")
		return
	}
	if seq <= recording.LastChunkSeq {
		OK(c, gin.H{"lastChunkSeq": recording.LastChunkSeq, "duplicate": true})
		return
	}
	if seq != recording.LastChunkSeq+1 {
		FailCode(c, http.StatusConflict, "chunk_out_of_order", "chunk sequence is out of order")
		return
	}
	if c.Request.ContentLength > h.classroomConfig().MaxChunkBytes {
		FailCode(c, http.StatusRequestEntityTooLarge, "chunk_too_large", "chunk exceeds the allowed size")
		return
	}
	tempSize, err := h.storage.RecordingTempSize(recording.RecordingID)
	if err != nil {
		FailCode(c, http.StatusInternalServerError, "recording_failed", "recording storage failed")
		return
	}
	if tempSize+c.Request.ContentLength > h.classroomConfig().MaxRecordingBytes {
		_, _ = h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{model.RecordingStateRecording}, model.RecordingStateFailed, map[string]any{
			"error":    "recording_too_large",
			"ended_at": time.Now(),
		})
		_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateOff)
		_ = h.storage.DeleteRecording(room.ID, recording.RecordingID, "")
		FailCode(c, http.StatusRequestEntityTooLarge, "recording_too_large", "recording exceeds the allowed size")
		return
	}

	extendReadDeadline(c, 2*time.Minute)
	extendWriteDeadline(c, time.Minute)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.classroomConfig().MaxChunkBytes+1)
	written, err := h.storage.AppendRecordingChunk(recording.RecordingID, c.Request.Body, h.classroomConfig().MaxChunkBytes)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			FailCode(c, http.StatusBadRequest, "invalid_request", "incomplete chunk")
			return
		}
		h.log.Error("append recording chunk failed", "error", err, "room_id", room.ID, "recording_id", recording.RecordingID, "seq", seq)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "recording storage failed")
		return
	}
	if tempSize+written > h.classroomConfig().MaxRecordingBytes {
		_, _ = h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{model.RecordingStateRecording}, model.RecordingStateFailed, map[string]any{
			"error":    "recording_too_large",
			"ended_at": time.Now(),
		})
		_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateOff)
		_ = h.storage.DeleteRecording(room.ID, recording.RecordingID, "")
		FailCode(c, http.StatusRequestEntityTooLarge, "recording_too_large", "recording exceeds the allowed size")
		return
	}
	updated, err := h.recordings.AddChunkSeq(c.Request.Context(), recording.ID, seq, written)
	if err != nil || !updated {
		FailCode(c, http.StatusConflict, "chunk_out_of_order", "chunk sequence changed")
		return
	}
	OK(c, gin.H{"lastChunkSeq": seq, "receivedBytes": written})
}

func (h *ClassroomHandler) FinalizeRecording(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	if h.recordings == nil || h.storage == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return
	}
	recording, ok := h.loadRecording(c, room.ID)
	if !ok {
		return
	}
	lock := h.recordingLock(recording.RecordingID)
	lock.Lock()
	defer lock.Unlock()

	recording, err := h.recordings.GetByRecordingID(c.Request.Context(), room.ID, recording.RecordingID)
	if err != nil {
		FailCode(c, http.StatusNotFound, "recording_not_found", "recording not found")
		return
	}
	if recording.Status != model.RecordingStateRecording && recording.Status != model.RecordingStateInterrupted && recording.Status != model.RecordingStateFinalizing {
		FailCode(c, http.StatusConflict, "recording_state", "recording cannot be finalized")
		return
	}
	if recording.LastChunkSeq < 0 {
		FailCode(c, http.StatusConflict, "recording_incomplete", "no recording chunks were uploaded")
		return
	}
	_, _ = h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{recording.Status}, model.RecordingStateFinalizing, nil)
	_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateFinalizing)

	stored, err := h.storage.FinalizeRecording(room.ID, recording.RecordingID)
	if err != nil {
		_, _ = h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{model.RecordingStateFinalizing}, model.RecordingStateFailed, map[string]any{
			"error":    "finalize_failed",
			"ended_at": time.Now(),
		})
		_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateOff)
		h.log.Error("finalize recording failed", "error", err, "room_id", room.ID, "recording_id", recording.RecordingID)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "finalize recording failed")
		return
	}
	now := time.Now()
	_, _ = h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{model.RecordingStateFinalizing}, model.RecordingStateCompleted, map[string]any{
		"file_key":   stored.Key,
		"size_bytes": stored.Size,
		"ended_at":   now,
	})
	_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateCompleted)
	h.broadcastRecordingState(room.ID, recording.RecordingID, model.RecordingStateCompleted)
	h.audit(c, room.ID, middleware.ParticipantID(c), "recording.finalize", "recording="+recording.RecordingID)
	recording.Status = model.RecordingStateCompleted
	recording.FileKey = stored.Key
	recording.SizeBytes = stored.Size
	recording.EndedAt = &now
	OK(c, gin.H{"recording": recording})
}

func (h *ClassroomHandler) AbortRecording(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	if h.recordings == nil || h.storage == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return
	}
	recording, ok := h.loadRecording(c, room.ID)
	if !ok {
		return
	}
	lock := h.recordingLock(recording.RecordingID)
	lock.Lock()
	defer lock.Unlock()

	recording, err := h.recordings.GetByRecordingID(c.Request.Context(), room.ID, recording.RecordingID)
	if err != nil {
		FailCode(c, http.StatusNotFound, "recording_not_found", "recording not found")
		return
	}
	if recording.Status != model.RecordingStatePendingConsent && recording.Status != model.RecordingStateRecording && recording.Status != model.RecordingStateInterrupted && recording.Status != model.RecordingStateFinalizing {
		FailCode(c, http.StatusConflict, "recording_state", "recording cannot be aborted")
		return
	}
	_, _ = h.recordings.UpdateStatus(c.Request.Context(), recording.ID, []string{recording.Status}, model.RecordingStateFailed, map[string]any{
		"error":    "aborted",
		"ended_at": time.Now(),
	})
	_ = h.storage.DeleteRecording(room.ID, recording.RecordingID, recording.FileKey)
	_ = h.classrooms.SetRecordingState(c.Request.Context(), room.ID, model.RecordingStateOff)
	h.broadcastRecordingState(room.ID, recording.RecordingID, model.RecordingStateFailed)
	h.audit(c, room.ID, middleware.ParticipantID(c), "recording.abort", "recording="+recording.RecordingID)
	OK(c, gin.H{"aborted": true})
}

func (h *ClassroomHandler) ListRecordings(c *gin.Context) {
	if h.recordings == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return
	}
	room, _, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	recordings, err := h.recordings.ListByRoom(c.Request.Context(), room.ID)
	if err != nil {
		h.log.Error("list recordings failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "list recordings failed")
		return
	}
	OK(c, gin.H{"recordings": recordings})
}

func (h *ClassroomHandler) DownloadRecording(c *gin.Context) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return
	}
	if h.storage == nil || h.recordings == nil {
		FailCode(c, http.StatusServiceUnavailable, "recording_unavailable", "recording service is not configured")
		return
	}
	if !h.authorizeRecordingDownload(c, roomID) {
		FailCode(c, http.StatusForbidden, "recording_forbidden", "teacher access required")
		return
	}
	extendWriteDeadline(c, 10*time.Minute)
	recordingID := strings.TrimSpace(c.Param("rid"))
	recording, err := h.recordings.GetByRecordingID(c.Request.Context(), roomID, recordingID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusNotFound, "recording_not_found", "recording not found")
			return
		}
		h.log.Error("get recording failed", "error", err, "room_id", roomID)
		FailCode(c, http.StatusInternalServerError, "recording_failed", "download failed")
		return
	}
	if recording.Status != model.RecordingStateCompleted && recording.Status != model.RecordingStateInterrupted {
		FailCode(c, http.StatusConflict, "recording_state", "recording is not ready")
		return
	}
	if strings.TrimSpace(recording.FileKey) == "" {
		FailCode(c, http.StatusNotFound, "recording_not_found", "recording file is missing")
		return
	}
	file, err := h.storage.OpenRecording(recording.FileKey)
	if err != nil {
		h.log.Error("open recording failed", "error", err, "room_id", roomID, "recording_id", recordingID)
		FailCode(c, http.StatusNotFound, "recording_not_found", "recording file is missing")
		return
	}
	defer file.Close()
	c.Header("Content-Type", "video/webm")
	c.Header("Content-Disposition", "inline; filename=\""+recording.RecordingID+".webm\"")
	c.Header("Cache-Control", "private, max-age=300")
	h.audit(c, roomID, middleware.ParticipantID(c), "recording.download", "recording="+recordingID)
	http.ServeContent(c.Writer, c.Request, recording.RecordingID+".webm", recording.CreatedAt, file)
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

func (h *ClassroomHandler) recordingLock(recordingID string) *sync.Mutex {
	value, _ := h.recordingMu.LoadOrStore(recordingID, &sync.Mutex{})
	return value.(*sync.Mutex)
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

func (h *ClassroomHandler) authorizeRecordingDownload(c *gin.Context, roomID uint64) bool {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(header), "bearer ") {
			token = strings.TrimSpace(header[len("Bearer "):])
		}
	}
	if token == "" {
		return false
	}
	claims, err := h.tokens.Parse(token)
	if err != nil {
		return false
	}
	if claims.Type == auth.TokenTypeRoom {
		if claims.RoomID != roomID {
			return false
		}
		participant, err := h.classrooms.GetParticipant(c.Request.Context(), roomID, claims.ParticipantID)
		return err == nil && participant.Role == model.ParticipantRoleTeacher
	}
	if claims.Type == auth.TokenTypeAccess {
		room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
		if err != nil {
			return false
		}
		return room.TeacherUserID == claims.UserID || claims.Role == model.RoleAdmin
	}
	return false
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
