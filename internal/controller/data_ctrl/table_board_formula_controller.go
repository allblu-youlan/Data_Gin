package data_ctrl

import (
	"errors"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/internal/tableboard"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
	"time"
)

func boardMappingError(c *gin.Context, err error) {
	_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	if errors.Is(err, data_svc.ErrBoardSourceInvalid) {
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, err.Error())
		return
	}
	responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "读取失败，请核对字段、权限和连接状态")
}
func PreviewTableBoard(c *gin.Context) {
	var input struct {
		Metric     tableboard.Metric `json:"metric"`
		Cell       tableboard.Cell   `json:"cell"`
		ReportDate string            `json:"reportDate"`
	}
	if err := decodeLimitedJSON(c, &input, 128<<10); err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "预览配置无效")
		return
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		boardMappingError(c, err)
		return
	}
	asOf := time.Now().In(location)
	before, after, err := data_svc.PreviewBoardForDate(c.Request.Context(), auth.CurrentUserID(c), input.Metric, input.Cell, asOf, input.ReportDate)
	if err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponse(gin.H{"asOf": asOf, "before": before, "after": after})
}
func TableBoardDimensionValues(c *gin.Context) {
	var input struct {
		Metric tableboard.Metric `json:"metric"`
		Field  string            `json:"field"`
		Search string            `json:"search"`
	}
	if err := decodeLimitedJSON(c, &input, 128<<10); err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "维度配置无效")
		return
	}
	values, err := data_svc.BoardDimensionValues(c.Request.Context(), auth.CurrentUserID(c), input.Metric, input.Field, input.Search)
	if err != nil {
		boardMappingError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponse(gin.H{"values": values})
}
