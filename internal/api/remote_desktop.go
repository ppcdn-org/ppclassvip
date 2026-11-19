package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"
	"ppclassvip/internal/ppcdn"
	"ppclassvip/internal/realtime"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

const (
	remoteDesktopConsentTTL    = 60 * time.Second
	remoteDesktopSessionTTL    = 30 * time.Minute
	remoteDesktopControlPrefix = "ppclass.control."
	remoteControlMaxMessage    = 16 * 1024
	remoteControlMaxPerSecond  = 300
)

type remoteDesktopRequest struct {
	Mode string `json:"mode"`
}

func (h *ClassroomHandler) RequestRemoteDesktop(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	if roomIsClosed(room) {
		FailCode(c, http.StatusConflict, "room_ended", "room is closed")
		return
	}
	if room.ScreenOwnerParticipantID != nil {
		FailCode(c, http.StatusConflict, "screen_busy", "stop screen sharing before requesting remote desktop")
		return
	}
	if h.remote == nil || h.control == nil {
		FailCode(c, http.StatusServiceUnavailable, "remote_desktop_unavailable", "remote desktop is not configured")
		return
	}
	if h.recordings != nil {
		if recording, err := h.recordings.GetActiveByRoom(c.Request.Context(), room.ID); err == nil {
			_ = recording
			FailCode(c, http.StatusConflict, "recording_active", "stop recording before requesting remote desktop")
			return
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "check recording state failed")
			return
		}
	}
	if existing, err := h.remote.GetActiveByRoom(c.Request.Context(), room.ID); err == nil {
		FailCode(c, http.StatusConflict, "remote_desktop_active", "a remote desktop session already exists")
		_ = existing
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "check remote desktop state failed")
		return
	}
	var req remoteDesktopRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
			return
		}
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = model.RemoteDesktopModeViewOnly
	}
	if mode != model.RemoteDesktopModeViewOnly && mode != model.RemoteDesktopModeControl {
		FailCode(c, http.StatusBadRequest, "invalid_request", "mode must be view_only or control")
		return
	}
	student, err := h.classrooms.GetActiveStudent(c.Request.Context(), room.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusConflict, "student_not_joined", "student has not joined the classroom")
			return
		}
		FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "load student failed")
		return
	}
	teacherName := "老师"
	if participantID := middleware.ParticipantID(c); participantID != 0 {
		if teacher, err := h.classrooms.GetParticipant(c.Request.Context(), room.ID, participantID); err == nil {
			teacherName = teacher.DisplayName
		}
	} else if teacher, err := h.classrooms.GetActiveTeacher(c.Request.Context(), room.ID); err == nil {
		teacherName = teacher.DisplayName
	}
	sessionID, err := newRemoteSessionID()
	if err != nil {
		FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "create session failed")
		return
	}
	now := time.Now()
	expiresAt := now.Add(remoteDesktopConsentTTL)
	session := &model.RemoteDesktopSession{
		SessionID:            sessionID,
		RoomID:               room.ID,
		StudentParticipantID: student.ID,
		Mode:                 mode,
		Status:               model.RemoteDesktopStatusPending,
		ExpiresAt:            &expiresAt,
	}
	teacherParticipantID := middleware.ParticipantID(c)
	if teacherParticipantID == 0 {
		if teacher, err := h.classrooms.GetActiveTeacher(c.Request.Context(), room.ID); err == nil {
			teacherParticipantID = teacher.ID
		}
	}
	if teacherParticipantID == 0 {
		FailCode(c, http.StatusConflict, "room_forbidden", "teacher must join the classroom first")
		return
	}
	session.TeacherParticipantID = teacherParticipantID
	if err := h.remote.Create(c.Request.Context(), session); err != nil {
		h.log.Error("create remote desktop session failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "create session failed")
		return
	}
	h.hub.SendTo(room.ID, student.ID, gin.H{
		"type": "remote_desktop.request",
		"data": gin.H{
			"sessionId":   sessionID,
			"teacherName": teacherName,
			"mode":        mode,
			"expiresAt":   expiresAt.UTC().Format(time.RFC3339),
		},
	})
	h.audit(c, room.ID, teacherParticipantID, "remote_desktop.request", "session="+sessionID+" mode="+mode)
	time.AfterFunc(remoteDesktopConsentTTL, func() {
		h.expirePendingRemoteSession(session.ID, sessionID, room.ID)
	})
	Created(c, gin.H{
		"session":   session,
		"expiresAt": expiresAt,
	})
}

