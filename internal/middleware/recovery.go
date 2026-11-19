package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Error("panic recovered",
					"panic", recovered,
					"request_id", GetRequestID(c),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":      http.StatusInternalServerError,
					"message":   "internal server error",
					"requestId": GetRequestID(c),
				})
			}
		}()
		c.Next()
	}
}
