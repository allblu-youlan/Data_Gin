package data_svc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/storage"

	"github.com/hibiken/asynq"
	"github.com/xuri/excelize/v2"
)

const (
	excelOperationExportMatch      = "export_match"
	excelOperationImportUpdate     = "import_update"
	excelOperationClearMatched     = "clear_matched_docno"
	excelMatchModeField            = "field"
	excelMatchModeOrderItemSKU     = "order_item_sku"
	excelMatchModeConditionalWrite = "conditional_write"

	excelMatchStatusPending = "pending"
	excelMatchStatusSuccess = "success"
	excelMatchStatusExpired = "expired"

	excelMatchRetention               = 24 * time.Hour
	excelMatchRootDirName             = "data-warehouse-excel-jobs"
	excelUploadRootDirName            = "data-warehouse-excel-uploads"
	excelPreviewRootDirName           = "data-warehouse-excel-previews"
	excelizeTempDirName               = "excelize-tmp"
	excelTempRootEnvName              = "DATA_WAREHOUSE_EXCEL_TEMP_DIR"
	excelMatchSourceFileName          = "source.xlsx"
	excelMatchResultFileName          = "result.xlsx"
	excelUploadChunksDirName          = "chunks"
	excelUploadMetaFileName           = "meta.json"
	excelUploadMergedFile             = "source.xlsx"
	excelMaxRowsPerSheet              = 1048576
	defaultExcelMatchBatch            = 1000
	maxBufferedExcelRows              = 5000
	maxExcelUploadChunks              = 10000
	maxExcelUploadChunkBytes          = 8 * 1024 * 1024
	defaultExcelSheetName             = "Sheet1"
	defaultExcelResultSheet           = "Result_1"
	bojunRetailOrderTemplate          = "bojun_retail_order"
	bojunRetailOrdersTable            = "bojun_retail_orders"
	defaultExcelPreviewRows           = 5000
	defaultExcelPreviewItems          = 50
	maxExcelMatchSteps                = 20
	excelMatchOSSUploadTimeoutEnvName = "EXCEL_MATCH_OSS_UPLOAD_TIMEOUT_SECONDS"
	excelMatchOSSUploadMinTimeout     = 10 * time.Minute
	excelMatchOSSUploadMaxTimeout     = 2 * time.Hour
	excelMatchOSSUploadBytesPerMinute = 32 * 1024 * 1024
	excelMatchOSSProgressBytes        = 32 * 1024 * 1024
	excelMatchOSSProgressInterval     = 15 * time.Second
	excelMatchRunningStaleAfter       = 35 * time.Minute
)

var excelizeTempMu sync.Mutex

var allowedBojunValueFields = map[string]struct{}{
	"billdate":             {},
	"completed_at":         {},
	"c_store_code":         {},
	"c_store_name":         {},
	"docno":                {},
	"retailbilltype":       {},
	"order_type_code":      {},
	"order_type_name":      {},
	"otherdocno":           {},
	"tot_amt_actual":       {},
	"tot_amt_list":         {},
	"tot_qty":              {},
	"vipno":                {},
	"related_normal_docno": {},
	"o2o_so_docno":         {},
	"matched_docno":        {},
}

var allowedExcelImportTables = map[string]struct{}{
	bojunRetailOrdersTable:   {},
	bojunRetailOrderTemplate: {},
}

var allowedBojunImportMatchFields = map[string]struct{}{
	"docno":                {},
	"otherdocno":           {},
	"o2o_so_docno":         {},
	"related_normal_docno": {},
	"matched_docno":        {},
}

var allowedBojunImportWriteFields = map[string]struct{}{
	"matched_docno":    {},
	"completed_at":     {},
	"oracle_retail_id": {},
	"order_phone":      {},
	"paid_amount":      {},
	"push_amount":      {},
	"is_to_shop":       {},
}

var bojunOracleImportWriteFields = map[string]struct{}{
	"oracle_retail_id": {},
	"order_phone":      {},
	"paid_amount":      {},
	"push_amount":      {},
	"is_to_shop":       {},
}

var bojunExcelDecimalPattern = regexp.MustCompile(`^[+-]?(?:0|[1-9][0-9]{0,15})(?:\.[0-9]{1,2})?$`)
var bojunExcelGroupedDecimalPattern = regexp.MustCompile(`^[+-]?[1-9][0-9]{0,2}(?:,[0-9]{3})+(?:\.[0-9]{1,2})?$`)

var allowedExcelExportColumnFormats = map[string]struct{}{
	"text":    {},
	"number":  {},
	"integer": {},
	"bool":    {},
	"date":    {},
}

var allowedExcelMatchFilterOps = map[string]struct{}{
	"eq":           {},
	"neq":          {},
	"contains":     {},
	"not_contains": {},
	"starts_with":  {},
	"ends_with":    {},
	"empty":        {},
	"not_empty":    {},
}

type ExcelMatchJobService struct {
	jobDAO       *data_dao.ExcelMatchJobDAO
	modelCatalog *excelMatchModelCatalogCache
}

func NewExcelMatchJobService() *ExcelMatchJobService {
	jobDAO := data_dao.NewExcelMatchJobDAO()
	return &ExcelMatchJobService{
		jobDAO:       jobDAO,
		modelCatalog: newExcelMatchModelCatalogCache(jobDAO, excelMatchModelCatalogTTL),
	}
}

type ExcelMatchFilter struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  string `json:"value"`
}

type ExcelExportColumnFormat struct {
	Column string `json:"column"`
	Format string `json:"format"`
}

// ExcelEmptyCellFill copies a value from SourceColumn into TargetColumn when
// the target cell in the same row is empty.
type ExcelEmptyCellFill struct {
	TargetColumn string `json:"targetColumn"`
	SourceColumn string `json:"sourceColumn"`
}

// ExcelImportWriteMapping maps one Excel column to one Bojun database field.
// Legacy single-field configs are normalized into this shape.
type ExcelImportWriteMapping struct {
	DBWriteField     string `json:"dbWriteField"`
	WriteExcelColumn string `json:"writeExcelColumn"`
}

type ExcelMatchStep struct {
	Name             string             `json:"name"`
	Filters          []ExcelMatchFilter `json:"filters,omitempty"`
	MatchMode        string             `json:"matchMode,omitempty"`
	TableName        string             `json:"tableName"`
	MatchExcelColumn string             `json:"matchExcelColumn"`
	SpecExcelColumn  string             `json:"specExcelColumn,omitempty"`
	PriceExcelColumn string             `json:"priceExcelColumn,omitempty"`
	QtyExcelColumn   string             `json:"qtyExcelColumn,omitempty"`
	DBMatchField     string             `json:"dbMatchField"`
	DBValueField     string             `json:"dbValueField"`
	OutputColumnName string             `json:"outputColumnName"`
	ContainsValue    string             `json:"containsValue,omitempty"`
	WriteValue       string             `json:"writeValue,omitempty"`
}

type ExcelMatchConfig struct {
	Operation           string                    `json:"operation"`
	SheetName           string                    `json:"sheetName"`
	Filters             []ExcelMatchFilter        `json:"filters,omitempty"`
	MatchExcelColumn    string                    `json:"matchExcelColumn"`
	DBTemplate          string                    `json:"dbTemplate"`
	DBMatchField        string                    `json:"dbMatchField"`
	DBValueField        string                    `json:"dbValueField"`
	TableName           string                    `json:"tableName"`
	DBWriteField        string                    `json:"dbWriteField"`
	WriteExcelColumn    string                    `json:"writeExcelColumn"`
	WriteMappings       []ExcelImportWriteMapping `json:"writeMappings,omitempty"`
	OutputColumnName    string                    `json:"outputColumnName"`
	Steps               []ExcelMatchStep          `json:"steps,omitempty"`
	EmptyCellFills      []ExcelEmptyCellFill      `json:"emptyCellFills,omitempty"`
	ExportColumnFormats []ExcelExportColumnFormat `json:"exportColumnFormats"`
	BatchSize           int                       `json:"batchSize"`
	DryRun              bool                      `json:"dryRun"`
	ConfirmWrite        bool                      `json:"confirmWrite"`
}

type ExcelUploadSession struct {
	UploadID       string `json:"uploadId"`
	FileName       string `json:"fileName"`
	TotalChunks    int    `json:"totalChunks"`
	UploadedChunks int    `json:"uploadedChunks"`
	Complete       bool   `json:"complete"`
	ExpiresAt      string `json:"expiresAt"`
}

type excelUploadMeta struct {
	UploadID       string    `json:"uploadId"`
	FileName       string    `json:"fileName"`
	TotalChunks    int       `json:"totalChunks"`
	UploadedChunks int       `json:"uploadedChunks"`
	Complete       bool      `json:"complete"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type ExcelMatchPreviewResult struct {
	Config      ExcelMatchConfig          `json:"config"`
	Stats       ExcelMatchJobStats        `json:"stats"`
	ScanLimit   int                       `json:"scanLimit"`
	SampleLimit int                       `json:"sampleLimit"`
	Truncated   bool                      `json:"truncated"`
	Samples     []ExcelMatchPreviewSample `json:"samples"`
}

type ExcelMatchPreviewSample struct {
	RowNumber    int                           `json:"rowNumber"`
	MatchKey     string                        `json:"matchKey"`
	MatchedValue string                        `json:"matchedValue"`
	Status       string                        `json:"status"`
	Reason       string                        `json:"reason"`
	Values       map[string]string             `json:"values"`
	StepResults  []ExcelMatchPreviewStepResult `json:"stepResults"`
}

type ExcelMatchPreviewStepResult struct {
	StepIndex    int    `json:"stepIndex"`
	StepName     string `json:"stepName"`
	MatchKey     string `json:"matchKey"`
	MatchedValue string `json:"matchedValue"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
}

