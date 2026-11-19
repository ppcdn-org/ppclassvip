package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func extendReadDeadline(c *gin.Context, d time.Duration) {
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetReadDeadline(time.Now().Add(d))
}

func extendWriteDeadline(c *gin.Context, d time.Duration) {
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetWriteDeadline(time.Now().Add(d))
}
