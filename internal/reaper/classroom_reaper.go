package reaper

import (
	"context"
	"log/slog"
	"time"

	"ppclassvip/internal/config"
	"ppclassvip/internal/model"
	"ppclassvip/internal/realtime"
	"ppclassvip/internal/settings"
	"ppclassvip/internal/storage"
	"ppclassvip/internal/store"

	"github.com/gin-gonic/gin"
)

type ClassroomReaper struct {
	cfg         config.Config
	settings    *settings.Service
	classrooms  *store.ClassroomStore
	recordings  *store.RecordingStore
	attachments *store.AttachmentStore
	mediaStats  *store.MediaStatStore
	remote      *store.RemoteDesktopStore
	control     *realtime.ControlHub
	storage     *storage.Local
	hub         *realtime.Hub
	log         *slog.Logger
	interval    time.Duration
}

func NewClassroomReaper(
	cfg config.Config,
	settingsService *settings.Service,
	classrooms *store.ClassroomStore,
	recordings *store.RecordingStore,
	attachments *store.AttachmentStore,
	mediaStats *store.MediaStatStore,
	remote *store.RemoteDesktopStore,
	control *realtime.ControlHub,
	storage *storage.Local,
	hub *realtime.Hub,
	log *slog.Logger,
) *ClassroomReaper {
	return &ClassroomReaper{
		cfg:         cfg,
		settings:    settingsService,
		classrooms:  classrooms,
		recordings:  recordings,
		attachments: attachments,
		mediaStats:  mediaStats,
		remote:      remote,
		control:     control,
		storage:     storage,
		hub:         hub,
		log:         log,
		interval:    time.Minute,
	}
}

func (r *ClassroomReaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *ClassroomReaper) tick(ctx context.Context) {
	r.purgeExpiredInvites(ctx)
	r.expireRooms(ctx)
	r.expireConsent(ctx)
	r.markInterrupted(ctx)
	r.purgeAttachments(ctx)
	r.purgeRecordings(ctx)
	r.purgeMediaStats(ctx)
	r.expireRemoteDesktop(ctx)
}

func (r *ClassroomReaper) expireRemoteDesktop(ctx context.Context) {
	if r.remote == nil {
		return
	}
	sessions, err := r.remote.ListExpired(ctx, time.Now(), 100)
	if err != nil {
		r.log.Warn("list expired remote desktop sessions failed", "error", err)
		return
	}
	for _, session := range sessions {
		status := model.RemoteDesktopStatusExpired
		reason := "timeout"
		if session.Status == model.RemoteDesktopStatusPending {
			reason = "consent_timeout"
		}
		_, _ = r.remote.UpdateStatus(ctx, session.ID, []string{session.Status}, status, map[string]any{
			"ended_at":   time.Now(),
			"end_reason": reason,
		})
		if r.control != nil {
			r.control.CloseSession(session.SessionID)
		}
		if r.hub != nil {
			r.hub.Broadcast(session.RoomID, gin.H{
				"type": "remote_desktop.stopped",
				"data": gin.H{"sessionId": session.SessionID, "reason": reason},
			})
		}
	}
}

func (r *ClassroomReaper) stopRemoteForRoom(ctx context.Context, roomID uint64, reason string) {
	if r.remote == nil {
		return
	}
	sessions, err := r.remote.ListActiveByRoom(ctx, roomID)
	if err != nil {
		return
	}
	for _, session := range sessions {
		_, _ = r.remote.UpdateStatus(ctx, session.ID, []string{session.Status}, model.RemoteDesktopStatusStopped, map[string]any{
			"ended_at":   time.Now(),
			"end_reason": reason,
		})
		if r.control != nil {
			r.control.CloseSession(session.SessionID)
		}
		if r.hub != nil {
			r.hub.Broadcast(roomID, gin.H{
				"type": "remote_desktop.stopped",
				"data": gin.H{"sessionId": session.SessionID, "reason": reason},
			})
		}
	}
}

func (r *ClassroomReaper) classroomConfig() config.ClassroomConfig {
	if r.settings != nil {
		return r.settings.Classroom()
	}
	return r.cfg.Classroom
}

func (r *ClassroomReaper) purgeExpiredInvites(ctx context.Context) {
	if r.classrooms == nil {
		return
	}
	if _, err := r.classrooms.DeleteExpiredInvites(ctx, time.Now()); err != nil {
		r.log.Warn("purge expired invites failed", "error", err)
	}
}

