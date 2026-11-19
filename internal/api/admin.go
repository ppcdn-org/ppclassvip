package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"
	"ppclassvip/internal/realtime"
	"ppclassvip/internal/settings"
	"ppclassvip/internal/storage"
	"ppclassvip/internal/store"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AdminHandlerDeps struct {
	Users       *store.UserStore
	Classrooms  *store.ClassroomStore
	Recordings  *store.RecordingStore
	Attachments *store.AttachmentStore
	Audits      *store.AuditStore
	Admin       *store.AdminStore
	Storage     *storage.Local
	Settings    *settings.Service
	Hub         *realtime.Hub
	Logger      *slog.Logger
}

type AdminHandler struct {
	users       *store.UserStore
	classrooms  *store.ClassroomStore
	recordings  *store.RecordingStore
	attachments *store.AttachmentStore
	audits      *store.AuditStore
	admin       *store.AdminStore
	storage     *storage.Local
	settings    *settings.Service
	hub         *realtime.Hub
	log         *slog.Logger
}

func NewAdminHandler(deps AdminHandlerDeps) *AdminHandler {
	return &AdminHandler{
		users:       deps.Users,
		classrooms:  deps.Classrooms,
		recordings:  deps.Recordings,
		attachments: deps.Attachments,
		audits:      deps.Audits,
		admin:       deps.Admin,
		storage:     deps.Storage,
		settings:    deps.Settings,
		hub:         deps.Hub,
		log:         deps.Logger,
	}
}

