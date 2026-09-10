package data_ctrl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/auth"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
)

type ReportDraftServiceAPI interface {
	Create(context.Context, uint, requestbody.ReportDraftSaveRequest) (*data_svc.ReportDraftDTO, error)
	Get(context.Context, uint, uint) (*data_svc.ReportDraftDTO, error)
	List(context.Context, uint, uint, int, string, string) (*data_svc.ReportDraftListDTO, error)
	Update(context.Context, uint, uint, requestbody.ReportDraftSaveRequest) (*data_svc.ReportDraftDTO, error)
	Delete(context.Context, uint, uint, uint64) (*data_svc.ReportDraftDeleteDTO, error)
}

type ReportController struct {
	service        ReportDraftServiceAPI
	publishService ReportPublishServiceAPI
	runService     ReportRunServiceAPI
	versionService ReportVersionServiceAPI
}

type ReportPublishServiceAPI interface {
	Publish(context.Context, uint, uint, uint64) (*data_svc.ReportPublicationDTO, error)
	Activate(context.Context, uint, uint, uint, uint64) (*data_svc.ReportPublicationDTO, error)
}

type ReportRunServiceAPI interface {
	Contract(context.Context, uint, uint) (*data_svc.ReportRunContractDTO, error)
	Create(context.Context, uint, uint, requestbody.ReportRunCreateRequest) (*data_svc.ReportRunDTO, error)
}

type ReportVersionServiceAPI interface {
	List(context.Context, uint, uint, uint, int) (*data_svc.ReportVersionPageDTO, error)
	Get(context.Context, uint, uint, uint) (*data_svc.ReportVersionDetailDTO, error)
	Diff(context.Context, uint, uint, uint, uint) (*data_svc.ReportVersionDiffDTO, error)
}

func NewReportController() *ReportController {
	repository := reportrepo.New()
	return NewReportControllerWithVersionService(
		data_svc.NewReportDraftServiceWithStore(repository),
		data_svc.NewReportPublishService(repository, reportsecret.EnvironmentKeyring{}, data_svc.OpenReportOracle),
		data_svc.NewReportRunServiceWithDependencies(repository, reportsecret.EnvironmentParameterCipher{}),
		data_svc.NewReportVersionService(repository),
	)
}

func NewReportControllerWithService(service ReportDraftServiceAPI) *ReportController {
	return NewReportControllerWithServices(service, nil)
}

func NewReportControllerWithServices(service ReportDraftServiceAPI, publishService ReportPublishServiceAPI) *ReportController {
	return NewReportControllerWithAllServices(service, publishService, nil)
}

func NewReportControllerWithAllServices(service ReportDraftServiceAPI, publishService ReportPublishServiceAPI, runService ReportRunServiceAPI) *ReportController {
	if service == nil {
		panic("report controller: nil service")
	}
	return &ReportController{service: service, publishService: publishService, runService: runService}
}

func NewReportControllerWithVersionService(service ReportDraftServiceAPI, publishService ReportPublishServiceAPI, runService ReportRunServiceAPI, versionService ReportVersionServiceAPI) *ReportController {
	controller := NewReportControllerWithAllServices(service, publishService, runService)
	controller.versionService = versionService
	return controller
}

func (controller *ReportController) ListVersions(c *gin.Context) {
	if controller.versionService == nil {
		writeReportError(c, errors.New("report version service is unavailable"))
		return
	}
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	afterID, limit, err := parseReportListQuery(c)
	if err != nil {
		writeReportError(c, err)
		return
	}
	result, err := controller.versionService.List(c.Request.Context(), auth.CurrentUserID(c), reportID, afterID, limit)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) GetVersion(c *gin.Context) {
	if controller.versionService == nil {
		writeReportError(c, errors.New("report version service is unavailable"))
		return
	}
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	versionID, err := parseReportUint(c.Param("versionId"), "version id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	result, err := controller.versionService.Get(c.Request.Context(), auth.CurrentUserID(c), reportID, versionID)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) VersionDiff(c *gin.Context) {
	if controller.versionService == nil {
		writeReportError(c, errors.New("report version service is unavailable"))
		return
	}
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	baseID, err := parseReportUint(c.Query("baseVersionId"), "baseVersionId")
	if err != nil {
		writeReportError(c, err)
		return
	}
	targetID, err := parseReportUint(c.Query("targetVersionId"), "targetVersionId")
	if err != nil || baseID == targetID {
		writeReportError(c, fmt.Errorf("%w: invalid version diff", data_svc.ErrReportInvalid))
		return
	}
	result, err := controller.versionService.Diff(c.Request.Context(), auth.CurrentUserID(c), reportID, baseID, targetID)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) CreateRun(c *gin.Context) {
	if controller.runService == nil {
		writeReportError(c, errors.New("report run service is unavailable"))
		return
	}
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	var request requestbody.ReportRunCreateRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeReportError(c, fmt.Errorf("%w: invalid run request", data_svc.ErrReportRunInvalid))
		return
	}
	result, err := controller.runService.Create(c.Request.Context(), auth.CurrentUserID(c), reportID, request)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusAccepted, result)
}

