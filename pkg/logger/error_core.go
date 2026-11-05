package logger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/pkg/providerhttp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ErrorRecord contains the diagnostic information captured at the failing call site.
type ErrorRecord struct {
	Time    time.Time
	Level   string
	Message string
	Caller  string
	Stack   string
	Details string
}

// Format keeps the cause and call site ahead of the stack within Oracle's 4000-byte limit.
func (record ErrorRecord) Format() string {
	var details map[string]interface{}
	_ = json.Unmarshal([]byte(record.Details), &details)
	causes := make([]string, 0, 3)
	for _, key := range []string{"error_cause", "error", "request_errors"} {
		if cause, ok := details[key]; ok {
			causes = append(causes, fmt.Sprint(cause))
		}
	}
	summary := fmt.Sprintf("时间=%s 级别=%s\n错误=%s\n原因=%s\n位置=%s\n详情=%s\n堆栈=%s",
		record.Time.Format(time.RFC3339Nano), record.Level,
		truncateErrorText(record.Message, 400), truncateErrorText(strings.Join(causes, " | "), 800),
		truncateErrorText(record.Caller, 250), truncateErrorText(record.Details, 1800), record.Stack)
	return truncateErrorText(summary, 4000)
}

func truncateErrorText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const suffix = "...[截断]"
	end := limit - len(suffix)
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + suffix
}

type errorLogItem struct {
	record  ErrorRecord
	flushed chan struct{}
}

type errorLogWorker struct {
	queue chan errorLogItem
	write func(context.Context, ErrorRecord) error
}

type errorCore struct {
	worker  *errorLogWorker
	encoder zapcore.Encoder
}

// NewErrorCore asynchronously persists Error and higher levels using a bounded queue.
// The existing local log retains the complete event if Oracle is unavailable.
func NewErrorCore(write func(context.Context, ErrorRecord) error) zapcore.Core {
	worker := &errorLogWorker{queue: make(chan errorLogItem, 256), write: write}
	go worker.run()
	return &errorCore{worker: worker, encoder: zapcore.NewJSONEncoder(zapcore.EncoderConfig{})}
}

// AddCore attaches an output during startup and updates the global Zap logger.
func AddCore(core zapcore.Core) {
	Logger = Logger.WithOptions(zap.WrapCore(func(existing zapcore.Core) zapcore.Core {
		return zapcore.NewTee(existing, core)
	}))
	zap.ReplaceGlobals(Logger)
}

func (core *errorCore) Enabled(level zapcore.Level) bool { return level >= zapcore.ErrorLevel }

func (core *errorCore) With(fields []zapcore.Field) zapcore.Core {
	clone := &errorCore{worker: core.worker, encoder: core.encoder.Clone()}
	for _, field := range fields {
		field.AddTo(clone.encoder)
	}
	return clone
}

func (core *errorCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if core.Enabled(entry.Level) {
		return checked.AddCore(entry, core)
	}
	return checked
}

func (core *errorCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	buffer, err := core.encoder.EncodeEntry(entry, fields)
	if err != nil {
		return err
	}
	details := sanitizeErrorDetails(buffer.Bytes())
	buffer.Free()
	record := ErrorRecord{
		Time: entry.Time, Level: entry.Level.String(),
		Message: providerhttp.RedactText(entry.Message), Caller: providerhttp.RedactText(entry.Caller.TrimmedPath()),
		Stack: providerhttp.RedactText(entry.Stack), Details: details,
	}
	select {
	case core.worker.queue <- errorLogItem{record: record}:
	default:
		return errors.New("Oracle error log queue is full; full event retained in local log")
	}
	// Zap exits immediately after Fatal and panics after Panic, so flush these synchronously.
	if entry.Level >= zapcore.DPanicLevel {
		return core.Sync()
	}
	return nil
}

func (core *errorCore) Sync() error {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	flushed := make(chan struct{})
	select {
	case core.worker.queue <- errorLogItem{flushed: flushed}:
	case <-timer.C:
		return errors.New("Oracle error log flush timed out")
	}
	select {
	case <-flushed:
		return nil
	case <-timer.C:
		return errors.New("Oracle error log flush timed out")
	}
}

func (worker *errorLogWorker) run() {
	for item := range worker.queue {
		if item.flushed != nil {
			close(item.flushed)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := worker.write(ctx, item.record)
		cancel()
		if err != nil {
			// Do not feed writer failures back into Zap, which would recursively enqueue them.
			fmt.Fprintf(os.Stderr, "Oracle error log write failed (event retained in local log): %s\n", providerhttp.RedactText(err.Error()))
		}
	}
}

func sanitizeErrorDetails(data []byte) string {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return "[日志字段编码失败]"
	}
	sanitized, err := json.Marshal(sanitizeErrorValue(value))
	if err != nil {
		return "[日志字段编码失败]"
	}
	return string(sanitized)
}

func sanitizeErrorValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "password") || strings.Contains(lower, "passwd") ||
				strings.Contains(lower, "secret") || strings.Contains(lower, "token") ||
				strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") ||
				strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") {
				typed[key] = "[REDACTED]"
			} else {
				typed[key] = sanitizeErrorValue(child)
			}
		}
	case []interface{}:
		for index, child := range typed {
			typed[index] = sanitizeErrorValue(child)
		}
	case string:
		return providerhttp.RedactText(typed)
	}
	return value
}
