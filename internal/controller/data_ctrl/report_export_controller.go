package data_ctrl

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"

	"github.com/gin-gonic/gin"
)

type ReportExportServiceAPI interface {
	Create(context.Context, uint, uint, reportquery.Input) (*data_svc.ReportExportDTO, bool, error)
}

type ReportExportQueryServiceAPI interface {
	List(context.Context, uint, uint, int, string) (*data_svc.ReportExportListDTO, error)
	Get(context.Context, uint, uint) (*data_svc.ReportExportViewDTO, error)
	Download(context.Context, uint, uint) (*data_svc.ReportExportDownloadDTO, error)
}

func (controller *ReportExportController) List(c *gin.Context) {
	afterID, limit := uint(0), 50
	if value := strings.TrimSpace(c.Query("afterId")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			writeReportExportError(c, data_svc.ErrReportExportQueryInvalid)
			return
		}
		afterID = uint(parsed)
	}
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 16)
		if err != nil || parsed == 0 || parsed > 100 {
			writeReportExportError(c, data_svc.ErrReportExportQueryInvalid)
			return
		}
		limit = int(parsed)
	}
	result, err := controller.query.List(c.Request.Context(), auth.CurrentUserID(c), afterID, limit, c.Query("status"))
	if err != nil {
		writeReportExportError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

type ReportExportController struct {
	create ReportExportServiceAPI
	query  ReportExportQueryServiceAPI
}

func NewReportExportController() *ReportExportController {
	return NewReportExportControllerWithServices(data_svc.NewReportExportService(), data_svc.NewReportExportQueryService())
}

func NewReportExportControllerWithServices(create ReportExportServiceAPI, query ReportExportQueryServiceAPI) *ReportExportController {
	if create == nil || query == nil {
		panic("report export controller: services are required")
	}
	return &ReportExportController{create: create, query: query}
}

func (controller *ReportExportController) Create(c *gin.Context) {
	runID, err := parseReportUint(c.Param("id"), "run id")
	if err != nil {
		writeReportExportError(c, data_svc.ErrReportExportInvalid)
		return
	}
	var request requestbody.ReportExportCreateRequest
	if c.Request.ContentLength != 0 {
		if err := decodeMallJSON(c, &request); err != nil {
			writeReportExportError(c, data_svc.ErrReportExportInvalid)
			return
		}
	}
	result, replayed, err := controller.create.Create(c.Request.Context(), auth.CurrentUserID(c), runID, reportquery.Input{Filters: request.Filters, Sort: request.Sort})
	if err != nil {
		writeReportExportError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	responses.New(c).ToResponseWithStatus(http.StatusAccepted, result)
}

func (controller *ReportExportController) Get(c *gin.Context) {
	exportID, err := parseReportUint(c.Param("id"), "export id")
	if err != nil {
		writeReportExportError(c, data_svc.ErrReportExportQueryInvalid)
		return
	}
	result, err := controller.query.Get(c.Request.Context(), auth.CurrentUserID(c), exportID)
	if err != nil {
		writeReportExportError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportExportController) Download(c *gin.Context) {
	exportID, err := parseReportUint(c.Param("id"), "export id")
	if err != nil {
		writeReportExportError(c, data_svc.ErrReportExportQueryInvalid)
		return
	}
	result, err := controller.query.Download(c.Request.Context(), auth.CurrentUserID(c), exportID)
	if err != nil {
		writeReportExportError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func writeReportExportError(c *gin.Context, err error) {
	if writeKnownDatasetError(c, err) {
		return
	}
	code, message := classifyReportExportError(err)
	if err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	}
	responses.New(c).ToSafeErrorResponse(code, message)
}

func classifyReportExportError(err error) (*errcode.Error, string) {
	switch {
	case errors.Is(err, data_svc.ErrReportExportInvalid), errors.Is(err, data_svc.ErrReportExportQueryInvalid):
		return errcode.UnprocessableEntity, "报表导出请求参数校验失败"
	case errors.Is(err, data_svc.ErrReportExportNotFound), errors.Is(err, data_svc.ErrReportExportQueryNotFound):
		return errcode.NotFound, "报表导出不存在"
	case errors.Is(err, data_svc.ErrReportExportConflict), errors.Is(err, data_svc.ErrReportExportQueryNotReady), errors.Is(err, data_svc.ErrReportExportQueryExpired), errors.Is(err, data_svc.ErrReportExportArtifactMissing):
		return errcode.Conflict, "报表导出尚未就绪、已过期或文件不可用"
	case errors.Is(err, data_svc.ErrReportExportStorageUnavailable):
		return errcode.BadGateway, "报表导出存储暂时不可用"
	default:
		return errcode.InternalServerError, "报表导出服务暂时不可用"
	}
}
