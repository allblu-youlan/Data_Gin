package data_ctrl

import (
	"encoding/json"
	"errors"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
	"github.com/gin-gonic/gin"
	"strings"
)

type ReportDatasetController struct {
	service *data_svc.ReportDatasetService
}

func NewReportDatasetController() *ReportDatasetController {
	return &ReportDatasetController{service: data_svc.NewReportDatasetService()}
}
func datasetID(c *gin.Context) (uint, bool) {
	id, e := parseReportUint(c.Param("id"), "report id")
	if e != nil {
		writeReportError(c, e)
		return 0, false
	}
	return id, true
}
func (h *ReportDatasetController) Validate(c *gin.Context) {
	id, ok := datasetID(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedLockVersion uint64 `json:"expectedLockVersion"`
	}
	if decodeMallJSON(c, &req) != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "校验参数无效")
		return
	}
	v, e := h.service.Validate(c.Request.Context(), auth.CurrentUserID(c), id, req.ExpectedLockVersion)
	if e != nil {
		writeDatasetError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
func (h *ReportDatasetController) Preview(c *gin.Context) {
	id, ok := datasetID(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedLockVersion uint64                     `json:"expectedLockVersion"`
		DraftVersionID      uint                       `json:"draftVersionId,omitempty"`
		Conditions          map[string]json.RawMessage `json:"conditions"`
		Parameters          map[string]json.RawMessage `json:"parameters,omitempty"`
		QueryInputs         reportdataset.QueryInputs  `json:"queryInputs,omitempty"`
	}
	if decodeMallJSON(c, &req) != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "试运行参数无效")
		return
	}
	if req.Conditions == nil {
		req.Conditions = req.Parameters
	}
	v, e := h.service.Preview(c.Request.Context(), auth.CurrentUserID(c), id, req.ExpectedLockVersion, req.Conditions, req.QueryInputs)
	if e != nil {
		writeDatasetError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
func (h *ReportDatasetController) Detail(c *gin.Context) {
	id, ok := datasetID(c)
	if !ok {
		return
	}
	var req struct {
		RowKey     string `json:"rowKey"`
		RelationID string `json:"relationId"`
		Mode       string `json:"mode"`
	}
	if decodeMallJSON(c, &req) != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "详情参数无效")
		return
	}
	v, e := h.service.RecordDetail(c.Request.Context(), auth.CurrentUserID(c), id, req.RowKey, req.RelationID, req.Mode)
	if e != nil {
		writeDatasetError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
func (h *ReportDatasetController) Options(c *gin.Context) {
	id, ok := datasetID(c)
	if !ok {
		return
	}
	var req data_svc.ReportRelationOptionsRequest
	if decodeMallJSON(c, &req) != nil {
		responses.New(c).ToSafeErrorResponse(errcode.BadRequest, "选项参数无效")
		return
	}
	v, e := h.service.RelationOptions(c.Request.Context(), auth.CurrentUserID(c), id, req)
	if e != nil {
		writeDatasetError(c, e)
		return
	}
	responses.New(c).ToResponse(v)
}
func writeDatasetError(c *gin.Context, e error) {
	if writeKnownDatasetError(c, e) {
		return
	}
	code, message := classifyDatasetError(e)
	if code != nil {
		_ = c.Error(e).SetType(gin.ErrorTypePrivate)
		responses.New(c).ToSafeErrorResponse(code, message)
		return
	}
	writeReportError(c, e)
}
func classifyDatasetError(e error) (*errcode.Error, string) {
	switch {
	case errors.Is(e, data_svc.ErrReportRunQueryNotFound):
		return errcode.NotFound, "记录不存在或不可访问"
	case errors.Is(e, data_svc.ErrReportRunQueryConflict):
		return errcode.Conflict, "结果契约或授权范围已变化，请重新查询"
	case errors.Is(e, data_svc.ErrReportRunQueryInvalid), errors.Is(e, data_svc.ErrReportRunInvalid):
		return errcode.BadRequest, "查询条件无效，请核对参数并重新生成报表"
	case errors.Is(e, data_svc.ErrReportDatasourceOracleUnavailable), errors.Is(e, data_svc.ErrReportRunResultTemporary):
		return errcode.ServiceUnavailable, "数据源暂时不可用，请稍后重试"
	case errors.Is(e, data_svc.ErrReportRunDenied):
		return errcode.Forbidden, "没有当前记录或字段的访问权限"
	case errors.Is(e, data_svc.ErrReportRunBusy):
		return errcode.Conflict, "数据源查询繁忙，请稍后重试"
	case strings.Contains(e.Error(), "REPORT_METADATA_CHANGED"):
		return errcode.Conflict, "依赖字段已发生变化，请刷新字典并重新发布"
	}
	return nil, ""
}
