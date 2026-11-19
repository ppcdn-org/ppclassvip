package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"
	"ppclassvip/internal/ppcdn"

	"github.com/gin-gonic/gin"
)

func roomWSInfo(roomID uint64) gin.H {
	return gin.H{
		"path":           "/api/v1/classrooms/" + uintString(roomID) + "/ws",
		"protocolPrefix": tokenProtocolPrefix,
	}
}

func (h *ClassroomHandler) mediaBundle(ctx context.Context, room *model.Classroom, participant *model.ClassroomParticipant) (gin.H, error) {
	if h.media == nil {
		return gin.H{"enabled": false}, nil
	}
	ownLeg := ppcdn.CameraLeg(participant.Role)
	publish, err := h.media.Publish(ctx, ppcdn.StreamName(room.ID, ownLeg))
	if err != nil {
		// Joining must stay possible for chat/files even when PPCDN is having a
		// transient outage; the client retries media credentials when it starts
		// the camera or shares a screen.
		h.log.Warn("publish credential unavailable during join", "room_id", room.ID, "leg", ownLeg, "error", err)
		return gin.H{
			"enabled":    true,
			"publish":    gin.H{},
			"subscribe":  gin.H{},
			"iceServers": []any{},
		}, nil
	}

	subscribe := gin.H{}
	peerLeg := ppcdn.PeerCameraLeg(participant.Role)
	if credential, err := h.media.Subscribe(ctx, ppcdn.StreamName(room.ID, peerLeg)); err == nil {
		subscribe[peerLeg] = credential
	} else {
		h.log.Debug("peer camera subscribe unavailable", "room_id", room.ID, "leg", peerLeg, "error", err)
	}
	if room.ScreenOwnerParticipantID != nil && *room.ScreenOwnerParticipantID != participant.ID {
		screenLeg := strings.TrimSpace(room.ScreenLeg)
		if screenLeg == "" {
			screenLeg = ppcdn.PeerScreenLeg(participant.Role)
		}
		if credential, err := h.media.Subscribe(ctx, ppcdn.StreamName(room.ID, screenLeg)); err == nil {
			subscribe[screenLeg] = credential
		} else {
			h.log.Debug("peer screen subscribe unavailable", "room_id", room.ID, "leg", screenLeg, "error", err)
		}
	}

	return gin.H{
		"enabled":    true,
		"publish":    gin.H{ownLeg: publish},
		"subscribe":  subscribe,
		"iceServers": []any{},
		"expiresAt":  publish.ExpiresAt,
	}, nil
}

func (h *ClassroomHandler) writeMediaError(c *gin.Context, err error) {
	var mediaErr *ppcdn.Error
	if errors.As(err, &mediaErr) {
		switch mediaErr.Code {
		case "account_in_arrears":
			FailCode(c, http.StatusPaymentRequired, mediaErr.Code, mediaErr.Message)
		case "no_origin_available", "no_healthy_origin", "no_edge_available", "ppcdn_unavailable", "whip_auth_unavailable":
			FailCode(c, http.StatusServiceUnavailable, mediaErr.Code, mediaErr.Message)
		default:
			FailCode(c, http.StatusBadGateway, mediaErr.Code, mediaErr.Message)
		}
		return
	}
	h.log.Error("media operation failed", "error", err, "request_id", middleware.GetRequestID(c))
	FailCode(c, http.StatusBadGateway, "media_error", "media service unavailable")
}

type mediaRefreshRequest struct {
	Leg  string `json:"leg" binding:"required"`
	Kind string `json:"kind"`
}

