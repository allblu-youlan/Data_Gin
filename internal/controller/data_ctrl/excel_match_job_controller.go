package data_ctrl

import (
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/msg"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/errcode"
	"gin-biz-web-api/pkg/responses"
)

type excelUploadSessionRequest struct {
	FileName    string `json:"fileName"`
	TotalChunks int    `json:"totalChunks"`
}

type excelUploadCompleteRequest struct {
	TotalChunks int `json:"totalChunks"`
}

type excelMatchSchemeRequest struct {
	Name      string          `json:"name"`
	Operation string          `json:"operation"`
	Config    json.RawMessage `json:"config"`
}

type ExcelMatchJobController struct {
	service *data_svc.ExcelMatchJobService
}

type excelMatchJobResponse struct {
	ID              uint              `json:"id"`
	SourceFileName  string            `json:"source_file_name"`
	Operation       string            `json:"operation"`
	Status          string            `json:"status"`
	TotalRows       int               `json:"total_rows"`
	ProcessedRows   int               `json:"processed_rows"`
	FilteredRows    int               `json:"filtered_rows"`
	MatchedRows     int               `json:"matched_rows"`
	UnmatchedRows   int               `json:"unmatched_rows"`
	StartedAt       *model.TimeNormal `json:"started_at"`
	FinishedAt      *model.TimeNormal `json:"finished_at"`
	ExpiresAt       *model.TimeNormal `json:"expires_at"`
	CanDownload     bool              `json:"can_download"`
	DownloadMessage string            `json:"download_message"`
	CreatedAt       int               `json:"created_at"`
}

type excelMatchJobLogResponse struct {
	ID             uint                               `json:"id"`
	JobID          uint                               `json:"job_id"`
	Level          string                             `json:"level"`
	Message        string                             `json:"message"`
	CreatedAt      int                                `json:"created_at"`
	ImportProgress *oracleExcelImportProgressResponse `json:"import_progress,omitempty"`
}

// Keep the response allowlist separate from stored details and service types.
type oracleExcelImportProgressResponse struct {
	StepIndex     int    `json:"stepIndex"`
	StepName      string `json:"stepName"`
	Stage         string `json:"stage"`
	ProcessedRows int    `json:"processedRows"`
	TotalRows     int    `json:"totalRows"`
	TotalKnown    bool   `json:"totalKnown"`
	ScannedRows   int    `json:"scannedRows"`
	FileIndex     int    `json:"fileIndex"`
	FileCount     int    `json:"fileCount"`
	FileName      string `json:"fileName"`
	SheetName     string `json:"sheetName"`
	RowNumber     int    `json:"rowNumber"`
	Identifier    string `json:"identifier"`
}

func NewExcelMatchJobController() *ExcelMatchJobController {
	return &ExcelMatchJobController{
		service: data_svc.NewExcelMatchJobService(),
	}
}

func (ctrl *ExcelMatchJobController) CreateJob(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	config := c.PostForm("config")
	if config == "" {
		c.JSON(400, msg.ErrResponseStr("匹配配置不能为空"))
		return
	}

	uploadID := c.PostForm("uploadId")
	var matchJob *model.ExcelMatchJob
	if err == nil {
		matchJob, err = ctrl.service.CreateJob(c.Request.Context(), fileHeader, config)
	} else if raw := c.PostForm("uploadIds"); raw != "" {
		var uploadIDs []string
		if err := json.Unmarshal([]byte(raw), &uploadIDs); err != nil || len(uploadIDs) == 0 {
			c.JSON(400, msg.ErrResponseStr("Excel 文件列表无效"))
			return
		}
		matchJob, err = ctrl.service.CreateMatchJobFromUploads(c.Request.Context(), uploadIDs, config)
	} else if uploadID != "" {
		matchJob, err = ctrl.service.CreateJobFromUpload(c.Request.Context(), uploadID, config)
	} else {
		c.JSON(400, msg.ErrResponse("读取 Excel 文件失败", err))
		return
	}
	if err != nil {
		c.JSON(400, msg.ErrResponse("创建 Excel 匹配任务失败", err))
		return
	}
	logs, _ := ctrl.service.GetJobLogs(c.Request.Context(), matchJob.ID)

	c.JSON(200, msg.SuccessResponse("Excel 匹配任务已创建", &map[string]any{
		"job":          safeExcelMatchJob(*matchJob),
		"logs":         safeExcelMatchJobLogs(logs),
		"downloadPath": "/api/v1/excel-match-jobs/" + strconv.FormatUint(uint64(matchJob.ID), 10) + "/download",
	}))
}

