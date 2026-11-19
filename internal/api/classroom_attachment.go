package api

import (
	"bufio"
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"
	"ppclassvip/internal/storage"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	imageExtensions = map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
	}
	fileExtensions = map[string]string{
		".pdf":  "application/pdf",
		".doc":  "application/msword",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls":  "application/vnd.ms-excel",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".ppt":  "application/vnd.ms-powerpoint",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".zip":  "application/zip",
		".txt":  "text/plain; charset=utf-8",
		".csv":  "text/csv; charset=utf-8",
		".md":   "text/markdown; charset=utf-8",
		".rtf":  "application/rtf",
		".odt":  "application/vnd.oasis.opendocument.text",
		".ods":  "application/vnd.oasis.opendocument.spreadsheet",
		".odp":  "application/vnd.oasis.opendocument.presentation",
	}
	forbiddenExtensions = map[string]bool{
		".exe": true, ".bat": true, ".cmd": true, ".com": true, ".msi": true,
		".js": true, ".mjs": true, ".cjs": true, ".html": true, ".htm": true,
		".svg": true, ".php": true, ".asp": true, ".aspx": true, ".jsp": true,
		".sh": true, ".ps1": true, ".vbs": true, ".scr": true, ".jar": true,
	}
)

type uploadKind struct {
	Kind        string
	ContentType string
	Limit       int64
}

func (h *ClassroomHandler) UploadAttachment(c *gin.Context) {
	room, participant, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if roomIsClosed(room) {
		FailCode(c, http.StatusConflict, "room_ended", "room is closed")
		return
	}
	if h.storage == nil || h.attachments == nil {
		FailCode(c, http.StatusServiceUnavailable, "storage_unavailable", "attachment storage is not configured")
		return
	}

	extendReadDeadline(c, 5*time.Minute)
	extendWriteDeadline(c, time.Minute)
	maxBody := h.classroomConfig().MaxFileBytes + (1 << 20)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			FailCode(c, http.StatusRequestEntityTooLarge, "attachment_too_large", "file exceeds the allowed size")
			return
		}
		if errors.Is(err, http.ErrMissingFile) {
			FailCode(c, http.StatusBadRequest, "invalid_request", "file is required")
			return
		}
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid multipart upload")
		return
	}
	defer file.Close()

	originalName := sanitizeFilename(header.Filename)
	if originalName == "" {
		originalName = "file"
	}
	kindInfo, err := inspectUploadFileReader(file, originalName, h.classroomConfig().MaxImageBytes, h.classroomConfig().MaxFileBytes)
	if err != nil {
		var inputErr *uploadInputError
		if errors.As(err, &inputErr) {
			FailCode(c, inputErr.status, inputErr.code, inputErr.message)
			return
		}
		h.log.Error("inspect upload failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid upload")
		return
	}
	if header.Size > kindInfo.Limit {
		FailCode(c, http.StatusRequestEntityTooLarge, "attachment_too_large", "file exceeds the allowed size")
		return
	}

	used, err := h.attachments.SumActiveSize(c.Request.Context(), room.ID)
	if err != nil {
		h.log.Error("sum room attachments failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	if used+header.Size > h.classroomConfig().RoomQuotaBytes {
		FailCode(c, http.StatusRequestEntityTooLarge, "attachment_quota_exceeded", "classroom attachment quota exceeded")
		return
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid upload")
		return
	}
	stored, err := h.storage.SaveAttachment(room.ID, file, kindInfo.Limit)
	if err != nil {
		if errors.Is(err, storage.ErrFileTooLarge) {
			FailCode(c, http.StatusRequestEntityTooLarge, "attachment_too_large", "file exceeds the allowed size")
			return
		}
		h.log.Error("save attachment failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	attachment := &model.ClassroomAttachment{
		RoomID:                room.ID,
		UploaderParticipantID: participant.ID,
		Kind:                  kindInfo.Kind,
		FileKey:               stored.Key,
		OriginalName:          originalName,
		ContentType:           kindInfo.ContentType,
		Size:                  stored.Size,
		SHA256:                stored.SHA256,
		Status:                model.AttachmentStatusStored,
	}
	if err := h.attachments.Create(c.Request.Context(), attachment); err != nil {
		_ = h.storage.DeleteAttachment(stored.Key)
		h.log.Error("create attachment row failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	h.audit(c, room.ID, participant.ID, "attachment.upload", "attachment="+uintString(attachment.ID)+" kind="+attachment.Kind)
	Created(c, gin.H{"attachment": attachment})
}

type uploadInputError struct {
	status  int
	code    string
	message string
}

func (e *uploadInputError) Error() string {
	return e.message
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if utf8.RuneCountInString(name) > 180 {
		runes := []rune(name)
		name = string(runes[:180])
	}
	return name
}

func (h *ClassroomHandler) GetAttachment(c *gin.Context) {
	room, _, ok := h.requireRoomParticipant(c)
	if !ok {
		return
	}
	if h.attachments == nil {
		FailCode(c, http.StatusServiceUnavailable, "storage_unavailable", "attachment storage is not configured")
		return
	}
	attachmentID, err := strconv.ParseUint(c.Param("aid"), 10, 64)
	if err != nil || attachmentID == 0 {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid attachment id")
		return
	}
	attachment, err := h.attachments.Get(c.Request.Context(), room.ID, attachmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusNotFound, "attachment_not_found", "attachment not found")
			return
		}
		h.log.Error("get attachment failed", "error", err, "room_id", room.ID)
		FailCode(c, http.StatusInternalServerError, "attachment_failed", "attachment lookup failed")
		return
	}
	if attachment.Status != model.AttachmentStatusStored {
		FailCode(c, http.StatusGone, "attachment_unavailable", "attachment is no longer available")
		return
	}
	OK(c, gin.H{"attachment": attachment})
}

func (h *ClassroomHandler) DownloadAttachment(c *gin.Context) {
	roomID, ok := parseRoomID(c)
	if !ok {
		return
	}
	attachmentID, err := strconv.ParseUint(c.Param("aid"), 10, 64)
	if err != nil || attachmentID == 0 {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid attachment id")
		return
	}
	if h.storage == nil || h.attachments == nil {
		FailCode(c, http.StatusServiceUnavailable, "storage_unavailable", "attachment storage is not configured")
		return
	}
	if !h.authorizeDownload(c, roomID) {
		FailCode(c, http.StatusForbidden, "room_forbidden", "room access required")
		return
	}
	extendWriteDeadline(c, 10*time.Minute)
	attachment, err := h.attachments.Get(c.Request.Context(), roomID, attachmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			FailCode(c, http.StatusNotFound, "attachment_not_found", "attachment not found")
			return
		}
		h.log.Error("get attachment failed", "error", err, "room_id", roomID)
		FailCode(c, http.StatusInternalServerError, "download_failed", "download failed")
		return
	}
	if attachment.Status != model.AttachmentStatusStored {
		FailCode(c, http.StatusGone, "attachment_unavailable", "attachment is no longer available")
		return
	}
	file, err := h.storage.OpenAttachment(attachment.FileKey)
	if err != nil {
		h.log.Error("open attachment failed", "error", err, "room_id", roomID, "attachment_id", attachmentID)
		FailCode(c, http.StatusNotFound, "attachment_not_found", "attachment not found")
		return
	}
	defer file.Close()

	disposition := "attachment"
	if attachment.Kind == model.AttachmentKindImage {
		disposition = "inline"
	}
	c.Header("Content-Type", attachment.ContentType)
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": attachment.OriginalName}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, max-age=300")
	h.audit(c, roomID, middleware.ParticipantID(c), "attachment.download", "attachment="+uintString(attachmentID))
	http.ServeContent(c.Writer, c.Request, attachment.OriginalName, attachment.CreatedAt, file)
}