type ExcelMatchScheme struct {
	ID         uint             `json:"id"`
	Name       string           `json:"name"`
	Operation  string           `json:"operation"`
	Config     ExcelMatchConfig `json:"config"`
	ConfigJSON string           `json:"config_json"`
	CreatedAt  int              `json:"created_at"`
	UpdatedAt  int              `json:"updated_at"`
}

type ExcelMatchJobStats = data_dao.ExcelMatchJobStats

type ExcelMatchLookup interface {
	Lookup(ctx context.Context, step ExcelMatchStep, keys []string) (map[string]string, error)
}

type ExcelMatchSchemaValidator interface {
	ValidateTableColumns(ctx context.Context, tableName string, columns []string) error
}

type ExcelImportUpdater interface {
	FindWritableKeys(
		ctx context.Context,
		matchField string,
		writeField string,
		onlyEmpty bool,
		allowPendingToShop bool,
		keys []string,
	) (map[string]struct{}, error)
	BatchUpdateFieldsByKeys(ctx context.Context, matchField string, valuesByField map[string]map[string]string, clearMatchedDocNo bool) (int64, error)
}

type databaseExcelMatchLookup struct {
	dao *data_dao.ExcelMatchJobDAO
}

func (l databaseExcelMatchLookup) Lookup(ctx context.Context, step ExcelMatchStep, keys []string) (map[string]string, error) {
	return l.dao.FindTableFieldByKeys(ctx, step.TableName, step.DBMatchField, keys, step.DBValueField)
}

type bojunExcelImportUpdater struct {
	dao *data_dao.ExcelMatchJobDAO
}

func (u bojunExcelImportUpdater) FindWritableKeys(
	ctx context.Context,
	matchField string,
	writeField string,
	onlyEmpty bool,
	allowPendingToShop bool,
	keys []string,
) (map[string]struct{}, error) {
	return u.dao.FindWritableBojunKeysForBatch(ctx, matchField, writeField, onlyEmpty, allowPendingToShop, keys)
}

func (u bojunExcelImportUpdater) BatchUpdateFieldsByKeys(
	ctx context.Context,
	matchField string,
	valuesByField map[string]map[string]string,
	clearMatchedDocNo bool,
) (int64, error) {
	return u.dao.BatchUpdateBojunFieldsByKeysWithMode(ctx, matchField, valuesByField, clearMatchedDocNo)
}

type excelJobSource struct {
	fileName string
	save     func(dst string) error
}

func (s *ExcelMatchJobService) CreateJob(ctx context.Context, fileHeader *multipart.FileHeader, rawConfig string) (*model.ExcelMatchJob, error) {
	if fileHeader == nil {
		return nil, errors.New("上传文件不能为空")
	}
	if strings.ToLower(filepath.Ext(fileHeader.Filename)) != ".xlsx" {
		return nil, errors.New("仅支持 .xlsx 文件")
	}
	return s.createJobFromSource(ctx, excelJobSource{
		fileName: filepath.Base(fileHeader.Filename),
		save: func(dst string) error {
			return saveUploadedExcel(fileHeader, dst)
		},
	}, rawConfig)
}

func (s *ExcelMatchJobService) CreateJobFromUpload(ctx context.Context, uploadID, rawConfig string) (*model.ExcelMatchJob, error) {
	meta, sourcePath, err := s.requireCompletedUpload(uploadID)
	if err != nil {
		return nil, err
	}
	return s.createJobFromSource(ctx, excelJobSource{
		fileName: meta.FileName,
		save: func(dst string) error {
			return copyFile(sourcePath, dst)
		},
	}, rawConfig)
}

func (s *ExcelMatchJobService) createJobFromSource(ctx context.Context, source excelJobSource, rawConfig string) (*model.ExcelMatchJob, error) {
	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return nil, fmt.Errorf("解析匹配配置失败: %w", err)
	}
	normalizedConfig, err := normalizeExcelMatchConfig(config)
	if err != nil {
		return nil, err
	}
	if normalizedConfig.Operation == excelOperationExportMatch {
		if err := validateExcelExportSteps(ctx, normalizedConfig, s.jobDAO); err != nil {
			return nil, err
		}
	}
	configBytes, err := json.Marshal(normalizedConfig)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(excelMatchRetention)
	matchJob := &model.ExcelMatchJob{
		SourceFileName: filepath.Base(source.fileName),
		ConfigJSON:     string(configBytes),
		Status:         excelMatchStatusPending,
		ExpiresAt:      &model.TimeNormal{Time: expiresAt},
	}
	if _, err := s.jobDAO.Create(ctx, matchJob); err != nil {
		return nil, err
	}
	s.logJob(ctx, matchJob.ID, "info", "Excel任务已创建", map[string]interface{}{
		"operation": normalizedConfig.Operation,
		"sheet":     normalizedConfig.SheetName,
		"file":      matchJob.SourceFileName,
	})

	workDir := excelMatchJobDir(matchJob.ID)
	sourcePath := filepath.Join(workDir, excelMatchSourceFileName)
	resultPath := filepath.Join(workDir, excelMatchResultFileName)
	if err := os.MkdirAll(workDir, 0700); err != nil {
		_ = s.jobDAO.MarkFailed(ctx, matchJob.ID, err.Error(), expiresAt)
		s.logJob(ctx, matchJob.ID, "error", "创建任务临时目录失败", map[string]interface{}{"error": err.Error()})
		return nil, err
	}
	if err := source.save(sourcePath); err != nil {
		_ = s.jobDAO.MarkFailed(ctx, matchJob.ID, err.Error(), expiresAt)
		_ = os.RemoveAll(workDir)
		s.logJob(ctx, matchJob.ID, "error", "保存上传Excel失败", map[string]interface{}{"error": err.Error()})
		return nil, err
	}
	s.logJob(ctx, matchJob.ID, "info", "上传Excel已保存到临时目录", map[string]interface{}{"source_file_name": matchJob.SourceFileName})
	if err := s.jobDAO.UpdatePaths(ctx, matchJob.ID, workDir, sourcePath, resultPath); err != nil {
		_ = s.jobDAO.MarkFailed(ctx, matchJob.ID, err.Error(), expiresAt)
		_ = os.RemoveAll(workDir)
		s.logJob(ctx, matchJob.ID, "error", "更新任务文件路径失败", map[string]interface{}{"error": err.Error()})
		return nil, err
	}

	task, err := job.NewExcelMatchExportTask(matchJob.ID)
	if err != nil {
		_ = s.jobDAO.MarkFailed(ctx, matchJob.ID, err.Error(), expiresAt)
		s.logJob(ctx, matchJob.ID, "error", "创建异步任务失败", map[string]interface{}{"error": err.Error()})
		return nil, err
	}
	if global.QueueJobClient == nil {
		err := errors.New("异步任务客户端未初始化")
		_ = s.jobDAO.MarkFailed(ctx, matchJob.ID, err.Error(), expiresAt)
		s.logJob(ctx, matchJob.ID, "error", "异步任务客户端未初始化", nil)
		return nil, err
	}
	if _, err := global.QueueJobClient.Enqueue(task, asynq.MaxRetry(1)); err != nil {
		_ = s.jobDAO.MarkFailed(ctx, matchJob.ID, err.Error(), expiresAt)
		s.logJob(ctx, matchJob.ID, "error", "投递异步任务失败", map[string]interface{}{"error": err.Error()})
		return nil, err
	}
	s.logJob(ctx, matchJob.ID, "info", "异步任务已投递", nil)

	return s.jobDAO.FindByID(ctx, matchJob.ID)
}

func (s *ExcelMatchJobService) GetJob(ctx context.Context, id uint) (*model.ExcelMatchJob, error) {
	matchJob, err := s.jobDAO.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	refreshExpiredJob(matchJob)
	s.refreshDownloadState(ctx, matchJob)
	return matchJob, nil
}

func (s *ExcelMatchJobService) GetJobLogs(ctx context.Context, id uint) ([]model.ExcelMatchJobLog, error) {
	return s.jobDAO.FindLogsByJobID(ctx, id, 200)
}

func (s *ExcelMatchJobService) ListJobs(ctx context.Context, limit int) ([]model.ExcelMatchJob, error) {
	jobs, err := s.jobDAO.ListJobs(ctx, limit)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		refreshExpiredJob(&jobs[i])
		s.refreshDownloadState(ctx, &jobs[i])
	}
	return jobs, nil
}

