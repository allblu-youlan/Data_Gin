package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/msg"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestHTTPFailuresLogOnceWithDiagnosticContext(t *testing.T) {
	if config.Instance() == nil {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("app: {}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		config.NewConfig("", directory+string(os.PathSeparator))
	}
	settings := config.Instance()
	for key, value := range map[string]any{
		"cfg.app.debug": false, "cfg.app.env": "prod", "cfg.upload.static_fs_relative_path": "/uploads/",
	} {
		previous := settings.Get(key)
		settings.Set(key, value)
		t.Cleanup(func() { settings.Set(key, previous) })
	}
	gin.SetMode(gin.TestMode)
	operationError := fmt.Errorf("load inventory: %w", errors.New("ORA-00942 missing table password=private-secret"))
	tests := []struct {
		name       string
		handler    gin.HandlerFunc
		wantStatus int
		wantError  string
		wantCount  int
		legacy     bool
	}{
		{
			name:       "legacy business error",
			handler:    func(c *gin.Context) { c.JSON(500, msg.ErrResponse("查询库存失败", operationError)) },
			wantStatus: 500, wantError: "load inventory: ORA-00942", wantCount: 1, legacy: true,
		},
		{
			name:       "legacy public error",
			handler:    func(c *gin.Context) { c.JSON(400, msg.ErrResponseStr("库存查询参数无效")) },
			wantStatus: 400, wantCount: 0, legacy: true,
		},
		{
			name: "application error with HTTP 200",
			handler: func(c *gin.Context) {
				responses.New(c).ToErrorResponse(errcode.DBError.WithError(operationError).WithDetails("inventory query failed"))
			},
			wantStatus: 200, wantError: "load inventory: ORA-00942", wantCount: 1,
		},
		{
			name: "safe response with private error",
			handler: func(c *gin.Context) {
				_ = c.Error(operationError).SetType(gin.ErrorTypePrivate)
				responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "查询库存失败")
			},
			wantStatus: 500, wantError: "load inventory: ORA-00942", wantCount: 1,
		},
		{
			name: "safe client error with private diagnostic",
			handler: func(c *gin.Context) {
				_ = c.Error(errors.New("invalid inventory query")).SetType(gin.ErrorTypePrivate)
				responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "查询参数无效")
			},
			wantStatus: 400, wantCount: 0,
		},
		{
			name: "application client error with HTTP 200",
			handler: func(c *gin.Context) {
				responses.New(c).ToErrorResponse(errcode.BadRequest.WithDetails("库存编号不能为空"))
			},
			wantStatus: 200, wantCount: 0,
		},
		{
			name:       "direct JSON error",
			handler:    func(c *gin.Context) { c.JSON(502, gin.H{"msg": "上游库存服务失败"}) },
			wantStatus: 502, wantError: "上游库存服务失败", wantCount: 1,
		},
		{
			name: "validation response",
			handler: func(c *gin.Context) {
				responses.New(c).ToErrorValidateResponse(errcode.BadRequest, map[string][]string{"inventory_id": {"库存编号不能为空"}})
			},
			wantStatus: 200, wantCount: 0,
		},
		{
			name:       "direct application error",
			handler:    func(c *gin.Context) { c.JSON(200, gin.H{"code": 201, "msg": "库存生成失败"}) },
			wantStatus: 200, wantCount: 0,
		},
		{
			name:       "direct client error",
			handler:    func(c *gin.Context) { c.JSON(403, gin.H{"msg": "库存查询权限不足"}) },
			wantStatus: 403, wantCount: 0,
		},
		{
			name: "private execution error",
			handler: func(c *gin.Context) {
				_ = c.Error(operationError).SetType(gin.ErrorTypePrivate)
				c.Status(200)
			},
			wantStatus: 200, wantError: "load inventory: ORA-00942", wantCount: 1,
		},
		{
			name: "client disconnected",
			handler: func(c *gin.Context) {
				panic(&net.OpError{Op: "write", Net: "tcp", Err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}})
			},
			wantStatus: 200, wantCount: 0,
		},
		{
			name:       "panic already recorded",
			handler:    func(c *gin.Context) { panic("inventory handler panic") },
			wantStatus: 200, wantError: "inventory handler panic", wantCount: 1,
		},
		{
			name:       "success",
			handler:    func(c *gin.Context) { c.JSON(200, msg.SuccessResponseStr("库存查询成功")) },
			wantStatus: 200, wantCount: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			core, logs := observer.New(zap.ErrorLevel)
			previous := logger.Logger
			logger.Logger = zap.New(core)
			t.Cleanup(func() { logger.Logger = previous })
			router := gin.New()
			router.Use(AccessLog(), Recovery())
			router.POST("/inventory/:id", func(c *gin.Context) {
				c.Set(constant.CurrentUserID, "17")
				test.handler(c)
			})
			request := httptest.NewRequest(http.MethodPost, "/inventory/42?token=query-secret", strings.NewReader(`{"password":"body-secret"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer header-secret")
			request.Header.Set("X-Request-ID", "inventory-request-42")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			entries := logs.All()
			if len(entries) != test.wantCount {
				t.Fatalf("error logs = %d, want %d: %v", len(entries), test.wantCount, entries)
			}
			if len(entries) == 0 {
				return
			}
			fields := entries[0].ContextMap()
			logged := fmt.Sprint(fields)
			if !strings.Contains(logged, test.wantError) {
				t.Fatalf("log lost error detail %q: %s", test.wantError, logged)
			}
			for _, secret := range []string{"private-secret", "query-secret", "body-secret", "header-secret"} {
				if strings.Contains(logged, secret) {
					t.Fatalf("log leaked %q: %s", secret, logged)
				}
			}
			if !test.legacy && test.name != "panic already recorded" {
				for key, value := range map[string]string{
					"request_method": "POST", "request_path": "/inventory/42",
					"request_route": "/inventory/:id", "request_id": "inventory-request-42", "user_id": "17",
				} {
					if fields[key] != value {
						t.Fatalf("%s = %v, want %s", key, fields[key], value)
					}
				}
			}
		})
	}
}
