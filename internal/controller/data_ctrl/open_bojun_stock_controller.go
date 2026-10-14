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

type OpenBojunStockQueryService interface {
	Query(context.Context, uint, requestbody.OpenBojunStockQueryRequest) (*data_svc.OpenBojunStockQueryDTO, error)
}

type OpenBojunStockController struct {
	service OpenBojunStockQueryService
}

func NewOpenBojunStockControllerWithService(service OpenBojunStockQueryService) *OpenBojunStockController {
	if service == nil {
		panic("open bojun stock controller: nil service")
	}
	return &OpenBojunStockController{service: service}
}

func (controller *OpenBojunStockController) Query(c *gin.Context) {
	defer logOpenBojunQuery(c)()
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenBojunStockError(c, data_svc.ErrOpenBojunStockInvalidQuery)
		return
	}
	var request *requestbody.OpenBojunStockQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenBojunStockError(c, data_svc.ErrOpenBojunStockInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), *request)
	if err != nil {
		writeOpenBojunStockError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBojunStockError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenBojunStockForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询伯俊库存数据")
	case errors.Is(err, data_svc.ErrOpenBojunStockInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "伯俊库存查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "伯俊库存查询服务暂时不可用")
	}
}