func (s *ExcelMatchJobService) ListJobsPage(ctx context.Context, query data_dao.ExcelMatchJobListQuery) (*data_dao.ExcelMatchJobListPage, error) {
	result, err := s.jobDAO.ListJobsPage(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range result.List {
		refreshExpiredJob(&result.List[i])
		s.refreshDownloadState(ctx, &result.List[i])
	}
	return result, nil
}

func (s *ExcelMatchJobService) ListSchemes(ctx context.Context, operation string) ([]ExcelMatchScheme, error) {
	operation = strings.TrimSpace(operation)
	if operation != "" && operation != excelOperationExportMatch && operation != excelOperationImportUpdate {
		return nil, fmt.Errorf("暂不支持方案类型: %s", operation)
	}
	rows, err := s.jobDAO.ListSchemes(ctx, operation)
	if err != nil {
		return nil, err
	}
	schemes := make([]ExcelMatchScheme, 0, len(rows))
	for _, row := range rows {
		scheme, err := excelMatchSchemeFromModel(row)
		if err != nil {
			return nil, err
		}
		schemes = append(schemes, scheme)
	}
	return schemes, nil
}

func (s *ExcelMatchJobService) SaveScheme(ctx context.Context, name, operation, rawConfig string) (*ExcelMatchScheme, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("方案名称不能为空")
	}
	if len([]rune(name)) > 100 {
		return nil, errors.New("方案名称不能超过 100 个字符")
	}
	operation = strings.TrimSpace(operation)
	if operation != excelOperationExportMatch && operation != excelOperationImportUpdate {
		return nil, fmt.Errorf("暂不支持方案类型: %s", operation)
	}
	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return nil, fmt.Errorf("解析方案配置失败: %w", err)
	}
	config.Operation = operation
	normalizedConfig, err := normalizeExcelMatchConfig(config)
	if err != nil {
		return nil, err
	}
	if normalizedConfig.Operation == excelOperationExportMatch {
		if err := validateExcelExportSteps(ctx, normalizedConfig, s.jobDAO); err != nil {
			return nil, err
		}
	}
	configBytes, err := json.Marshal(normalizedConfig)
	if err != nil {
		return nil, err
	}
	row := &model.ExcelMatchScheme{
		Name:       name,
		Operation:  operation,
		ConfigJSON: string(configBytes),
	}
	if err := s.jobDAO.SaveScheme(ctx, row); err != nil {
		return nil, err
	}
	rows, err := s.jobDAO.ListSchemes(ctx, operation)
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		if item.Name == name {
			scheme, err := excelMatchSchemeFromModel(item)
			if err != nil {
				return nil, err
			}
			return &scheme, nil
		}
	}
	scheme, err := excelMatchSchemeFromModel(*row)
	if err != nil {
		return nil, err
	}
	return &scheme, nil
}

func (s *ExcelMatchJobService) DeleteScheme(ctx context.Context, id uint) error {
	if id == 0 {
		return errors.New("方案ID不能为空")
	}
	return s.jobDAO.DeleteScheme(ctx, id)
}

func (s *ExcelMatchJobService) CreateUploadSession(ctx context.Context, fileName string, totalChunks int) (*ExcelUploadSession, error) {
	if strings.ToLower(filepath.Ext(fileName)) != ".xlsx" {
		return nil, errors.New("仅支持 .xlsx 文件")
	}
	if totalChunks <= 0 || totalChunks > maxExcelUploadChunks {
		return nil, fmt.Errorf("分片数量必须在 1 到 %d 之间", maxExcelUploadChunks)
	}
	uploadID, err := newExcelUploadID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	meta := excelUploadMeta{
		UploadID:       uploadID,
		FileName:       filepath.Base(fileName),
		TotalChunks:    totalChunks,
		UploadedChunks: 0,
		Complete:       false,
		CreatedAt:      now,
		ExpiresAt:      now.Add(excelMatchRetention),
	}
	if err := os.MkdirAll(excelUploadChunksDir(uploadID), 0700); err != nil {
		return nil, err
	}
	if err := writeExcelUploadMeta(uploadID, meta); err != nil {
		_ = os.RemoveAll(excelUploadDir(uploadID))
		return nil, err
	}
	_ = ctx
	return excelUploadSessionFromMeta(meta), nil
}

func (s *ExcelMatchJobService) SaveUploadChunk(ctx context.Context, uploadID string, index int, totalChunks int, fileHeader *multipart.FileHeader) (*ExcelUploadSession, error) {
	if fileHeader == nil {
		return nil, errors.New("分片文件不能为空")
	}
	if fileHeader.Size > maxExcelUploadChunkBytes {
		return nil, fmt.Errorf("单个分片不能超过 %d MB", maxExcelUploadChunkBytes/(1024*1024))
	}
	meta, err := readExcelUploadMeta(uploadID)
	if err != nil {
		return nil, err
	}
	if time.Now().After(meta.ExpiresAt) {
		_ = os.RemoveAll(excelUploadDir(uploadID))
		return nil, errors.New("上传会话已过期")
	}
	if totalChunks != meta.TotalChunks {
		return nil, errors.New("分片总数与上传会话不一致")
	}
	if index < 0 || index >= meta.TotalChunks {
		return nil, errors.New("分片序号超出范围")
	}

	dst := filepath.Join(excelUploadChunksDir(uploadID), excelUploadChunkName(index))
	if !isPathInside(excelUploadDir(uploadID), dst) {
		return nil, errors.New("分片路径非法")
	}
	if err := saveUploadedExcel(fileHeader, dst); err != nil {
		return nil, err
	}
	uploaded, err := countExcelUploadChunks(uploadID, meta.TotalChunks)
	if err != nil {
		return nil, err
	}
	meta.UploadedChunks = uploaded
	if err := writeExcelUploadMeta(uploadID, meta); err != nil {
		return nil, err
	}
	_ = ctx
	return excelUploadSessionFromMeta(meta), nil
}

func (s *ExcelMatchJobService) CompleteUpload(ctx context.Context, uploadID string, totalChunks int) (*ExcelUploadSession, error) {
	meta, err := readExcelUploadMeta(uploadID)
	if err != nil {
		return nil, err
	}
	if time.Now().After(meta.ExpiresAt) {
		_ = os.RemoveAll(excelUploadDir(uploadID))
		return nil, errors.New("上传会话已过期")
	}
	if totalChunks != meta.TotalChunks {
		return nil, errors.New("分片总数与上传会话不一致")
	}
	if err := assembleExcelUpload(uploadID, meta.TotalChunks); err != nil {
		return nil, err
	}
	meta.Complete = true
	meta.UploadedChunks = meta.TotalChunks
	if err := writeExcelUploadMeta(uploadID, meta); err != nil {
		return nil, err
	}
	_ = ctx
	return excelUploadSessionFromMeta(meta), nil
}

func (s *ExcelMatchJobService) Preview(ctx context.Context, fileHeader *multipart.FileHeader, rawConfig string) (*ExcelMatchPreviewResult, error) {
	if fileHeader == nil {
		return nil, errors.New("上传文件不能为空")
	}
	if strings.ToLower(filepath.Ext(fileHeader.Filename)) != ".xlsx" {
		return nil, errors.New("仅支持 .xlsx 文件")
	}
	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return nil, fmt.Errorf("解析匹配配置失败: %w", err)
	}
	config.Operation = excelOperationExportMatch
	normalizedConfig, err := normalizeExcelMatchConfig(config)
	if err != nil {
		return nil, err
	}
	if err := validateExcelExportSteps(ctx, normalizedConfig, s.jobDAO); err != nil {
		return nil, err
	}

	src, err := fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	lookup := databaseExcelMatchLookup{dao: s.jobDAO}
	var result *ExcelMatchPreviewResult
	previewID, err := newExcelUploadID()
	if err != nil {
		return nil, err
	}
	tempDir := filepath.Join(excelPreviewRootDir(), previewID, excelizeTempDirName)
	defer func() { _ = os.RemoveAll(filepath.Dir(tempDir)) }()
	err = withExcelizeTempDir(tempDir, func() error {
		input, err := excelize.OpenReader(src)
		if err != nil {
			return err
		}
		defer func() { _ = input.Close() }()
		result, err = processExcelMatchPreview(ctx, input, normalizedConfig, lookup, defaultExcelPreviewRows, defaultExcelPreviewItems)
		return err
	})
	return result, err
}

func (s *ExcelMatchJobService) PreviewUploaded(ctx context.Context, uploadID, rawConfig string) (*ExcelMatchPreviewResult, error) {
	_, sourcePath, err := s.requireCompletedUpload(uploadID)
	if err != nil {
		return nil, err
	}
	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return nil, fmt.Errorf("解析匹配配置失败: %w", err)
	}
	config.Operation = excelOperationExportMatch
	normalizedConfig, err := normalizeExcelMatchConfig(config)
	if err != nil {
		return nil, err
	}
	if err := validateExcelExportSteps(ctx, normalizedConfig, s.jobDAO); err != nil {
		return nil, err
	}
	lookup := databaseExcelMatchLookup{dao: s.jobDAO}
	var result *ExcelMatchPreviewResult
	err = withExcelizeTempDir(excelizeTempDirForPath(sourcePath), func() error {
		input, err := excelize.OpenFile(sourcePath)
		if err != nil {
			return err
		}
		defer func() { _ = input.Close() }()
		result, err = processExcelMatchPreview(ctx, input, normalizedConfig, lookup, defaultExcelPreviewRows, defaultExcelPreviewItems)
		return err
	})
	return result, err
}

