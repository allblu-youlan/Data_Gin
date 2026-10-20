package data_ctrl

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
)

type ReportRunQueryServiceAPI interface {
	Get(context.Context, uint, uint) (*data_svc.ReportRunViewDTO, error)
	Cancel(context.Context, uint, uint) (*data_svc.ReportRunViewDTO, error)
	ReadResults(context.Context, uint, uint, string, int) (*data_svc.ReportResultPageDTO, error)
	QueryResults(context.Context, uint, uint, reportquery.Input, string, int) (*data_svc.ReportResultPageDTO, error)
}

type ReportRunController struct {
	service ReportRunQueryServiceAPI
}

func NewReportRunController() *ReportRunController {
	return NewReportRunControllerWithService(data_svc.NewReportRunQueryService())
}

func NewReportRunControllerWithService(service ReportRunQueryServiceAPI) *ReportRunController {
	if service == nil {
		panic("report run controller: nil service")
	}
	return &ReportRunController{service: service}
}

func (controller *ReportRunController) Get(c *gin.Context) {
	runID, err := parseReportUint(c.Param("id"), "run id")
	if err != nil {
		writeReportRunQueryError(c, data_svc.ErrReportRunQueryInvalid)
		return
	}
	result, err := controller.service.Get(c.Request.Context(), auth.CurrentUserID(c), runID)
	if err != nil {
		writeReportRunQueryError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportRunController) Cancel(c *gin.Context) {
	runID, err := parseReportUint(c.Param("id"), "run id")
	if err != nil {
		writeReportRunQueryError(c, data_svc.ErrReportRunQueryInvalid)
		return
	}
	result, err := controller.service.Cancel(c.Request.Context(), auth.CurrentUserID(c), runID)
	if err != nil {
		writeReportRunQueryError(c, err)
		return
	}
	status := http.StatusOK
	if result.Status == "RUNNING" && result.CancelRequested {
		status = http.StatusAccepted
	}
	responses.New(c).ToResponseWithStatus(status, result)
}

func (controller *ReportRunController) Results(c *gin.Context) {
	runID, err := parseReportUint(c.Param("id"), "run id")
	if err != nil {
		writeReportRunQueryError(c, data_svc.ErrReportRunQueryInvalid)
		return
	}
	limit := 100
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, parseErr := strconv.ParseUint(value, 10, 16)
		if parseErr != nil || parsed == 0 || parsed > 1000 {
			writeReportRunQueryError(c, data_svc.ErrReportRunQueryInvalid)
			return
		}
		limit = int(parsed)
	}
	result, err := controller.service.ReadResults(c.Request.Context(), auth.CurrentUserID(c), runID, strings.TrimSpace(c.Query("cursor")), limit)
	if err != nil {
		writeReportRunQueryError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportRunController) QueryResults(c *gin.Context) {
	runID, err := parseReportUint(c.Param("id"), "run id")
	if err != nil {
		writeReportRunQueryError(c, data_svc.ErrReportRunQueryInvalid)
		return
	}
	var request requestbody.ReportResultQueryRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeReportRunQueryError(c, data_svc.ErrReportRunQueryInvalid)
		return
	}
	limit := request.Limit
	if limit == 0 {
		limit = 100
	}
	result, err := controller.service.QueryResults(c.Request.Context(), auth.CurrentUserID(c), runID, reportquery.Input{Filters: request.Filters, Sort: request.Sort}, strings.TrimSpace(request.Cursor), limit)
	if err != nil {
		writeReportRunQueryError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeReportRunQueryError(c *gin.Context, err error) {
	if writeKnownDatasetError(c, err) {
		return
	}
	code, message := classifyReportRunQueryError(err)
	responses.New(c).ToSafeErrorResponse(code, message)
}

func classifyReportRunQueryError(err error) (*errcode.Error, string) {
	switch {
	case errors.Is(err, data_svc.ErrReportRunQueryInvalid):
		return errcode.UnprocessableEntity, "报表运行请求参数校验失败"
	case errors.Is(err, data_svc.ErrReportRunQueryNotFound):
		return errcode.NotFound, "报表运行不存在"
	case errors.Is(err, data_svc.ErrReportRunQueryConflict):
		return errcode.Conflict, "当前运行状态不支持该操作或结果已不可用"
	case errors.Is(err, data_svc.ErrReportRunResultTemporary):
		return errcode.ServiceUnavailable, "报表结果查询暂时不可用"
	default:
		return errcode.InternalServerError, "报表运行服务暂时不可用"
	}
}
