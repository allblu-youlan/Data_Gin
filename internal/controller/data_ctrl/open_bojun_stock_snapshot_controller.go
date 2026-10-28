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

type OpenBojunStockSnapshotQueryService interface {
	Query(context.Context, uint, requestbody.OpenBojunStockSnapshotQueryRequest) (*data_svc.OpenBojunStockSnapshotQueryDTO, error)
}

type OpenBojunStockSnapshotController struct {
	service OpenBojunStockSnapshotQueryService
}

func NewOpenBojunStockSnapshotControllerWithService(service OpenBojunStockSnapshotQueryService) *OpenBojunStockSnapshotController {
	if service == nil {
		panic("open bojun stock snapshot controller: nil service")
	}
	return &OpenBojunStockSnapshotController{service: service}
}

func (controller *OpenBojunStockSnapshotController) Query(c *gin.Context) {
	defer logOpenBojunQuery(c)()
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeOpenBojunStockSnapshotError(c, data_svc.ErrOpenBojunStockSnapshotInvalidQuery)
		return
	}
	var request *requestbody.OpenBojunStockSnapshotQueryRequest
	if err := decodeMallJSON(c, &request); err != nil || request == nil {
		writeOpenBojunStockSnapshotError(c, data_svc.ErrOpenBojunStockSnapshotInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), *request)
	if err != nil {
		writeOpenBojunStockSnapshotError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBojunStockSnapshotError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenBojunStockSnapshotForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询伯俊库存快照数据")
	case errors.Is(err, data_svc.ErrOpenBojunStockSnapshotInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "伯俊库存快照查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "伯俊库存快照查询服务暂时不可用")
	}
}