func (s *ExcelMatchJobService) Download(ctx context.Context, id uint) (*model.ExcelMatchJob, string, string, error) {
	matchJob, err := s.GetJob(ctx, id)
	if err != nil {
		return nil, "", "", err
	}
	if matchJob.Status != excelMatchStatusSuccess {
		return nil, "", "", fmt.Errorf("任务状态为 %s，无法下载", matchJob.Status)
	}
	if excelJobOperationFromConfigJSON(matchJob.ConfigJSON) != excelOperationExportMatch {
		return nil, "", "", errors.New("该任务不是匹配导出任务，不会生成结果文件")
	}
	if strings.TrimSpace(matchJob.ResultURL) != "" {
		return matchJob, "", fmt.Sprintf("excel_match_job_%d.xlsx", matchJob.ID), nil
	}
	if !isPathInside(excelMatchJobDir(matchJob.ID), matchJob.ResultFilePath) {
		return nil, "", "", errors.New("结果文件路径非法")
	}
	if _, err := os.Stat(matchJob.ResultFilePath); err != nil {
		if os.IsNotExist(err) {
			if s.jobDAO != nil {
				_ = s.jobDAO.MarkExpired(ctx, matchJob.ID)
			}
			s.logJob(ctx, matchJob.ID, "warn", "结果文件不存在，任务已标记为过期", nil)
			return nil, "", "", errors.New("结果文件不存在或已被清理，请重新创建匹配导出任务")
		}
		return nil, "", "", errors.New("读取结果文件失败，请稍后重试")
	}
	return nil, "", "", errors.New("结果文件尚未上传到OSS，上传成功后才能下载，请稍后刷新任务状态")
}

func (s *ExcelMatchJobService) ProcessJob(ctx context.Context, id uint) error {
	matchJob, err := s.jobDAO.FindByID(ctx, id)
	if err != nil {
		return err
	}

	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(matchJob.ConfigJSON), &config); err != nil {
		_ = s.jobDAO.MarkFailed(ctx, id, err.Error(), time.Now().Add(excelMatchRetention))
		return err
	}
	config, err = normalizeExcelMatchConfig(config)
	if err != nil {
		_ = s.jobDAO.MarkFailed(ctx, id, err.Error(), time.Now().Add(excelMatchRetention))
		s.logJob(ctx, id, "error", "任务配置校验失败", map[string]interface{}{"error": err.Error()})
		return err
	}
	if config.Operation == excelOperationExportMatch {
		if err := validateExcelExportSteps(ctx, config, s.jobDAO); err != nil {
			_ = s.jobDAO.MarkFailed(ctx, id, err.Error(), time.Now().Add(excelMatchRetention))
			s.logJob(ctx, id, "error", "任务数据库字段校验失败", map[string]interface{}{"error": err.Error()})
			return err
		}
	}

	claimed, err := s.jobDAO.MarkRunning(ctx, id, time.Now().Add(-excelMatchRunningStaleAfter))
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("%w: Excel任务已过期、已完成或正在由其他Worker处理", asynq.SkipRetry)
	}
	s.logJob(ctx, id, "info", "任务开始处理", map[string]interface{}{
		"operation": config.Operation,
		"sheet":     config.SheetName,
	})

	var stats ExcelMatchJobStats
	excelTempDir := filepath.Join(matchJob.WorkDir, excelizeTempDirName)
	s.logJob(ctx, id, "info", "Excel临时目录已设置", map[string]interface{}{"temp_dir": excelTempDir})
	err = withExcelizeTempDir(excelTempDir, func() error {
		if config.Operation == excelOperationImportUpdate || config.Operation == excelOperationClearMatched {
			updater := bojunExcelImportUpdater{dao: s.jobDAO}
			var processErr error
			stats, processErr = processExcelImportUpdateFileWithProgress(ctx, matchJob.SourceFilePath, config, updater, func(stats ExcelMatchJobStats) {
				_ = s.jobDAO.UpdateProgress(ctx, id, stats)
				s.logJob(ctx, id, "info", "导入匹配进度", statsDetail(stats))
			})
			return processErr
		}
		lookup := databaseExcelMatchLookup{dao: s.jobDAO}
		var processErr error
		stats, processErr = processExcelMatchFileWithProgress(ctx, matchJob.SourceFilePath, matchJob.ResultFilePath, config, lookup, func(stats ExcelMatchJobStats) {
			_ = s.jobDAO.UpdateProgress(ctx, id, stats)
			s.logJob(ctx, id, "info", "匹配导出进度", statsDetail(stats))
		})
		return processErr
	})
	expiresAt := time.Now().Add(excelMatchRetention)
	if err != nil {
		_ = s.jobDAO.MarkFailed(ctx, id, err.Error(), expiresAt)
		s.logJob(ctx, id, "error", "任务处理失败", map[string]interface{}{"error": err.Error()})
		return err
	}
	_ = os.Remove(matchJob.SourceFilePath)
	s.logJob(ctx, id, "info", "源文件已删除", nil)

	if config.Operation == excelOperationExportMatch && storage.OSSStorageEnabled() {
		fileSize := fileSizeOrZero(matchJob.ResultFilePath)
		uploadTimeout := excelMatchOSSUploadTimeout(fileSize)
		objectKey := strings.TrimSpace(matchJob.ResultObjectKey)
		if objectKey == "" {
			objectKey = excelMatchResultObjectKey(id, time.Now())
		}
		if !validExcelMatchResultObjectKey(objectKey, id, storage.BuildObjectKey("excel-match-results")) {
			err := errors.New("OSS结果文件Key非法")
			_ = s.jobDAO.MarkFailed(ctx, id, err.Error(), expiresAt)
			s.logJob(ctx, id, "error", "OSS结果文件Key校验失败", map[string]interface{}{"object_key": objectKey})
			return err
		}
		if err := s.jobDAO.UpdateResultStorage(ctx, id, objectKey, ""); err != nil {
			_ = s.jobDAO.MarkFailed(ctx, id, err.Error(), expiresAt)
			s.logJob(ctx, id, "error", "上传前保存OSS结果文件Key失败", map[string]interface{}{
				"object_key": objectKey,
				"error":      err.Error(),
			})
			return err
		}
		matchJob.ResultObjectKey = objectKey
		matchJob.ResultURL = ""
		s.logJob(ctx, id, "info", "Excel匹配完成，开始上传结果文件到OSS", map[string]interface{}{
			"object_key":      objectKey,
			"file_size":       fileSize,
			"timeout_seconds": int(uploadTimeout.Seconds()),
		})
		result, err := s.uploadExcelResultToOSS(ctx, matchJob, objectKey, uploadTimeout)
		if err != nil {
			errorMessage := fmt.Sprintf("上传结果文件到OSS失败: %v", err)
			_ = s.jobDAO.MarkFailed(ctx, id, errorMessage, expiresAt)
			s.logJob(ctx, id, "error", "上传结果文件到OSS失败", map[string]interface{}{
				"object_key":      objectKey,
				"file_size":       fileSize,
				"timeout_seconds": int(uploadTimeout.Seconds()),
				"error":           err.Error(),
			})
			return nil
		}
		s.logJob(ctx, id, "info", "结果文件已上传OSS", map[string]interface{}{
			"object_key": result.ObjectKey,
			"result_url": result.URL,
			"file_size":  fileSize,
		})
		_ = os.Remove(matchJob.ResultFilePath)
	}
	if err := s.jobDAO.MarkSuccess(ctx, id, stats, expiresAt); err != nil {
		s.logJob(ctx, id, "error", "更新任务成功状态失败", map[string]interface{}{"error": err.Error()})
		return err
	}
	s.logJob(ctx, id, "info", "任务处理成功", statsDetail(stats))
	return nil
}

func (s *ExcelMatchJobService) CleanupExpiredJobs(ctx context.Context) error {
	if s == nil || s.jobDAO == nil || ctx == nil {
		return errors.New("excel match cleanup: invalid service")
	}
	cleaner := newExcelMatchJobCleaner(s.jobDAO, s.logJob)
	_, cleanupErr := cleaner.Cleanup(ctx)
	uploadErr := s.CleanupExpiredUploads(ctx)
	return errors.Join(cleanupErr, uploadErr)
}

func (s *ExcelMatchJobService) CleanupExpiredUploads(ctx context.Context) error {
	root := excelUploadRootDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	now := time.Now()
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !isValidExcelUploadID(entry.Name()) {
			continue
		}
		meta, err := readExcelUploadMeta(entry.Name())
		if err != nil || now.After(meta.ExpiresAt) {
			_ = os.RemoveAll(excelUploadDir(entry.Name()))
		}
	}
	return nil
}

func (s *ExcelMatchJobService) requireCompletedUpload(uploadID string) (excelUploadMeta, string, error) {
	meta, err := readExcelUploadMeta(uploadID)
	if err != nil {
		return excelUploadMeta{}, "", err
	}
	if time.Now().After(meta.ExpiresAt) {
		_ = os.RemoveAll(excelUploadDir(uploadID))
		return excelUploadMeta{}, "", errors.New("上传会话已过期")
	}
	if !meta.Complete {
		return excelUploadMeta{}, "", errors.New("上传会话尚未合并完成")
	}
	sourcePath := filepath.Join(excelUploadDir(uploadID), excelUploadMergedFile)
	if !isPathInside(excelUploadDir(uploadID), sourcePath) {
		return excelUploadMeta{}, "", errors.New("上传文件路径非法")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return excelUploadMeta{}, "", err
	}
	return meta, sourcePath, nil
}

