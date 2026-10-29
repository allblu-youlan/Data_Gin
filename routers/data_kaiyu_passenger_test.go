package routers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gin-biz-web-api/internal/controller/data_ctrl"
	pkgconfig "gin-biz-web-api/pkg/config"

	"github.com/gin-gonic/gin"
)

func TestKaiyuPassengerRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("JWT:\n  Key: kaiyu-route-test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pkgconfig.NewConfig("", directory+string(os.PathSeparator))
	router := gin.New()
	registerKaiyuPassengerRoutes(router.Group("/api"), &data_ctrl.KaiyuPassengerController{})
	want := map[string]bool{
		"GET /api/v1/kaiyu-passenger":         true,
		"GET /api/v1/kaiyu-passenger/malls":   true,
		"POST /api/v1/kaiyu-passenger/export": true,
	}
	if len(router.Routes()) != len(want) {
		t.Fatalf("routes=%v", router.Routes())
	}
	for _, route := range router.Routes() {
		if !want[route.Method+" "+route.Path] {
			t.Fatalf("unexpected route %s %s", route.Method, route.Path)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(route.Method, route.Path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d", route.Method, route.Path, recorder.Code)
		}
	}
}
