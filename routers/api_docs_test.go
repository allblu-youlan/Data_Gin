package routers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gin-biz-web-api/internal/apidoc"
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"

	"github.com/gin-gonic/gin"
)

func TestAPIDocsPublicWithoutDatabaseAndCacheable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	registerAPIDocsRoutes(router, api)
	api.Use(middleware.RequireDatabaseAvailability())
	registerOpenTmallRoutes(api, data_ctrl.NewOpenTmallController())

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
	if payload.Code != 0 || document.Title != "allblu开放接口文档" || len(document.Groups) != 1 {
		t.Fatalf("unexpected docs: %+v", payload)
	}
	count := 0
	for _, child := range document.Groups[0].Children {
		count += len(child.Operations)
	}
	if count != 6 {
		t.Fatalf("expected six registered Tmall APIs, got %d", count)
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