type consentRemoteDesktopRequest struct {
	Mode string `json:"mode"`
}

func (h *ClassroomHandler) ConsentRemoteDesktop(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if participant.Role != model.ParticipantRoleStudent {
		FailCode(c, http.StatusForbidden, "remote_desktop_forbidden", "only the student can consent")
		return
	}
	session, ok := h.loadRemoteSession(c, room.ID)
	if !ok {
		return
	}
	if session.StudentParticipantID != participant.ID {
		FailCode(c, http.StatusForbidden, "remote_desktop_forbidden", "session does not belong to this student")
		return
	}
	if session.Status != model.RemoteDesktopStatusPending {
		FailCode(c, http.StatusConflict, "remote_desktop_state", "session is not waiting for consent")
		return
	}
	now := time.Now()
	if session.ExpiresAt != nil && now.After(*session.ExpiresAt) {
		h.finishRemoteSession(context.Background(), session, model.RemoteDesktopStatusExpired, "consent_timeout", true)
		FailCode(c, http.StatusGone, "remote_desktop_expired", "remote desktop request expired")
		return
	}
	var req consentRemoteDesktopRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
			return
		}
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = session.Mode
	}
	if mode != model.RemoteDesktopModeViewOnly && mode != model.RemoteDesktopModeControl {
		FailCode(c, http.StatusBadRequest, "invalid_request", "mode must be view_only or control")
		return
	}
	if session.Mode == model.RemoteDesktopModeViewOnly && mode != model.RemoteDesktopModeViewOnly {
		FailCode(c, http.StatusBadRequest, "invalid_request", "teacher requested view-only mode")
		return
	}
	expiresAt := now.Add(remoteDesktopSessionTTL)
	agentCode := newRemoteAgentCode()
	agentCodeExpiresAt := now.Add(5 * time.Minute)
	updated, err := h.remote.UpdateStatus(c.Request.Context(), session.ID, []string{model.RemoteDesktopStatusPending}, model.RemoteDesktopStatusActive, map[string]any{
		"mode":                  mode,
		"consent_at":            now,
		"started_at":            now,
		"expires_at":            expiresAt,
		"end_reason":            "",
		"agent_code_hash":       hashRemoteAgentCode(agentCode),
		"agent_code_expires_at": agentCodeExpiresAt,
	})
	if err != nil || !updated {
		FailCode(c, http.StatusConflict, "remote_desktop_state", "session state changed")
		return
	}
	controllerToken, err := h.tokens.IssueRemoteControl(session.SessionID, room.ID, session.TeacherParticipantID, model.RemoteDesktopRoleController, remoteDesktopSessionTTL)
	if err != nil {
		FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "issue control token failed")
		return
	}
	path := remoteControlPath(room.ID, session.SessionID)
	h.hub.SendTo(room.ID, session.TeacherParticipantID, gin.H{
		"type": "remote_desktop.consented",
		"data": gin.H{
			"sessionId":       session.SessionID,
			"mode":            mode,
			"controllerToken": controllerToken,
			"controlWsPath":   path,
			"expiresAt":       expiresAt.UTC().Format(time.RFC3339),
		},
	})
	h.hub.Broadcast(room.ID, gin.H{
		"type": "remote_desktop.state",
		"data": gin.H{
			"sessionId": session.SessionID,
			"status":    model.RemoteDesktopStatusActive,
			"mode":      mode,
			"startedAt": now,
			"expiresAt": expiresAt.UTC().Format(time.RFC3339),
		},
	})
	h.audit(c, room.ID, participant.ID, "remote_desktop.consent", "session="+session.SessionID+" mode="+mode)
	time.AfterFunc(remoteDesktopSessionTTL, func() {
		h.finishRemoteSessionByID(session.ID, model.RemoteDesktopStatusExpired, "timeout", true)
	})
	session.Status = model.RemoteDesktopStatusActive
	session.Mode = mode
	session.ConsentAt = &now
	session.StartedAt = &now
	session.ExpiresAt = &expiresAt
	OK(c, gin.H{
		"session":            session,
		"agentCode":          agentCode,
		"agentCodeExpiresAt": agentCodeExpiresAt,
		"controlWsPath":      path,
		"expiresAt":          expiresAt,
	})
}