func (ctrl *ExcelMatchJobController) Preview(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	config := c.PostForm("config")
	if config == "" {
		c.JSON(400, msg.ErrResponseStr("匹配配置不能为空"))
		return
	}

	uploadID := c.PostForm("uploadId")
	var preview *data_svc.ExcelMatchPreviewResult
	if err == nil {
		preview, err = ctrl.service.Preview(c.Request.Context(), fileHeader, config)
	} else if uploadID != "" {
		preview, err = ctrl.service.PreviewUploaded(c.Request.Context(), uploadID, config)
	} else {
		c.JSON(400, msg.ErrResponse("读取 Excel 文件失败", err))
		return
	}
	if err != nil {
		c.JSON(400, msg.ErrResponse("预览 Excel 匹配失败", err))
		return
	}

	c.JSON(200, msg.SuccessResponse("预览 Excel 匹配成功", &map[string]any{
		"preview": preview,
	}))
}

func (ctrl *ExcelMatchJobController) CreateUploadSession(c *gin.Context) {
	var req excelUploadSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, msg.ErrResponse("读取上传会话参数失败", err))
		return
	}
	session, err := ctrl.service.CreateUploadSession(c.Request.Context(), req.FileName, req.TotalChunks)
	if err != nil {
		c.JSON(400, msg.ErrResponse("创建 Excel 分片上传会话失败", err))
		return
	}
	c.JSON(200, msg.SuccessResponse("Excel 分片上传会话已创建", &map[string]any{
		"upload": session,
	}))
}

func (ctrl *ExcelMatchJobController) UploadChunk(c *gin.Context) {
	uploadID := c.Param("upload_id")
	index, err := strconv.Atoi(c.PostForm("index"))
	if err != nil {
		c.JSON(400, msg.ErrResponse("无效的分片序号", err))
		return
	}
	totalChunks, err := strconv.Atoi(c.PostForm("totalChunks"))
	if err != nil {
		c.JSON(400, msg.ErrResponse("无效的分片总数", err))
		return
	}
	fileHeader, err := c.FormFile("chunk")
	if err != nil {
		c.JSON(400, msg.ErrResponse("读取 Excel 分片失败", err))
		return
	}

	session, err := ctrl.service.SaveUploadChunk(c.Request.Context(), uploadID, index, totalChunks, fileHeader)
	if err != nil {
		c.JSON(400, msg.ErrResponse("保存 Excel 分片失败", err))
		return
	}
	c.JSON(200, msg.SuccessResponse("Excel 分片已保存", &map[string]any{
		"upload": session,
	}))
}

func (ctrl *ExcelMatchJobController) CompleteUpload(c *gin.Context) {
	uploadID := c.Param("upload_id")
	var req excelUploadCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, msg.ErrResponse("读取上传合并参数失败", err))
		return
	}
	session, err := ctrl.service.CompleteUpload(c.Request.Context(), uploadID, req.TotalChunks)
	if err != nil {
		c.JSON(400, msg.ErrResponse("合并 Excel 分片失败", err))
		return
	}
	c.JSON(200, msg.SuccessResponse("Excel 分片已合并", &map[string]any{
		"upload": session,
	}))
}

func (ctrl *ExcelMatchJobController) ListModels(c *gin.Context) {
	models, err := ctrl.service.ListModels(c.Request.Context())
	if err != nil {
		zap.L().Error("Excel model catalog unavailable", zap.Error(err))
		responses.New(c).ToSafeErrorResponse(errcode.ServiceUnavailable, "模型目录暂时不可用")
		return
	}
	c.JSON(200, msg.SuccessResponse("查询 Excel 可选模型成功", &map[string]any{
		"models": models,
	}))
}

func (ctrl *ExcelMatchJobController) ListSchemes(c *gin.Context) {
	schemes, err := ctrl.service.ListSchemes(c.Request.Context(), c.Query("operation"))
	if err != nil {
		c.JSON(400, msg.ErrResponse("查询 Excel 匹配方案失败", err))
		return
	}
	c.JSON(200, msg.SuccessResponse("查询 Excel 匹配方案成功", &map[string]any{
		"schemes": schemes,
	}))
}

