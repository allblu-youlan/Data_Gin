package job

import (
	"fmt"

	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type asynqLogger struct{}

func (l asynqLogger) Debug(args ...interface{}) { l.write(zap.DebugLevel, args...) }
func (l asynqLogger) Info(args ...interface{})  { l.write(zap.InfoLevel, args...) }
func (l asynqLogger) Warn(args ...interface{})  { l.write(zap.WarnLevel, args...) }
func (l asynqLogger) Error(args ...interface{}) { l.write(zap.ErrorLevel, args...) }
func (l asynqLogger) Fatal(args ...interface{}) { l.write(zap.FatalLevel, args...) }

func (l asynqLogger) write(level zapcore.Level, args ...interface{}) {
	if logger.Logger == nil {
		return
	}
	entry := logger.Logger.WithOptions(zap.AddCallerSkip(1)).Check(level, "Asynq internal event")
	if entry != nil {
		entry.Write(zap.String("detail", providerhttp.RedactText(fmt.Sprint(args...))))
	}
}
