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
	for _, route := range []string{"GET /chats", "POST /chats", "PUT /chats/:id", "GET /recipients", "GET /recipients/:id", "GET /reports", "GET /reports/:id", "GET /reports/:id/messages", "GET /reports/:id/parts/:partNo", "GET /jobs", "POST /jobs/:id/retry", "GET /status", "PUT /digest-settings"} {
		parts := strings.SplitN(route, " ", 2)
		if !routes[parts[0]+" /api/v1/feishu-mention"+parts[1]] {
			t.Fatalf("missing route %s", route)
		}
	}
	if len(routes) != 13 {
		t.Fatalf("unexpected route count %d", len(routes))
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