func normalizeExcelMatchConfig(config ExcelMatchConfig) (ExcelMatchConfig, error) {
	config.Operation = strings.TrimSpace(config.Operation)
	config.SheetName = strings.TrimSpace(config.SheetName)
	config.MatchExcelColumn = strings.TrimSpace(config.MatchExcelColumn)
	config.DBTemplate = strings.TrimSpace(config.DBTemplate)
	config.DBMatchField = strings.TrimSpace(config.DBMatchField)
	config.DBValueField = strings.TrimSpace(config.DBValueField)
	config.TableName = strings.TrimSpace(config.TableName)
	config.DBWriteField = strings.TrimSpace(config.DBWriteField)
	config.WriteExcelColumn = strings.TrimSpace(config.WriteExcelColumn)
	config.OutputColumnName = strings.TrimSpace(config.OutputColumnName)

	if config.Operation == "" {
		config.Operation = excelOperationExportMatch
	}
	if config.SheetName == "" {
		config.SheetName = defaultExcelSheetName
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultExcelMatchBatch
	}
	if config.BatchSize < 500 || config.BatchSize > 2000 {
		return config, errors.New("batchSize 必须在 500 到 2000 之间")
	}

	switch config.Operation {
	case excelOperationExportMatch:
		return normalizeExcelExportConfig(config)
	case excelOperationImportUpdate, excelOperationClearMatched:
		return normalizeExcelImportConfig(config)
	default:
		return config, fmt.Errorf("暂不支持Excel任务操作: %s", config.Operation)
	}
}

func normalizeExcelExportConfig(config ExcelMatchConfig) (ExcelMatchConfig, error) {
	legacyFilters, err := normalizeExcelMatchFilters(config.Filters, "顶层")
	if err != nil {
		return config, err
	}
	config.Filters = nil

	if len(config.Steps) == 0 {
		if config.MatchExcelColumn == "" {
			return config, errors.New("Excel 订单号列不能为空")
		}
		if config.DBTemplate != bojunRetailOrderTemplate {
			return config, fmt.Errorf("暂不支持数据库模板: %s", config.DBTemplate)
		}
		if config.DBMatchField == "" {
			config.DBMatchField = "docno"
		}
		if _, ok := allowedBojunImportMatchFields[config.DBMatchField]; !ok {
			return config, fmt.Errorf("伯俊匹配字段不在白名单: %s", config.DBMatchField)
		}
		if _, ok := allowedBojunValueFields[config.DBValueField]; !ok {
			return config, fmt.Errorf("伯俊取值字段不在白名单: %s", config.DBValueField)
		}
		if config.OutputColumnName == "" {
			return config, errors.New("输出列名不能为空")
		}
		config.Steps = []ExcelMatchStep{{
			Name:             "步骤 1",
			TableName:        bojunRetailOrdersTable,
			MatchExcelColumn: config.MatchExcelColumn,
			DBMatchField:     config.DBMatchField,
			DBValueField:     config.DBValueField,
			OutputColumnName: config.OutputColumnName,
		}}
	}
	if len(config.Steps) > maxExcelMatchSteps {
		return config, fmt.Errorf("匹配步骤不能超过 %d 个", maxExcelMatchSteps)
	}
	seenOutputs := make(map[string]struct{}, len(config.Steps))
	for i := range config.Steps {
		step := &config.Steps[i]
		step.Name = strings.TrimSpace(step.Name)
		step.MatchMode = strings.ToLower(strings.TrimSpace(step.MatchMode))
		step.TableName = strings.TrimSpace(step.TableName)
		step.MatchExcelColumn = strings.TrimSpace(step.MatchExcelColumn)
		step.SpecExcelColumn = strings.TrimSpace(step.SpecExcelColumn)
		step.PriceExcelColumn = strings.TrimSpace(step.PriceExcelColumn)
		step.QtyExcelColumn = strings.TrimSpace(step.QtyExcelColumn)
		step.DBMatchField = strings.TrimSpace(step.DBMatchField)
		step.DBValueField = strings.TrimSpace(step.DBValueField)
		step.OutputColumnName = strings.TrimSpace(step.OutputColumnName)
		step.ContainsValue = strings.TrimSpace(step.ContainsValue)
		if step.Name == "" {
			step.Name = fmt.Sprintf("步骤 %d", i+1)
		}
		if step.MatchMode == "" {
			step.MatchMode = excelMatchModeField
		}
		switch step.MatchMode {
		case excelMatchModeConditionalWrite:
			if step.MatchExcelColumn == "" || step.ContainsValue == "" || step.OutputColumnName == "" {
				return config, fmt.Errorf("第 %d 个条件写入步骤必须配置判断列、包含文本和目标列", i+1)
			}
		case excelMatchModeField, excelMatchModeOrderItemSKU:
			missingSource := step.TableName == "" || step.MatchExcelColumn == ""
			missingFields := step.DBMatchField == "" || step.DBValueField == "" || step.OutputColumnName == ""
			if missingSource || missingFields {
				return config, fmt.Errorf("第 %d 个匹配步骤配置不完整", i+1)
			}
		default:
			return config, fmt.Errorf("第 %d 个匹配步骤模式不支持: %s", i+1, step.MatchMode)
		}
		if step.MatchMode == excelMatchModeOrderItemSKU && (step.SpecExcelColumn == "" || step.PriceExcelColumn == "" || step.QtyExcelColumn == "") {
			return config, fmt.Errorf("第 %d 个订单商品SKU匹配步骤必须配置规格编码、价格和销售数量列", i+1)
		}
		step.Filters, err = normalizeExcelMatchFilters(step.Filters, fmt.Sprintf("第 %d 个匹配步骤", i+1))
		if err != nil {
			return config, err
		}
		if _, exists := seenOutputs[step.OutputColumnName]; exists && step.MatchMode != excelMatchModeConditionalWrite {
			return config, fmt.Errorf("匹配步骤输出列名重复: %s", step.OutputColumnName)
		}
		seenOutputs[step.OutputColumnName] = struct{}{}
	}
	if len(legacyFilters) > 0 {
		config.Steps[0].Filters = append(legacyFilters, config.Steps[0].Filters...)
	}
	normalizedFormats := make([]ExcelExportColumnFormat, 0, len(config.ExportColumnFormats))
	seenFormatColumns := map[string]struct{}{}
	for _, item := range config.ExportColumnFormats {
		column := strings.TrimSpace(item.Column)
		format := strings.ToLower(strings.TrimSpace(item.Format))
		if column == "" && format == "" {
			continue
		}
		if column == "" {
			return config, errors.New("导出格式配置列名不能为空")
		}
		if _, ok := seenFormatColumns[column]; ok {
			return config, fmt.Errorf("导出格式配置列名重复: %s", column)
		}
		seenFormatColumns[column] = struct{}{}
		if format == "" {
			format = "text"
		}
		if _, ok := allowedExcelExportColumnFormats[format]; !ok {
			return config, fmt.Errorf("导出格式配置不支持: %s", format)
		}
		normalizedFormats = append(normalizedFormats, ExcelExportColumnFormat{
			Column: column,
			Format: format,
		})
	}
	config.ExportColumnFormats = normalizedFormats
	config.EmptyCellFills, err = normalizeExcelEmptyCellFills(config.EmptyCellFills)
	if err != nil {
		return config, err
	}

	return config, nil
}

func normalizeExcelEmptyCellFills(fills []ExcelEmptyCellFill) ([]ExcelEmptyCellFill, error) {
	normalized := make([]ExcelEmptyCellFill, 0, len(fills))
	seenTargets := make(map[string]struct{}, len(fills))
	for _, fill := range fills {
		fill.TargetColumn = strings.TrimSpace(fill.TargetColumn)
		fill.SourceColumn = strings.TrimSpace(fill.SourceColumn)
		if fill.TargetColumn == "" && fill.SourceColumn == "" {
			continue
		}
		if fill.TargetColumn == "" || fill.SourceColumn == "" {
			return nil, errors.New("空值填充的目标列和来源列不能为空")
		}
		if fill.TargetColumn == fill.SourceColumn {
			return nil, fmt.Errorf("空值填充的目标列和来源列不能相同: %s", fill.TargetColumn)
		}
		if _, exists := seenTargets[fill.TargetColumn]; exists {
			return nil, fmt.Errorf("空值填充目标列重复: %s", fill.TargetColumn)
		}
		seenTargets[fill.TargetColumn] = struct{}{}
		normalized = append(normalized, fill)
	}
	return normalized, nil
}

func normalizeExcelMatchFilters(filters []ExcelMatchFilter, scope string) ([]ExcelMatchFilter, error) {
	normalized := make([]ExcelMatchFilter, 0, len(filters))
	for _, filter := range filters {
		filter.Column = strings.TrimSpace(filter.Column)
		filter.Op = strings.ToLower(strings.TrimSpace(filter.Op))
		filter.Value = strings.TrimSpace(filter.Value)
		if filter.Column == "" && filter.Op == "" && filter.Value == "" {
			continue
		}
		if filter.Column == "" {
			return nil, fmt.Errorf("%s筛选列不能为空", scope)
		}
		if filter.Op == "" {
			filter.Op = "eq"
		}
		if _, ok := allowedExcelMatchFilterOps[filter.Op]; !ok {
			return nil, fmt.Errorf("%s暂不支持筛选操作: %s", scope, filter.Op)
		}
		normalized = append(normalized, filter)
	}
	return normalized, nil
}

