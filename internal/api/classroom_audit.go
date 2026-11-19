package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"

	"github.com/gin-gonic/gin"
)

func (h *ClassroomHandler) audit(c *gin.Context, roomID, participantID uint64, action, detail string) {
	if h.audits == nil {
		return
	}
	entry := &model.ClassroomAuditLog{
		RoomID:        roomID,
		ParticipantID: participantID,
		UserID:        middleware.UserID(c),
		Action:        action,
		Detail:        detail,
		ClientIP:      c.ClientIP(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.audits.Create(ctx, entry); err != nil {
		h.log.Warn("write audit log failed", "error", err, "room_id", roomID, "action", action)
	}
}

func (h *ClassroomHandler) ListAudit(c *gin.Context) {
	if h.audits == nil {
		FailCode(c, http.StatusServiceUnavailable, "audit_unavailable", "audit service is not configured")
		return
	}
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			FailCode(c, http.StatusBadRequest, "invalid_request", "invalid limit")
			return
		}
		limit = parsed
	}
	entries, err := h.audits.ListByRoom(c.Request.Context(), room.ID, limit)
	if err != nil {
		h.log.Error("list audit logs failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "audit_failed", "list audit logs failed")
		return
	}
	OK(c, gin.H{"entries": entries})
}