func (h *ClassroomHandler) DeclineRemoteDesktop(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	session, ok := h.loadRemoteSession(c, room.ID)
	if !ok {
		return
	}
	if participant.Role != model.ParticipantRoleStudent || session.StudentParticipantID != participant.ID {
		FailCode(c, http.StatusForbidden, "remote_desktop_forbidden", "only the student can decline")
		return
	}
	now := time.Now()
	updated, err := h.remote.UpdateStatus(c.Request.Context(), session.ID, []string{model.RemoteDesktopStatusPending}, model.RemoteDesktopStatusDeclined, map[string]any{
		"ended_at":   now,
		"end_reason": "declined",
	})
	if err != nil || !updated {
		FailCode(c, http.StatusConflict, "remote_desktop_state", "session is not waiting for consent")
		return
	}
	h.hub.SendTo(room.ID, session.TeacherParticipantID, gin.H{
		"type": "remote_desktop.declined",
		"data": gin.H{"sessionId": session.SessionID, "reason": "declined"},
	})
	h.audit(c, room.ID, participant.ID, "remote_desktop.decline", "session="+session.SessionID)
	OK(c, gin.H{"declined": true})
}

func (h *ClassroomHandler) StopRemoteDesktop(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	session, ok := h.loadRemoteSession(c, room.ID)
	if !ok {
		return
	}
	if participant.ID != session.TeacherParticipantID && participant.ID != session.StudentParticipantID {
		FailCode(c, http.StatusForbidden, "remote_desktop_forbidden", "not a participant of this session")
		return
	}
	reason := "teacher_stop"
	if participant.ID == session.StudentParticipantID {
		reason = "student_stop"
	}
	h.finishRemoteSession(c.Request.Context(), session, model.RemoteDesktopStatusStopped, reason, true)
	h.audit(c, room.ID, participant.ID, "remote_desktop.stop", "session="+session.SessionID+" reason="+reason)
	OK(c, gin.H{"stopped": true})
}

func (h *ClassroomHandler) GetRemoteDesktop(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	session, ok := h.loadRemoteSession(c, room.ID)
	if !ok {
		return
	}
	if participant.ID != session.TeacherParticipantID && participant.ID != session.StudentParticipantID {
		FailCode(c, http.StatusForbidden, "remote_desktop_forbidden", "not a participant of this session")
		return
	}
	data := gin.H{"session": session}
	if session.Status == model.RemoteDesktopStatusActive {
		path := remoteControlPath(room.ID, session.SessionID)
		data["controlWsPath"] = path
		if participant.ID == session.TeacherParticipantID {
			if token, err := h.tokens.IssueRemoteControl(session.SessionID, room.ID, session.TeacherParticipantID, model.RemoteDesktopRoleController, remoteDesktopSessionTTL); err == nil {
				data["controllerToken"] = token
			}
		} else {
			if token, err := h.tokens.IssueRemoteControl(session.SessionID, room.ID, session.StudentParticipantID, model.RemoteDesktopRoleAgent, remoteDesktopSessionTTL); err == nil {
				data["agentToken"] = token
			}
		}
	}
	OK(c, data)
}