func (controller *ReportController) GetRunContract(c *gin.Context) {
	if controller.runService == nil {
		writeReportError(c, errors.New("report run service is unavailable"))
		return
	}
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	result, err := controller.runService.Contract(c.Request.Context(), auth.CurrentUserID(c), reportID)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) Create(c *gin.Context) {
	var request requestbody.ReportDraftSaveRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeReportError(c, fmt.Errorf("%w: invalid JSON body", data_svc.ErrReportInvalid))
		return
	}
	result, err := controller.service.Create(c.Request.Context(), auth.CurrentUserID(c), request)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusCreated, result)
}

func (controller *ReportController) Get(c *gin.Context) {
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	result, err := controller.service.Get(c.Request.Context(), auth.CurrentUserID(c), reportID)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) List(c *gin.Context) {
	afterID, limit, err := parseReportListQuery(c)
	if err != nil {
		writeReportError(c, err)
		return
	}
	result, err := controller.service.List(
		c.Request.Context(), auth.CurrentUserID(c), afterID, limit,
		strings.TrimSpace(c.Query("category")), strings.TrimSpace(c.Query("search")),
	)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) Update(c *gin.Context) {
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	var request requestbody.ReportDraftSaveRequest
	if err := decodeMallJSON(c, &request); err != nil {
		writeReportError(c, fmt.Errorf("%w: invalid JSON body", data_svc.ErrReportInvalid))
		return
	}
	result, err := controller.service.Update(c.Request.Context(), auth.CurrentUserID(c), reportID, request)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) Delete(c *gin.Context) {
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	expectedLockVersion, err := strconv.ParseUint(strings.TrimSpace(c.Query("expectedLockVersion")), 10, 64)
	if err != nil || expectedLockVersion == 0 {
		writeReportError(c, fmt.Errorf("%w: invalid expectedLockVersion", data_svc.ErrReportInvalid))
		return
	}
	result, err := controller.service.Delete(c.Request.Context(), auth.CurrentUserID(c), reportID, expectedLockVersion)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) Publish(c *gin.Context) {
	if controller.publishService == nil {
		writeReportError(c, errors.New("report publication service is unavailable"))
		return
	}
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	var request requestbody.ReportPublishRequest
	if err := decodeMallJSON(c, &request); err != nil || request.ExpectedLockVersion == 0 {
		writeReportError(c, fmt.Errorf("%w: invalid publication request", data_svc.ErrReportInvalid))
		return
	}
	result, err := controller.publishService.Publish(c.Request.Context(), auth.CurrentUserID(c), reportID, request.ExpectedLockVersion)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func (controller *ReportController) ActivateVersion(c *gin.Context) {
	if controller.publishService == nil {
		writeReportError(c, errors.New("report publication service is unavailable"))
		return
	}
	reportID, err := parseReportUint(c.Param("id"), "report id")
	if err != nil {
		writeReportError(c, err)
		return
	}
	var request requestbody.ReportVersionActivateRequest
	if err := decodeMallJSON(c, &request); err != nil || request.VersionID == 0 || request.ExpectedLockVersion == 0 {
		writeReportError(c, fmt.Errorf("%w: invalid version activation request", data_svc.ErrReportInvalid))
		return
	}
	result, err := controller.publishService.Activate(c.Request.Context(), auth.CurrentUserID(c), reportID, request.VersionID, request.ExpectedLockVersion)
	if err != nil {
		writeReportError(c, err)
		return
	}
	responses.New(c).ToResponseWithStatus(http.StatusOK, result)
}

func parseReportListQuery(c *gin.Context) (uint, int, error) {
	var afterID uint
	var limit int
	var err error
	if value := strings.TrimSpace(c.Query("afterId")); value != "" {
		afterID, err = parseReportUint(value, "afterId")
		if err != nil {
			return 0, 0, err
		}
	}
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		parsed, parseErr := strconv.ParseUint(value, 10, 16)
		if parseErr != nil || parsed == 0 {
			return 0, 0, fmt.Errorf("%w: invalid limit", data_svc.ErrReportInvalid)
		}
		limit = int(parsed)
	}
	return afterID, limit, nil
}

func parseReportUint(value, field string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, strconv.IntSize)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%w: invalid %s", data_svc.ErrReportInvalid, field)
	}
	return uint(parsed), nil
}

func writeReportError(c *gin.Context, err error) {
	if c != nil && err != nil {
		_ = c.Error(err).SetType(gin.ErrorTypePrivate)
	}
	code, message := classifyReportError(err)
	responses.New(c).ToSafeErrorResponse(code, message)
}