func validateExcelExportSteps(ctx context.Context, config ExcelMatchConfig, validator ExcelMatchSchemaValidator) error {
	for i, step := range config.Steps {
		if step.MatchMode == excelMatchModeConditionalWrite {
			continue
		}
		if validator == nil {
			return errors.New("数据库结构校验器未初始化")
		}
		if err := validator.ValidateTableColumns(ctx, step.TableName, []string{step.DBMatchField, step.DBValueField}); err != nil {
			return fmt.Errorf("第 %d 个匹配步骤数据库字段校验失败: %w", i+1, err)
		}
	}
	return nil
}

func normalizeExcelImportConfig(config ExcelMatchConfig) (ExcelMatchConfig, error) {
	if config.TableName == "" {
		config.TableName = bojunRetailOrdersTable
	}
	if _, ok := allowedExcelImportTables[config.TableName]; !ok {
		return config, fmt.Errorf("导入匹配表不在白名单: %s", config.TableName)
	}
	config.TableName = bojunRetailOrdersTable
	if config.DBMatchField == "" {
		config.DBMatchField = "docno"
	}
	if _, ok := allowedBojunImportMatchFields[config.DBMatchField]; !ok {
		return config, fmt.Errorf("导入匹配字段不在白名单: %s", config.DBMatchField)
	}
	if config.MatchExcelColumn == "" {
		return config, errors.New("匹配Excel列名不能为空")
	}
	if config.Operation == excelOperationClearMatched {
		config.DBWriteField = "matched_docno"
		config.WriteExcelColumn = ""
		config.WriteMappings = []ExcelImportWriteMapping{{DBWriteField: "matched_docno"}}
	} else {
		mappings, err := normalizeExcelImportWriteMappings(config)
		if err != nil {
			return config, err
		}
		config.WriteMappings = mappings
		config.DBWriteField = mappings[0].DBWriteField
		config.WriteExcelColumn = mappings[0].WriteExcelColumn
	}
	config.DryRun = !config.ConfirmWrite
	return config, nil
}

func normalizeExcelImportWriteMappings(config ExcelMatchConfig) ([]ExcelImportWriteMapping, error) {
	mappings := config.WriteMappings
	if len(mappings) == 0 {
		writeField := config.DBWriteField
		if writeField == "" {
			writeField = "matched_docno"
		}
		mappings = []ExcelImportWriteMapping{{
			DBWriteField:     writeField,
			WriteExcelColumn: config.WriteExcelColumn,
		}}
	}
	if len(mappings) > len(allowedBojunImportWriteFields) {
		return nil, fmt.Errorf("导入写入映射不能超过 %d 个", len(allowedBojunImportWriteFields))
	}
	normalized := make([]ExcelImportWriteMapping, 0, len(mappings))
	seenFields := make(map[string]struct{}, len(mappings))
	hasOracleField := false
	for index, mapping := range mappings {
		mapping.DBWriteField = strings.TrimSpace(mapping.DBWriteField)
		mapping.WriteExcelColumn = strings.TrimSpace(mapping.WriteExcelColumn)
		if mapping.DBWriteField == "" || mapping.WriteExcelColumn == "" {
			return nil, fmt.Errorf("第 %d 个写入映射配置不完整", index+1)
		}
		if _, ok := allowedBojunImportWriteFields[mapping.DBWriteField]; !ok {
			return nil, fmt.Errorf("导入写入字段不在白名单: %s", mapping.DBWriteField)
		}
		if _, exists := seenFields[mapping.DBWriteField]; exists {
			return nil, fmt.Errorf("导入写入字段重复: %s", mapping.DBWriteField)
		}
		seenFields[mapping.DBWriteField] = struct{}{}
		_, oracleField := bojunOracleImportWriteFields[mapping.DBWriteField]
		hasOracleField = hasOracleField || oracleField
		normalized = append(normalized, mapping)
	}
	if hasOracleField && config.DBMatchField != "docno" {
		return nil, fmt.Errorf("伯俊 Oracle 字段导入必须使用唯一订单号 docno 匹配")
	}
	return normalized, nil
}

func processExcelImportUpdateFileWithProgress(
	ctx context.Context,
	inputPath string,
	config ExcelMatchConfig,
	updater ExcelImportUpdater,
	onProgress func(ExcelMatchJobStats),
) (ExcelMatchJobStats, error) {
	input, err := excelize.OpenFile(inputPath)
	if err != nil {
		return ExcelMatchJobStats{}, err
	}
	defer func() { _ = input.Close() }()

	sheets := input.GetSheetList()
	if len(sheets) == 0 {
		return ExcelMatchJobStats{}, errors.New("Excel 没有可读取的 sheet")
	}
	if !sheetExists(sheets, config.SheetName) {
		return ExcelMatchJobStats{}, fmt.Errorf("Excel 不存在 sheet: %s", config.SheetName)
	}
	rows, err := input.Rows(config.SheetName)
	if err != nil {
		return ExcelMatchJobStats{}, err
	}
	defer func() { _ = rows.Close() }()

	var headers []string
	columnIndexes := map[string]int{}
	matchColumnIndex := -1
	writeColumnIndexes := make(map[string]int, len(config.WriteMappings))
	stats := ExcelMatchJobStats{}

	type importRow struct {
		key    string
		values map[string]string
	}
	var buffered []importRow

	flush := func() error {
		if len(buffered) == 0 {
			return nil
		}
		uniqueValues := make(map[string]map[string]string, len(buffered))
		for _, row := range buffered {
			if previous, exists := uniqueValues[row.key]; exists {
				for field, value := range row.values {
					if previous[field] != value {
						return fmt.Errorf("Excel 匹配键 %q 的字段 %s 在同一批次存在冲突写入值", row.key, field)
					}
				}
				continue
			}
			uniqueValues[row.key] = row.values
		}
		keys := make([]string, 0, len(uniqueValues))
		for key := range uniqueValues {
			keys = append(keys, key)
		}
		writableByField := make(map[string]map[string]struct{}, len(config.WriteMappings))
		hasPendingToShop := false
		for _, mapping := range config.WriteMappings {
			hasPendingToShop = hasPendingToShop || mapping.DBWriteField == "is_to_shop"
		}
		for _, mapping := range config.WriteMappings {
			writable, err := updater.FindWritableKeys(
				ctx,
				config.DBMatchField,
				mapping.DBWriteField,
				config.Operation != excelOperationClearMatched,
				hasPendingToShop && mapping.DBWriteField == "oracle_retail_id",
				keys,
			)
			if err != nil {
				return err
			}
			writableByField[mapping.DBWriteField] = writable
		}
		updatesByField := make(map[string]map[string]string, len(config.WriteMappings))
		for _, row := range buffered {
			matched := false
			for field, value := range row.values {
				if _, ok := writableByField[field][row.key]; !ok {
					continue
				}
				updates := updatesByField[field]
				if updates == nil {
					updates = make(map[string]string)
					updatesByField[field] = updates
				}
				updates[row.key] = value
				matched = true
			}
			if matched {
				stats.MatchedRows++
			} else {
				stats.UnmatchedRows++
			}
		}
		if !config.DryRun && len(updatesByField) > 0 {
			if _, err := updater.BatchUpdateFieldsByKeys(ctx, config.DBMatchField, updatesByField, config.Operation == excelOperationClearMatched); err != nil {
				return err
			}
		}
		stats.ProcessedRows += len(buffered)
		if onProgress != nil {
			onProgress(stats)
		}
		buffered = buffered[:0]
		return nil
	}

	headerRead := false
	rowNumber := 0
	for rows.Next() {
		rowNumber++
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		columns, err := rows.Columns()
		if err != nil {
			return stats, err
		}
		if !headerRead {
			headers = normalizeHeaders(columns)
			if len(headers) == 0 {
				return stats, errors.New("Excel 表头不能为空")
			}
			for i, header := range headers {
				columnIndexes[header] = i
			}
			var ok bool
			matchColumnIndex, ok = columnIndexes[config.MatchExcelColumn]
			if !ok {
				return stats, fmt.Errorf("Excel 缺少匹配列: %s", config.MatchExcelColumn)
			}
			if config.Operation != excelOperationClearMatched {
				for _, mapping := range config.WriteMappings {
					writeColumnIndex, ok := columnIndexes[mapping.WriteExcelColumn]
					if !ok {
						return stats, fmt.Errorf("Excel 缺少写入值列: %s", mapping.WriteExcelColumn)
					}
					writeColumnIndexes[mapping.DBWriteField] = writeColumnIndex
				}
			}
			headerRead = true
			continue
		}

		stats.TotalRows++
		normalized := normalizeExcelRow(columns, len(headers))
		key := strings.TrimSpace(normalized[matchColumnIndex])
		if key == "" {
			stats.UnmatchedRows++
			stats.ProcessedRows++
			continue
		}
		values := make(map[string]string, len(config.WriteMappings))
		for _, mapping := range config.WriteMappings {
			value := ""
			if config.Operation != excelOperationClearMatched {
				value = strings.TrimSpace(normalized[writeColumnIndexes[mapping.DBWriteField]])
			}
			if mapping.DBWriteField == "order_phone" && value == "" {
				continue
			}
			value, err = normalizeExcelImportWriteValue(mapping.DBWriteField, value)
			if err != nil {
				return stats, fmt.Errorf("Excel 第 %d 行字段 %s 写入值无效: %w", rowNumber, mapping.DBWriteField, err)
			}
			values[mapping.DBWriteField] = value
		}
		buffered = append(buffered, importRow{key: key, values: values})
		if len(buffered) >= config.BatchSize {
			if err := flush(); err != nil {
				return stats, err
			}
		}
	}
	if err := rows.Error(); err != nil {
		return stats, err
	}
	if !headerRead {
		return stats, errors.New("Excel 表头不能为空")
	}
	if err := flush(); err != nil {
		return stats, err
	}
	return stats, nil
}

