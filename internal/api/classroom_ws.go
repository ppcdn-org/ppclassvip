package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/model"
	"ppclassvip/internal/ppcdn"
	"ppclassvip/internal/realtime"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

const tokenProtocolPrefix = "ppclass.token."

type wsInbound struct {
	Type         string          `json:"type"`
	ClientMsgID  string          `json:"clientMsgId"`
	Body         string          `json:"body"`
	MessageType  string          `json:"messageType"`
	AttachmentID *uint64         `json:"attachmentId"`
	UpToID       uint64          `json:"upToId"`
	Leg          string          `json:"leg"`
	RTTMs        int             `json:"rttMs"`
	LossPct      float64         `json:"lossPct"`
	JitterMs     int             `json:"jitterMs"`
	BitrateBps   int64           `json:"bitrateBps"`
	Data         json.RawMessage `json:"data,omitempty"`
}

// normalize accepts both the documented {type, data:{...}} envelope and the
// legacy flat {type, ...fields} shape.
func (in wsInbound) normalize() wsInbound {
	if len(in.Data) == 0 {
		return in
	}
	var nested wsInbound
	if err := json.Unmarshal(in.Data, &nested); err != nil {
		return in
	}
	if nested.Type == "" {
		nested.Type = in.Type
	}
	nested.Data = nil
	return nested
}

func (h *ClassroomHandler) ConnectWS(c *gin.Context) {
	if h.hub == nil {
		Fail(c, http.StatusServiceUnavailable, "realtime service unavailable")
		return
	}
	roomID, ok := parseRoomID(c)
	if !ok {
		return
	}

	token, protocol := websocketToken(c.Request)
	if token == "" {
		Fail(c, http.StatusUnauthorized, "missing room token")
		return
	}
	claims, err := h.tokens.Parse(token)
	if err != nil || claims.Type != auth.TokenTypeRoom || claims.RoomID != roomID || claims.ParticipantID == 0 {
		Fail(c, http.StatusUnauthorized, "invalid room token")
		return
	}

	room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "room not found")
			return
		}
		h.log.Error("get classroom failed", "error", err, "room_id", roomID)
		Fail(c, http.StatusInternalServerError, "websocket failed")
		return
	}
	if roomIsClosed(room) {
		Fail(c, http.StatusConflict, "room is closed")
		return
	}
	participant, err := h.classrooms.GetParticipant(c.Request.Context(), roomID, claims.ParticipantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusForbidden, "participant not found")
			return
		}
		h.log.Error("get participant failed", "error", err, "room_id", roomID, "participant_id", claims.ParticipantID)
		Fail(c, http.StatusInternalServerError, "websocket failed")
		return
	}

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
		h.log.Warn("websocket upgrade failed", "error", err, "room_id", roomID, "participant_id", claims.ParticipantID)
		return
	}

	client := realtime.NewClient(roomID, claims.ParticipantID, claims.Role, conn)
	h.hub.Register(client)
	defer client.Close()
	defer func() {
		if !h.hub.Unregister(client) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.classrooms.SetParticipantOffline(ctx, roomID, claims.ParticipantID)
		if claims.Role == model.ParticipantRoleTeacher {
			h.markRecordingInterrupted(ctx, roomID)
		}
		if fresh, err := h.classrooms.GetClassroom(ctx, roomID); err == nil && fresh.ScreenOwnerParticipantID != nil && *fresh.ScreenOwnerParticipantID == claims.ParticipantID {
			leg := strings.TrimSpace(fresh.ScreenLeg)
			if leg == "" {
				leg = ppcdn.ScreenLeg(claims.Role)
			}
			if released, err := h.classrooms.ReleaseScreen(ctx, roomID, claims.ParticipantID); err == nil && released {
				h.broadcastScreen(ctx, roomID, claims.ParticipantID, leg, false)
			}
		}
		h.hub.Broadcast(roomID, gin.H{
			"type": "presence.left",
			"data": gin.H{"participantId": claims.ParticipantID, "reason": "disconnected"},
		})
		h.hub.Broadcast(roomID, gin.H{
			"type": "room.left",
			"data": gin.H{"participantId": claims.ParticipantID, "reason": "disconnected"},
		})
	}()

	if err := h.classrooms.SetParticipantOnline(c.Request.Context(), roomID, claims.ParticipantID); err != nil {
		h.log.Warn("set participant online failed", "error", err, "room_id", roomID, "participant_id", claims.ParticipantID)
	}
	participants, err := h.classrooms.ListParticipants(c.Request.Context(), roomID)
	if err != nil {
		h.log.Warn("list participants failed", "error", err, "room_id", roomID)
	}

	client.Send(gin.H{
		"type": "room.state",
		"data": gin.H{
			"room":              room,
			"participants":      participants,
			"selfParticipantId": claims.ParticipantID,
		},
	})
	h.replayRemoteDesktopState(c.Request.Context(), client, claims.ParticipantID, claims.Role)
	h.hub.BroadcastExcept(roomID, claims.ParticipantID, gin.H{
		"type": "presence.joined",
		"data": gin.H{
			"participantId": claims.ParticipantID,
			"role":          claims.Role,
		},
	})
	h.hub.BroadcastExcept(roomID, claims.ParticipantID, gin.H{
		"type": "room.joined",
		"data": gin.H{"participant": participant},
	})

	go client.WritePump()

	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = h.classrooms.SetParticipantOnline(ctx, roomID, claims.ParticipantID)
				cancel()
			case <-heartbeatDone:
				return
			}
		}
	}()
	defer close(heartbeatDone)

	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	for {
		var inbound wsInbound
		if err := conn.ReadJSON(&inbound); err != nil {
			return
		}
		inbound = inbound.normalize()
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		switch inbound.Type {
		case "ping", "room.ping":
			client.Send(gin.H{"type": "pong", "data": gin.H{"ts": time.Now().UnixMilli()}})
		case "chat.send":
			h.handleWSChat(c.Request.Context(), room, client, inbound)
		case "chat.read":
			h.hub.Broadcast(room.ID, gin.H{
				"type": "chat.read",
				"data": gin.H{"participantId": claims.ParticipantID, "upToId": inbound.UpToID},
			})
		case "typing.start", "typing.stop":
			h.hub.BroadcastExcept(room.ID, claims.ParticipantID, gin.H{
				"type": "typing",
				"data": gin.H{"participantId": claims.ParticipantID, "active": inbound.Type == "typing.start"},
			})
		case "screen.start.request":
			h.handleWSScreenRequest(c.Request.Context(), room, client)
		case "screen.stop":
			h.handleWSScreenStop(c.Request.Context(), room, client)
		case "recording.stop.request":
			h.handleWSRecordingStopRequest(room, client)
		case "media.ready":
			h.handleWSMediaReady(c.Request.Context(), room, client, inbound)
		case "media.stats":
			h.handleWSMediaStats(c.Request.Context(), room, client, inbound)
		default:
			client.Send(gin.H{
				"type": "error",
				"data": gin.H{"code": "unsupported_event", "message": "unsupported event type"},
			})
		}
	}
}

