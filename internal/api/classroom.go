package api

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/config"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"
	"ppclassvip/internal/ppcdn"
	"ppclassvip/internal/realtime"
	"ppclassvip/internal/settings"
	"ppclassvip/internal/storage"
	"ppclassvip/internal/store"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ClassroomHandlerDeps struct {
	Classrooms  *store.ClassroomStore
	Users       *store.UserStore
	Tokens      *auth.TokenManager
	Config      config.Config
	Logger      *slog.Logger
	Hub         *realtime.Hub
	Media       ppcdn.Provider
	Storage     *storage.Local
	Attachments *store.AttachmentStore
	Recordings  *store.RecordingStore
	Audits      *store.AuditStore
	MediaStats  *store.MediaStatStore
	Limiter     *middleware.RateLimiter
	Settings    *settings.Service
	Remote      *store.RemoteDesktopStore
	Control     *realtime.ControlHub
}

type ClassroomHandler struct {
	classrooms  *store.ClassroomStore
	users       *store.UserStore
	tokens      *auth.TokenManager
	config      config.Config
	log         *slog.Logger
	hub         *realtime.Hub
	media       ppcdn.Provider
	storage     *storage.Local
	attachments *store.AttachmentStore
	recordings  *store.RecordingStore
	audits      *store.AuditStore
	mediaStats  *store.MediaStatStore
	limiter     *middleware.RateLimiter
	settings    *settings.Service
	remote      *store.RemoteDesktopStore
	control     *realtime.ControlHub
}

func NewClassroomHandler(deps ClassroomHandlerDeps) *ClassroomHandler {
	settingsService := deps.Settings
	if settingsService == nil {
		settingsService = settings.NewStatic(deps.Config.Classroom)
	}
	return &ClassroomHandler{
		classrooms:  deps.Classrooms,
		users:       deps.Users,
		tokens:      deps.Tokens,
		config:      deps.Config,
		log:         deps.Logger,
		hub:         deps.Hub,
		media:       deps.Media,
		storage:     deps.Storage,
		attachments: deps.Attachments,
		recordings:  deps.Recordings,
		audits:      deps.Audits,
		mediaStats:  deps.MediaStats,
		limiter:     deps.Limiter,
		settings:    settingsService,
		remote:      deps.Remote,
		control:     deps.Control,
	}
}

func (h *ClassroomHandler) classroomConfig() config.ClassroomConfig {
	if h.settings != nil {
		return h.settings.Classroom()
	}
	return h.config.Classroom
}

type createClassroomRequest struct {
	Title string `json:"title"`
}

func (h *ClassroomHandler) Create(c *gin.Context) {
	userID, ok := h.requireTeacherUser(c)
	if !ok {
		return
	}
	if limit := h.settings.MaxRoomsPerTeacher(); limit > 0 {
		count, err := h.classrooms.CountOpenByTeacher(c.Request.Context(), userID)
		if err != nil {
			h.log.Error("count teacher rooms failed", "error", err, "user_id", userID)
			Fail(c, http.StatusInternalServerError, "create classroom failed")
			return
		}
		if count >= int64(limit) {
			FailCode(c, http.StatusConflict, "teacher_room_limit", "teacher open classroom limit reached")
			return
		}
	}

	var req createClassroomRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid request")
			return
		}
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "1v1 课堂"
	}
	if utf8.RuneCountInString(title) > 120 {
		Fail(c, http.StatusBadRequest, "title is too long")
		return
	}

	room := &model.Classroom{
		RoomCode:       newRoomCode(),
		Title:          title,
		TeacherUserID:  userID,
		State:          model.ClassroomStateCreated,
		RecordingState: model.RecordingStateOff,
	}
	if err := h.classrooms.CreateClassroom(c.Request.Context(), room); err != nil {
		h.log.Error("create classroom failed", "error", err, "user_id", userID)
		Fail(c, http.StatusInternalServerError, "create classroom failed")
		return
	}
	h.audit(c, room.ID, 0, "room.create", "room_code="+room.RoomCode)

	Created(c, gin.H{"room": room})
}

