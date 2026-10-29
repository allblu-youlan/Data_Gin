package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"
	"github.com/gin-gonic/gin"
)

func registerTableEmailRoutes(api *gin.RouterGroup) {
	controller := data_ctrl.NewTableEmailController()
	group := api.Group("/v1/table-email-notifications")
	// Oracle table access requires the same trusted report administrator role as
	// datasource configuration, in addition to office message permissions.
	group.Use(middleware.AuthJWT(), middleware.RequirePermission(model.PermissionReportManage), middleware.RequirePermission(model.PermissionOfficeMessageRead))
	group.GET("", controller.List)
	group.GET("/datasources", controller.Datasources)
	group.GET("/tables", controller.Tables)
	group.GET("/columns", controller.Columns)
	group.GET("/deliveries", controller.Deliveries)
	group.POST("", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.Create)
	group.PUT("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.Update)
	group.DELETE("/:id", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.Delete)
	group.POST("/preview", middleware.RequirePermission(model.PermissionOfficeMessageManage), controller.Preview)
	group.POST("/deliveries/:id/retry", middleware.RequirePermission(model.PermissionOfficeMessagePush), controller.Retry)
}