func (ctrl *ExcelMatchJobController) SaveScheme(c *gin.Context) {
	var req excelMatchSchemeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, msg.ErrResponse("读取 Excel 匹配方案失败", err))
		return
	}
	scheme, err := ctrl.service.SaveScheme(c.Request.Context(), req.Name, req.Operation, string(req.Config))
	if err != nil {
		c.JSON(400, msg.ErrResponse("保存 Excel 匹配方案失败", err))
		return
	}
	c.JSON(200, msg.SuccessResponse("保存 Excel 匹配方案成功", &map[string]any{
		"scheme": scheme,
	}))
}

func (ctrl *ExcelMatchJobController) DeleteScheme(c *gin.Context) {
	id, err := parseUintParam(c, "scheme_id")
	if err != nil {
		c.JSON(400, msg.ErrResponse("无效的 Excel 匹配方案ID", err))
		return
	}
	if err := ctrl.service.DeleteScheme(c.Request.Context(), id); err != nil {
		c.JSON(400, msg.ErrResponse("删除 Excel 匹配方案失败", err))
		return
	}
	c.JSON(200, msg.SuccessResponseStr("删除 Excel 匹配方案成功"))
}

func (ctrl *ExcelMatchJobController) ListJobs(c *gin.Context) {
	values := c.Request.URL.Query()
	if !monitoringQueryKeysAllowed(values, "limit", "page", "page_size", "keyword", "status", "operation") {
		c.JSON(400, msg.ErrResponseStr("无效的 Excel 匹配任务查询参数"))
		return
	}
	if monitoringHasAnyKey(values, "page", "page_size", "keyword", "status", "operation") {
		if values.Has("limit") {
			c.JSON(400, msg.ErrResponseStr("分页查询不支持 limit 参数"))
			return
		}
		page, pageSize, err := parseMonitoringPagination(values)
		if err != nil {
			c.JSON(400, msg.ErrResponseStr("无效的 Excel 匹配任务分页参数"))
			return
		}
		keyword, err := parseMonitoringText(values.Get("keyword"), 255)
		if err != nil {
			c.JSON(400, msg.ErrResponseStr("无效的 Excel 匹配任务查询参数"))
			return
		}
		status := strings.TrimSpace(values.Get("status"))
		if !validExcelMatchJobStatus(status) {
			c.JSON(400, msg.ErrResponseStr("无效的 Excel 匹配任务查询参数"))
			return
		}
		operation := strings.TrimSpace(values.Get("operation"))
		if !validExcelMatchJobOperation(operation) {
			c.JSON(400, msg.ErrResponseStr("无效的 Excel 匹配任务查询参数"))
			return
		}
		if operation == "all" {
			operation = ""
		}
		result, err := ctrl.service.ListJobsPage(c.Request.Context(), data_dao.ExcelMatchJobListQuery{
			Page: page, PageSize: pageSize, Keyword: keyword, Status: status, Operation: operation,
		})
		if err != nil {
			c.JSON(500, msg.ErrResponse("查询 Excel 匹配任务列表失败", err))
			return
		}
		c.JSON(200, msg.SuccessResponse("查询 Excel 匹配任务列表成功", &map[string]any{
			"jobs":       safeExcelMatchJobs(result.List),
			"pagination": monitoringPaginationResponse(page, pageSize, result.Total),
		}))
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "30"))
	if err != nil {
		c.JSON(400, msg.ErrResponse("无效的 Excel 匹配任务数量", err))
		return
	}
	jobs, err := ctrl.service.ListJobs(c.Request.Context(), limit)
	if err != nil {
		c.JSON(500, msg.ErrResponse("查询 Excel 匹配任务列表失败", err))
		return
	}
	c.JSON(200, msg.SuccessResponse("查询 Excel 匹配任务列表成功", &map[string]any{
		"jobs": safeExcelMatchJobs(jobs),
	}))
}

func (ctrl *ExcelMatchJobController) GetJob(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, msg.ErrResponse("无效的 Excel 匹配任务ID", err))
		return
	}

	matchJob, err := ctrl.service.GetJob(c.Request.Context(), id)
	if err != nil {
		c.JSON(404, msg.ErrResponse("查询 Excel 匹配任务失败", err))
		return
	}
	logs, err := ctrl.service.GetJobLogs(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, msg.ErrResponse("查询 Excel 任务日志失败", err))
		return
	}

	c.JSON(200, msg.SuccessResponse("查询 Excel 匹配任务成功", &map[string]any{
		"job":          safeExcelMatchJob(*matchJob),
		"logs":         safeExcelMatchJobLogs(logs),
		"downloadPath": "/api/v1/excel-match-jobs/" + strconv.FormatUint(uint64(matchJob.ID), 10) + "/download",
	}))
}

