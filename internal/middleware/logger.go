package middleware

import (
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func sanitizeQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "redacted"
	}
	for key := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "invite") || strings.Contains(lower, "password") ||
			strings.Contains(lower, "authorization") {
			values.Set(key, "redacted")
		}
	}
	return values.Encode()
}

func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
			"request_id", GetRequestID(c),
		}
		if query != "" {
			attrs = append(attrs, "query", sanitizeQuery(query))
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}

		switch {
		case c.Writer.Status() >= 500:
			log.Error("http_request", attrs...)
		case c.Writer.Status() >= 400:
			log.Warn("http_request", attrs...)
		default:
			log.Info("http_request", attrs...)
		}
	}
}