func (h *ClassroomHandler) List(c *gin.Context) {
	userID, ok := h.requireTeacherUser(c)
	if !ok {
		return
	}
	rooms, err := h.classrooms.ListByTeacher(c.Request.Context(), userID, 50)
	if err != nil {
		h.log.Error("list classrooms failed", "error", err, "user_id", userID)
		Fail(c, http.StatusInternalServerError, "list classrooms failed")
		return
	}
	OK(c, gin.H{"rooms": rooms})
}

func (h *ClassroomHandler) CreateInvite(c *gin.Context) {
	userID, ok := h.requireTeacherUser(c)
	if !ok {
		return
	}
	room, ok := h.loadOwnedRoom(c, userID)
	if !ok {
		return
	}
	// An invite can also be generated for an already-ended classroom: doing so
	// reopens the same room for a new session (chat history is kept) instead of
	// forcing the teacher to create a brand new room.
	if roomIsClosed(room) {
		reopened, err := h.classrooms.ReopenClassroom(c.Request.Context(), room.ID)
		if err != nil {
			h.log.Error("reopen classroom failed", "error", err, "room_id", room.ID)
			Fail(c, http.StatusInternalServerError, "create invite failed")
			return
		}
		room = reopened
		h.audit(c, room.ID, 0, "room.reopen", "via=invite")
	}

	inviteToken, err := newInviteToken()
	if err != nil {
		h.log.Error("generate invite token failed", "error", err)
		Fail(c, http.StatusInternalServerError, "create invite failed")
		return
	}
	ttl := h.classroomConfig().InviteTTL.Std()
	invite := &model.ClassroomInvite{
		RoomID:    room.ID,
		TokenHash: store.HashInviteToken(inviteToken),
		Role:      model.ParticipantRoleStudent,
		ExpiresAt: time.Now().Add(ttl),
		CreatedBy: userID,
	}
	if err := h.classrooms.CreateInvite(c.Request.Context(), invite); err != nil {
		h.log.Error("create invite failed", "error", err, "room_id", room.ID)
		Fail(c, http.StatusInternalServerError, "create invite failed")
		return
	}
	h.audit(c, room.ID, 0, "room.invite", "expires_at="+invite.ExpiresAt.UTC().Format(time.RFC3339))

	OK(c, gin.H{
		"inviteToken": inviteToken,
		"expiresAt":   invite.ExpiresAt,
		"roomCode":    room.RoomCode,
		"joinPath":    "/join?invite=" + inviteToken,
	})
}

func (h *ClassroomHandler) TeacherJoin(c *gin.Context) {
	userID, ok := h.requireTeacherUser(c)
	if !ok {
		return
	}
	room, ok := h.loadOwnedRoom(c, userID)
	if !ok {
		return
	}

	user, err := h.users.GetByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "user not found")
			return
		}
		h.log.Error("load teacher failed", "error", err, "user_id", userID)
		Fail(c, http.StatusInternalServerError, "join failed")
		return
	}
	// The teacher's display name is chosen in the UI and cached client-side, so
	// accept it from the request and fall back to the account's own nickname /
	// email only when the client did not send one.
	var req teacherJoinRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid request")
			return
		}
	}
	nickname := strings.TrimSpace(req.Nickname)
	if nickname == "" {
		nickname = strings.TrimSpace(user.Nickname)
	}
	if nickname == "" {
		nickname = strings.Split(user.Email, "@")[0]
	}
	if utf8.RuneCountInString(nickname) > 32 {
		if strings.TrimSpace(req.Nickname) != "" {
			Fail(c, http.StatusBadRequest, "nickname must be 1-32 characters")
			return
		}
		nickname = string([]rune(nickname)[:32])
	}

	updatedRoom, participant, err := h.classrooms.UpsertTeacherParticipant(c.Request.Context(), room.ID, userID, nickname)
	if err != nil {
		h.writeClassroomError(c, err)
		return
	}

	roomToken, expiresAt, ok := h.issueRoomToken(c, updatedRoom.ID, participant.ID, model.ParticipantRoleTeacher)
	if !ok {
		return
	}
	media, err := h.mediaBundle(c.Request.Context(), updatedRoom, participant)
	if err != nil {
		h.writeMediaError(c, err)
		return
	}
	h.audit(c, updatedRoom.ID, participant.ID, "room.join", "role=teacher")
	OK(c, gin.H{
		"room":        updatedRoom,
		"participant": participant,
		"roomToken":   roomToken,
		"expiresAt":   expiresAt,
		"ws":          roomWSInfo(updatedRoom.ID),
		"media":       media,
	})
}

