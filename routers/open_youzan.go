package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

const (
	openYouzanPreAuthIPRateLimit = "300-M"
	openYouzanUserRouteRateLimit = "30-M"
)

func registerOpenYouzanRoutes(api *gin.RouterGroup, controller *data_ctrl.OpenYouzanController) {
	registerOpenYouzanRoutesWithLimiters(api, controller,
		middleware.LimitOpenAPIIP("youzan", openYouzanPreAuthIPRateLimit),
		middleware.LimitOpenAPIUserRoute)
}

func registerOpenYouzanRoutesWithLimiters(
	api *gin.RouterGroup,
	controller *data_ctrl.OpenYouzanController,
	preAuthLimiter gin.HandlerFunc,
	newUserRouteLimiter func(module, limit string) gin.HandlerFunc,
) {
	group := api.Group("/open/youzan")
	group.Use(preAuthLimiter, middleware.AuthOpenToken())
	userRouteLimiter := newUserRouteLimiter("youzan", openYouzanUserRouteRateLimit)
	group.POST("/orders/query", middleware.RequirePermission(model.PermissionYouzanOrdersRead), userRouteLimiter, controller.QueryOrders)
	group.POST("/orders/items/query", middleware.RequirePermission(model.PermissionYouzanOrderItemsRead), userRouteLimiter, controller.QueryOrderItems)
}