func (h *AdminHandler) Dashboard(c *gin.Context) {
	stats, err := h.admin.Stats(c.Request.Context())
	if err != nil {
		h.log.Error("admin dashboard failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "dashboard_failed", "load dashboard failed")
		return
	}
	rooms, _, err := h.admin.ListRooms(c.Request.Context(), "", "", 0, 8, 0)
	if err != nil {
		h.log.Warn("admin recent rooms failed", "error", err)
	}
	OK(c, gin.H{
		"stats":       stats,
		"recentRooms": h.roomsWithTeachers(c.Request.Context(), rooms),
	})
}

func (h *AdminHandler) ListUsers(c *gin.Context) {
	limit, offset := parsePagination(c)
	users, total, err := h.admin.ListUsers(c.Request.Context(), c.Query("query"), c.Query("role"), c.Query("status"), limit, offset)
	if err != nil {
		h.log.Error("admin list users failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "users_failed", "list users failed")
		return
	}
	OK(c, gin.H{"users": users, "total": total, "limit": limit, "offset": offset})
}

type adminCreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"`
}

func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req adminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	req.Email = store.NormalizeEmail(req.Email)
	req.Nickname = strings.TrimSpace(req.Nickname)
	req.Role = strings.TrimSpace(req.Role)
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		FailCode(c, http.StatusBadRequest, "invalid_request", "valid email is required")
		return
	}
	if req.Role == "" {
		req.Role = model.RoleTeacher
	}
	if req.Role != model.RoleTeacher && req.Role != model.RoleAdmin {
		FailCode(c, http.StatusBadRequest, "invalid_request", "role must be teacher or admin")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		FailCode(c, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if req.Nickname == "" {
		req.Nickname = strings.Split(req.Email, "@")[0]
	}
	if utf8len := len([]rune(req.Nickname)); utf8len > 64 {
		FailCode(c, http.StatusBadRequest, "invalid_request", "nickname is too long")
		return
	}
	if _, err := h.users.GetByEmail(c.Request.Context(), req.Email); err == nil {
		FailCode(c, http.StatusConflict, "email_exists", "email already exists")
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		h.log.Error("admin get user by email failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "user_failed", "create user failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		FailCode(c, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	user := &model.User{
		Email:        req.Email,
		PasswordHash: hash,
		Nickname:     req.Nickname,
		Role:         req.Role,
		Status:       model.StatusActive,
	}
	if err := h.users.Create(c.Request.Context(), user); err != nil {
		h.log.Error("admin create user failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "user_failed", "create user failed")
		return
	}
	h.adminAudit(c, "admin.user.create", "user="+uintString(user.ID)+" role="+user.Role)
	Created(c, gin.H{"user": user})
}

type adminUpdateUserRequest struct {
	Nickname *string `json:"nickname"`
	Role     *string `json:"role"`
	Status   *string `json:"status"`
	Password *string `json:"password"`
}

func (h *AdminHandler) UpdateUser(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	user, err := h.users.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		FailCode(c, http.StatusInternalServerError, "user_failed", "load user failed")
		return
	}
	var req adminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	fields := map[string]any{}
	if req.Nickname != nil {
		nickname := strings.TrimSpace(*req.Nickname)
		if nickname == "" || len([]rune(nickname)) > 64 {
			FailCode(c, http.StatusBadRequest, "invalid_request", "nickname must be 1-64 characters")
			return
		}
		fields["nickname"] = nickname
	}
	if req.Role != nil {
		role := strings.TrimSpace(*req.Role)
		if role != model.RoleTeacher && role != model.RoleAdmin {
			FailCode(c, http.StatusBadRequest, "invalid_request", "role must be teacher or admin")
			return
		}
		if id == middleware.UserID(c) && role != user.Role {
			FailCode(c, http.StatusBadRequest, "invalid_request", "cannot change your own role")
			return
		}
		if role != model.RoleAdmin && user.Role == model.RoleAdmin {
			if count, err := h.users.CountAdmins(c.Request.Context()); err == nil && count <= 1 {
				FailCode(c, http.StatusBadRequest, "last_admin", "cannot demote the last active admin")
				return
			}
		}
		fields["role"] = role
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if status != model.StatusActive && status != model.StatusDisabled {
			FailCode(c, http.StatusBadRequest, "invalid_request", "status must be active or disabled")
			return
		}
		if id == middleware.UserID(c) && status != user.Status {
			FailCode(c, http.StatusBadRequest, "invalid_request", "cannot change your own status")
			return
		}
		if status == model.StatusDisabled && user.Role == model.RoleAdmin {
			if count, err := h.users.CountAdmins(c.Request.Context()); err == nil && count <= 1 {
				FailCode(c, http.StatusBadRequest, "last_admin", "cannot disable the last active admin")
				return
			}
		}
		fields["status"] = status
	}
	if req.Password != nil && *req.Password != "" {
		if err := auth.ValidatePassword(*req.Password); err != nil {
			FailCode(c, http.StatusBadRequest, "weak_password", err.Error())
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			FailCode(c, http.StatusBadRequest, "weak_password", err.Error())
			return
		}
		fields["password_hash"] = hash
	}
	if len(fields) == 0 {
		FailCode(c, http.StatusBadRequest, "invalid_request", "no changes supplied")
		return
	}
	if err := h.users.UpdateFields(c.Request.Context(), id, fields); err != nil {
		h.log.Error("admin update user failed", "error", err, "user_id", id)
		FailCode(c, http.StatusInternalServerError, "user_failed", "update user failed")
		return
	}
	updated, _ := h.users.GetByID(c.Request.Context(), id)
	h.adminAudit(c, "admin.user.update", "user="+uintString(id))
	OK(c, gin.H{"user": updated})
}

func (h *AdminHandler) ListClassrooms(c *gin.Context) {
	limit, offset := parsePagination(c)
	teacherID, _ := strconv.ParseUint(strings.TrimSpace(c.Query("teacherId")), 10, 64)
	rooms, total, err := h.admin.ListRooms(c.Request.Context(), c.Query("query"), c.Query("state"), teacherID, limit, offset)
	if err != nil {
		h.log.Error("admin list rooms failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "rooms_failed", "list classrooms failed")
		return
	}
	OK(c, gin.H{
		"rooms":    h.roomsWithTeachers(c.Request.Context(), rooms),
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"teachers": h.teacherOptions(c.Request.Context()),
	})
}

func (h *AdminHandler) GetClassroom(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	room, err := h.classrooms.GetClassroom(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusNotFound, "room_not_found", "classroom not found")
			return
		}
		FailCode(c, http.StatusInternalServerError, "room_failed", "load classroom failed")
		return
	}
	participants, err := h.classrooms.ListParticipants(c.Request.Context(), id)
	if err != nil {
		h.log.Warn("admin list participants failed", "error", err, "room_id", id)
	}
	attachments, _ := h.attachments.ListByRoom(c.Request.Context(), id)
	audit, _ := h.audits.ListByRoom(c.Request.Context(), id, 100)
	OK(c, gin.H{
		"room":         room,
		"teacher":      h.userSummary(c.Request.Context(), room.TeacherUserID),
		"participants": participants,
		"attachments":  attachments,
		"audit":        audit,
	})
}

func (h *AdminHandler) EndClassroom(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	room, err := h.classrooms.GetClassroom(c.Request.Context(), id)
	if err != nil {
		FailCode(c, http.StatusNotFound, "room_not_found", "classroom not found")
		return
	}
	updated, err := h.classrooms.EndClassroomWithReason(c.Request.Context(), id, "admin_end")
	if err != nil {
		h.log.Error("admin end classroom failed", "error", err, "room_id", id)
		FailCode(c, http.StatusInternalServerError, "room_failed", "end classroom failed")
		return
	}
	if active, err := h.recordings.GetActiveByRoom(c.Request.Context(), id); err == nil {
		from := active.Status
		to := model.RecordingStateInterrupted
		if from == model.RecordingStatePendingConsent {
			to = model.RecordingStateDeclined
		}
		_, _ = h.recordings.UpdateStatus(c.Request.Context(), active.ID, []string{from}, to, map[string]any{
			"error":    "room_ended_by_admin",
			"ended_at": time.Now(),
		})
		_ = h.classrooms.SetRecordingState(c.Request.Context(), id, model.RecordingStateOff)
	}
	if h.hub != nil {
		h.hub.Broadcast(id, gin.H{"type": "room.ended", "data": gin.H{"reason": "admin_end"}})
		h.hub.CloseRoomWithReason(id, "room_ended")
	}
	h.adminAudit(c, "admin.room.end", "room="+uintString(id)+" title="+room.Title)
	OK(c, gin.H{"room": updated})
}

func (h *AdminHandler) ListAttachments(c *gin.Context) {
	limit, offset := parsePagination(c)
	roomID, _ := strconv.ParseUint(strings.TrimSpace(c.Query("roomId")), 10, 64)
	attachments, total, err := h.admin.ListAttachments(c.Request.Context(), roomID, c.Query("status"), limit, offset)
	if err != nil {
		h.log.Error("admin list attachments failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "attachments_failed", "list attachments failed")
		return
	}
	OK(c, gin.H{"attachments": attachments, "total": total, "limit": limit, "offset": offset})
}

func (h *AdminHandler) DeleteAttachment(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	attachment, err := h.attachments.GetByID(c.Request.Context(), id)
	if err != nil {
		FailCode(c, http.StatusNotFound, "attachment_not_found", "attachment not found")
		return
	}
	if h.storage != nil {
		_ = h.storage.DeleteAttachment(attachment.FileKey)
	}
	_ = h.attachments.MarkPurged(c.Request.Context(), attachment.ID, time.Now())
	h.adminAudit(c, "admin.attachment.delete", "attachment="+uintString(attachment.ID)+" room="+uintString(attachment.RoomID))
	OK(c, gin.H{"deleted": true})
}

func (h *AdminHandler) ListAudit(c *gin.Context) {
	limit, offset := parsePagination(c)
	roomID, _ := strconv.ParseUint(strings.TrimSpace(c.Query("roomId")), 10, 64)
	entries, total, err := h.admin.ListAudit(c.Request.Context(), roomID, c.Query("action"), limit, offset)
	if err != nil {
		h.log.Error("admin list audit failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "audit_failed", "list audit failed")
		return
	}
	OK(c, gin.H{"entries": entries, "total": total, "limit": limit, "offset": offset})
}

func (h *AdminHandler) GetSettings(c *gin.Context) {
	OK(c, gin.H{"settings": h.settings.Values()})
}

func (h *AdminHandler) UpdateSettings(c *gin.Context) {
	if h.settings == nil {
		FailCode(c, http.StatusServiceUnavailable, "settings_unavailable", "settings service is unavailable")
		return
	}
	var input model.PlatformSetting
	if err := c.ShouldBindJSON(&input); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	updated, err := h.settings.Update(c.Request.Context(), input, middleware.UserID(c))
	if err != nil {
		if errors.Is(err, settings.ErrInvalidSetting) {
			FailCode(c, http.StatusBadRequest, "invalid_setting", err.Error())
			return
		}
		h.log.Error("admin update settings failed", "error", err)
		FailCode(c, http.StatusInternalServerError, "settings_failed", "update settings failed")
		return
	}
	h.adminAudit(c, "admin.settings.update", "recording_enabled="+boolString(input.RecordingEnabled))
	OK(c, gin.H{"settings": updated})
}

func (h *AdminHandler) roomsWithTeachers(ctx context.Context, rooms []model.Classroom) []gin.H {
	ids := make([]uint64, 0, len(rooms))
	seen := map[uint64]bool{}
	for _, room := range rooms {
		if !seen[room.TeacherUserID] {
			seen[room.TeacherUserID] = true
			ids = append(ids, room.TeacherUserID)
		}
	}
	users, _ := h.users.ListByIDs(ctx, ids)
	teachers := map[uint64]gin.H{}
	for _, user := range users {
		teachers[user.ID] = gin.H{"id": user.ID, "email": user.Email, "nickname": user.Nickname}
	}
	result := make([]gin.H, 0, len(rooms))
	for _, room := range rooms {
		result = append(result, gin.H{
			"room":    room,
			"teacher": teachers[room.TeacherUserID],
		})
	}
	return result
}

func (h *AdminHandler) teacherOptions(ctx context.Context) []gin.H {
	users, _, err := h.admin.ListUsers(ctx, "", model.RoleTeacher, "", 200, 0)
	if err != nil {
		return nil
	}
	options := make([]gin.H, 0, len(users))
	for _, user := range users {
		options = append(options, gin.H{"id": user.ID, "email": user.Email, "nickname": user.Nickname})
	}
	return options
}

func (h *AdminHandler) userSummary(ctx context.Context, id uint64) gin.H {
	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		return nil
	}
	return gin.H{"id": user.ID, "email": user.Email, "nickname": user.Nickname, "role": user.Role}
}

func (h *AdminHandler) adminAudit(c *gin.Context, action, detail string) {
	if h.audits == nil {
		return
	}
	entry := &model.ClassroomAuditLog{
		RoomID:   0,
		UserID:   middleware.UserID(c),
		Action:   action,
		Detail:   detail,
		ClientIP: c.ClientIP(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.audits.Create(ctx, entry); err != nil {
		h.log.Warn("admin audit write failed", "error", err, "action", action)
	}
}

func parsePagination(c *gin.Context) (int, int) {
	limit := 50
	offset := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return limit, offset
}

func parseUintParam(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid id")
		return 0, false
	}
	return id, true
}
