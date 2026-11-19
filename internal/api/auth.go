package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/middleware"
	"ppclassvip/internal/model"
	"ppclassvip/internal/store"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AuthHandler struct {
	users  *store.UserStore
	tokens *auth.TokenManager
	log    *slog.Logger
}

func NewAuthHandler(users *store.UserStore, tokens *auth.TokenManager, log *slog.Logger) *AuthHandler {
	return &AuthHandler{users: users, tokens: tokens, log: log}
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	if h.users == nil || h.tokens == nil {
		Fail(c, http.StatusServiceUnavailable, "auth service unavailable")
		return
	}

	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid request")
		return
	}

	req.Email = store.NormalizeEmail(req.Email)
	if req.Email == "" || req.Password == "" {
		Fail(c, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.users.GetByEmail(c.Request.Context(), req.Email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusUnauthorized, "invalid email or password")
			return
		}
		h.log.Error("login query failed", "error", err, "request_id", middleware.GetRequestID(c))
		Fail(c, http.StatusInternalServerError, "login failed")
		return
	}

	if user.Status != model.StatusActive {
		Fail(c, http.StatusForbidden, "account is disabled")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		Fail(c, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := h.tokens.Issue(user.ID, user.Role)
	if err != nil {
		h.log.Error("issue token failed", "error", err, "user_id", user.ID)
		Fail(c, http.StatusInternalServerError, "login failed")
		return
	}

	OK(c, gin.H{
		"token":     token,
		"tokenType": "Bearer",
		"expiresIn": int64(h.tokens.TTL() / time.Second),
		"user":      user,
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	if h.users == nil {
		Fail(c, http.StatusServiceUnavailable, "auth service unavailable")
		return
	}
	userID := middleware.UserID(c)
	if userID == 0 {
		Fail(c, http.StatusUnauthorized, "not authenticated")
		return
	}
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		FailCode(c, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	user, err := h.users.GetByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "user not found")
			return
		}
		h.log.Error("load user for password change failed", "error", err, "user_id", userID)
		Fail(c, http.StatusInternalServerError, "password_change_failed")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		FailCode(c, http.StatusBadRequest, "invalid_current_password", "current password is incorrect")
		return
	}
	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		FailCode(c, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if req.NewPassword == req.CurrentPassword {
		FailCode(c, http.StatusBadRequest, "password_unchanged", "new password must be different")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		FailCode(c, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if err := h.users.UpdateFields(c.Request.Context(), userID, map[string]any{"password_hash": hash}); err != nil {
		h.log.Error("update password failed", "error", err, "user_id", userID)
		Fail(c, http.StatusInternalServerError, "password_change_failed")
		return
	}
	OK(c, gin.H{"changed": true})
}

func (h *AuthHandler) Me(c *gin.Context) {
	if h.users == nil {
		Fail(c, http.StatusServiceUnavailable, "auth service unavailable")
		return
	}
	userID := middleware.UserID(c)
	if userID == 0 {
		Fail(c, http.StatusUnauthorized, "not authenticated")
		return
	}

	user, err := h.users.GetByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, http.StatusNotFound, "user not found")
			return
		}
		h.log.Error("me query failed", "error", err, "user_id", userID)
		Fail(c, http.StatusInternalServerError, "query failed")
		return
	}

	OK(c, user)
}
