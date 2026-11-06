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

type OpenTmallQueryService interface {
	Query(context.Context, uint, reportoracle.TmallDataset, requestbody.OpenTmallQueryRequest) (*data_svc.OpenTmallQueryDTO, error)
}

type OpenTmallController struct {
	service OpenTmallQueryService
}

func NewOpenTmallController() *OpenTmallController {
	return NewOpenTmallControllerWithService(data_svc.NewOpenTmallQueryService())
}

func NewOpenTmallControllerWithService(service OpenTmallQueryService) *OpenTmallController {
	if service == nil {
		panic("open tmall controller: nil service")
	}
	return &OpenTmallController{service: service}
}

func (controller *OpenTmallController) QuerySycm(c *gin.Context) {
	controller.query(c, reportoracle.TmallSycm)
}

func (controller *OpenTmallController) QueryQztg(c *gin.Context) {
	controller.query(c, reportoracle.TmallQztg)
}

func (controller *OpenTmallController) QueryTot(c *gin.Context) {
	controller.query(c, reportoracle.TmallTot)
}

// QueryQztgHourly 天猫全站推广数据-小时维度查询。
func (controller *OpenTmallController) QueryQztgHourly(c *gin.Context) {
	controller.queryHourly(c, reportoracle.TmallQztgHourly)
}

// QuerySycmHourly 天猫生意参谋商品ID数据-小时维度查询。
func (controller *OpenTmallController) QuerySycmHourly(c *gin.Context) {
	controller.queryHourly(c, reportoracle.TmallSycmHourly)
}

func (controller *OpenTmallController) queryHourly(c *gin.Context, dataset reportoracle.TmallDataset) {
	if c.ContentType() != "application/json" || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenTmallError(c, data_svc.ErrOpenTmallInvalidQuery)
		return
	}
	var request *requestbody.OpenTmallHourlyQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenTmallError(c, data_svc.ErrOpenTmallInvalidQuery)
		return
	}
	controller.executeQuery(c, dataset, requestbody.OpenTmallQueryRequest{
		BatchNo: request.BatchNo, Filters: request.Filters, Page: request.Page, PageSize: request.PageSize,
	})
}

func (controller *OpenTmallController) query(c *gin.Context, dataset reportoracle.TmallDataset) {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenTmallError(c, data_svc.ErrOpenTmallInvalidQuery)
		return
	}
	var request *requestbody.OpenTmallQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenTmallError(c, data_svc.ErrOpenTmallInvalidQuery)
		return
	}
	controller.executeQuery(c, dataset, *request)
}

func (controller *OpenTmallController) executeQuery(c *gin.Context, dataset reportoracle.TmallDataset, request requestbody.OpenTmallQueryRequest) {
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), dataset, request)
	if err != nil {
		writeOpenTmallError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenTmallError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenTmallForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询天猫数据")
	case errors.Is(err, data_svc.ErrOpenTmallInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "天猫查询参数校验失败")
	case errors.Is(err, data_svc.ErrOpenTmallUnavailable):
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "天猫查询服务暂时不可用")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "天猫查询服务暂时不可用")
	}
}
