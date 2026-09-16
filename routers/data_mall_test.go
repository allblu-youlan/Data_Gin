package routers

import (
	"net/http"
	"testing"

	"gin-biz-web-api/internal/controller/data_ctrl"

	"github.com/gin-gonic/gin"
)

func TestAPIDataRegistersMallCRUDRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerMallRoutes(router.Group("/api"), &data_ctrl.MallController{})
	registerMallWeatherRoutes(router.Group("/api"), &data_ctrl.MallWeatherController{})
	registerOpenWeatherRoutes(
		router.Group("/api"),
		&data_ctrl.MallWeatherController{},
		&data_ctrl.OpenWeatherMallController{},
	)
	registerOpenBojunRoutes(
		router.Group("/api"),
		&data_ctrl.OpenBojunOrderController{},
		&data_ctrl.OpenBojunProductController{},
		&data_ctrl.OpenBojunSyncStatusController{},
	)
	registerOpenBusinessOverviewRoutes(router.Group("/api"), &data_ctrl.BusinessOverviewController{})
	registerMallWeatherRefreshRoutes(router.Group("/api"), &data_ctrl.MallWeatherRefreshController{})
	registerMallWeatherExportProfileRoutes(router.Group("/api"), &data_ctrl.MallWeatherExportProfileController{})
	registerMallWeatherExportJobRoutes(router.Group("/api"), &data_ctrl.MallWeatherExportJobController{})
	registerMallWeatherFeishuPushRoutes(router.Group("/api"), &data_ctrl.MallWeatherFeishuPushController{})
	registerMallWeatherSheetPushOptionRoutes(router.Group("/api"), &data_ctrl.MallWeatherSheetPushOptionController{})

	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, expected := range []string{
		http.MethodPost + " /api/v1/malls",
		http.MethodPost + " /api/v1/malls/import",
		http.MethodGet + " /api/v1/malls",
		http.MethodGet + " /api/v1/malls/:id",
		http.MethodPatch + " /api/v1/malls/:id",
		http.MethodDelete + " /api/v1/malls/:id",
		http.MethodGet + " /api/v1/malls/:id/weather/overview",
		http.MethodGet + " /api/v1/malls/:id/weather/realtime",
		http.MethodGet + " /api/v1/malls/:id/weather/minutely",
		http.MethodGet + " /api/v1/malls/:id/weather/hourly",
		http.MethodGet + " /api/v1/malls/:id/weather/daily",
		http.MethodGet + " /api/v1/malls/:id/weather/alerts",
		http.MethodGet + " /api/v1/malls/:id/weather/life-indices",
		http.MethodGet + " /api/v1/malls/:id/weather/fetch-runs",
		http.MethodPost + " /api/open/weather/malls/:id/overview",
		http.MethodPost + " /api/open/weather/malls/:id/realtime",
		http.MethodPost + " /api/open/weather/malls/:id/minutely",
		http.MethodPost + " /api/open/weather/malls/:id/hourly",
		http.MethodPost + " /api/open/weather/malls/:id/daily",
		http.MethodPost + " /api/open/weather/malls/:id/alerts",
		http.MethodPost + " /api/open/weather/malls/:id/life-indices",
		http.MethodPost + " /api/open/weather/overview",
		http.MethodPost + " /api/open/weather/realtime",
		http.MethodPost + " /api/open/weather/history/day",
		http.MethodPost + " /api/open/weather/history/day/summary",
		http.MethodPost + " /api/open/weather/history/range",
		http.MethodPost + " /api/open/weather/minutely",
		http.MethodPost + " /api/open/weather/hourly",
		http.MethodPost + " /api/open/weather/daily",
		http.MethodPost + " /api/open/weather/alerts",
		http.MethodPost + " /api/open/weather/life-indices",
		http.MethodPost + " /api/open/weather/malls/query",
		http.MethodPost + " /api/open/bojun/orders/query",
		http.MethodPost + " /api/open/bojun/products/query",
		http.MethodPost + " /api/open/bojun/sync-status/query",
		http.MethodPost + " /api/open/business-overview/payments/query",
		http.MethodPost + " /api/v1/malls/:id/weather-refresh",
		http.MethodPost + " /api/v1/weather-export-profiles",
		http.MethodGet + " /api/v1/weather-export-profiles",
		http.MethodPost + " /api/v1/weather-exports",
		http.MethodGet + " /api/v1/weather-exports/:job_id",
		http.MethodGet + " /api/v1/weather-exports/:job_id/download",
		http.MethodGet + " /api/v1/weather-exports/:job_id/content",
		http.MethodPost + " /api/v1/weather-exports/:job_id/content",
		http.MethodGet + " /api/v1/weather-exports/:job_id/content/status",
		http.MethodPost + " /api/v1/weather-sheet-pushes",
		http.MethodGet + " /api/v1/weather-sheet-pushes/:run_id",
		http.MethodPost + " /api/v1/weather-sheet-pushes/dry-run",
		http.MethodGet + " /api/v1/weather-sheet-push-options",
	} {
		if _, ok := routes[expected]; !ok {
			t.Errorf("route %q is not registered", expected)
		}
	}
	for _, forbidden := range []string{
		http.MethodGet + " /api/open/weather/malls/:id/hourly",
		http.MethodGet + " /api/open/weather/history/day",
		http.MethodGet + " /api/open/weather/history/day/summary",
		http.MethodGet + " /api/open/weather/history/range",
		http.MethodPost + " /api/open/weather/malls/:id/fetch-runs",
		http.MethodPost + " /api/open/weather/malls/:id/download",
		http.MethodPost + " /api/open/weather/malls/:id/refresh",
		http.MethodGet + " /api/open/bojun/orders/query",
		http.MethodGet + " /api/open/bojun/products/query",
		http.MethodGet + " /api/open/bojun/sync-status/query",
		http.MethodGet + " /api/open/business-overview/payments/query",
	} {
		if _, ok := routes[forbidden]; ok {
			t.Errorf("route %q must not be registered", forbidden)
		}
	}
}
