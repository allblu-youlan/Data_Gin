package data_ctrl

import (
	"errors"
	"net/http"

	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/internal/tableboard"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
)

func QueryTableBoardSource(c *gin.Context) {
	var source tableboard.Source
	if err := decodeMallJSON(c, &source); err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "数据源配置格式不正确")
		return
	}
	rows, err := data_svc.QueryBoardSource(c.Request.Context(), source)
	if err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
		switch {
		case errors.Is(err, data_svc.ErrBoardSourceInvalid):
			responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, err.Error())
		case errors.Is(err, reportrepo.ErrReportRunBusy):
			c.Header("Retry-After", "2")
			responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "当前连接正在执行查询，请稍后刷新")
		default:
			responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "读取失败，请检查数据源、字段和连接状态后重试")
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponse(gin.H{"rows": rows})
}

func ExportTableBoardXLSX(c *gin.Context) {
	var input data_svc.BoardExport
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	if err := c.ShouldBindJSON(&input); err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, "导出内容格式无效或超过 16 MiB")
		return
	}
	if err := input.Validate(); err != nil {
		responses.New(c).ToSafeErrorResponse(errcode.UnprocessableEntity, err.Error())
		return
	}
	data, err := data_svc.RenderBoardXLSX(input)
	if err != nil {
		_ = c.Error(err)
		responses.New(c).ToSafeErrorResponse(errcode.InternalServerError, "Excel 文件生成失败")
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponse(gin.H{"content": data})
}
