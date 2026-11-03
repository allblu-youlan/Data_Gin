package routers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/pkg/config"

	"github.com/gin-gonic/gin"
)

func TestSalesSummaryRequiresAuthentication(t *testing.T) {
	if config.Instance() == nil {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("cfg:\n  jwt:\n    key: sales-summary-test\n"), 0600); err != nil {
			t.Fatal(err)
		}
		config.NewConfig("", directory+string(os.PathSeparator))
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerSalesSummaryRoutes(router.Group("/api"), &data_ctrl.SalesSummaryController{})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/sales-summary?mallCode=TEST&startDate=2026-10-01&endDate=2026-10-31", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated sales summary returned %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