type agentPairRequest struct {
	Code     string `json:"code"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

func (h *ClassroomHandler) PairAgent(c *gin.Context) {
	var req agentPairRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if len(code) < 6 || len(code) > 12 {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid agent code")
		return
	}
	session, err := h.remote.GetByAgentCodeHash(c.Request.Context(), hashRemoteAgentCode(code))
	if err != nil {
		FailCode(c, http.StatusNotFound, "agent_code_invalid", "agent code is invalid")
		return
	}
	if session.Status != model.RemoteDesktopStatusActive {
		FailCode(c, http.StatusConflict, "remote_desktop_state", "remote desktop is not active")
		return
	}
	if session.AgentCodeExpiresAt == nil || time.Now().After(*session.AgentCodeExpiresAt) {
		FailCode(c, http.StatusGone, "agent_code_expired", "agent code expired")
		return
	}
	agentToken, err := h.tokens.IssueRemoteControl(session.SessionID, session.RoomID, session.StudentParticipantID, model.RemoteDesktopRoleAgent, remoteDesktopSessionTTL)
	if err != nil {
		FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "issue agent token failed")
		return
	}
	if h.media == nil {
		FailCode(c, http.StatusServiceUnavailable, "media_unavailable", "media service is not configured")
		return
	}
	screenPublish, err := h.media.Publish(c.Request.Context(), ppcdn.StreamName(session.RoomID, ppcdn.LegStudentScreen))
	if err != nil {
		h.writeMediaError(c, err)
		return
	}
	teacherName := "teacher"
	if teacher, err := h.classrooms.GetParticipant(c.Request.Context(), session.RoomID, session.TeacherParticipantID); err == nil {
		teacherName = teacher.DisplayName
	}
	_ = h.remote.Update(c.Request.Context(), session.ID, map[string]any{
		"agent_version": strings.TrimSpace(req.Version),
		"platform":      strings.TrimSpace(req.Platform),
	})
	if h.hub != nil {
		h.hub.Broadcast(session.RoomID, gin.H{
			"type": "screen.state",
			"data": gin.H{"owner": session.StudentParticipantID, "active": true, "leg": ppcdn.LegStudentScreen},
		})
		h.hub.Broadcast(session.RoomID, gin.H{
			"type": "peer.publishing",
			"data": gin.H{
				"leg":           ppcdn.LegStudentScreen,
				"participantId": session.StudentParticipantID,
				"streamPath":    screenPublish.StreamPath,
			},
		})
	}
	OK(c, gin.H{
		"sessionId":     session.SessionID,
		"agentToken":    agentToken,
		"controlWsPath": remoteControlPath(session.RoomID, session.SessionID),
		"expiresAt":     session.ExpiresAt,
		"mode":          session.Mode,
		"teacherName":   teacherName,
		"screenPublish": screenPublish,
	})
}

type agentStopRequest struct {
	Token string `json:"token"`
}

func (h *ClassroomHandler) AgentStop(c *gin.Context) {
	var req agentStopRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		FailCode(c, http.StatusBadRequest, "invalid_request", "missing agent token")
		return
	}
	claims, err := h.tokens.Parse(strings.TrimSpace(req.Token))
	if err != nil || claims.Type != auth.TokenTypeRemoteControl || claims.Role != model.RemoteDesktopRoleAgent {
		Fail(c, http.StatusUnauthorized, "invalid agent token")
		return
	}
	session, err := h.remote.Get(c.Request.Context(), claims.RoomID, claims.SessionID)
	if err != nil || session.StudentParticipantID != claims.ParticipantID {
		FailCode(c, http.StatusNotFound, "remote_desktop_not_found", "remote desktop session not found")
		return
	}
	h.finishRemoteSession(c.Request.Context(), session, model.RemoteDesktopStatusStopped, "local_declined", true)
	OK(c, gin.H{"stopped": true})
}

func newRemoteAgentCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "PPCLASS1"
	}
	out := make([]byte, len(raw))
	for i, value := range raw {
		out[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(out)
}

func hashRemoteAgentCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}
func (h *ClassroomHandler) RemoteControlWS(c *gin.Context) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return
	}
	sessionID := strings.TrimSpace(c.Param("sid"))
	if sessionID == "" {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid session id")
		return
	}
	token := controlToken(c.Request)
	if token == "" {
		Fail(c, http.StatusUnauthorized, "missing control token")
		return
	}
	claims, err := h.tokens.Parse(token)
	if err != nil || claims.Type != auth.TokenTypeRemoteControl || claims.RoomID != roomID || claims.SessionID != sessionID {
		Fail(c, http.StatusUnauthorized, "invalid control token")
		return
	}
	session, err := h.remote.Get(c.Request.Context(), roomID, sessionID)
	if err != nil || session.Status != model.RemoteDesktopStatusActive {
		FailCode(c, http.StatusConflict, "remote_desktop_state", "remote desktop is not active")
		return
	}
	if session.ExpiresAt != nil && time.Now().After(*session.ExpiresAt) {
		h.finishRemoteSession(c.Request.Context(), session, model.RemoteDesktopStatusExpired, "timeout", true)
		FailCode(c, http.StatusGone, "remote_desktop_expired", "remote desktop expired")
		return
	}
	if claims.Role == model.RemoteDesktopRoleController && claims.ParticipantID != session.TeacherParticipantID {
		FailCode(c, http.StatusForbidden, "remote_desktop_forbidden", "invalid controller")
		return
	}
	if claims.Role == model.RemoteDesktopRoleAgent && claims.ParticipantID != session.StudentParticipantID {
		FailCode(c, http.StatusForbidden, "remote_desktop_forbidden", "invalid agent")
		return
	}
	protocol := controlProtocol(c.Request)
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
		CheckOrigin:      h.checkWebSocketOrigin,
	}
	if protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := h.control.Register(sessionID, claims.Role, conn)
	defer func() {
		removed := h.control.Unregister(client)
		client.Close()
		if removed && claims.Role == model.RemoteDesktopRoleAgent {
			h.finishRemoteSession(context.Background(), session, model.RemoteDesktopStatusStopped, "agent_offline", true)
		}
	}()
	go client.WritePump()
	conn.SetReadLimit(remoteControlMaxMessage)
	if session.ExpiresAt != nil {
		_ = conn.SetReadDeadline(*session.ExpiresAt)
	}
	windowStart := time.Now()
	windowCount := 0
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		now := time.Now()
		if now.Sub(windowStart) > time.Second {
			windowStart = now
			windowCount = 0
		}
		windowCount++
		if windowCount > remoteControlMaxPerSecond {
			return
		}
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &event); err != nil {
			continue
		}
		if claims.Role == model.RemoteDesktopRoleController {
			switch event.Type {
			case "mouse.move", "mouse.button", "mouse.wheel", "key":
				h.control.Forward(sessionID, claims.Role, payload)
			}
		}
	}
}

func (h *ClassroomHandler) replayRemoteDesktopState(ctx context.Context, client *realtime.Client, participantID uint64, role string) {
	if h.remote == nil {
		return
	}
	session, err := h.remote.GetActiveByRoom(ctx, client.RoomID)
	if err != nil {
		return
	}
	if session.Status == model.RemoteDesktopStatusPending {
		if session.StudentParticipantID != participantID {
			return
		}
		teacherName := "老师"
		if teacher, err := h.classrooms.GetParticipant(ctx, client.RoomID, session.TeacherParticipantID); err == nil {
			teacherName = teacher.DisplayName
		}
		expiresAt := ""
		if session.ExpiresAt != nil {
			expiresAt = session.ExpiresAt.UTC().Format(time.RFC3339)
		}
		client.Send(gin.H{
			"type": "remote_desktop.request",
			"data": gin.H{
				"sessionId":   session.SessionID,
				"teacherName": teacherName,
				"mode":        session.Mode,
				"expiresAt":   expiresAt,
			},
		})
		return
	}
	if session.Status != model.RemoteDesktopStatusActive {
		return
	}
	if participantID != session.TeacherParticipantID && participantID != session.StudentParticipantID {
		return
	}
	if role == model.ParticipantRoleTeacher && participantID == session.TeacherParticipantID {
		controllerToken, err := h.tokens.IssueRemoteControl(session.SessionID, client.RoomID, session.TeacherParticipantID, model.RemoteDesktopRoleController, remoteDesktopSessionTTL)
		if err == nil {
			expiresAt := ""
			if session.ExpiresAt != nil {
				expiresAt = session.ExpiresAt.UTC().Format(time.RFC3339)
			}
			client.Send(gin.H{
				"type": "remote_desktop.consented",
				"data": gin.H{
					"sessionId":       session.SessionID,
					"mode":            session.Mode,
					"controllerToken": controllerToken,
					"controlWsPath":   remoteControlPath(client.RoomID, session.SessionID),
					"expiresAt":       expiresAt,
				},
			})
		}
		return
	}
	client.Send(gin.H{
		"type": "remote_desktop.state",
		"data": gin.H{
			"sessionId": session.SessionID,
			"status":    session.Status,
			"mode":      session.Mode,
			"startedAt": session.StartedAt,
			"expiresAt": session.ExpiresAt,
		},
	})
}
func (h *ClassroomHandler) loadRemoteSession(c *gin.Context, roomID uint64) (*model.RemoteDesktopSession, bool) {
	sessionID := strings.TrimSpace(c.Param("sid"))
	if sessionID == "" {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid session id")
		return nil, false
	}
	session, err := h.remote.Get(c.Request.Context(), roomID, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusNotFound, "remote_desktop_not_found", "remote desktop session not found")
			return nil, false
		}
		FailCode(c, http.StatusInternalServerError, "remote_desktop_failed", "load remote desktop failed")
		return nil, false
	}
	return session, true
}

func (h *ClassroomHandler) expirePendingRemoteSession(id uint64, sessionID string, roomID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, err := h.remote.UpdateStatus(ctx, id, []string{model.RemoteDesktopStatusPending}, model.RemoteDesktopStatusExpired, map[string]any{
		"ended_at":   time.Now(),
		"end_reason": "consent_timeout",
	})
	if err != nil || !updated {
		return
	}
	h.hub.Broadcast(roomID, gin.H{
		"type": "remote_desktop.stopped",
		"data": gin.H{"sessionId": sessionID, "reason": "consent_timeout"},
	})
}

func (h *ClassroomHandler) finishRemoteSessionByID(id uint64, status, reason string, notify bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := h.remote.GetByID(ctx, id)
	if err != nil {
		return
	}
	h.finishRemoteSession(ctx, session, status, reason, notify)
}

func (h *ClassroomHandler) finishRemoteSession(ctx context.Context, session *model.RemoteDesktopSession, status, reason string, notify bool) {
	if session == nil {
		return
	}
	now := time.Now()
	_, _ = h.remote.UpdateStatus(ctx, session.ID, []string{
		model.RemoteDesktopStatusPending,
		model.RemoteDesktopStatusActive,
	}, status, map[string]any{
		"ended_at":   now,
		"end_reason": reason,
	})
	if h.control != nil {
		h.control.CloseSession(session.SessionID)
	}
	if notify && h.hub != nil {
		h.hub.Broadcast(session.RoomID, gin.H{
			"type": "screen.state",
			"data": gin.H{"owner": session.StudentParticipantID, "active": false, "leg": ppcdn.LegStudentScreen},
		})
		h.hub.Broadcast(session.RoomID, gin.H{
			"type": "peer.unpublished",
			"data": gin.H{"leg": ppcdn.LegStudentScreen, "participantId": session.StudentParticipantID},
		})
		h.hub.Broadcast(session.RoomID, gin.H{
			"type": "remote_desktop.stopped",
			"data": gin.H{"sessionId": session.SessionID, "reason": reason},
		})
	}
}

func (h *ClassroomHandler) stopRemoteDesktopForRoom(ctx context.Context, roomID uint64, reason string) {
	if h.remote == nil || h.control == nil {
		return
	}
	sessions, err := h.remote.ListActiveByRoom(ctx, roomID)
	if err != nil {
		return
	}
	for index := range sessions {
		h.finishRemoteSession(ctx, &sessions[index], model.RemoteDesktopStatusStopped, reason, true)
	}
}

func newRemoteSessionID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func remoteControlPath(roomID uint64, sessionID string) string {
	return "/api/v1/classrooms/" + uintString(roomID) + "/remote-desktop/" + sessionID + "/control"
}

func controlToken(r *http.Request) string {
	for _, item := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		item = strings.TrimSpace(item)
		if strings.HasPrefix(item, remoteDesktopControlPrefix) {
			return strings.TrimPrefix(item, remoteDesktopControlPrefix)
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func controlProtocol(r *http.Request) string {
	for _, item := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		item = strings.TrimSpace(item)
		if strings.HasPrefix(item, remoteDesktopControlPrefix) {
			return item
		}
	}
	return ""
}
