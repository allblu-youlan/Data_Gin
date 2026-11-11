package data_ctrl

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/providerhttp"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type KaiyuRegionQueryService interface {
	Query(context.Context, uint, data_svc.KaiyuRegionQuery) (*data_svc.KaiyuRegionResult, error)
	Missing(context.Context, uint, data_svc.KaiyuRegionQuery) (*data_svc.KaiyuRegionMissingResult, error)
	Backfill(context.Context, uint, data_svc.KaiyuRegionBackfillRequest) (*data_svc.KaiyuRegionBackfillResult, error)
	Export(context.Context, uint, data_svc.KaiyuRegionQuery) (*data_svc.KaiyuPassengerExport, error)
}
type KaiyuRegionController struct{ service KaiyuRegionQueryService }

func NewKaiyuRegionController() *KaiyuRegionController {
	return NewKaiyuRegionControllerWithService(data_svc.NewKaiyuRegionService())
}
func NewKaiyuRegionControllerWithService(service KaiyuRegionQueryService) *KaiyuRegionController {
	if service == nil {
		panic("kaiyu region controller: nil service")
	}
	return &KaiyuRegionController{service: service}
}
func (controller *KaiyuRegionController) Query(c *gin.Context)   { controller.query(c, false) }
func (controller *KaiyuRegionController) Missing(c *gin.Context) { controller.query(c, true) }
func (controller *KaiyuRegionController) query(c *gin.Context, missing bool) {
	values := c.Request.URL.Query()
	if len(values["startDate"]) != 1 || len(values["endDate"]) != 1 {
		writeKaiyuRegionError(c, data_svc.ErrKaiyuRegionInvalid)
		return
	}
	for key, value := range values {
		if (key != "startDate" && key != "endDate" && key != "mallId" && key != "regionName") || len(value) != 1 {
			writeKaiyuRegionError(c, data_svc.ErrKaiyuRegionInvalid)
			return
		}
	}
	input := data_svc.KaiyuRegionQuery{StartDate: values.Get("startDate"), EndDate: values.Get("endDate"), RegionName: values.Get("regionName")}
	if raw, exists := values["mallId"]; exists {
		id, err := strconv.ParseUint(raw[0], 10, strconv.IntSize)
		if err != nil {
			writeKaiyuRegionError(c, data_svc.ErrKaiyuRegionInvalid)
			return
		}
		input.MallID = uint(id)
	}
	var result any
	var err error
	if missing {
		result, err = controller.service.Missing(c.Request.Context(), auth.CurrentUserID(c), input)
	} else {
		result, err = controller.service.Query(c.Request.Context(), auth.CurrentUserID(c), input)
	}
	if err != nil {
		writeKaiyuRegionError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}
func (controller *KaiyuRegionController) Backfill(c *gin.Context) {
	var input data_svc.KaiyuRegionBackfillRequest
	if len(c.Request.URL.Query()) != 0 || decodeMallJSON(c, &input) != nil {
		writeKaiyuRegionError(c, data_svc.ErrKaiyuRegionInvalid)
		return
	}
	result, err := controller.service.Backfill(c.Request.Context(), auth.CurrentUserID(c), input)
	if err != nil {
		writeKaiyuRegionError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}
func (controller *KaiyuRegionController) Export(c *gin.Context) {
	var input data_svc.KaiyuRegionQuery
	if len(c.Request.URL.Query()) != 0 || decodeMallJSON(c, &input) != nil {
		writeKaiyuRegionError(c, data_svc.ErrKaiyuRegionInvalid)
		return
	}
	result, err := controller.service.Export(c.Request.Context(), auth.CurrentUserID(c), input)
	if err != nil {
		writeKaiyuRegionError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+result.FileName+`"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", result.Content)
}
func writeKaiyuRegionError(c *gin.Context, err error) {
	response := responses.New(c)
	switch {
	case errors.Is(err, data_svc.ErrKaiyuRegionBindingConflict):
		response.ToSafeErrorResponse(errcode.Conflict, "所选门店存在重复的开域店铺 ID，请先更正绑定")
	case errors.Is(err, data_svc.ErrKaiyuRegionBusy):
		response.ToSafeErrorResponse(errcode.Conflict, "区域数据同步正在执行，请稍后重试")
	case errors.Is(err, auth_svc.ErrMallScopeForbidden):
		response.ToSafeErrorResponse(errcode.Forbidden, "无权访问所选门店的区域数据")
	case errors.Is(err, reportoracle.ErrKaiyuRegionTooManyRows):
		response.ToSafeErrorResponse(errcode.UnprocessableEntity, "区域记录超过100000行，请缩短日期范围或选择单个门店")
	case errors.Is(err, data_svc.ErrKaiyuRegionInvalid):
		response.ToSafeErrorResponse(errcode.UnprocessableEntity, "请提供有效的起止日期、门店和游标，日期范围最多31天")
	default:
		zap.L().Error("Kaiyu region request failed", zap.String("error", providerhttp.RedactText(err.Error())))
		response.ToSafeErrorResponse(errcode.ServiceUnavailable, "区域数据服务暂时不可用，请稍后重试")
	}
}
