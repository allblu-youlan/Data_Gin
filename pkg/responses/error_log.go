package responses

import (
	"fmt"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const requestErrorLoggedKey = "responses.error_logged"

// MarkRequestErrorLogged prevents response and access logging from repeating a recovered panic.
func MarkRequestErrorLogged(c *gin.Context) {
	c.Set(requestErrorLoggedKey, true)
}

// LogRequestFailure records one request failure with safe request metadata and its severity.
func LogRequestFailure(c *gin.Context, level zapcore.Level, message string, fields ...zap.Field) {
	if logger.Logger == nil || c.GetBool(requestErrorLoggedKey) {
		return
	}
	MarkRequestErrorLogged(c)
	fields = append(fields,
		zap.String("error_message", providerhttp.RedactText(message)),
		zap.String("request_route", c.FullPath()),
	)
	if c.Request != nil {
		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = c.Writer.Header().Get("X-Request-ID")
		}
		if requestID == "" {
			requestID = c.GetHeader("X-Request-ID")
		}
		fields = append(fields,
			zap.String("request_method", c.Request.Method),
			zap.String("request_path", providerhttp.RedactText(c.Request.URL.Path)),
			zap.String("request_id", providerhttp.RedactText(requestID)),
			zap.String("client_ip", c.ClientIP()),
		)
	}
	if userID, ok := c.Get(constant.CurrentUserID); ok {
		fields = append(fields, zap.String("user_id", fmt.Sprint(userID)))
	}
	if len(c.Errors) > 0 {
		fields = append(fields, zap.String("request_errors", providerhttp.RedactText(c.Errors.String())))
	}
	if level >= zap.ErrorLevel {
		logger.Error("HTTP request failed", fields...)
		return
	}
	logger.Warn("HTTP request rejected", fields...)
}