func normalizeExcelImportWriteValue(writeField, value string) (string, error) {
	switch writeField {
	case "matched_docno":
		return value, nil
	case "completed_at":
		const layout = "2006-01-02 15:04:05"
		location := time.FixedZone("Asia/Shanghai", 8*60*60)
		parsed, err := time.ParseInLocation(layout, value, location)
		if err != nil || parsed.Format(layout) != value {
			return "", errors.New("订单完成时间必须使用 yyyy-mm-dd hh:mm:ss 格式")
		}
		return parsed.Format(layout), nil
	case "oracle_retail_id":
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return "", errors.New("Oracle 零售单 ID 必须为正整数")
		}
		return strconv.FormatUint(parsed, 10), nil
	case "order_phone":
		if value == "" {
			return "", errors.New("订单手机号不能为空")
		}
		if utf8.RuneCountInString(value) > 64 {
			return "", errors.New("订单手机号不能超过 64 个字符")
		}
		return value, nil
	case "paid_amount", "push_amount":
		normalized, ok := normalizeBojunExcelDecimal(value)
		if !ok {
			return "", errors.New("金额必须为最多 16 位整数、2 位小数的有效数字")
		}
		return normalized, nil
	case "is_to_shop":
		normalized := strings.ToUpper(value)
		if normalized != "Y" && normalized != "N" {
			return "", errors.New("是否到店必须为 Y 或 N")
		}
		return normalized, nil
	default:
		return "", fmt.Errorf("暂不支持导入写入字段: %s", writeField)
	}
}

func normalizeBojunExcelDecimal(value string) (string, bool) {
	if strings.Contains(value, ",") {
		if !bojunExcelGroupedDecimalPattern.MatchString(value) {
			return "", false
		}
		value = strings.ReplaceAll(value, ",", "")
	}
	if !bojunExcelDecimalPattern.MatchString(value) {
		return "", false
	}
	return strings.TrimPrefix(value, "+"), true
}

func initExcelMatchWriter(output *excelize.File) (*excelize.StreamWriter, string, error) {
	defaultSheet := output.GetSheetName(0)
	if err := output.SetSheetName(defaultSheet, defaultExcelResultSheet); err != nil {
		return nil, "", err
	}
	writer, err := output.NewStreamWriter(defaultExcelResultSheet)
	return writer, defaultExcelResultSheet, err
}

func normalizeHeaders(values []string) []string {
	headers := make([]string, len(values))
	for i, value := range values {
		headers[i] = strings.TrimSpace(value)
	}
	return headers
}

func normalizeExcelRow(values []string, width int) []string {
	row := make([]string, width)
	copy(row, values)
	return row
}

func excelExportColumnFormatMap(formats []ExcelExportColumnFormat) map[string]string {
	byColumn := make(map[string]string, len(formats))
	for _, item := range formats {
		byColumn[item.Column] = item.Format
	}
	return byColumn
}

func excelExportColumnFormatsForHeaders(headers []string, byColumn map[string]string) []string {
	result := make([]string, len(headers))
	for index, header := range headers {
		result[index] = byColumn[header]
	}
	return result
}

func excelExportValueForFormat(raw string, format string) interface{} {
	value := strings.TrimSpace(raw)
	if value == "" {
		if format == "" || format == "text" {
			return raw
		}
		return ""
	}
	switch format {
	case "number":
		if number, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", ""), 64); err == nil {
			return number
		}
	case "integer":
		normalized := strings.ReplaceAll(value, ",", "")
		if integer, err := strconv.ParseInt(normalized, 10, 64); err == nil {
			return integer
		}
		if number, err := strconv.ParseFloat(normalized, 64); err == nil {
			return int64(number)
		}
	case "bool":
		if value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || value == "是" {
			return true
		}
		if value == "0" || strings.EqualFold(value, "false") || strings.EqualFold(value, "no") || value == "否" {
			return false
		}
	case "date":
		if parsed, ok := parseExcelExportDateValue(value); ok {
			return parsed
		}
	}
	return raw
}

func parseExcelExportDateValue(value string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func excelRowMatchesFilters(row []string, columnIndexes map[string]int, filters []ExcelMatchFilter) bool {
	for _, filter := range filters {
		index, ok := columnIndexes[filter.Column]
		if !ok || index >= len(row) {
			return false
		}
		value := strings.TrimSpace(row[index])
		matched := false
		switch filter.Op {
		case "eq", "":
			matched = value == filter.Value
		case "neq":
			matched = value != filter.Value
		case "contains":
			matched = strings.Contains(value, filter.Value)
		case "not_contains":
			matched = !strings.Contains(value, filter.Value)
		case "starts_with":
			matched = strings.HasPrefix(value, filter.Value)
		case "ends_with":
			matched = strings.HasSuffix(value, filter.Value)
		case "empty":
			matched = value == ""
		case "not_empty":
			matched = value != ""
		}
		if !matched {
			return false
		}
	}
	return true
}

func sheetExists(sheets []string, target string) bool {
	for _, sheet := range sheets {
		if sheet == target {
			return true
		}
	}
	return false
}

func (s *ExcelMatchJobService) logJob(ctx context.Context, jobID uint, level, message string, detail map[string]interface{}) {
	if jobID == 0 || s.jobDAO == nil {
		return
	}
	detailJSON := "{}"
	if detail != nil {
		if data, err := json.Marshal(detail); err == nil {
			detailJSON = string(data)
		}
	}
	_ = s.jobDAO.CreateLog(ctx, &model.ExcelMatchJobLog{
		JobID:      jobID,
		Level:      level,
		Message:    message,
		DetailJSON: detailJSON,
	})
}

func (s *ExcelMatchJobService) uploadExcelResultToOSS(ctx context.Context, matchJob *model.ExcelMatchJob, objectKey string, uploadTimeout time.Duration) (storage.UploadResult, error) {
	client, err := storage.NewOSSClientFromConfig()
	if err != nil {
		return storage.UploadResult{}, err
	}
	fileSize := fileSizeOrZero(matchJob.ResultFilePath)
	uploadPlan := client.UploadPlan(fileSize)
	s.logJob(ctx, matchJob.ID, "info", "OSS上传配置", map[string]interface{}{
		"endpoint":                  uploadPlan.Endpoint,
		"use_internal":              uploadPlan.UseInternal,
		"multipart":                 uploadPlan.Multipart,
		"multipart_threshold_bytes": uploadPlan.MultipartThresholdBytes,
		"part_size_bytes":           uploadPlan.PartSizeBytes,
		"parallel_num":              uploadPlan.ParallelNum,
		"enable_checkpoint":         uploadPlan.EnableCheckpoint,
		"checkpoint_dir":            uploadPlan.CheckpointDir,
	})
	uploadCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	progressLogger := newExcelOSSProgressLogger(func(progress storage.UploadProgress, elapsed time.Duration) {
		s.logJob(ctx, matchJob.ID, "info", "OSS结果文件上传进度", map[string]interface{}{
			"object_key":  objectKey,
			"transferred": progress.Transferred,
			"total":       progress.Total,
			"percent":     fmt.Sprintf("%.2f", progress.Percent),
			"increment":   progress.Increment,
			"file_size":   fileSize,
			"timeout_sec": int(uploadTimeout.Seconds()),
			"elapsed_sec": int(elapsed.Seconds()),
		})
	})
	defer progressLogger.Flush()
	result, err := client.UploadFileWithProgress(uploadCtx, objectKey, matchJob.ResultFilePath, fmt.Sprintf("excel_match_job_%d.xlsx", matchJob.ID), progressLogger.Handle)
	if err != nil {
		return storage.UploadResult{}, err
	}
	if err := s.jobDAO.UpdateResultStorage(ctx, matchJob.ID, result.ObjectKey, result.URL); err != nil {
		return storage.UploadResult{}, err
	}
	matchJob.ResultObjectKey = result.ObjectKey
	matchJob.ResultURL = result.URL
	return result, nil
}

func excelMatchResultObjectKey(jobID uint, now time.Time) string {
	return storage.BuildObjectKey(
		"excel-match-results",
		now.Format("2006/01/02"),
		strconv.FormatUint(uint64(jobID), 10),
		excelMatchResultFileName,
	)
}

func statsDetail(stats ExcelMatchJobStats) map[string]interface{} {
	return map[string]interface{}{
		"total_rows":     stats.TotalRows,
		"processed_rows": stats.ProcessedRows,
		"filtered_rows":  stats.FilteredRows,
		"matched_rows":   stats.MatchedRows,
		"unmatched_rows": stats.UnmatchedRows,
	}
}

func fileSizeOrZero(filePath string) int64 {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0
	}
	return info.Size()
}

