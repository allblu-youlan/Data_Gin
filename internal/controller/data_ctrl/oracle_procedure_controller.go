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

func (ctrl *ExcelMatchJobController) ListOracleProcedures(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("存储过程目录数量须为 1–100"))
		return
	}
	items, err := data_svc.ListOracleOperationProcedures(c.Request.Context(), c.Query("owner"), c.Query("search"), limit)
	if err != nil {
		zap.L().Error("Oracle operation procedure catalog failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("读取存储过程目录失败，请检查 Oracle 环境或查询条件"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponse("查询存储过程目录成功", &map[string]any{"items": items}))
}

func (ctrl *ExcelMatchJobController) GetOracleProcedureSignature(c *gin.Context) {
	items, err := data_svc.GetOracleOperationProcedureSignature(c.Request.Context(), reportoracle.ProcedureRef{
		Owner: c.Query("owner"), Package: c.Query("package"), Name: c.Query("name"),
	})
	if err != nil {
		zap.L().Error("Oracle operation procedure signature failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("读取存储过程参数失败，请检查过程名称及访问权限"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponse("查询存储过程参数成功", &map[string]any{"items": items}))
}
