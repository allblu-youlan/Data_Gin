package data_ctrl

import (
	"net/http"
	"strconv"

	"gin-biz-web-api/internal/msg"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (ctrl *ExcelMatchJobController) ListOracleTables(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("表目录数量须为 1–100"))
		return
	}
	tables, err := data_svc.NewOfficeOracleMetadataService().ListResultTables(c.Request.Context(), c.Query("owner"), c.Query("search"), limit)
	if err != nil {
		zap.L().Error("Oracle Excel table catalog failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("读取 Oracle 表目录失败，请检查 Oracle 环境或查询条件"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponse("查询 Oracle 表目录成功", &map[string]any{"tables": tables}))
}

func (ctrl *ExcelMatchJobController) ListOracleColumns(c *gin.Context) {
	columns, err := data_svc.NewOfficeOracleMetadataService().ResultTableSchema(c.Request.Context(), reportoracle.ResultTableRef{Owner: c.Query("owner"), Name: c.Query("name")})
	if err != nil {
		zap.L().Error("Oracle Excel column catalog failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("读取 Oracle 字段失败，请检查表名及访问权限"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponse("查询 Oracle 字段成功", &map[string]any{"columns": columns}))
}

func (ctrl *ExcelMatchJobController) CreateOracleExport(c *gin.Context) {
	var input data_svc.OracleExcelExportConfig
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("Oracle 导出参数格式无效"))
		return
	}
	export, err := ctrl.service.CreateOracleExportJob(c.Request.Context(), input)
	if err != nil {
		zap.L().Error("Oracle Excel export creation failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("创建 Oracle 导出任务失败，请检查表字段、筛选值及服务配置"))
		return
	}
	logs, _ := ctrl.service.GetJobLogs(c.Request.Context(), export.ID)
	c.JSON(http.StatusOK, msg.SuccessResponse("Oracle Excel 导出任务已创建", &map[string]any{
		"job": safeExcelMatchJob(*export), "logs": safeExcelMatchJobLogs(logs),
		"downloadPath": "/api/v1/excel-match-jobs/" + strconv.FormatUint(uint64(export.ID), 10) + "/download",
	}))
}
