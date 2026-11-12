package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

const (
	openFeishuAttendancePreAuthIPRateLimit = "300-M"
	openFeishuAttendanceUserRouteRateLimit = "30-M"
)

func registerOpenFeishuAttendanceRoutes(api *gin.RouterGroup, controller *data_ctrl.OpenFeishuAttendanceController) {
	registerOpenFeishuAttendanceRoutesWithLimiters(api, controller,
		middleware.LimitOpenAPIIP("feishu_attendance", openFeishuAttendancePreAuthIPRateLimit),
		middleware.LimitOpenAPIUserRoute)
}

func registerOpenFeishuAttendanceRoutesWithLimiters(
	api *gin.RouterGroup,
	controller *data_ctrl.OpenFeishuAttendanceController,
	preAuthLimiter gin.HandlerFunc,
	newUserRouteLimiter func(module, limit string) gin.HandlerFunc,
) {
	group := api.Group("/open/feishu/attendance")
	group.Use(preAuthLimiter, middleware.AuthOpenToken())
	userRouteLimiter := newUserRouteLimiter("feishu_attendance", openFeishuAttendanceUserRouteRateLimit)
	group.POST("/daily/query", middleware.RequirePermission(model.PermissionFeishuAttendanceDailyRead), userRouteLimiter, controller.QueryDaily)
	group.POST("/punch/query", middleware.RequirePermission(model.PermissionFeishuAttendancePunchRead), userRouteLimiter, controller.QueryPunch)
	group.POST("/item/query", middleware.RequirePermission(model.PermissionFeishuAttendanceItemRead), userRouteLimiter, controller.QueryItem)
}