func excelMatchOSSUploadTimeout(fileSize int64) time.Duration {
	if override := strings.TrimSpace(os.Getenv(excelMatchOSSUploadTimeoutEnvName)); override != "" {
		if seconds, err := strconv.Atoi(override); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	if fileSize <= 0 {
		return excelMatchOSSUploadMinTimeout
	}
	minutes := fileSize / excelMatchOSSUploadBytesPerMinute
	if fileSize%excelMatchOSSUploadBytesPerMinute != 0 {
		minutes++
	}
	timeout := time.Duration(minutes) * time.Minute
	if timeout < excelMatchOSSUploadMinTimeout {
		return excelMatchOSSUploadMinTimeout
	}
	if timeout > excelMatchOSSUploadMaxTimeout {
		return excelMatchOSSUploadMaxTimeout
	}
	return timeout
}

type excelOSSProgressLogger struct {
	startedAt       time.Time
	lastLoggedAt    time.Time
	lastLoggedBytes int64
	latest          storage.UploadProgress
	log             func(storage.UploadProgress, time.Duration)
}

func newExcelOSSProgressLogger(log func(storage.UploadProgress, time.Duration)) *excelOSSProgressLogger {
	now := time.Now()
	return &excelOSSProgressLogger{
		startedAt:    now,
		lastLoggedAt: now,
		log:          log,
	}
}

func (l *excelOSSProgressLogger) Handle(progress storage.UploadProgress) {
	l.latest = progress
	now := time.Now()
	if progress.Total > 0 && progress.Transferred >= progress.Total {
		l.emit(progress, now)
		return
	}
	if progress.Transferred-l.lastLoggedBytes >= excelMatchOSSProgressBytes || now.Sub(l.lastLoggedAt) >= excelMatchOSSProgressInterval {
		l.emit(progress, now)
	}
}

func (l *excelOSSProgressLogger) Flush() {
	if l.latest.Transferred <= 0 || l.latest.Transferred == l.lastLoggedBytes {
		return
	}
	l.emit(l.latest, time.Now())
}

func (l *excelOSSProgressLogger) emit(progress storage.UploadProgress, now time.Time) {
	if l.log == nil {
		return
	}
	l.lastLoggedAt = now
	l.lastLoggedBytes = progress.Transferred
	l.log(progress, now.Sub(l.startedAt))
}

func excelMatchSchemeFromModel(row model.ExcelMatchScheme) (ExcelMatchScheme, error) {
	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(row.ConfigJSON), &config); err != nil {
		return ExcelMatchScheme{}, err
	}
	return ExcelMatchScheme{
		ID:         row.ID,
		Name:       row.Name,
		Operation:  row.Operation,
		Config:     config,
		ConfigJSON: row.ConfigJSON,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}, nil
}

func saveUploadedExcel(fileHeader *multipart.FileHeader, dst string) error {
	src, err := fileHeader.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func excelMatchJobDir(id uint) string {
	return filepath.Join(excelTempRootDir(), excelMatchRootDirName, strconv.FormatUint(uint64(id), 10))
}

func excelUploadRootDir() string {
	return filepath.Join(excelTempRootDir(), excelUploadRootDirName)
}

func excelPreviewRootDir() string {
	return filepath.Join(excelTempRootDir(), excelPreviewRootDirName)
}

func excelTempRootDir() string {
	for _, key := range []string{excelTempRootEnvName, "EXCEL_MATCH_TEMP_DIR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return filepath.Clean(value)
		}
	}
	if abs, err := filepath.Abs(filepath.Join("storage", "tmp", "data-warehouse-excel")); err == nil {
		return abs
	}
	return filepath.Join(os.TempDir(), "data-warehouse-excel")
}

func excelizeTempDirForPath(filePath string) string {
	base := filepath.Dir(filePath)
	if base == "." || strings.TrimSpace(base) == "" {
		base = excelTempRootDir()
	}
	return filepath.Join(base, excelizeTempDirName)
}

func withExcelizeTempDir(tempDir string, fn func() error) error {
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return err
	}
	excelizeTempMu.Lock()
	defer excelizeTempMu.Unlock()

	keys := []string{"TMPDIR", "TMP", "TEMP"}
	previous := make(map[string]string, len(keys))
	present := make(map[string]bool, len(keys))
	for _, key := range keys {
		value, ok := os.LookupEnv(key)
		previous[key] = value
		present[key] = ok
		if err := os.Setenv(key, tempDir); err != nil {
			return err
		}
	}
	defer func() {
		for _, key := range keys {
			if present[key] {
				_ = os.Setenv(key, previous[key])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}()

	return fn()
}

func excelUploadDir(uploadID string) string {
	return filepath.Join(excelUploadRootDir(), uploadID)
}

func excelUploadChunksDir(uploadID string) string {
	return filepath.Join(excelUploadDir(uploadID), excelUploadChunksDirName)
}

func excelUploadMetaPath(uploadID string) string {
	return filepath.Join(excelUploadDir(uploadID), excelUploadMetaFileName)
}

func excelUploadChunkName(index int) string {
	return fmt.Sprintf("%06d.part", index)
}

func newExcelUploadID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isValidExcelUploadID(uploadID string) bool {
	if len(uploadID) != 32 {
		return false
	}
	for _, ch := range uploadID {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func writeExcelUploadMeta(uploadID string, meta excelUploadMeta) error {
	if !isValidExcelUploadID(uploadID) || uploadID != meta.UploadID {
		return errors.New("上传会话ID非法")
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(excelUploadMetaPath(uploadID), data, 0600)
}

func readExcelUploadMeta(uploadID string) (excelUploadMeta, error) {
	if !isValidExcelUploadID(uploadID) {
		return excelUploadMeta{}, errors.New("上传会话ID非法")
	}
	metaPath := excelUploadMetaPath(uploadID)
	if !isPathInside(excelUploadDir(uploadID), metaPath) {
		return excelUploadMeta{}, errors.New("上传会话路径非法")
	}
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return excelUploadMeta{}, err
	}
	var meta excelUploadMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return excelUploadMeta{}, err
	}
	if meta.UploadID != uploadID {
		return excelUploadMeta{}, errors.New("上传会话元数据不一致")
	}
	return meta, nil
}

func countExcelUploadChunks(uploadID string, totalChunks int) (int, error) {
	count := 0
	for i := 0; i < totalChunks; i++ {
		chunkPath := filepath.Join(excelUploadChunksDir(uploadID), excelUploadChunkName(i))
		if !isPathInside(excelUploadDir(uploadID), chunkPath) {
			return 0, errors.New("分片路径非法")
		}
		if _, err := os.Stat(chunkPath); err == nil {
			count++
		} else if !os.IsNotExist(err) {
			return 0, err
		}
	}
	return count, nil
}

func assembleExcelUpload(uploadID string, totalChunks int) error {
	mergedPath := filepath.Join(excelUploadDir(uploadID), excelUploadMergedFile)
	if !isPathInside(excelUploadDir(uploadID), mergedPath) {
		return errors.New("合并文件路径非法")
	}
	out, err := os.OpenFile(mergedPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	for i := 0; i < totalChunks; i++ {
		chunkPath := filepath.Join(excelUploadChunksDir(uploadID), excelUploadChunkName(i))
		if !isPathInside(excelUploadDir(uploadID), chunkPath) {
			return errors.New("分片路径非法")
		}
		in, err := os.Open(chunkPath)
		if err != nil {
			return fmt.Errorf("读取分片 %d 失败: %w", i, err)
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = in.Close()
			return err
		}
		if err := in.Close(); err != nil {
			return err
		}
	}
	return nil
}

func excelUploadSessionFromMeta(meta excelUploadMeta) *ExcelUploadSession {
	return &ExcelUploadSession{
		UploadID:       meta.UploadID,
		FileName:       meta.FileName,
		TotalChunks:    meta.TotalChunks,
		UploadedChunks: meta.UploadedChunks,
		Complete:       meta.Complete,
		ExpiresAt:      meta.ExpiresAt.Format(time.RFC3339),
	}
}

func isExcelMatchJobExpired(matchJob *model.ExcelMatchJob) bool {
	return matchJob.ExpiresAt != nil && time.Now().After(matchJob.ExpiresAt.Time)
}

func refreshExpiredJob(matchJob *model.ExcelMatchJob) {
	if !isExcelMatchJobExpired(matchJob) || matchJob.Status == excelMatchStatusExpired ||
		(matchJob.Status != excelMatchStatusSuccess && matchJob.Status != "failed") {
		return
	}
	matchJob.Status = excelMatchStatusExpired
}

func (s *ExcelMatchJobService) refreshDownloadState(ctx context.Context, matchJob *model.ExcelMatchJob) {
	matchJob.CanDownload = false
	matchJob.DownloadMessage = ""
	if matchJob.Status != excelMatchStatusSuccess {
		return
	}
	if excelJobOperationFromConfigJSON(matchJob.ConfigJSON) != excelOperationExportMatch {
		matchJob.DownloadMessage = "该任务不会生成结果文件"
		return
	}
	if strings.TrimSpace(matchJob.ResultURL) != "" {
		matchJob.CanDownload = true
		return
	}
	matchJob.DownloadMessage = "结果文件正在上传OSS，上传成功后才能下载，请稍后刷新任务状态"
}

func excelJobOperationFromConfigJSON(rawConfig string) string {
	var config ExcelMatchConfig
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		return ""
	}
	config.Operation = strings.TrimSpace(config.Operation)
	if config.Operation == "" {
		return excelOperationExportMatch
	}
	return config.Operation
}

func isPathInside(basePath, targetPath string) bool {
	if basePath == "" || targetPath == "" {
		return false
	}
	baseAbs, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}
