package data_ctrl

import (
	"context"
	"errors"
	"net/http"

	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

type OpenMonitoringQueryService interface {
	QueryHourly(context.Context, uint, requestbody.OpenMonitoringHourlyQueryRequest) (*data_svc.OpenMonitoringHourlyResult, error)
	QueryDaily(context.Context, uint, requestbody.OpenMonitoringHourlyQueryRequest) (*data_svc.OpenMonitoringDailyResult, error)
	QueryAreas(context.Context, uint, requestbody.OpenMonitoringAreaQueryRequest) (*data_svc.OpenMonitoringAreaResult, error)
}

type OpenMonitoringController struct {
	service OpenMonitoringQueryService
}

func NewOpenMonitoringController() *OpenMonitoringController {
	return NewOpenMonitoringControllerWithService(data_svc.NewOpenMonitoringQueryService())
}

func NewOpenMonitoringControllerWithService(service OpenMonitoringQueryService) *OpenMonitoringController {
	if service == nil {
		panic("open monitoring controller: nil service")
	}
	return &OpenMonitoringController{service: service}
}

func (controller *OpenMonitoringController) QueryHourly(c *gin.Context) {
	var request *requestbody.OpenMonitoringHourlyQueryRequest
	if err := decodeOpenMonitoringJSON(c, &request); err != nil || request == nil {
		writeOpenMonitoringError(c, data_svc.ErrOpenMonitoringInvalidQuery)
		return
	}
	result, err := controller.service.QueryHourly(c.Request.Context(), auth.CurrentUserID(c), *request)
	if err != nil {
		writeOpenMonitoringError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *OpenMonitoringController) QueryDaily(c *gin.Context) {
	var request *requestbody.OpenMonitoringHourlyQueryRequest
	if err := decodeOpenMonitoringJSON(c, &request); err != nil || request == nil {
		writeOpenMonitoringError(c, data_svc.ErrOpenMonitoringInvalidQuery)
		return
	}
	result, err := controller.service.QueryDaily(c.Request.Context(), auth.CurrentUserID(c), *request)
	if err != nil {
		writeOpenMonitoringError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *OpenMonitoringController) QueryAreas(c *gin.Context) {
	var request *requestbody.OpenMonitoringAreaQueryRequest
	if err := decodeOpenMonitoringJSON(c, &request); err != nil || request == nil {
		writeOpenMonitoringError(c, data_svc.ErrOpenMonitoringInvalidQuery)
		return
	}
	result, err := controller.service.QueryAreas(c.Request.Context(), auth.CurrentUserID(c), *request)
	if err != nil {
		writeOpenMonitoringError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func decodeOpenMonitoringJSON(c *gin.Context, destination any) error {
	if c.ContentType() != "application/json" || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return data_svc.ErrOpenMonitoringInvalidQuery
	}
	return decodeMallJSON(c, destination)
}

func writeOpenMonitoringError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenMonitoringForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询监控数据")
	case errors.Is(err, data_svc.ErrOpenMonitoringInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "监控查询参数校验失败")
	case errors.Is(err, data_svc.ErrOpenMonitoringUnavailable):
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "监控查询服务暂时不可用")
	case errors.Is(err, data_svc.ErrOpenMonitoringNotFound):
		responses.New(c).ToSafeErrorResponse(errcode.NotFound, "监控查询数据不存在")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "监控查询服务暂时不可用")
	}
}
