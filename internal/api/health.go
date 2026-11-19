package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"ppclassvip/internal/version"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type HealthHandler struct {
	db *gorm.DB
}

func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Health(c *gin.Context) {
	h.report(c)
}

func (h *HealthHandler) Ready(c *gin.Context) {
	h.report(c)
}

func (h *HealthHandler) report(c *gin.Context) {
	result := gin.H{
		"status":  "ok",
		"service": "ppclassvip",
		"version": version.String(),
	}

	status := http.StatusOK
	if h.db == nil {
		result["status"] = "degraded"
		result["database"] = "unavailable"
		status = http.StatusServiceUnavailable
	} else {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		sqlDB, err := h.db.DB()
		if err != nil {
			result["status"] = "degraded"
			result["database"] = "unavailable"
			status = http.StatusServiceUnavailable
		} else if pingDatabase(ctx, sqlDB) {
			result["database"] = "ok"
		} else {
			result["status"] = "degraded"
			result["database"] = "unavailable"
			status = http.StatusServiceUnavailable
		}
	}

	c.JSON(status, Envelope{
		Code:    status,
		Message: http.StatusText(status),
		Data:    result,
	})
}

func pingDatabase(ctx context.Context, db *sql.DB) bool {
	if err := db.PingContext(ctx); err == nil {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(100 * time.Millisecond):
	}
	return db.PingContext(ctx) == nil
}
