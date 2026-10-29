package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

func registerKaiyuPassengerRoutes(api *gin.RouterGroup, controller *data_ctrl.KaiyuPassengerController) {
	group := api.Group("/v1/kaiyu-passenger")
	group.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionDataRead))
	group.GET("", middleware.LimitRoute("60-M"), controller.Query)
	group.GET("/malls", middleware.LimitRoute("60-M"), controller.ListMalls)
	group.POST("/export", middleware.LimitRoute("10-M"), controller.Export)
}
