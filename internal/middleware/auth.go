package middleware

import (
	"net/http"
	"strings"

	"ppclassvip/internal/auth"

	"github.com/gin-gonic/gin"
)

const (
	ContextUserID        = "auth_user_id"
	ContextRole          = "auth_role"
	ContextRoomID        = "auth_room_id"
	ContextParticipantID = "auth_participant_id"
)

func AuthRequired(tokens *auth.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			abortUnauthorized(c, "missing bearer token")
			return
		}

		token := strings.TrimSpace(header[len("Bearer "):])
		claims, err := tokens.Parse(token)
		if err != nil {
			abortUnauthorized(c, "invalid or expired token")
			return
		}

		c.Set(ContextUserID, claims.UserID)
		c.Set(ContextRole, claims.Role)
		c.Set(ContextRoomID, claims.RoomID)
		c.Set(ContextParticipantID, claims.ParticipantID)
		c.Next()
	}
}

func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if Role(c) != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":      http.StatusForbidden,
				"message":   "admin access required",
				"error":     "admin_required",
				"requestId": GetRequestID(c),
			})
			return
		}
		c.Next()
	}
}

func UserID(c *gin.Context) uint64 {
	return c.GetUint64(ContextUserID)
}

func Role(c *gin.Context) string {
	return c.GetString(ContextRole)
}

func RoomID(c *gin.Context) uint64 {
	return c.GetUint64(ContextRoomID)
}

func ParticipantID(c *gin.Context) uint64 {
	return c.GetUint64(ContextParticipantID)
}

func abortUnauthorized(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"code":      http.StatusUnauthorized,
		"message":   message,
		"requestId": GetRequestID(c),
	})
}