type teacherJoinRequest struct {
	Nickname string `json:"nickname"`
}

type guestJoinRequest struct {
	InviteToken string `json:"inviteToken" binding:"required"`
	Nickname    string `json:"nickname" binding:"required"`
}

func (h *ClassroomHandler) GuestJoin(c *gin.Context) {
	var req guestJoinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid request")
		return
	}
	req.InviteToken = strings.TrimSpace(req.InviteToken)
	req.Nickname = strings.TrimSpace(req.Nickname)
	if len(req.InviteToken) < 16 || len(req.InviteToken) > 256 {
		Fail(c, http.StatusBadRequest, "invalid invite token")
		return
	}
	if req.Nickname == "" || utf8.RuneCountInString(req.Nickname) > 32 {
		Fail(c, http.StatusBadRequest, "nickname must be 1-32 characters")
		return
	}

	room, participant, err := h.classrooms.JoinByInvite(
		c.Request.Context(),
		store.HashInviteToken(req.InviteToken),
		req.Nickname,
	)
	if err != nil {
		h.writeClassroomError(c, err)
		return
	}

	roomToken, expiresAt, ok := h.issueRoomToken(c, room.ID, participant.ID, model.ParticipantRoleStudent)
	if !ok {
		return
	}
	media, err := h.mediaBundle(c.Request.Context(), room, participant)
	if err != nil {
		h.writeMediaError(c, err)
		return
	}
	h.audit(c, room.ID, participant.ID, "room.join", "role=student")
	OK(c, gin.H{
		"room":        room,
		"participant": participant,
		"roomToken":   roomToken,
		"expiresAt":   expiresAt,
		"ws":          roomWSInfo(room.ID),
		"media":       media,
	})
}

func (h *ClassroomHandler) Get(c *gin.Context) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return
	}
	room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "room not found")
			return
		}
		h.log.Error("get classroom failed", "error", err, "room_id", roomID)
		Fail(c, http.StatusInternalServerError, "get classroom failed")
		return
	}
	if !authorizeRoomAccess(c, room) {
		Fail(c, http.StatusForbidden, "room forbidden")
		return
	}

	participants, err := h.classrooms.ListParticipants(c.Request.Context(), room.ID)
	if err != nil {
		h.log.Error("list participants failed", "error", err, "room_id", room.ID)
		Fail(c, http.StatusInternalServerError, "get classroom failed")
		return
	}
	OK(c, gin.H{
		"room":         room,
		"participants": participants,
		"myRole":       middleware.Role(c),
	})
}

type messageInput struct {
	ClientMsgID  string  `json:"clientMsgId" binding:"required"`
	Body         string  `json:"body"`
	Type         string  `json:"type"`
	AttachmentID *uint64 `json:"attachmentId"`
}

type messageInputError struct {
	message string
}

func (e *messageInputError) Error() string {
	return e.message
}

func (h *ClassroomHandler) SendMessage(c *gin.Context) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return
	}
	participantID := middleware.ParticipantID(c)
	if participantID == 0 {
		Fail(c, http.StatusForbidden, "join room first")
		return
	}

	room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "room not found")
			return
		}
		h.log.Error("get classroom failed", "error", err, "room_id", roomID)
		Fail(c, http.StatusInternalServerError, "send message failed")
		return
	}
	if !authorizeRoomAccess(c, room) {
		Fail(c, http.StatusForbidden, "room forbidden")
		return
	}
	if roomIsClosed(room) {
		Fail(c, http.StatusConflict, "room is closed")
		return
	}

	var input messageInput
	if err := c.ShouldBindJSON(&input); err != nil {
		Fail(c, http.StatusBadRequest, "invalid request")
		return
	}

	message, created, err := h.createMessage(c.Request.Context(), room, participantID, middleware.Role(c), input)
	if err != nil {
		var inputErr *messageInputError
		if errors.As(err, &inputErr) {
			Fail(c, http.StatusBadRequest, inputErr.Error())
			return
		}
		h.log.Error("create message failed", "error", err, "room_id", room.ID)
		Fail(c, http.StatusInternalServerError, "send message failed")
		return
	}
	if created && h.hub != nil {
		h.hub.Broadcast(room.ID, gin.H{"type": "chat.message", "data": message})
	}
	if created {
		Created(c, gin.H{"message": message})
		return
	}
	OK(c, gin.H{"message": message})
}

