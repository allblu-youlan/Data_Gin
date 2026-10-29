package data_ctrl

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type KaiyuPassengerQueryService interface {
	Query(context.Context, uint, data_svc.KaiyuPassengerQuery) (*data_svc.KaiyuPassengerResult, error)
	ListMalls(context.Context, uint) (*data_svc.KaiyuPassengerMallListResult, error)
	Export(context.Context, uint, data_svc.KaiyuPassengerQuery) (*data_svc.KaiyuPassengerExport, error)
}

type KaiyuPassengerController struct{ service KaiyuPassengerQueryService }

func NewKaiyuPassengerController() *KaiyuPassengerController {
	return NewKaiyuPassengerControllerWithService(data_svc.NewKaiyuPassengerQueryService())
}

func NewKaiyuPassengerControllerWithService(service KaiyuPassengerQueryService) *KaiyuPassengerController {
	if service == nil {
		panic("kaiyu passenger controller: nil service")
	}
	return &KaiyuPassengerController{service: service}
}

func (controller *KaiyuPassengerController) Query(c *gin.Context) {
	values := c.Request.URL.Query()
	if len(values["startDate"]) != 1 || len(values["endDate"]) != 1 {
		writeKaiyuPassengerError(c, data_svc.ErrKaiyuPassengerInvalid)
		return
	}
	for key, value := range values {
		if (key != "startDate" && key != "endDate" && key != "mallId") || len(value) != 1 {
			writeKaiyuPassengerError(c, data_svc.ErrKaiyuPassengerInvalid)
			return
		}
	}
	input := data_svc.KaiyuPassengerQuery{StartDate: values.Get("startDate"), EndDate: values.Get("endDate")}
	if raw, exists := values["mallId"]; exists {
		id, err := strconv.ParseUint(raw[0], 10, strconv.IntSize)
		if err != nil {
			writeKaiyuPassengerError(c, data_svc.ErrKaiyuPassengerInvalid)
			return
		}
		input.MallID = uint(id)
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), input)
	if err != nil {
		writeKaiyuPassengerError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *KaiyuPassengerController) ListMalls(c *gin.Context) {
	if len(c.Request.URL.Query()) != 0 {
		writeKaiyuPassengerError(c, data_svc.ErrKaiyuPassengerInvalid)
		return
	}
	result, err := controller.service.ListMalls(c.Request.Context(), auth.CurrentUserID(c))
	if err != nil {
		writeKaiyuPassengerError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *KaiyuPassengerController) Export(c *gin.Context) {
	var input data_svc.KaiyuPassengerQuery
	if len(c.Request.URL.Query()) != 0 {
		writeKaiyuPassengerError(c, data_svc.ErrKaiyuPassengerInvalid)
		return
	}
	if err := decodeMallJSON(c, &input); err != nil {
		writeKaiyuPassengerError(c, data_svc.ErrKaiyuPassengerInvalid)
		return
	}
	result, err := controller.service.Export(c.Request.Context(), auth.CurrentUserID(c), input)
	if err != nil {
		writeKaiyuPassengerError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+result.FileName+`"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", result.Content)
}

func writeKaiyuPassengerError(c *gin.Context, err error) {
	response := responses.New(c)
	switch {
	case errors.Is(err, auth_svc.ErrMallScopeForbidden):
		response.ToSafeErrorResponse(errcode.Forbidden, "无权访问所选门店的客流数据")
	case errors.Is(err, reportoracle.ErrKaiyuPassengerTooManyRows):
		response.ToSafeErrorResponse(errcode.UnprocessableEntity, "客流记录超过100000行，请缩短日期范围或选择单个门店")
	case errors.Is(err, data_svc.ErrKaiyuPassengerInvalid):
		response.ToSafeErrorResponse(errcode.UnprocessableEntity, "请提供有效的起止日期和门店，日期范围最多31天")
	default:
		zap.L().Error("Kaiyu passenger request failed", zap.Error(err))
		response.ToSafeErrorResponse(errcode.ServiceUnavailable, "客流数据服务暂时不可用，请稍后重试")
	}
}