func (h *ClassroomHandler) RefreshMedia(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if roomIsClosed(room) {
		FailCode(c, http.StatusConflict, "room_ended", "room is closed")
		return
	}
	if h.media == nil {
		FailCode(c, http.StatusServiceUnavailable, "media_unavailable", "media service is not configured")
		return
	}

	var req mediaRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	req.Leg = strings.TrimSpace(req.Leg)
	if !validCameraLeg(req.Leg) && !validScreenLeg(req.Leg) {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid media leg")
		return
	}

	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		if req.Leg == ppcdn.CameraLeg(participant.Role) || req.Leg == ppcdn.ScreenLeg(participant.Role) {
			kind = ppcdn.MediaKindPublish
		} else {
			kind = ppcdn.MediaKindSubscribe
		}
	}

	var credential ppcdn.Credential
	var err error
	switch kind {
	case ppcdn.MediaKindPublish:
		ownCamera := req.Leg == ppcdn.CameraLeg(participant.Role)
		ownScreen := validScreenLeg(req.Leg) && req.Leg == ppcdn.ScreenLeg(participant.Role) &&
			room.ScreenOwnerParticipantID != nil && *room.ScreenOwnerParticipantID == participant.ID
		if !ownCamera && !ownScreen {
			FailCode(c, http.StatusForbidden, "media_forbidden", "only your own publish credential can be refreshed here")
			return
		}
		credential, err = h.media.Publish(c.Request.Context(), ppcdn.StreamName(room.ID, req.Leg))
	case ppcdn.MediaKindSubscribe:
		if req.Leg == ppcdn.CameraLeg(participant.Role) {
			FailCode(c, http.StatusForbidden, "media_forbidden", "cannot subscribe to your own camera leg")
			return
		}
		credential, err = h.media.Subscribe(c.Request.Context(), ppcdn.StreamName(room.ID, req.Leg))
	default:
		FailCode(c, http.StatusBadRequest, "invalid_request", "kind must be publish or subscribe")
		return
	}
	if err != nil {
		h.writeMediaError(c, err)
		return
	}
	OK(c, gin.H{"leg": req.Leg, "kind": kind, "credential": credential})
}

