package data_ctrl

import (
	"context"
	"errors"
	"net/http"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

type OpenFeishuAttendanceQueryService interface {
	Query(context.Context, uint, reportoracle.FeishuAttendanceDataset, requestbody.OpenFeishuAttendanceQueryRequest) (*data_svc.OpenFeishuAttendanceQueryDTO, error)
}

type OpenFeishuAttendanceController struct {
	service OpenFeishuAttendanceQueryService
}

func NewOpenFeishuAttendanceController() *OpenFeishuAttendanceController {
	return NewOpenFeishuAttendanceControllerWithService(data_svc.NewOpenFeishuAttendanceQueryService())
}

func NewOpenFeishuAttendanceControllerWithService(service OpenFeishuAttendanceQueryService) *OpenFeishuAttendanceController {
	if service == nil {
		panic("open feishu attendance controller: nil service")
	}
	return &OpenFeishuAttendanceController{service: service}
}

func (controller *OpenFeishuAttendanceController) QueryDaily(c *gin.Context) {
	controller.query(c, reportoracle.FeishuAttendanceDaily)
}

func (controller *OpenFeishuAttendanceController) QueryPunch(c *gin.Context) {
	controller.query(c, reportoracle.FeishuAttendancePunch)
}

func (controller *OpenFeishuAttendanceController) QueryItem(c *gin.Context) {
	controller.query(c, reportoracle.FeishuAttendanceItem)
}

func (controller *OpenFeishuAttendanceController) query(c *gin.Context, dataset reportoracle.FeishuAttendanceDataset) {
	if c.ContentType() != "application/json" || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenFeishuAttendanceError(c, data_svc.ErrOpenFeishuAttendanceInvalidQuery)
		return
	}
	var request *requestbody.OpenFeishuAttendanceQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenFeishuAttendanceError(c, data_svc.ErrOpenFeishuAttendanceInvalidQuery)
		return
	}
	controller.executeQuery(c, dataset, *request)
}

func (controller *OpenFeishuAttendanceController) executeQuery(c *gin.Context, dataset reportoracle.FeishuAttendanceDataset, request requestbody.OpenFeishuAttendanceQueryRequest) {
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), dataset, request)
	if err != nil {
		writeOpenFeishuAttendanceError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenFeishuAttendanceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenFeishuAttendanceForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询工时数据")
	case errors.Is(err, data_svc.ErrOpenFeishuAttendanceInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "工时查询参数校验失败")
	case errors.Is(err, data_svc.ErrOpenFeishuAttendanceUnavailable):
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "工时查询服务暂时不可用")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "工时查询服务暂时不可用")
	}
}