func (h *ClassroomHandler) createMessage(ctx context.Context, room *model.Classroom, participantID uint64, role string, input messageInput) (*model.ClassroomMessage, bool, error) {
	input.ClientMsgID = strings.TrimSpace(input.ClientMsgID)
	input.Body = strings.TrimSpace(input.Body)
	msgType := strings.TrimSpace(input.Type)
	if msgType == "" {
		msgType = "text"
	}
	if len(input.ClientMsgID) == 0 || len(input.ClientMsgID) > 64 {
		return nil, false, &messageInputError{message: "clientMsgId must be 1-64 characters"}
	}
	if !room.ChatEnabled && role == model.ParticipantRoleStudent {
		return nil, false, &messageInputError{message: "chat is disabled"}
	}
	var attachment *model.ClassroomAttachment
	switch msgType {
	case model.MessageTypeText:
		if input.Body == "" || utf8.RuneCountInString(input.Body) > 2000 {
			return nil, false, &messageInputError{message: "body must be 1-2000 characters"}
		}
		if input.AttachmentID != nil {
			return nil, false, &messageInputError{message: "text messages cannot carry attachments"}
		}
	case model.MessageTypeImage, model.MessageTypeFile:
		if input.AttachmentID == nil || *input.AttachmentID == 0 {
			return nil, false, &messageInputError{message: "attachmentId is required"}
		}
		if h.attachments == nil {
			return nil, false, errors.New("attachment store is unavailable")
		}
		fetched, err := h.attachments.Get(ctx, room.ID, *input.AttachmentID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, &messageInputError{message: "attachment not found"}
			}
			return nil, false, err
		}
		attachment = fetched
		if attachment.Status != model.AttachmentStatusStored {
			return nil, false, &messageInputError{message: "attachment is unavailable"}
		}
		expectedKind := model.AttachmentKindFile
		if msgType == model.MessageTypeImage {
			expectedKind = model.AttachmentKindImage
		}
		if attachment.Kind != expectedKind {
			return nil, false, &messageInputError{message: "attachment kind does not match message type"}
		}
		if input.Body == "" {
			input.Body = attachment.OriginalName
		}
		if utf8.RuneCountInString(input.Body) > 2000 {
			return nil, false, &messageInputError{message: "body must be at most 2000 characters"}
		}
	default:
		return nil, false, &messageInputError{message: "unsupported message type"}
	}

	existing, err := h.classrooms.GetMessageByClientID(ctx, room.ID, participantID, input.ClientMsgID)
	if err == nil {
		existing.Attachment = attachment
		return existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	message := &model.ClassroomMessage{
		RoomID:              room.ID,
		SenderParticipantID: participantID,
		SenderRole:          role,
		Type:                msgType,
		Body:                input.Body,
		AttachmentID:        input.AttachmentID,
		ClientMsgID:         input.ClientMsgID,
		Attachment:          attachment,
	}
	if err := h.classrooms.CreateMessage(ctx, message); err != nil {
		// A concurrent retry may have won the unique (room, sender, clientMsgId) race.
		if existing, findErr := h.classrooms.GetMessageByClientID(ctx, room.ID, participantID, input.ClientMsgID); findErr == nil {
			return existing, false, nil
		}
		return nil, false, err
	}
	return message, true, nil
}

