package data_ctrl

import (
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/internal/msg"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/auth"
	"github.com/gin-gonic/gin"
)

func authorizeExcelWorkflowWrite(c *gin.Context, config data_svc.ExcelWorkflowConfig, confirmed bool, checker middleware.PermissionChecker) bool {
	for _, step := range config.Steps {
		if step.Operation != "import_update" {
			continue
		}
		if !confirmed {
			c.JSON(400, msg.ErrResponseStr("组合操作包含 Excel 写入，请确认实际写入后再执行"))
			return false
		}
		allowed, err := checker.HasPermission(c.Request.Context(), auth.CurrentUser(c), model.PermissionExcelManage)
		if err != nil {
			c.JSON(503, msg.ErrResponseStr("暂时无法核对 Excel 写入权限，请稍后重试"))
			return false
		}
		if !allowed {
			c.JSON(403, msg.ErrResponseStr("Excel 写入需要 Excel 管理权限"))
			return false
		}
		break
	}
	return true
}
