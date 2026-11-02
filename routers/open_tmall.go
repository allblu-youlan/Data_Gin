package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

const (
	openTmallPreAuthIPRateLimit = "300-M"
	openTmallUserRouteRateLimit = "30-M"
)

func registerOpenTmallRoutes(api *gin.RouterGroup, controller *data_ctrl.OpenTmallController) {
	registerOpenTmallRoutesWithLimiters(api, controller,
		middleware.LimitOpenAPIIP("tmall", openTmallPreAuthIPRateLimit),
		middleware.LimitOpenAPIUserRoute)
}

func registerOpenTmallRoutesWithLimiters(
	api *gin.RouterGroup,
	controller *data_ctrl.OpenTmallController,
	preAuthLimiter gin.HandlerFunc,
	newUserRouteLimiter func(module, limit string) gin.HandlerFunc,
) {
	group := api.Group("/open/tmall")
	group.Use(preAuthLimiter, middleware.AuthOpenToken())
	userRouteLimiter := newUserRouteLimiter("tmall", openTmallUserRouteRateLimit)
	group.POST("/sycm/query", middleware.RequirePermission(model.PermissionTmallSycmRead), userRouteLimiter, controller.QuerySycm)
	group.POST("/qztg/query", middleware.RequirePermission(model.PermissionTmallQztgRead), userRouteLimiter, controller.QueryQztg)
	group.POST("/tot/query", middleware.RequirePermission(model.PermissionTmallTotRead), userRouteLimiter, controller.QueryTot)
}
