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

type OpenBojunOrderDetailQueryService interface {
	Query(context.Context, uint, requestbody.OpenBojunOrderDetailQueryRequest) (*data_svc.OpenBojunOrderDetailQueryResult, error)
}

type OpenBojunOrderDetailController struct {
	service OpenBojunOrderDetailQueryService
}

func NewOpenBojunOrderDetailController() *OpenBojunOrderDetailController {
	return NewOpenBojunOrderDetailControllerWithService(data_svc.NewOpenBojunOrderDetailQueryService())
}

func NewOpenBojunOrderDetailControllerWithService(
	service OpenBojunOrderDetailQueryService,
) *OpenBojunOrderDetailController {
	if service == nil {
		panic("open bojun order detail controller: nil service")
	}
	return &OpenBojunOrderDetailController{service: service}
}

func (controller *OpenBojunOrderDetailController) Query(c *gin.Context) {
	if len(c.Request.URL.Query()) > 0 {
		writeOpenBojunOrderDetailError(c, data_svc.ErrOpenBojunOrderInvalidQuery)
		return
	}
	var request requestbody.OpenBojunOrderDetailQueryRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeOpenBojunOrderDetailError(c, data_svc.ErrOpenBojunOrderInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), request)
	if err != nil {
		writeOpenBojunOrderDetailError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBojunOrderDetailError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenBojunOrderForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询伯俊订单明细")
	case errors.Is(err, data_svc.ErrOpenBojunOrderNotFound):
		responses.New(c).ToSafeErrorResponse(errcode.NotFound, "伯俊订单不存在")
	case errors.Is(err, data_svc.ErrOpenBojunOrderInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "伯俊订单明细查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "伯俊订单明细查询服务暂时不可用")
	}
}
