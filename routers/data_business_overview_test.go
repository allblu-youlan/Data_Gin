package routers

import (
	"net/http"
	"testing"

	"gin-biz-web-api/internal/controller/data_ctrl"

	"github.com/gin-gonic/gin"
)

func TestRegisterBusinessOverviewRoutesUsesExpectedMethodAndPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerBusinessOverviewRoutes(router.Group("/api"), &data_ctrl.BusinessOverviewController{})
	routes := router.Routes()
	if len(routes) != 2 || routes[0].Method != http.MethodGet || routes[0].Path != "/api/v1/business-overview/malls" ||
		routes[1].Method != http.MethodGet || routes[1].Path != "/api/v1/business-overview/payments" {
		t.Fatalf("routes = %#v", routes)
	}
}

func TestRegisterOpenBusinessOverviewRoutesUsesPOST(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerOpenBusinessOverviewRoutes(router.Group("/api"), &data_ctrl.BusinessOverviewController{})
	routes := router.Routes()
	if len(routes) != 1 || routes[0].Method != http.MethodPost || routes[0].Path != "/api/open/business-overview/payments/query" {
		t.Fatalf("routes = %#v", routes)
	}
}
