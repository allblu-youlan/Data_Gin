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

type OpenBojunProductInfoQueryService interface {
	Query(context.Context, uint, requestbody.OpenBojunProductInfoQueryRequest) (*data_svc.OpenBojunProductInfoQueryDTO, error)
}

type OpenBojunProductInfoController struct {
	service OpenBojunProductInfoQueryService
}

func NewOpenBojunProductInfoControllerWithService(service OpenBojunProductInfoQueryService) *OpenBojunProductInfoController {
	if service == nil {
		panic("open bojun product info controller: nil service")
	}
	return &OpenBojunProductInfoController{service: service}
}

func (controller *OpenBojunProductInfoController) Query(c *gin.Context) {
	defer logOpenBojunQuery(c)()
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenBojunProductInfoError(c, data_svc.ErrOpenBojunProductInfoInvalidQuery)
		return
	}
	var request *requestbody.OpenBojunProductInfoQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenBojunProductInfoError(c, data_svc.ErrOpenBojunProductInfoInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), *request)
	if err != nil {
		writeOpenBojunProductInfoError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBojunProductInfoError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenBojunProductInfoForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询基础商品资料")
	case errors.Is(err, data_svc.ErrOpenBojunProductInfoInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "基础商品资料查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "基础商品资料查询服务暂时不可用")
	}
}
