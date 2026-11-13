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

type OpenYouzanQueryService interface {
	Query(context.Context, uint, reportoracle.YouzanDataset, requestbody.OpenYouzanQueryRequest) (*data_svc.OpenYouzanQueryDTO, error)
}

type OpenYouzanController struct {
	service OpenYouzanQueryService
}

func NewOpenYouzanController() *OpenYouzanController {
	return NewOpenYouzanControllerWithService(data_svc.NewOpenYouzanQueryService())
}

func NewOpenYouzanControllerWithService(service OpenYouzanQueryService) *OpenYouzanController {
	if service == nil {
		panic("open youzan controller: nil service")
	}
	return &OpenYouzanController{service: service}
}

func (controller *OpenYouzanController) QueryOrders(c *gin.Context) {
	controller.query(c, reportoracle.YouzanOrders)
}

func (controller *OpenYouzanController) QueryOrderItems(c *gin.Context) {
	controller.query(c, reportoracle.YouzanOrderItems)
}

func (controller *OpenYouzanController) query(c *gin.Context, dataset reportoracle.YouzanDataset) {
	if c.ContentType() != "application/json" || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenYouzanError(c, data_svc.ErrOpenYouzanInvalidQuery)
		return
	}
	var request *requestbody.OpenYouzanQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenYouzanError(c, data_svc.ErrOpenYouzanInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), dataset, *request)
	if err != nil {
		writeOpenYouzanError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenYouzanError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenYouzanForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询有赞订单数据")
	case errors.Is(err, data_svc.ErrOpenYouzanInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "有赞订单查询参数校验失败")
	case errors.Is(err, data_svc.ErrOpenYouzanUnavailable):
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "有赞订单查询服务暂时不可用")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "有赞订单查询服务暂时不可用")
	}
}
