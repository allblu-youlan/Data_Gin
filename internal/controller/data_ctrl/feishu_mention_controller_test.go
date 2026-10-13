package data_ctrl

import (
	"errors"
	appconfig "gin-biz-web-api/config"
	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/service/data_svc"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFeishuMentionControllerRejectsInvalidWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller := &FeishuMentionController{service: data_svc.NewFeishuMentionAdminService(nil, "cli_current", appconfig.FeishuMentionConfig{DefaultFollowupMinutes: 30})}
	tests := []struct{ name, method, path, body string }{
		{"unknown fields", "POST", "/chats", `{"chatId":"oc_x","chatName":"群","isExternal":false,"enabled":true,"appId":"other"}`},
		{"trailing JSON", "POST", "/chats", `{"chatId":"oc_x","chatName":"群","isExternal":false,"enabled":true}{}`},
		{"omitted update boolean", "PUT", "/chats/1", `{"chatName":"群","followupMinutes":30,"enabled":true}`},
		{"immutable chat ID", "PUT", "/chats/1", `{"chatId":"other","chatName":"群","followupMinutes":30,"isExternal":false,"enabled":true}`},
		{"nonpositive interval", "POST", "/chats", `{"chatId":"oc_x","chatName":"群","isExternal":false,"enabled":true,"followupMinutes":0}`},
		{"null retry body", "POST", "/jobs/1/retry", `null`}, {"retry unknown field", "POST", "/jobs/1/retry", `{"force":true}`}, {"retry trailing body", "POST", "/jobs/1/retry", `{}{}`},
		{"invalid id", "PUT", "/chats/zero", `{}`}, {"size limit", "POST", "/chats", `{"chatName":"` + strings.Repeat("a", 1<<20) + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, uint(7)); c.Next() })
			router.POST("/chats", controller.CreateChat)
			router.PUT("/chats/:id", controller.UpdateChat)
			router.POST("/jobs/:id/retry", controller.Retry)
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":100400`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
func TestFeishuMentionControllerUsesSafeHTTPErrorStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"not found", data_svc.ErrFeishuMentionAdminNotFound, 404}, {"conflict", data_svc.ErrFeishuMentionAdminConflict, 409}, {"paused", data_svc.ErrFeishuMentionAdminPaused, 409}, {"internal", errors.New("SQL password=private-value raw_content=private"), 500}} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/", nil)
			writeFeishuMentionError(c, tc.err)
			if response.Code != tc.status || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
