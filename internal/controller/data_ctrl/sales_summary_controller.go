package data_ctrl

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

type SalesSummaryQueryService interface {
	Query(context.Context, uint, string, string, string) (*data_svc.SalesSummaryResult, error)
}

type SalesSummaryController struct {
	service SalesSummaryQueryService
}

func NewSalesSummaryController() *SalesSummaryController {
	return &SalesSummaryController{service: data_svc.NewSalesSummaryService()}
}

func NewSalesSummaryControllerWithService(service SalesSummaryQueryService) *SalesSummaryController {
	return &SalesSummaryController{service: service}
}

func (controller *SalesSummaryController) Query(c *gin.Context) {
	query := c.Request.URL.Query()
	valid := len(query) == 3 && len(query["mallCode"]) == 1 &&
		len(query["startDate"]) == 1 && len(query["endDate"]) == 1
	if !valid {
		writeSalesSummaryError(c, data_svc.ErrSalesSummaryInvalid)
		return
	}
	result, err := controller.service.Query(
		c.Request.Context(),
		auth.CurrentUserID(c),
		strings.TrimSpace(query.Get("mallCode")),
		strings.TrimSpace(query.Get("startDate")),
		strings.TrimSpace(query.Get("endDate")),
	)
	if err != nil {
		writeSalesSummaryError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeSalesSummaryError(c *gin.Context, err error) {
	_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	switch {
	case errors.Is(err, data_svc.ErrSalesSummaryInvalid):
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "销售汇总查询参数校验失败")
	case errors.Is(err, data_svc.ErrSalesSummaryForbidden):
		responses.New(c).ToSafeErrorResponse(errcode.Forbidden, "无权查看该商场销售汇总")
	default:
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "销售汇总暂时不可用，请稍后刷新")
	}
}