func validExcelMatchJobStatus(value string) bool {
	switch value {
	case "", "pending", "running", "success", "failed", "expired":
		return true
	default:
		return false
	}
}

func validExcelMatchJobOperation(value string) bool {
	switch value {
	case "", "all", "match", "write", "export":
		return true
	default:
		return false
	}
}

func safeExcelMatchJob(job model.ExcelMatchJob) excelMatchJobResponse {
	return excelMatchJobResponse{
		ID: job.ID, SourceFileName: job.SourceFileName, Operation: excelMatchJobOperation(job.ConfigJSON),
		Status: job.Status, TotalRows: job.TotalRows, ProcessedRows: job.ProcessedRows, FilteredRows: job.FilteredRows,
		MatchedRows: job.MatchedRows, UnmatchedRows: job.UnmatchedRows, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
		ExpiresAt: job.ExpiresAt, CanDownload: job.CanDownload, DownloadMessage: job.DownloadMessage, CreatedAt: job.CreatedAt,
	}
}

func safeExcelMatchJobs(jobs []model.ExcelMatchJob) []excelMatchJobResponse {
	result := make([]excelMatchJobResponse, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, safeExcelMatchJob(job))
	}
	return result
}

func safeExcelMatchJobLogs(logs []model.ExcelMatchJobLog) []excelMatchJobLogResponse {
	result := make([]excelMatchJobLogResponse, 0, len(logs))
	for _, log := range logs {
		result = append(result, excelMatchJobLogResponse{
			ID: log.ID, JobID: log.JobID, Level: log.Level,
			Message: safeExcelMatchJobLogMessage(log.Message), CreatedAt: log.CreatedAt,
			ImportProgress: safeOracleExcelImportProgress(log.DetailJSON),
		})
	}
	return result
}

func safeOracleExcelImportProgress(raw string) *oracleExcelImportProgressResponse {
	var details struct {
		Progress *oracleExcelImportProgressResponse `json:"oracleImportProgress"`
	}
	if err := json.Unmarshal([]byte(raw), &details); err != nil || details.Progress == nil {
		return nil
	}
	progress := details.Progress
	switch progress.Stage {
	case "counting", "importing", "committing", "completed", "failed":
	default:
		return nil
	}
	if progress.StepIndex < 0 || progress.ProcessedRows < 0 || progress.TotalRows < 0 || progress.ScannedRows < 0 ||
		progress.FileIndex < 0 || progress.FileCount < 0 || progress.RowNumber < 0 {
		return nil
	}
	if name := strings.TrimSpace(progress.FileName); name != "" {
		progress.FileName = path.Base(strings.ReplaceAll(name, `\`, "/"))
	}
	for _, text := range []*string{&progress.StepName, &progress.FileName, &progress.SheetName, &progress.Identifier} {
		if *text = strings.TrimSpace(*text); *text != "" {
			*text = safeExcelMatchJobLogMessage(*text)
		}
	}
	return progress
}

func safeExcelMatchJobLogMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "任务日志已记录"
	}
	lower := strings.ToLower(message)
	for _, marker := range []string{"token", "password", "passwd", "secret", "authorization", "cookie", "apikey", "api_key", "accesskey", "access_key"} {
		if strings.Contains(lower, marker) {
			return "任务日志已记录（敏感内容已隐藏）"
		}
	}
	return message
}

func excelMatchJobOperation(rawConfig string) string {
	var config struct {
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return ""
	}
	if operation := strings.TrimSpace(config.Operation); operation != "" {
		return operation
	}
	return "export_match"
}

func (ctrl *ExcelMatchJobController) Download(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, msg.ErrResponse("无效的 Excel 匹配任务ID", err))
		return
	}

	matchJob, resultPath, filename, err := ctrl.service.Download(c.Request.Context(), id)
	if err != nil {
		c.JSON(400, msg.ErrResponse("下载 Excel 匹配结果失败", err))
		return
	}
	if strings.TrimSpace(matchJob.ResultURL) != "" {
		c.Redirect(http.StatusFound, matchJob.ResultURL)
		return
	}

	c.FileAttachment(resultPath, filename)
}
