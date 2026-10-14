package data_ctrl

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func logOpenBojunQuery(c *gin.Context) func() {
	started := time.Now()
	requestID := uuid.NewString()
	c.Header("X-Request-ID", requestID)
	return func() {
		category := "success"
		switch c.Writer.Status() {
		case http.StatusForbidden:
			category = "forbidden"
		case http.StatusUnprocessableEntity:
			category = "invalid_query"
		case http.StatusInternalServerError:
			category = "unavailable"
		}
		if err := c.Request.Context().Err(); err != nil {
			category = err.Error()
		}
		zap.L().Info("open bojun query", zap.String("route", c.FullPath()),
			zap.String("request_id", requestID), zap.Duration("duration", time.Since(started)),
			zap.String("category", category), zap.Int("status", c.Writer.Status()))
	}
}
