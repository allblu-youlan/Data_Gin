package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"github.com/gin-gonic/gin"
	"os"
	"strings"
	"testing"
)

func TestRegisterFeishuMentionRoutesAreInternalOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerFeishuMentionRoutes(router.Group("/api"), &data_ctrl.FeishuMentionController{})
	routes := map[string]bool{}
	for _, r := range router.Routes() {
		routes[r.Method+" "+r.Path] = true
		if strings.Contains(r.Path, "/open/") || strings.Contains(r.Path, "/confirm") {
			t.Fatalf("unexpected route %s", r.Path)
		}
	}
	for _, route := range []string{"GET /chats", "POST /chats", "PUT /chats/:id", "GET /recipients", "GET /recipients/:id", "GET /reports", "GET /reports/:id", "POST /reports/:id/resend", "GET /reports/:id/messages", "GET /reports/:id/parts/:partNo", "GET /jobs", "POST /jobs/:id/retry", "GET /status", "PUT /digest-settings"} {
		parts := strings.SplitN(route, " ", 2)
		if !routes[parts[0]+" /api/v1/feishu-mention"+parts[1]] {
			t.Fatalf("missing route %s", route)
		}
	}
	if len(routes) != 14 {
		t.Fatalf("unexpected route count %d", len(routes))
	}
}

func TestFeishuMentionViewOnlyRegistersRecipientReadAndOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerFeishuMentionViewRoutes(router.Group("/api"), &data_ctrl.FeishuMentionViewController{})
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	if len(routes) != 2 || !routes["GET /api/v1/feishu-mention-view/reports/:id"] || !routes["GET /api/v1/feishu-mention-view/callback"] {
		t.Fatalf("unexpected recipient routes: %#v", routes)
	}
}

func TestFeishuMentionResendUsesReadAndRetryPermissions(t *testing.T) {
	source, err := os.ReadFile("data.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `group.POST("/reports/:id/resend", read, middleware.RequirePermission(model.PermissionOfficeMentionRetry), controller.ResendReport)`) {
		t.Fatal("resend must require read and retry permissions")
	}
}

func TestFeishuMentionDigestSettingsUsesManagePermission(t *testing.T) {
	source, err := os.ReadFile("data.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `manage := middleware.RequirePermission(model.PermissionOfficeMentionManage)`) ||
		!strings.Contains(string(source), `group.PUT("/digest-settings", manage, controller.SaveDigestSettings)`) {
		t.Fatal("digest settings must require dedicated office_mention.manage permission")
	}
}
