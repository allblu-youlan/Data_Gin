package data_ctrl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

type BusinessOverviewQueryService interface {
	QueryPayments(context.Context, uint, string, string) (*data_svc.BusinessOverviewPaymentResult, error)
	QueryOpenPayments(context.Context, uint, requestbody.OpenBusinessOverviewPaymentQueryRequest) (*data_svc.OpenBusinessOverviewPaymentResult, error)
	ListMalls(context.Context, uint, uint, int) (*data_svc.BusinessOverviewMallListResult, error)
}

func (controller *BusinessOverviewController) ListMalls(c *gin.Context) {
	query := c.Request.URL.Query()
	if len(query) > 2 || len(query["limit"]) > 1 || len(query["afterId"]) > 1 {
		writeBusinessOverviewMallListError(c, data_svc.ErrBusinessOverviewInvalid)
		return
	}
	for key := range query {
		if key != "limit" && key != "afterId" {
			writeBusinessOverviewMallListError(c, data_svc.ErrBusinessOverviewInvalid)
			return
		}
	}
	limit := 50
	if values, exists := query["limit"]; exists {
		parsed, err := strconv.ParseUint(strings.TrimSpace(values[0]), 10, 16)
		if err != nil || parsed < 1 || parsed > 200 {
			writeBusinessOverviewMallListError(c, data_svc.ErrBusinessOverviewInvalid)
			return
		}
		limit = int(parsed)
	}
	var afterID uint
	if values, exists := query["afterId"]; exists {
		parsed, err := strconv.ParseUint(strings.TrimSpace(values[0]), 10, strconv.IntSize)
		if err != nil || parsed < 1 {
			writeBusinessOverviewMallListError(c, data_svc.ErrBusinessOverviewInvalid)
			return
		}
		afterID = uint(parsed)
	}
	if controller.initErr != nil || controller.service == nil {
		writeBusinessOverviewMallListError(c, fmt.Errorf("%w: initialization failed", data_svc.ErrBusinessOverviewUnavailable))
		return
	}
	result, err := controller.service.ListMalls(c.Request.Context(), auth.CurrentUserID(c), afterID, limit)
	if err != nil {
		writeBusinessOverviewMallListError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

type BusinessOverviewController struct {
	service BusinessOverviewQueryService
	initErr error
}

func NewBusinessOverviewController() *BusinessOverviewController {
	service, err := data_svc.NewBusinessOverviewService()
	return &BusinessOverviewController{service: service, initErr: err}
}

func NewBusinessOverviewControllerWithService(service BusinessOverviewQueryService) *BusinessOverviewController {
	if service == nil {
		panic("business overview controller: nil service")
	}
	return &BusinessOverviewController{service: service}
}

func (controller *BusinessOverviewController) QueryPayments(c *gin.Context) {
	query := c.Request.URL.Query()
	if len(query) != 2 || len(query["date"]) != 1 || len(query["mallCode"]) != 1 {
		writeBusinessOverviewError(c, data_svc.ErrBusinessOverviewInvalid)
		return
	}
	if controller.initErr != nil || controller.service == nil {
		writeBusinessOverviewError(c, fmt.Errorf("%w: initialization failed", data_svc.ErrBusinessOverviewUnavailable))
		return
	}
	result, err := controller.service.QueryPayments(
		c.Request.Context(), auth.CurrentUserID(c), strings.TrimSpace(query.Get("date")), strings.TrimSpace(query.Get("mallCode")),
	)
	if err != nil {
		writeBusinessOverviewError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *BusinessOverviewController) OpenQueryPayments(c *gin.Context) {
	if len(c.Request.URL.Query()) > 0 {
		writeOpenBusinessOverviewError(c, data_svc.ErrBusinessOverviewInvalid)
		return
	}
	var request requestbody.OpenBusinessOverviewPaymentQueryRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeOpenBusinessOverviewError(c, data_svc.ErrBusinessOverviewInvalid)
		return
	}
	if controller.initErr != nil || controller.service == nil {
		writeOpenBusinessOverviewError(c, fmt.Errorf("%w: initialization failed", data_svc.ErrBusinessOverviewUnavailable))
		return
	}
	result, err := controller.service.QueryOpenPayments(c.Request.Context(), auth.CurrentUserID(c), request)
	if err != nil {
		writeOpenBusinessOverviewError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBusinessOverviewError(c *gin.Context, err error) {
	if c != nil && err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	}
	switch {
	case errors.Is(err, data_svc.ErrBusinessOverviewForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询该商场营业金额")
	case errors.Is(err, data_svc.ErrBusinessOverviewInvalid):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "营业金额查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "营业金额查询服务暂时不可用")
	}
}

func writeBusinessOverviewError(c *gin.Context, err error) {
	if c != nil && err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	}
	switch {
	case errors.Is(err, data_svc.ErrBusinessOverviewForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询该商场营业数据")
	case errors.Is(err, data_svc.ErrBusinessOverviewInvalid):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "营业数据查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "营业数据查询服务暂时不可用")
	}
}

func writeBusinessOverviewMallListError(c *gin.Context, err error) {
	if c != nil && err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	}
	if errors.Is(err, data_svc.ErrBusinessOverviewInvalid) {
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "商场列表查询参数校验失败")
		return
	}
	responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "商场列表服务暂时不可用")
}
