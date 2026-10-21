package data_ctrl

import (
	"net/http"

	"gin-biz-web-api/internal/msg"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (ctrl *ExcelMatchJobController) ListOracleExportSchemes(c *gin.Context) {
	schemes, err := data_svc.NewOracleExcelExportSchemeService().List(c.Request.Context())
	if err != nil {
		zap.L().Error("Oracle export configurations list failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, msg.ErrResponseStr("读取导出配置失败，请稍后重试"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponse("查询导出配置成功", &map[string]any{"schemes": schemes}))
}

func (ctrl *ExcelMatchJobController) SaveOracleExportScheme(c *gin.Context) {
	var input struct {
		Name   string                           `json:"name"`
		Config data_svc.OracleExcelExportConfig `json:"config"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("导出配置参数格式无效"))
		return
	}
	scheme, err := data_svc.NewOracleExcelExportSchemeService().Save(c.Request.Context(), input.Name, input.Config)
	if err != nil {
		zap.L().Error("Oracle export configuration save failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("保存导出配置失败，请检查名称、表字段及筛选条件"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponse("导出配置已保存", &map[string]any{"scheme": scheme}))
}

func (ctrl *ExcelMatchJobController) DeleteOracleExportScheme(c *gin.Context) {
	id, err := parseUintParam(c, "scheme_id")
	if err != nil {
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("导出配置 ID 无效"))
		return
	}
	if err := data_svc.NewOracleExcelExportSchemeService().Delete(c.Request.Context(), id); err != nil {
		zap.L().Error("Oracle export configuration delete failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, msg.ErrResponseStr("删除导出配置失败，请稍后重试"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponseStr("导出配置已删除"))
}