func (h *ClassroomHandler) handleWSChat(ctx context.Context, room *model.Classroom, client *realtime.Client, inbound wsInbound) {
	if h.limiter != nil && !h.limiter.Allow("ws-chat:"+uintString(client.ParticipantID), h.classroomConfig().MessageRatePerMinute) {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "rate_limited", "message": "too many messages"}})
		return
	}
	message, created, err := h.createMessage(ctx, room, client.ParticipantID, client.Role, messageInput{
		ClientMsgID:  inbound.ClientMsgID,
		Body:         inbound.Body,
		Type:         inbound.MessageType,
		AttachmentID: inbound.AttachmentID,
	})
	if err != nil {
		var inputErr *messageInputError
		if errors.As(err, &inputErr) {
			client.Send(gin.H{
				"type": "error",
				"data": gin.H{"code": "invalid_message", "message": inputErr.Error()},
			})
			return
		}
		h.log.Error("create websocket message failed", "error", err, "room_id", room.ID, "participant_id", client.ParticipantID)
		client.Send(gin.H{
			"type": "error",
			"data": gin.H{"code": "message_failed", "message": "send message failed"},
		})
		return
	}

	client.Send(gin.H{
		"type": "chat.ack",
		"data": gin.H{
			"clientMsgId": inbound.ClientMsgID,
			"created":     created,
			"message":     message,
		},
	})
	if created {
		h.hub.BroadcastExcept(room.ID, client.ParticipantID, gin.H{
			"type": "chat.message",
			"data": message,
		})
	}
}

func (h *ClassroomHandler) handleWSScreenRequest(ctx context.Context, room *model.Classroom, client *realtime.Client) {
	if client.Role == model.ParticipantRoleTeacher {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "screen_start_rest", "message": "use the screen start endpoint to publish your screen"}})
		return
	}
	teacher, err := h.classrooms.GetActiveTeacher(ctx, room.ID)
	if err != nil {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "screen_forbidden", "message": "teacher is not available"}})
		return
	}
	h.hub.SendTo(room.ID, teacher.ID, gin.H{
		"type": "screen.request",
		"data": gin.H{"participantId": client.ParticipantID},
	})
}