func (h *ClassroomHandler) ListMessages(c *gin.Context) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return
	}

	room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "room not found")
			return
		}
		h.log.Error("get classroom failed", "error", err, "room_id", roomID)
		Fail(c, http.StatusInternalServerError, "list messages failed")
		return
	}
	if !authorizeRoomAccess(c, room) {
		Fail(c, http.StatusForbidden, "room forbidden")
		return
	}

	afterID, err := parseOptionalUint(c.Query("afterId"))
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid afterId")
		return
	}
	beforeID, err := parseOptionalUint(c.Query("beforeId"))
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid beforeId")
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			Fail(c, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	messages, err := h.classrooms.ListMessages(c.Request.Context(), room.ID, afterID, beforeID, limit)
	if err != nil {
		h.log.Error("list messages failed", "error", err, "room_id", room.ID)
		Fail(c, http.StatusInternalServerError, "list messages failed")
		return
	}
	h.decorateMessageAttachments(c.Request.Context(), room.ID, messages)
	OK(c, gin.H{"messages": messages})
}

// decorateMessageAttachments attaches the stored attachment metadata to chat
// messages so clients can render captions and download names without issuing an
// extra request per message.
func (h *ClassroomHandler) decorateMessageAttachments(ctx context.Context, roomID uint64, messages []model.ClassroomMessage) {
	if h.attachments == nil {
		return
	}
	ids := make([]uint64, 0, len(messages))
	for i := range messages {
		if messages[i].AttachmentID != nil && *messages[i].AttachmentID != 0 {
			ids = append(ids, *messages[i].AttachmentID)
		}
	}
	if len(ids) == 0 {
		return
	}
	attachments, err := h.attachments.ListByIDs(ctx, roomID, ids)
	if err != nil {
		return
	}
	byID := make(map[uint64]*model.ClassroomAttachment, len(attachments))
	for i := range attachments {
		byID[attachments[i].ID] = &attachments[i]
	}
	for i := range messages {
		if messages[i].AttachmentID == nil {
			continue
		}
		if attachment, ok := byID[*messages[i].AttachmentID]; ok {
			messages[i].Attachment = attachment
		}
	}
}

func parseOptionalUint(raw string) (uint64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return strconv.ParseUint(raw, 10, 64)
}

func (h *ClassroomHandler) End(c *gin.Context) {
	room, ok := h.requireTeacherRoom(c)
	if !ok {
		return
	}

	updated, err := h.classrooms.EndClassroomWithReason(c.Request.Context(), room.ID, model.EndReasonTeacherEnd)
	if err != nil {
		h.writeClassroomError(c, err)
		return
	}
	h.finishActiveRecordingOnEnd(c.Request.Context(), room.ID)
	h.stopRemoteDesktopForRoom(c.Request.Context(), room.ID, "room_ended")
	h.audit(c, room.ID, 0, "room.end", "reason=teacher_end")
	if h.hub != nil {
		h.hub.Broadcast(room.ID, gin.H{
			"type": "room.ended",
			"data": gin.H{"reason": model.EndReasonTeacherEnd},
		})
		h.hub.CloseRoomWithReason(room.ID, "room_ended")
	}
	OK(c, gin.H{"room": updated})
}

// Delete permanently removes an already-stopped classroom (ended/failed/
// expired) together with its chat, attachments, recordings and audit data.
// Only the owning teacher (or an admin) may do this, and never while the room
// is still open.
func (h *ClassroomHandler) Delete(c *gin.Context) {
	userID, ok := h.requireTeacherUser(c)
	if !ok {
		return
	}
	room, ok := h.loadOwnedRoom(c, userID)
	if !ok {
		return
	}
	if !roomIsClosed(room) {
		Fail(c, http.StatusConflict, "room is still running")
		return
	}

	ctx := c.Request.Context()

	// Collect file keys before the rows disappear so the files can be removed
	// after the database transaction commits.
	var attachmentKeys []string
	if h.attachments != nil {
		if list, err := h.attachments.ListByRoom(ctx, room.ID); err != nil {
			h.log.Warn("list attachments for delete failed", "error", err, "room_id", room.ID)
		} else {
			for _, attachment := range list {
				if attachment.FileKey != "" {
					attachmentKeys = append(attachmentKeys, attachment.FileKey)
				}
			}
		}
	}

	h.stopRemoteDesktopForRoom(ctx, room.ID, "room_deleted")

	if err := h.classrooms.DeleteClassroom(ctx, room.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "room not found")
			return
		}
		h.log.Error("delete classroom failed", "error", err, "room_id", room.ID)
		Fail(c, http.StatusInternalServerError, "delete classroom failed")
		return
	}

	if h.storage != nil {
		for _, key := range attachmentKeys {
			if err := h.storage.DeleteAttachment(key); err != nil {
				h.log.Warn("delete attachment file failed", "error", err, "room_id", room.ID)
			}
		}
	}

	if h.hub != nil {
		h.hub.Broadcast(room.ID, gin.H{
			"type": "room.deleted",
			"data": gin.H{"roomId": room.ID},
		})
		h.hub.CloseRoomWithReason(room.ID, "room_deleted")
	}

	OK(c, gin.H{"deleted": true})
}

