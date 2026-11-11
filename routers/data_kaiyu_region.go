package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"
	"github.com/gin-gonic/gin"
)

func registerKaiyuRegionRoutes(api *gin.RouterGroup, controller *data_ctrl.KaiyuRegionController) {
	group := api.Group("/v1/kaiyu-region-stay")
	group.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionDataRead))
	group.GET("", middleware.LimitRoute("60-M"), controller.Query)
	group.GET("/missing", middleware.LimitRoute("60-M"), controller.Missing)
	group.POST("/backfill", middleware.RequirePermission(model.PermissionDataManage), middleware.LimitRoute("120-M"), controller.Backfill)
	group.POST("/export", middleware.LimitRoute("10-M"), controller.Export)
}
