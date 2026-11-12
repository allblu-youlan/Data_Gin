package routers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gin-biz-web-api/internal/apidoc"
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

func TestAPIDocsPublicWithoutDatabaseAndCacheable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	registerAPIDocsRoutes(router, api)
	api.Use(middleware.RequireDatabaseAvailability())
	registerOpenTmallRoutes(api, data_ctrl.NewOpenTmallController())
	registerOpenMonitoringRoutes(api, data_ctrl.NewOpenMonitoringController())

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/api-docs", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("public docs: %d %s", response.Code, response.Body)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Document apidoc.Document `json:"document"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	document := payload.Data.Document
	if payload.Code != 0 || document.Title != "allblu开放接口文档" || len(document.Groups) != 2 {
		t.Fatalf("unexpected docs: code=%d title=%q groups=%d", payload.Code, document.Title, len(document.Groups))
	}
	count := 0
	monitoringCount := 0
	for _, group := range document.Groups {
		for _, child := range group.Children {
			count += len(child.Operations)
			if group.Code == model.PermissionMonitoringAPIRead {
				for _, operation := range child.Operations {
					monitoringCount++
					if operation.ParentPermission != model.PermissionMonitoringAPIRead || len(operation.RequestFields) == 0 || len(operation.ResponseFields) == 0 {
						t.Fatalf("incomplete monitoring contract: %+v", operation)
					}
				}
			}
		}
	}
	if count != 9 || monitoringCount != 3 {
		t.Fatalf("expected six Tmall and three monitoring APIs, got %d total and %d monitoring", count, monitoringCount)
	}
	etag := response.Header().Get("ETag")
	if etag == "" || response.Header().Get("Cache-Control") == "" {
		t.Fatal("missing public cache headers")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/api-docs", nil)
	request.Header.Set("If-None-Match", etag)
	cached := httptest.NewRecorder()
	router.ServeHTTP(cached, request)
	if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
		t.Fatalf("cached docs: %d", cached.Code)
	}
}
