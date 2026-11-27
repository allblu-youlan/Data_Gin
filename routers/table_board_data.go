package routers

import (
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/middleware"
	"github.com/gin-gonic/gin"
)

func registerTableBoardDataRoutes(group *gin.RouterGroup) {
	group.POST("/:id/evaluate", middleware.LimitRoute("60-M"), data_ctrl.EvaluateTableBoard)
	group.POST("/:id/export", middleware.LimitRoute("30-M"), data_ctrl.ExportBoardCalculation)
	group.POST("/preview", middleware.LimitRoute("120-M"), data_ctrl.PreviewTableBoard)
	group.POST("/dimension-values", middleware.LimitRoute("120-M"), data_ctrl.TableBoardDimensionValues)
	group.POST("/query", middleware.LimitRoute("120-M"), data_ctrl.QueryTableBoardSource)
	group.POST("/xlsx", middleware.LimitRoute("30-M"), data_ctrl.ExportTableBoardXLSX)
}