func (r *ClassroomReaper) expireRooms(ctx context.Context) {
	if r.classrooms == nil {
		return
	}
	rooms, err := r.classrooms.ListStaleRooms(ctx, time.Now().Add(-r.classroomConfig().IdleTTL.Std()), 200)
	if err != nil {
		r.log.Warn("list stale classrooms failed", "error", err)
		return
	}
	for _, room := range rooms {
		if room.State == model.ClassroomStateWaiting || room.State == model.ClassroomStateActive {
			if latest, ok, err := r.classrooms.LatestParticipantSeen(ctx, room.ID); err == nil && ok && latest.After(time.Now().Add(-r.classroomConfig().IdleTTL.Std())) {
				continue
			}
		}
		updated, err := r.classrooms.EndClassroomWithReason(ctx, room.ID, model.EndReasonTimeout)
		if err != nil {
			r.log.Warn("expire classroom failed", "error", err, "room_id", room.ID)
			continue
		}
		r.stopRemoteForRoom(ctx, room.ID, "room_ended")
		if r.hub != nil {
			r.hub.Broadcast(room.ID, gin.H{
				"type": "room.ended",
				"data": gin.H{"reason": model.EndReasonTimeout},
			})
			r.hub.CloseRoomWithReason(room.ID, "room_ended")
		}
		r.log.Info("classroom expired", "room_id", room.ID, "state", updated.State)
	}
}

func (r *ClassroomReaper) expireConsent(ctx context.Context) {
	if r.recordings == nil {
		return
	}
	recordings, err := r.recordings.ListStalePending(ctx, time.Now().Add(-r.classroomConfig().ConsentTTL.Std()), 100)
	if err != nil {
		r.log.Warn("list stale pending recordings failed", "error", err)
		return
	}
	for _, recording := range recordings {
		_, _ = r.recordings.UpdateStatus(ctx, recording.ID, []string{model.RecordingStatePendingConsent}, model.RecordingStateDeclined, map[string]any{
			"error":    "consent_timeout",
			"ended_at": time.Now(),
		})
		if r.classrooms != nil {
			_ = r.classrooms.SetRecordingState(ctx, recording.RoomID, model.RecordingStateOff)
		}
		if r.hub != nil {
			r.hub.SendTo(recording.RoomID, recording.UploaderParticipantID, gin.H{
				"type": "recording.declined",
				"data": gin.H{"recordingId": recording.RecordingID, "reason": "timeout"},
			})
		}
	}
}

func (r *ClassroomReaper) markInterrupted(ctx context.Context) {
	if r.recordings == nil {
		return
	}
	recordings, err := r.recordings.ListStaleRecording(ctx, time.Now().Add(-r.classroomConfig().IdleTTL.Std()), 100)
	if err != nil {
		r.log.Warn("list stale recordings failed", "error", err)
		return
	}
	for _, recording := range recordings {
		_, _ = r.recordings.UpdateStatus(ctx, recording.ID, []string{model.RecordingStateRecording, model.RecordingStateFinalizing}, model.RecordingStateInterrupted, map[string]any{
			"error":    "interrupted",
			"ended_at": time.Now(),
		})
		if r.classrooms != nil {
			_ = r.classrooms.SetRecordingState(ctx, recording.RoomID, model.RecordingStateInterrupted)
		}
	}
}

func (r *ClassroomReaper) purgeAttachments(ctx context.Context) {
	if r.attachments == nil || r.storage == nil {
		return
	}
	attachments, err := r.attachments.ListExpired(ctx, time.Now().Add(-r.classroomConfig().AttachmentRetention.Std()), 200)
	if err != nil {
		r.log.Warn("list expired attachments failed", "error", err)
		return
	}
	for _, attachment := range attachments {
		if err := r.storage.DeleteAttachment(attachment.FileKey); err != nil {
			r.log.Warn("delete attachment file failed", "error", err, "attachment_id", attachment.ID)
			continue
		}
		if err := r.attachments.MarkPurged(ctx, attachment.ID, time.Now()); err != nil {
			r.log.Warn("mark attachment purged failed", "error", err, "attachment_id", attachment.ID)
		}
	}
}

func (r *ClassroomReaper) purgeMediaStats(ctx context.Context) {
	if r.mediaStats == nil {
		return
	}
	if _, err := r.mediaStats.DeleteOlderThan(ctx, time.Now().Add(-30*24*time.Hour)); err != nil {
		r.log.Warn("purge media stats failed", "error", err)
	}
}

func (r *ClassroomReaper) purgeRecordings(ctx context.Context) {
	if r.recordings == nil || r.storage == nil {
		return
	}
	recordings, err := r.recordings.ListExpired(ctx, time.Now().Add(-r.classroomConfig().RecordingRetention.Std()), 100)
	if err != nil {
		r.log.Warn("list expired recordings failed", "error", err)
		return
	}
	for _, recording := range recordings {
		if err := r.storage.DeleteRecording(recording.RoomID, recording.RecordingID, recording.FileKey); err != nil {
			r.log.Warn("delete recording file failed", "error", err, "recording_id", recording.ID)
			continue
		}
		if err := r.recordings.Update(ctx, recording.ID, map[string]any{
			"status":   model.RecordingStatePurged,
			"file_key": "",
		}); err != nil {
			r.log.Warn("mark recording purged failed", "error", err, "recording_id", recording.ID)
		}
	}
}
