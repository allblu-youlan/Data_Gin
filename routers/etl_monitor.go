package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

// RegisterETLMonitorRoutes exposes cached snapshots behind existing console authorization.
func RegisterETLMonitorRoutes(api *gin.RouterGroup, controller *data_ctrl.ETLMonitorController) {
	api.GET("/v1/etl-monitor", middleware.AuthJWT(), middleware.RequirePermission(model.PermissionDataRead), controller.Snapshot)
}
