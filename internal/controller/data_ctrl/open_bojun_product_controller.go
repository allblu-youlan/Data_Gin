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

type OpenBojunProductQueryService interface {
	Query(context.Context, uint, requestbody.OpenBojunProductQueryRequest) (*data_svc.OpenBojunProductDTO, error)
}

type OpenBojunProductController struct {
	service OpenBojunProductQueryService
}

func NewOpenBojunProductController() *OpenBojunProductController {
	return NewOpenBojunProductControllerWithService(data_svc.NewOpenBojunProductQueryService())
}

func NewOpenBojunProductControllerWithService(service OpenBojunProductQueryService) *OpenBojunProductController {
	if service == nil {
		panic("open bojun product controller: nil service")
	}
	return &OpenBojunProductController{service: service}
}

func (controller *OpenBojunProductController) Query(c *gin.Context) {
	if len(c.Request.URL.Query()) > 0 {
		writeOpenBojunProductError(c, data_svc.ErrOpenBojunProductInvalidQuery)
		return
	}
	var request requestbody.OpenBojunProductQueryRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeOpenBojunProductError(c, data_svc.ErrOpenBojunProductInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), request)
	if err != nil {
		writeOpenBojunProductError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBojunProductError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenBojunProductForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询伯俊商品数据")
	case errors.Is(err, data_svc.ErrOpenBojunProductInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "伯俊商品查询参数校验失败")
	case errors.Is(err, data_svc.ErrOpenBojunProductNotFound):
		responses.New(c).ToSafeErrorResponse(errcode.NotFound, "伯俊商品不存在")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "伯俊商品查询服务暂时不可用")
	}
}