func (h *ClassroomHandler) authorizeDownload(c *gin.Context, roomID uint64) bool {
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
		return claims.RoomID == roomID && claims.ParticipantID != 0
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

func inspectUploadFileReader(file io.ReadSeeker, name string, maxImage, maxFile int64) (uploadKind, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if forbiddenExtensions[ext] {
		return uploadKind{}, &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "this file type is not allowed"}
	}
	reader := bufio.NewReader(file)
	sniff, _ := reader.Peek(512)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return uploadKind{}, err
	}
	detected := http.DetectContentType(sniff)

	if ct, ok := imageExtensions[ext]; ok {
		if err := validateImage(sniff, ext); err != nil {
			return uploadKind{}, err
		}
		if ext != ".webp" {
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return uploadKind{}, err
			}
			if _, _, err := image.DecodeConfig(file); err != nil {
				return uploadKind{}, &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "invalid image file"}
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return uploadKind{}, err
			}
		}
		return uploadKind{Kind: model.AttachmentKindImage, ContentType: ct, Limit: maxImage}, nil
	}
	if strings.HasPrefix(detected, "image/") {
		if _, ok := imageExtensions[ext]; !ok {
			return uploadKind{}, &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "image extension is required"}
		}
	}
	ct, ok := fileExtensions[ext]
	if !ok {
		return uploadKind{}, &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "this file type is not allowed"}
	}
	if strings.HasPrefix(detected, "text/plain") && strings.HasPrefix(ct, "text/") {
		return uploadKind{Kind: model.AttachmentKindFile, ContentType: ct, Limit: maxFile}, nil
	}
	return uploadKind{Kind: model.AttachmentKindFile, ContentType: ct, Limit: maxFile}, nil
}

func validateImage(sniff []byte, ext string) error {
	switch ext {
	case ".jpg", ".jpeg":
		if !bytes.HasPrefix(sniff, []byte{0xff, 0xd8, 0xff}) {
			return &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "invalid JPEG file"}
		}
	case ".png":
		if !bytes.HasPrefix(sniff, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
			return &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "invalid PNG file"}
		}
	case ".gif":
		if !bytes.HasPrefix(sniff, []byte("GIF87a")) && !bytes.HasPrefix(sniff, []byte("GIF89a")) {
			return &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "invalid GIF file"}
		}
	case ".webp":
		if len(sniff) < 12 || !bytes.Equal(sniff[0:4], []byte("RIFF")) || !bytes.Equal(sniff[8:12], []byte("WEBP")) {
			return &uploadInputError{status: http.StatusUnsupportedMediaType, code: "attachment_type_forbidden", message: "invalid WebP file"}
		}
	}
	return nil
}