func (h *ClassroomHandler) requireTeacherUser(c *gin.Context) (uint64, bool) {
	userID := middleware.UserID(c)
	role := middleware.Role(c)
	if userID == 0 || (role != model.RoleTeacher && role != model.RoleAdmin) {
		Fail(c, http.StatusForbidden, "teacher access required")
		return 0, false
	}
	return userID, true
}

func (h *ClassroomHandler) loadOwnedRoom(c *gin.Context, userID uint64) (*model.Classroom, bool) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return nil, false
	}
	room, err := h.classrooms.GetClassroom(c.Request.Context(), roomID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "room not found")
			return nil, false
		}
		h.log.Error("get classroom failed", "error", err, "room_id", roomID)
		Fail(c, http.StatusInternalServerError, "get classroom failed")
		return nil, false
	}
	if room.TeacherUserID != userID && middleware.Role(c) != model.RoleAdmin {
		Fail(c, http.StatusForbidden, "room forbidden")
		return nil, false
	}
	return room, true
}

func (h *ClassroomHandler) issueRoomToken(c *gin.Context, roomID, participantID uint64, role string) (string, time.Time, bool) {
	token, err := h.tokens.IssueRoom(roomID, participantID, role, h.classroomConfig().RoomTokenTTL.Std())
	if err != nil {
		h.log.Error("issue room token failed", "error", err, "room_id", roomID)
		Fail(c, http.StatusInternalServerError, "issue room token failed")
		return "", time.Time{}, false
	}
	return token, time.Now().Add(h.classroomConfig().RoomTokenTTL.Std()), true
}

func (h *ClassroomHandler) writeClassroomError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrInviteInvalid):
		Fail(c, http.StatusNotFound, "invite invalid")
	case errors.Is(err, store.ErrInviteUsed):
		Fail(c, http.StatusConflict, "invite already used")
	case errors.Is(err, store.ErrInviteExpired):
		Fail(c, http.StatusGone, "invite expired")
	case errors.Is(err, store.ErrRoomNotReady):
		Fail(c, http.StatusConflict, "teacher has not joined yet")
	case errors.Is(err, store.ErrRoomClosed):
		Fail(c, http.StatusConflict, "room is closed")
	case errors.Is(err, store.ErrRoomFull):
		FailCode(c, http.StatusConflict, "room_full", "classroom already has a student")
	case errors.Is(err, gorm.ErrRecordNotFound):
		Fail(c, http.StatusNotFound, "not found")
	default:
		h.log.Error("classroom operation failed", "error", err, "request_id", middleware.GetRequestID(c))
		Fail(c, http.StatusInternalServerError, "classroom operation failed")
	}
}

func parseRoomID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		Fail(c, http.StatusBadRequest, "invalid room id")
		return 0, false
	}
	return id, true
}

func authorizeRoomAccess(c *gin.Context, room *model.Classroom) bool {
	if middleware.RoomID(c) == room.ID && middleware.ParticipantID(c) != 0 {
		return true
	}
	userID := middleware.UserID(c)
	role := middleware.Role(c)
	return userID != 0 && (room.TeacherUserID == userID || role == model.RoleAdmin)
}

func roomIsClosed(room *model.Classroom) bool {
	switch room.State {
	case model.ClassroomStateEnded, model.ClassroomStateFailed, model.ClassroomStateExpired:
		return true
	default:
		return false
	}
}

func newRoomCode() string {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return strings.ToUpper(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

func newInviteToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
