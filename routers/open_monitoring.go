package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

const (
	openMonitoringPreAuthIPRateLimit = "300-M"
	openMonitoringUserRouteRateLimit = "30-M"
)

func registerOpenMonitoringRoutes(api *gin.RouterGroup, controller *data_ctrl.OpenMonitoringController) {
	registerOpenMonitoringRoutesWithLimiters(api, controller,
		middleware.LimitOpenAPIIP("monitoring", openMonitoringPreAuthIPRateLimit),
		middleware.LimitOpenAPIUserRoute)
}

func registerOpenMonitoringRoutesWithLimiters(
	api *gin.RouterGroup,
	controller *data_ctrl.OpenMonitoringController,
	preAuthLimiter gin.HandlerFunc,
	newUserRouteLimiter func(module, limit string) gin.HandlerFunc,
) {
	group := api.Group("/open/monitoring")
	group.Use(preAuthLimiter, middleware.AuthOpenToken())
	userRouteLimiter := newUserRouteLimiter("monitoring", openMonitoringUserRouteRateLimit)
	group.POST("/hourly/query", middleware.RequirePermission(model.PermissionMonitoringHourlyRead), userRouteLimiter, controller.QueryHourly)
	group.POST("/daily/query", middleware.RequirePermission(model.PermissionMonitoringDailyRead), userRouteLimiter, controller.QueryDaily)
	group.POST("/areas/query", middleware.RequirePermission(model.PermissionMonitoringAreaRead), userRouteLimiter, controller.QueryAreas)
}