func classifyReportError(err error) (*errcode.Error, string) {
	switch {
	case errors.Is(err, data_svc.ErrReportNotFound):
		return errcode.NotFound, "报表草稿不存在"
	case errors.Is(err, data_svc.ErrReportConflict):
		return errcode.Conflict, "报表草稿已被修改或编码已存在，请刷新后重试"
	case errors.Is(err, data_svc.ErrReportDeleteConflict):
		return errcode.Conflict, "仅未发布且从未运行的草稿模板可以删除"
	case errors.Is(err, data_svc.ErrReportInvalid):
		return errcode.UnprocessableEntity, reportDraftValidationMessage(err)
	case errors.Is(err, data_svc.ErrReportPublicationTemporaryTable):
		return errcode.UnprocessableEntity, "Oracle结果表不能使用临时表，请改为普通永久表；系统会在导出成功后清空结果数据"
	case errors.Is(err, data_svc.ErrReportPublicationInvalid):
		return errcode.UnprocessableEntity, "Oracle过程或结果表与报表配置不一致"
	case errors.Is(err, data_svc.ErrReportRunInvalid):
		return errcode.UnprocessableEntity, "报表查询参数校验失败"
	case errors.Is(err, data_svc.ErrReportRunDenied):
		return errcode.Forbidden, "没有该报表的查询权限"
	case errors.Is(err, data_svc.ErrReportRunBusy):
		return errcode.Conflict, "该报表已有生成任务，或上一份结果仍在读取、导出或清理，请稍后重试"
	case errors.Is(err, data_svc.ErrReportRunCredentialUnavailable):
		return errcode.ServiceUnavailable, "报表敏感参数加密配置不可用，请联系管理员"
	case errors.Is(err, data_svc.ErrReportInputQueryUnavailable):
		return errcode.ServiceUnavailable, "报表输入选项暂时不可用，请稍后重试"
	default:
		return errcode.InternalServerError, "报表配置服务暂时不可用"
	}
}

func reportDraftValidationMessage(err error) string {
	detail := strings.TrimPrefix(err.Error(), data_svc.ErrReportInvalid.Error()+": ")
	messages := map[string]string{
		"invalid report identity":                                               "请检查报表名称、编码、分类、说明和 Oracle 数据源",
		"published report category cannot be changed":                           "已上线报表不能直接修改分类，请新建报表后重新发布",
		"invalid Oracle procedure":                                              "存储过程名称不合法，请从 Oracle 目录重新选择",
		"invalid JSON input argument":                                           "JSON 入参名不合法，请重新选择存储过程",
		"invalid JSON result-table arguments":                                   "所选存储过程必须只有一个 JSON 入参且不得有出参",
		"invalid Oracle result table":                                           "Oracle 结果表不合法，请从结果表目录重新选择",
		"invalid result run id column":                                          "run_id 字段不合法，请从结果表字段中选择",
		"invalid result row id column":                                          "行游标字段不合法或与 run_id 相同，请重新选择",
		"input schema is required and must not exceed 64 KiB":                   "筛选条件 JSON 不能为空且不能超过 64 KiB",
		"input schema must be a non-empty JSON object with at most 128 fields":  "筛选条件必须是非空 JSON 对象，且最多 128 项",
		"input schema contains an invalid condition code":                       "筛选条件中包含不合法的字段编码",
		"input schema field configuration is invalid":                           "筛选条件字段配置不完整",
		"input schema field type or displayName is invalid":                     "筛选条件的 Oracle 类型或显示名不合法",
		"input schema field control is invalid":                                 "筛选条件的控件类型不受支持",
		"input schema field queryName is invalid":                               "查询选择字段必须配置有效的查询名称",
		"input schema field queryName and allowedValues are mutually exclusive": "查询选择字段不能同时配置静态允许值",
		"result columns are required":                                           "请先选择 Oracle 结果表并生成 Excel 字段映射",
		"columns must contain between 1 and 512 items":                          "Excel 字段映射需要 1 至 512 项",
		"invalid result column configuration":                                   "Excel 字段映射不完整，请重新从结果表字段生成",
		"invalid Oracle result column":                                          "Excel 映射中包含不合法的 Oracle 字段",
		"duplicated Oracle result column":                                       "Excel 映射中存在重复的 Oracle 字段",
		"duplicated Excel header":                                               "Excel 映射中存在重复表头",
		"visible export column requires an Excel header":                        "所有导出字段都必须填写 Excel 表头",
		"at least one exportable result column is required":                     "请至少选择一个可导出字段",
		"result key columns must not be configured as report columns":           "run_id 和行游标是系统字段，不能加入预览或 Excel 映射",
		"invalid grant subject":                                                 "权限主体 ID 必须大于 0",
		"grant actions must be a non-empty string array":                        "每个权限主体至少需要选择查询或导出权限",
	}
	if message, ok := messages[detail]; ok {
		return "报表草稿校验失败：" + message
	}
	return "报表草稿参数校验失败"
}
