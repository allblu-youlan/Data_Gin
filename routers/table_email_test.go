package routers

import (
	"gin-biz-web-api/pkg/config"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTableEmailRoutesRejectUnauthenticatedRequests(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("cfg:\n  jwt:\n    key: table-email-test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", directory+string(os.PathSeparator))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerTableEmailRoutes(router.Group("/api"))
	for _, route := range []struct{ method, path string }{{"GET", ""}, {"POST", ""}, {"PUT", "/1"}, {"DELETE", "/1"}, {"GET", "/datasources"}, {"GET", "/tables"}, {"GET", "/columns"}, {"POST", "/preview"}, {"GET", "/deliveries"}, {"POST", "/deliveries/1/retry"}} {
		t.Run(route.method+route.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(route.method, "/api/v1/table-email-notifications"+route.path, nil))
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