func (h *ClassroomHandler) StartScreen(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if roomIsClosed(room) {
		FailCode(c, http.StatusConflict, "room_ended", "room is closed")
		return
	}
	if participant.Role == model.ParticipantRoleStudent && !room.StudentScreenAllowed {
		remoteAllowed := false
		if h.remote != nil {
			remoteAllowed, _ = h.remote.HasActiveForParticipant(c.Request.Context(), room.ID, participant.ID)
		}
		if !remoteAllowed {
			FailCode(c, http.StatusForbidden, "screen_forbidden", "student screen sharing is not allowed")
			return
		}
	}
	if h.media == nil {
		FailCode(c, http.StatusServiceUnavailable, "media_unavailable", "media service is not configured")
		return
	}

	leg := ppcdn.ScreenLeg(participant.Role)
	claimed, err := h.classrooms.ClaimScreen(c.Request.Context(), room.ID, participant.ID, leg)
	if err != nil {
		h.log.Error("claim screen failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "screen_failed", "screen sharing failed")
		return
	}
	if !claimed {
		FailCode(c, http.StatusConflict, "screen_busy", "another participant is already sharing their screen")
		return
	}

	credential, err := h.media.Publish(c.Request.Context(), ppcdn.StreamName(room.ID, leg))
	if err != nil {
		_, _ = h.classrooms.ReleaseScreen(c.Request.Context(), room.ID, participant.ID)
		h.writeMediaError(c, err)
		return
	}

	h.broadcastScreen(c.Request.Context(), room.ID, participant.ID, leg, true)
	h.audit(c, room.ID, participant.ID, "screen.start", "leg="+leg)
	OK(c, gin.H{
		"leg":        leg,
		"publish":    credential,
		"streamPath": credential.StreamPath,
		"expiresAt":  credential.ExpiresAt,
	})
}

func (h *ClassroomHandler) StopScreen(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	owner := uint64(0)
	if room.ScreenOwnerParticipantID != nil {
		owner = *room.ScreenOwnerParticipantID
	}
	if owner == 0 {
		OK(c, gin.H{"stopped": false})
		return
	}
	if owner != participant.ID && participant.Role != model.ParticipantRoleTeacher {
		FailCode(c, http.StatusForbidden, "screen_forbidden", "only the screen owner or the teacher can stop sharing")
		return
	}

	leg := strings.TrimSpace(room.ScreenLeg)
	if leg == "" {
		leg = ppcdn.ScreenLeg(participant.Role)
	}
	released, err := h.classrooms.ReleaseScreen(c.Request.Context(), room.ID, owner)
	if err != nil {
		h.log.Error("release screen failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "screen_failed", "stop screen sharing failed")
		return
	}
	if released {
		h.broadcastScreen(c.Request.Context(), room.ID, owner, leg, false)
		h.audit(c, room.ID, participant.ID, "screen.stop", "leg="+leg)
	}
	OK(c, gin.H{"stopped": released})
}

func (h *ClassroomHandler) broadcastScreen(ctx context.Context, roomID, ownerID uint64, leg string, active bool) {
	if h.hub == nil {
		return
	}
	h.hub.Broadcast(roomID, gin.H{
		"type": "screen.state",
		"data": gin.H{
			"owner":  ownerID,
			"active": active,
			"leg":    leg,
		},
	})
	if active {
		h.hub.Broadcast(roomID, gin.H{
			"type": "peer.publishing",
			"data": gin.H{
				"leg":           leg,
				"participantId": ownerID,
				"streamPath":    ppcdn.StreamName(roomID, leg),
			},
		})
	} else {
		h.hub.Broadcast(roomID, gin.H{
			"type": "peer.unpublished",
			"data": gin.H{"leg": leg, "participantId": ownerID},
		})
	}
}

type controlRequest struct {
	Action        string `json:"action" binding:"required"`
	ParticipantID uint64 `json:"participantId"`
	Muted         bool   `json:"muted"`
	Granted       bool   `json:"granted"`
	Enabled       bool   `json:"enabled"`
}

func (h *ClassroomHandler) Control(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	if roomIsClosed(room) {
		FailCode(c, http.StatusConflict, "room_ended", "room is closed")
		return
	}
	var req controlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	switch strings.TrimSpace(req.Action) {
	case "mute":
		if req.ParticipantID == 0 {
			FailCode(c, http.StatusBadRequest, "invalid_request", "participantId is required")
			return
		}
		if _, err := h.classrooms.GetParticipant(c.Request.Context(), room.ID, req.ParticipantID); err != nil {
			FailCode(c, http.StatusNotFound, "participant_not_found", "participant not found")
			return
		}
		if err := h.classrooms.SetParticipantMuted(c.Request.Context(), room.ID, req.ParticipantID, req.Muted); err != nil {
			h.log.Error("set participant muted failed", "error", err, "room_id", room.ID)
			FailCode(c, http.StatusInternalServerError, "control_failed", "mute failed")
			return
		}
		if h.hub != nil {
			h.hub.Broadcast(room.ID, gin.H{
				"type": "control.mute",
				"data": gin.H{"participantId": req.ParticipantID, "muted": req.Muted},
			})
		}
		h.audit(c, room.ID, 0, "control.mute", "participant="+uintString(req.ParticipantID))
	case "screen.permission":
		if err := h.classrooms.UpdateRoomFields(c.Request.Context(), room.ID, map[string]any{"student_screen_allowed": req.Granted}); err != nil {
			h.log.Error("update screen permission failed", "error", err, "room_id", room.ID)
			FailCode(c, http.StatusInternalServerError, "control_failed", "update permission failed")
			return
		}
		if h.hub != nil {
			h.hub.Broadcast(room.ID, gin.H{
				"type": "control.screen.permission",
				"data": gin.H{"granted": req.Granted},
			})
		}
		h.audit(c, room.ID, 0, "control.screen.permission", "granted="+boolString(req.Granted))
	case "chat":
		if err := h.classrooms.UpdateRoomFields(c.Request.Context(), room.ID, map[string]any{"chat_enabled": req.Enabled}); err != nil {
			h.log.Error("update chat permission failed", "error", err, "room_id", room.ID)
			FailCode(c, http.StatusInternalServerError, "control_failed", "update chat permission failed")
			return
		}
		if h.hub != nil {
			h.hub.Broadcast(room.ID, gin.H{
				"type": "control.chat",
				"data": gin.H{"enabled": req.Enabled},
			})
		}
		h.audit(c, room.ID, 0, "control.chat", "enabled="+boolString(req.Enabled))
	default:
		FailCode(c, http.StatusBadRequest, "invalid_request", "unsupported control action")
		return
	}
	OK(c, gin.H{"ok": true})
}

func (h *ClassroomHandler) Leave(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if err := h.classrooms.LeaveParticipant(c.Request.Context(), room.ID, participant.ID); err != nil {
		h.log.Error("leave participant failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "leave_failed", "leave classroom failed")
		return
	}
	if room.ScreenOwnerParticipantID != nil && *room.ScreenOwnerParticipantID == participant.ID {
		leg := strings.TrimSpace(room.ScreenLeg)
		if leg == "" {
			leg = ppcdn.ScreenLeg(participant.Role)
		}
		if released, err := h.classrooms.ReleaseScreen(c.Request.Context(), room.ID, participant.ID); err == nil && released {
			h.broadcastScreen(c.Request.Context(), room.ID, participant.ID, leg, false)
		}
	}
	if h.hub != nil {
		h.hub.Broadcast(room.ID, gin.H{
			"type": "presence.left",
			"data": gin.H{"participantId": participant.ID, "reason": "leave"},
		})
		h.hub.Broadcast(room.ID, gin.H{
			"type": "room.left",
			"data": gin.H{"participantId": participant.ID, "reason": "leave"},
		})
	}
	h.audit(c, room.ID, participant.ID, "room.leave", "")
	OK(c, gin.H{"left": true})
}

func (h *ClassroomHandler) requireRoomParticipant(c *gin.Context) (*model.Classroom, *model.ClassroomParticipant, bool) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return nil, nil, false
	}
	participantID := middleware.ParticipantID(c)
	if participantID == 0 || middleware.RoomID(c) != roomID {
		FailCode(c, http.StatusForbidden, "room_forbidden", "room token required")
		return nil, nil, false
	}
	room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
	if err != nil {
		h.writeClassroomError(c, err)
		return nil, nil, false
	}
	participant, err := h.classrooms.GetParticipant(c.Request.Context(), roomID, participantID)
	if err != nil {
		h.writeClassroomError(c, err)
		return nil, nil, false
	}
	return room, participant, true
}

func (h *ClassroomHandler) requireTeacherRoom(c *gin.Context) (*model.Classroom, bool) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return nil, false
	}
	room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
	if err != nil {
		h.writeClassroomError(c, err)
		return nil, false
	}
	if userID := middleware.UserID(c); userID != 0 && (room.TeacherUserID == userID || middleware.Role(c) == model.RoleAdmin) {
		return room, true
	}
	participantID := middleware.ParticipantID(c)
	if participantID != 0 && middleware.RoomID(c) == roomID {
		participant, err := h.classrooms.GetParticipant(c.Request.Context(), roomID, participantID)
		if err == nil && participant.Role == model.ParticipantRoleTeacher {
			return room, true
		}
	}
	FailCode(c, http.StatusForbidden, "room_forbidden", "teacher access required")
	return nil, false
}

func validCameraLeg(leg string) bool {
	return leg == ppcdn.LegTeacherCamera || leg == ppcdn.LegStudentCamera
}

func validScreenLeg(leg string) bool {
	return leg == ppcdn.LegTeacherScreen || leg == ppcdn.LegStudentScreen
}

func uintString(v uint64) string {
	return strconv.FormatUint(v, 10)
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
