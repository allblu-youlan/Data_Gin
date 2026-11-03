package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/model"

	"github.com/gin-gonic/gin"
)

func registerSalesSummaryRoutes(api *gin.RouterGroup, controller *data_ctrl.SalesSummaryController) {
	api.GET("/v1/sales-summary",
		middleware.AuthJWT(),
		middleware.RequirePermission(model.PermissionBusinessOverviewRead),
		middleware.LimitRoute("120-M"),
		controller.Query,
	)
}
