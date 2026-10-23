package data_ctrl

import (
	"encoding/json"
	"errors"
	"net/http"

	"gin-biz-web-api/internal/msg"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (ctrl *ExcelMatchJobController) ListOperationSchemes(c *gin.Context) {
	schemes, err := data_svc.NewExcelOperationSchemeService().List(c.Request.Context(), c.Param("operation"))
	if err != nil {
		c.JSON(http.StatusBadRequest, msg.ErrResponseStr("读取方法失败，请检查操作类型"))
		return
	}
	c.JSON(http.StatusOK, msg.SuccessResponse("读取方法成功", &map[string]any{"schemes": schemes}))
}

func (ctrl *ExcelMatchJobController) SaveOperationScheme(c *gin.Context) {
	var input struct {
		Name   string          `json:"name"`
		Config json.RawMessage `json:"config"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, msg.ErrResponseStr("方法配置格式无效"))
		return
	}
	scheme, err := data_svc.NewExcelOperationSchemeService().Save(c.Request.Context(), input.Name, c.Param("operation"), input.Config)
	if err != nil {
		zap.L().Error("Oracle operation scheme save failed", zap.Error(err))
		message := "保存失败，请检查名称、Oracle 表字段、过程参数和成功标志配置"
		if c.Param("operation") == data_svc.ExcelOperationOracleProcedure && errors.Is(err, reportoracle.ErrMetadataMismatch) {
			message = "当前 Oracle 连接中未找到该存储过程，请从查询结果重新选择并读取参数，核对 Oracle 用户、包名和账号访问权限"
		}
		c.JSON(400, msg.ErrResponseStr(message))
		return
	}
	c.JSON(200, msg.SuccessResponse("方法已保存", &map[string]any{"scheme": scheme}))
}

func (ctrl *ExcelMatchJobController) DeleteOperationScheme(c *gin.Context) {
	id, err := parseUintParam(c, "scheme_id")
	if err == nil {
		err = data_svc.NewExcelOperationSchemeService().Delete(c.Request.Context(), c.Param("operation"), id)
	}
	if err != nil {
		c.JSON(400, msg.ErrResponseStr("删除方法失败，请检查类型与 ID"))
		return
	}
	c.JSON(200, msg.SuccessResponseStr("方法已删除"))
}

func (ctrl *ExcelMatchJobController) ListWorkflows(c *gin.Context) {
	schemes, err := ctrl.service.ListWorkflows(c.Request.Context())
	if err != nil {
		c.JSON(500, msg.ErrResponseStr("读取组合操作失败，请稍后重试"))
		return
	}
	c.JSON(200, msg.SuccessResponse("读取组合操作成功", &map[string]any{"schemes": schemes}))
}

func (ctrl *ExcelMatchJobController) SaveWorkflow(c *gin.Context) {
	var input struct {
		Name   string                       `json:"name"`
		Config data_svc.ExcelWorkflowConfig `json:"config"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, msg.ErrResponseStr("组合操作配置格式无效"))
		return
	}
	scheme, err := ctrl.service.SaveWorkflow(c.Request.Context(), input.Name, input.Config)
	if err != nil {
		zap.L().Error("Excel workflow save failed", zap.Error(err))
		c.JSON(400, msg.ErrResponseStr("保存失败，请检查名称、步骤顺序及引用的方法"))
		return
	}
	c.JSON(200, msg.SuccessResponse("组合操作已保存", &map[string]any{"scheme": scheme}))
}

func (ctrl *ExcelMatchJobController) DeleteWorkflow(c *gin.Context) {
	id, err := parseUintParam(c, "scheme_id")
	if err == nil {
		err = ctrl.service.DeleteWorkflow(c.Request.Context(), id)
	}
	if err != nil {
		c.JSON(400, msg.ErrResponseStr("删除组合操作失败"))
		return
	}
	c.JSON(200, msg.SuccessResponseStr("组合操作已删除"))
}

func (ctrl *ExcelMatchJobController) CreateWorkflow(c *gin.Context) {
	var input struct {
		Config       data_svc.ExcelWorkflowConfig `json:"config"`
		UploadID     string                       `json:"uploadId"`
		UploadIDs    []string                     `json:"uploadIds"`
		StepUploads  map[string][]string          `json:"stepUploads"`
		ConfirmWrite bool                         `json:"confirmWrite"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, msg.ErrResponseStr("执行参数格式无效"))
		return
	}
	if !authorizeExcelWorkflowWrite(c, input.Config, input.ConfirmWrite, auth_svc.NewAuthorizer(database.DB)) {
		return
	}
	if len(input.UploadIDs) == 0 && input.UploadID != "" {
		input.UploadIDs = []string{input.UploadID}
	}
	var record *model.ExcelMatchJob
	var err error
	if input.StepUploads != nil {
		record, err = ctrl.service.CreateWorkflowJobFromStepUploads(c.Request.Context(), input.Config, input.StepUploads)
	} else {
		record, err = ctrl.service.CreateWorkflowJobFromUploads(c.Request.Context(), input.Config, input.UploadIDs)
	}
	if err != nil {
		zap.L().Error("Excel workflow creation failed", zap.Error(err))
		c.JSON(400, msg.ErrResponseStr("创建任务失败，请检查已上传文件、步骤及方法是否有效"))
		return
	}
	c.JSON(200, msg.SuccessResponse("组合操作任务已创建", &map[string]any{"job": safeExcelMatchJob(*record)}))
}
