package msg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"

	"gin-biz-web-api/pkg/logger"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestResponseLoggingSeparatesValidationFromExecutionFailure(t *testing.T) {
	validationError := validator.New().Struct(struct {
		InventoryID string `validate:"required"`
	}{})
	if validationError == nil {
		t.Fatal("expected required-field validation failure")
	}
	jsonSyntaxError := json.Unmarshal([]byte("{"), &struct{}{})
	jsonTypeError := json.Unmarshal([]byte(`{"count":"invalid"}`), &struct{ Count int }{})
	_, numberError := strconv.Atoi("invalid")
	tests := []struct {
		name      string
		err       error
		wantLevel zapcore.Level
	}{
		{name: "public business message", wantLevel: zap.WarnLevel},
		{name: "validation failure", err: validationError, wantLevel: zap.WarnLevel},
		{name: "wrapped validation failure", err: fmt.Errorf("invalid request: %w", validationError), wantLevel: zap.WarnLevel},
		{name: "JSON syntax failure", err: jsonSyntaxError, wantLevel: zap.WarnLevel},
		{name: "wrapped JSON syntax failure", err: fmt.Errorf("decode request: %w", jsonSyntaxError), wantLevel: zap.WarnLevel},
		{name: "JSON type failure", err: jsonTypeError, wantLevel: zap.WarnLevel},
		{name: "wrapped JSON type failure", err: fmt.Errorf("decode request: %w", jsonTypeError), wantLevel: zap.WarnLevel},
		{name: "number parsing failure", err: numberError, wantLevel: zap.WarnLevel},
		{name: "wrapped number parsing failure", err: fmt.Errorf("parse inventory id: %w", numberError), wantLevel: zap.WarnLevel},
		{name: "empty request body", err: io.EOF, wantLevel: zap.WarnLevel},
		{name: "wrapped empty request body", err: fmt.Errorf("bind request: %w", io.EOF), wantLevel: zap.WarnLevel},
		{name: "execution failure", err: errors.New("ORA-00942 missing inventory table"), wantLevel: zap.ErrorLevel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			previous := logger.Logger
			logger.Logger = zap.New(core)
			t.Cleanup(func() { logger.Logger = previous })

			ErrResponse("库存查询失败", test.err)

			entries := logs.All()
			if len(entries) != 1 || entries[0].Level != test.wantLevel {
				t.Fatalf("logs = %v, want one %s entry", entries, test.wantLevel)
			}
		})
	}
}
