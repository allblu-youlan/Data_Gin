package data_ctrl

import (
	"context"
	"errors"
	"net/http"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

type OpenBojunSyncStatusQueryService interface {
	Query(context.Context, uint) (*data_svc.OpenBojunSyncStatusResult, error)
}

type OpenBojunSyncStatusController struct {
	service OpenBojunSyncStatusQueryService
}

func NewOpenBojunSyncStatusController() *OpenBojunSyncStatusController {
	return NewOpenBojunSyncStatusControllerWithService(data_svc.NewOpenBojunSyncStatusService())
}

func NewOpenBojunSyncStatusControllerWithService(service OpenBojunSyncStatusQueryService) *OpenBojunSyncStatusController {
	if service == nil {
		panic("open bojun sync status controller: nil service")
	}
	return &OpenBojunSyncStatusController{service: service}
}

func (controller *OpenBojunSyncStatusController) Query(c *gin.Context) {
	if len(c.Request.URL.Query()) > 0 {
		writeOpenBojunSyncStatusError(c, data_svc.ErrOpenBojunOrderInvalidQuery)
		return
	}
	var request struct{}
	if err := decodeMallJSON(c, &request); err != nil {
		writeOpenBojunSyncStatusError(c, data_svc.ErrOpenBojunOrderInvalidQuery)
		return
	}
	result, err := controller.service.Query(c.Request.Context(), auth.CurrentUserID(c))
	if err != nil {
		writeOpenBojunSyncStatusError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeOpenBojunSyncStatusError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, data_svc.ErrOpenBojunOrderForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查询伯俊同步水位")
	case errors.Is(err, data_svc.ErrOpenBojunOrderInvalidQuery):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "伯俊同步水位查询参数校验失败")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "伯俊同步水位查询服务暂时不可用")
	}
}
