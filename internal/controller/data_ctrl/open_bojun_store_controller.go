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

type OpenBojunStoreQueryService interface {
	Query(context.Context, uint, requestbody.OpenBojunStoreQueryRequest) (*data_svc.OpenBojunStoreQueryDTO, error)
}

type OpenBojunStoreController struct {
	service OpenBojunStoreQueryService
}

func NewOpenBojunStoreControllerWithService(service OpenBojunStoreQueryService) *OpenBojunStoreController {
	if service == nil {
		panic("open bojun store controller: nil service")
	}
	return &OpenBojunStoreController{service: service}
}

func (controller *OpenBojunStoreController) Query(c *gin.Context) {
	defer logOpenBojunQuery(c)()
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenBojunStoreError(c, data_svc.ErrOpenBojunStoreInvalidQuery)
		return
	}
	var request *requestbody.OpenBojunStoreQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenBojunStoreError(c, data_svc.ErrOpenBojunStoreInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), *request)
	if err != nil {
		writeOpenBojunStoreError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBojunStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenBojunStoreForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询伯俊店仓数据")
	case errors.Is(err, data_svc.ErrOpenBojunStoreInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "伯俊店仓查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "伯俊店仓查询服务暂时不可用")
	}
}
