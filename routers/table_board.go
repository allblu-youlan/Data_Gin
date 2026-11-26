package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

func registerTableBoardRoutes(api *gin.RouterGroup) {
	group := api.Group("/v1/table-boards",
		middleware.AuthJWT(),
		middleware.RequirePermission(model.PermissionReportRead),
		middleware.RequirePermission(model.PermissionReportManage),
		middleware.RequirePermission(model.PermissionTableBoardUse),
	)
	group.GET("", data_ctrl.ListTableBoards)
	group.GET("/:id", data_ctrl.GetTableBoard)
	group.POST("", data_ctrl.CreateTableBoard)
	group.PUT("/:id", data_ctrl.UpdateTableBoard)
	group.DELETE("/:id", data_ctrl.DeleteTableBoard)
	registerTableBoardDataRoutes(group)
}
