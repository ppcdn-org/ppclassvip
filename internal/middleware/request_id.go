package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	HeaderRequestID  = "X-Request-ID"
	ContextRequestID = "request_id"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(HeaderRequestID))
		if requestID == "" || len(requestID) > 128 {
			requestID = newRequestID()
		}
		c.Set(ContextRequestID, requestID)
		c.Header(HeaderRequestID, requestID)
		c.Next()
	}
}

func GetRequestID(c *gin.Context) string {
	return c.GetString(ContextRequestID)
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}