func (h *ClassroomHandler) handleWSScreenStop(ctx context.Context, room *model.Classroom, client *realtime.Client) {
	fresh, err := h.classrooms.GetClassroom(ctx, room.ID)
	if err != nil {
		return
	}
	room = fresh
	if room.ScreenOwnerParticipantID == nil {
		return
	}
	owner := *room.ScreenOwnerParticipantID
	if owner != client.ParticipantID && client.Role != model.ParticipantRoleTeacher {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "screen_forbidden", "message": "you are not sharing your screen"}})
		return
	}
	leg := strings.TrimSpace(room.ScreenLeg)
	if leg == "" {
		leg = ppcdn.ScreenLeg(client.Role)
	}
	released, err := h.classrooms.ReleaseScreen(ctx, room.ID, owner)
	if err != nil {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "screen_failed", "message": "stop screen sharing failed"}})
		return
	}
	if released {
		h.broadcastScreen(ctx, room.ID, owner, leg, false)
	}
}

func (h *ClassroomHandler) handleWSRecordingStopRequest(room *model.Classroom, client *realtime.Client) {
	if client.Role != model.ParticipantRoleStudent {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "recording_forbidden", "message": "only the student can request stopping a recording"}})
		return
	}
	teacher, err := h.classrooms.GetActiveTeacher(context.Background(), room.ID)
	if err != nil {
		return
	}
	h.hub.SendTo(room.ID, teacher.ID, gin.H{
		"type": "recording.stop.requested",
		"data": gin.H{"participantId": client.ParticipantID},
	})
}

func (h *ClassroomHandler) handleWSMediaReady(ctx context.Context, room *model.Classroom, client *realtime.Client, inbound wsInbound) {
	if fresh, err := h.classrooms.GetClassroom(ctx, room.ID); err == nil {
		room = fresh
	}
	leg := strings.TrimSpace(inbound.Leg)
	if leg != ppcdn.LegTeacherCamera && leg != ppcdn.LegStudentCamera && leg != ppcdn.LegTeacherScreen && leg != ppcdn.LegStudentScreen {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "invalid_request", "message": "invalid media leg"}})
		return
	}
	if validCameraLeg(leg) && leg != ppcdn.CameraLeg(client.Role) {
		client.Send(gin.H{"type": "error", "data": gin.H{"code": "media_forbidden", "message": "camera leg does not belong to this participant"}})
		return
	}
	if validScreenLeg(leg) {
		if room.ScreenOwnerParticipantID == nil || *room.ScreenOwnerParticipantID != client.ParticipantID {
			client.Send(gin.H{"type": "error", "data": gin.H{"code": "screen_forbidden", "message": "you are not sharing your screen"}})
			return
		}
	}
	h.hub.BroadcastExcept(room.ID, client.ParticipantID, gin.H{
		"type": "peer.publishing",
		"data": gin.H{
			"leg":           leg,
			"participantId": client.ParticipantID,
			"streamPath":    ppcdn.StreamName(room.ID, leg),
		},
	})
}

func (h *ClassroomHandler) handleWSMediaStats(ctx context.Context, room *model.Classroom, client *realtime.Client, inbound wsInbound) {
	if h.mediaStats == nil {
		return
	}
	leg := strings.TrimSpace(inbound.Leg)
	if !validCameraLeg(leg) && !validScreenLeg(leg) {
		return
	}
	stat := &model.ClassroomMediaStat{
		RoomID:        room.ID,
		ParticipantID: client.ParticipantID,
		Leg:           leg,
		RTTMs:         max(inbound.RTTMs, 0),
		LossPct:       max(inbound.LossPct, 0),
		JitterMs:      max(inbound.JitterMs, 0),
		BitrateBps:    max(inbound.BitrateBps, 0),
	}
	if err := h.mediaStats.Create(ctx, stat); err != nil {
		h.log.Debug("save media stats failed", "error", err, "room_id", room.ID)
	}
}

func websocketToken(r *http.Request) (token, protocol string) {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[len("Bearer "):]), ""
	}
	for _, item := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		item = strings.TrimSpace(item)
		if strings.HasPrefix(item, tokenProtocolPrefix) {
			return strings.TrimPrefix(item, tokenProtocolPrefix), item
		}
	}
	return "", ""
}

func (h *ClassroomHandler) checkWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	for _, allowed := range h.config.HTTP.CORSOrigins {
		if allowed == "*" || strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}
