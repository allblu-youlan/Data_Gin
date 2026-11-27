package routers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gin-biz-web-api/pkg/config"
	"github.com/gin-gonic/gin"
)

func TestTableBoardRoutesRequireConsoleLogin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("cfg:\n  jwt:\n    key: table-board-test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", dir+string(os.PathSeparator))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerTableBoardRoutes(router.Group("/api"))
	for _, route := range []struct{ method, suffix string }{
		{"GET", ""}, {"POST", ""}, {"GET", "/1"}, {"PUT", "/1"}, {"DELETE", "/1"}, {"POST", "/1/update"}, {"POST", "/1/delete?revision=1"},
		{"POST", "/query"}, {"POST", "/xlsx"}, {"POST", "/preview"}, {"POST", "/dimension-values"}, {"POST", "/1/evaluate"}, {"POST", "/1/export"},
	} {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(route.method, "/api/v1/table-boards"+route.suffix, nil)
		router.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status %d", route.method, route.suffix, res.Code)
		}
	}
}
