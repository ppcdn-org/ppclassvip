package api

import (
	"net/http"

	"ppclassvip/internal/middleware"

	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Error     string `json:"error,omitempty"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{
		Code:      http.StatusOK,
		Message:   http.StatusText(http.StatusOK),
		Data:      data,
		RequestID: middleware.GetRequestID(c),
	})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{
		Code:      http.StatusCreated,
		Message:   http.StatusText(http.StatusCreated),
		Data:      data,
		RequestID: middleware.GetRequestID(c),
	})
}

func Fail(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, Envelope{
		Code:      status,
		Message:   message,
		RequestID: middleware.GetRequestID(c),
	})
}

func FailCode(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, Envelope{
		Code:      status,
		Message:   message,
		Error:     code,
		RequestID: middleware.GetRequestID(c),
	})
}
