package data_dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

type ExcelMatchJobDAO struct {
	db *gorm.DB
}

// ExcelMatchJobListQuery contains validated management list filters.
type ExcelMatchJobListQuery struct {
	Page      int
	PageSize  int
	Keyword   string
	Status    string
	Operation string
}

type ExcelMatchJobListPage struct {
	List  []model.ExcelMatchJob
	Total int64
}

var ErrExcelMatchCleanupLeaseLost = errors.New("excel match cleanup: lease lost")
var ErrExcelMatchJobNotRunning = errors.New("Excel 任务已停止运行")

var allowedBojunExcelFields = map[string]struct{}{
	"billdate":             {},
	"c_store_code":         {},
	"c_store_name":         {},
	"docno":                {},
	"matched_docno":        {},
	"o2o_so_docno":         {},
	"order_type_code":      {},
	"order_type_name":      {},
	"otherdocno":           {},
	"related_normal_docno": {},
	"retailbilltype":       {},
	"tot_amt_actual":       {},
	"tot_amt_list":         {},
	"tot_qty":              {},
	"vipno":                {},
}

var excelSQLIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var bojunExcelDecimalValuePattern = regexp.MustCompile(`^[+-]?(?:0|[1-9][0-9]{0,15})(?:\.[0-9]{1,2})?$`)

func NewExcelMatchJobDAO() *ExcelMatchJobDAO {
	return &ExcelMatchJobDAO{db: database.DB}
}

func (dao *ExcelMatchJobDAO) Create(ctx context.Context, job *model.ExcelMatchJob) (uint, error) {
	now := int(time.Now().Unix())
	job.CreatedAt = now
	job.UpdatedAt = now
	err := dao.db.WithContext(ctx).Create(job).Error
	return job.ID, err
}

func (dao *ExcelMatchJobDAO) FindByID(ctx context.Context, id uint) (*model.ExcelMatchJob, error) {
	var job model.ExcelMatchJob
	err := dao.db.WithContext(ctx).First(&job, id).Error
	return &job, err
}

func (dao *ExcelMatchJobDAO) ListJobs(ctx context.Context, limit int) ([]model.ExcelMatchJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var jobs []model.ExcelMatchJob
	err := dao.db.WithContext(ctx).
		Order("id DESC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (dao *ExcelMatchJobDAO) ListJobsPage(ctx context.Context, params ExcelMatchJobListQuery) (*ExcelMatchJobListPage, error) {
	query := dao.applyListFilters(dao.db.WithContext(ctx).Model(&model.ExcelMatchJob{}), params)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	jobs := make([]model.ExcelMatchJob, 0)
	offset := (params.Page - 1) * params.PageSize
	if err := query.Order("id DESC").Offset(offset).Limit(params.PageSize).Find(&jobs).Error; err != nil {
		return nil, err
	}
	return &ExcelMatchJobListPage{List: jobs, Total: total}, nil
}

func (dao *ExcelMatchJobDAO) applyListFilters(query *gorm.DB, params ExcelMatchJobListQuery) *gorm.DB {
	if params.Keyword != "" {
		pattern := "%" + params.Keyword + "%"
		if jobID, err := strconv.ParseUint(params.Keyword, 10, 64); err == nil && jobID > 0 {
			query = query.Where(
				"id = ? OR source_file_name LIKE ? OR error_message LIKE ?",
				jobID, pattern, pattern,
			)
		} else {
			query = query.Where(
				"source_file_name LIKE ? OR error_message LIKE ?",
				pattern, pattern,
			)
		}
	}
	if params.Status != "" {
		query = query.Where("status = ?", params.Status)
	}
	const operationSQL = "JSON_UNQUOTE(JSON_EXTRACT(config_json, '$.operation'))"
	switch params.Operation {
	case "match":
		// Batch matching shares the workflow worker, but a multi-step workflow
		// must remain distinct from a standalone matching task in the list.
		query = query.Where(
			"(COALESCE(NULLIF("+operationSQL+", ''), ?) = ? OR ("+
				operationSQL+" = ? AND JSON_LENGTH(config_json, '$.steps') = ? AND "+
				"JSON_UNQUOTE(JSON_EXTRACT(config_json, '$.steps[0].operation')) = ?))",
			"export_match", "export_match", "excel_workflow", 1, "export_match",
		)
	case "write":
		query = query.Where(
			operationSQL+" IN ?",
			[]string{"import_update", "clear_matched_docno"},
		)
	case "export":
		query = query.Where(operationSQL+" = ?", "oracle_export")
	}
	return query
}

func (dao *ExcelMatchJobDAO) CreateLog(ctx context.Context, log *model.ExcelMatchJobLog) error {
	now := int(time.Now().Unix())
	log.CreatedAt = now
	log.UpdatedAt = now
	return dao.db.WithContext(ctx).Create(log).Error
}

func (dao *ExcelMatchJobDAO) FindLogsByJobID(ctx context.Context, jobID uint, limit int) ([]model.ExcelMatchJobLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var logs []model.ExcelMatchJobLog
	err := dao.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Order("id DESC").
		Limit(limit).
		Find(&logs).Error
	if err != nil {
		return nil, err
	}
	// Keep the newest bounded window, displayed in chronological order.
	for left, right := 0, len(logs)-1; left < right; left, right = left+1, right-1 {
		logs[left], logs[right] = logs[right], logs[left]
	}
	return logs, nil
}

func (dao *ExcelMatchJobDAO) ListSchemes(ctx context.Context, operation string) ([]model.ExcelMatchScheme, error) {
	var schemes []model.ExcelMatchScheme
	query := dao.db.WithContext(ctx).Model(&model.ExcelMatchScheme{})
	if operation != "" {
		query = query.Where("operation = ?", operation)
	} else {
		// The legacy matching DTO does not contain Oracle export fields.
		query = query.Where("operation IN ?", []string{"export_match", "import_update"})
	}
	err := query.Order("updated_at DESC, id DESC").Find(&schemes).Error
	return schemes, err
}

func (dao *ExcelMatchJobDAO) SaveScheme(ctx context.Context, scheme *model.ExcelMatchScheme) error {
	now := int(time.Now().Unix())
	var existing model.ExcelMatchScheme
	err := dao.db.WithContext(ctx).
		Where("operation = ? AND name = ?", scheme.Operation, scheme.Name).
		First(&existing).Error
	if err == nil {
		scheme.ID = existing.ID
		return dao.db.WithContext(ctx).
			Model(&model.ExcelMatchScheme{}).
			Where("id = ?", existing.ID).
			Updates(map[string]interface{}{
				"config_json": scheme.ConfigJSON,
				"updated_at":  now,
			}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	scheme.CreatedAt = now
	scheme.UpdatedAt = now
	return dao.db.WithContext(ctx).Create(scheme).Error
}

func (dao *ExcelMatchJobDAO) DeleteScheme(ctx context.Context, id uint) error {
	return dao.db.WithContext(ctx).Delete(&model.ExcelMatchScheme{}, id).Error
}

func (dao *ExcelMatchJobDAO) UpdatePaths(ctx context.Context, id uint, workDir, sourcePath, resultPath string) error {
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("id = ? AND result_object_key = ?", id, "").
		Updates(map[string]interface{}{
			"work_dir":         workDir,
			"source_file_path": sourcePath,
			"result_file_path": resultPath,
			"updated_at":       time.Now().Unix(),
		}).Error
}

func (dao *ExcelMatchJobDAO) UpdateResultStorage(ctx context.Context, id uint, objectKey, resultURL string) error {
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"result_object_key": objectKey,
			"result_url":        resultURL,
			"updated_at":        time.Now().Unix(),
		}).Error
}

func (dao *ExcelMatchJobDAO) MarkRunning(ctx context.Context, id uint, staleBefore time.Time) (bool, error) {
	now := time.Now()
	result := dao.markRunningClaimQuery(ctx, id, now, staleBefore).
		Updates(map[string]interface{}{
			"status":     "running",
			"started_at": model.TimeNormal{Time: now},
			"updated_at": now.Unix(),
		})
	return result.RowsAffected == 1, result.Error
}

func (dao *ExcelMatchJobDAO) markRunningClaimQuery(
	ctx context.Context,
	id uint,
	now time.Time,
	staleBefore time.Time,
) *gorm.DB {
	// A stale running attempt may recover after its original expiry. The cleaner
	// deliberately excludes running rows, so recovery is what moves it back to
	// a terminal state with a fresh retention window.
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("id = ?", id).
		Where(
			`(status IN ? AND (expires_at IS NULL OR expires_at > ?)) OR
				(status = ? AND updated_at <= ?)`,
			[]string{"pending", "failed"},
			now,
			"running", staleBefore.Unix(),
		)
}

func (dao *ExcelMatchJobDAO) UpdateProgress(ctx context.Context, id uint, stats ExcelMatchJobStats) error {
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]interface{}{
			"total_rows":     stats.TotalRows,
			"processed_rows": stats.ProcessedRows,
			"filtered_rows":  stats.FilteredRows,
			"matched_rows":   stats.MatchedRows,
			"unmatched_rows": stats.UnmatchedRows,
			"updated_at":     time.Now().Unix(),
		}).Error
}

func (dao *ExcelMatchJobDAO) MarkSuccess(ctx context.Context, id uint, stats ExcelMatchJobStats, expiresAt time.Time) error {
	now := time.Now()
	result := dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]interface{}{
			"status":         "success",
			"total_rows":     stats.TotalRows,
			"processed_rows": stats.ProcessedRows,
			"filtered_rows":  stats.FilteredRows,
			"matched_rows":   stats.MatchedRows,
			"unmatched_rows": stats.UnmatchedRows,
			"finished_at":    model.TimeNormal{Time: now},
			"expires_at":     model.TimeNormal{Time: expiresAt},
			"updated_at":     now.Unix(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrExcelMatchJobNotRunning
	}
	return nil
}

func (dao *ExcelMatchJobDAO) MarkFailed(ctx context.Context, id uint, errMessage string, expiresAt time.Time) error {
	now := time.Now()
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("id = ? AND status IN ?", id, []string{"pending", "running"}).
		Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": errMessage,
			"finished_at":   model.TimeNormal{Time: now},
			"expires_at":    model.TimeNormal{Time: expiresAt},
			"updated_at":    now.Unix(),
		}).Error
}

func (dao *ExcelMatchJobDAO) MarkExpired(ctx context.Context, id uint) error {
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("id = ? AND result_object_key = ?", id, "").
		Updates(map[string]interface{}{
			"status":            "expired",
			"result_object_key": "",
			"result_url":        "",
			"work_dir":          "",
			"source_file_path":  "",
			"result_file_path":  "",
			"updated_at":        time.Now().Unix(),
		}).Error
}

func (dao *ExcelMatchJobDAO) ListExpiredCleanupCandidates(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
	afterID uint,
	limit int,
) ([]model.ExcelMatchJob, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var jobs []model.ExcelMatchJob
	err := dao.expiredCleanupCandidateQuery(ctx, now, staleBefore).
		Where("id > ?", afterID).
		Order("id ASC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (dao *ExcelMatchJobDAO) ClaimExpiredCleanup(
	ctx context.Context,
	candidate model.ExcelMatchJob,
	claimAt time.Time,
	staleBefore time.Time,
) (bool, error) {
	result := dao.expiredCleanupCandidateQuery(ctx, claimAt, staleBefore).
		Where(
			"id = ? AND status = ? AND result_object_key = ? AND updated_at = ?",
			candidate.ID,
			candidate.Status,
			candidate.ResultObjectKey,
			candidate.UpdatedAt,
		).
		Updates(map[string]interface{}{
			"status":     "expired",
			"updated_at": claimAt.Unix(),
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (dao *ExcelMatchJobDAO) FinishExpiredCleanup(
	ctx context.Context,
	id uint,
	expectedObjectKey string,
	claimUnix int64,
) error {
	result := dao.expiredCleanupLeaseQuery(ctx, id, expectedObjectKey, claimUnix).
		Updates(map[string]interface{}{
			"status":            "expired",
			"result_object_key": "",
			"result_url":        "",
			"work_dir":          "",
			"source_file_path":  "",
			"result_file_path":  "",
			"updated_at":        time.Now().Unix(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrExcelMatchCleanupLeaseLost
	}
	return nil
}

func (dao *ExcelMatchJobDAO) ReleaseExpiredCleanup(
	ctx context.Context,
	candidate model.ExcelMatchJob,
	claimUnix int64,
) error {
	result := dao.expiredCleanupLeaseQuery(ctx, candidate.ID, candidate.ResultObjectKey, claimUnix).
		Updates(map[string]interface{}{
			"status":     candidate.Status,
			"updated_at": candidate.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrExcelMatchCleanupLeaseLost
	}
	return nil
}

func (dao *ExcelMatchJobDAO) expiredCleanupCandidateQuery(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
) *gorm.DB {
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where("expires_at IS NOT NULL AND expires_at <= ?", now).
		Where(
			`status IN ? OR
				(status = ? AND updated_at <= ? AND (
					result_object_key <> ? OR result_url <> ? OR work_dir <> ? OR
					source_file_path <> ? OR result_file_path <> ?
				))`,
			[]string{"success", "failed", "pending"},
			"expired", staleBefore.Unix(),
			"", "", "", "", "",
		)
}

func (dao *ExcelMatchJobDAO) expiredCleanupLeaseQuery(
	ctx context.Context,
	id uint,
	expectedObjectKey string,
	claimUnix int64,
) *gorm.DB {
	return dao.db.WithContext(ctx).
		Model(&model.ExcelMatchJob{}).
		Where(
			"id = ? AND status = ? AND result_object_key = ? AND updated_at = ?",
			id,
			"expired",
			expectedObjectKey,
			claimUnix,
		)
}

type ExcelMatchJobStats struct {
	TotalRows     int
	ProcessedRows int
	FilteredRows  int
	MatchedRows   int
	UnmatchedRows int
}

// ExcelMatchModelColumn describes one application database column for the
// Excel matching model catalog. The catalog query returns every model and
// field in one round trip so the web UI can render cascading selectors.
type ExcelMatchModelColumn struct {
	TableName       string `gorm:"column:table_name"`
	TableComment    string `gorm:"column:table_comment"`
	ColumnName      string `gorm:"column:column_name"`
	DataType        string `gorm:"column:data_type"`
	ColumnType      string `gorm:"column:column_type"`
	ColumnComment   string `gorm:"column:column_comment"`
	OrdinalPosition int    `gorm:"column:ordinal_position"`
	IsNullable      string `gorm:"column:is_nullable"`
}

// ListModelColumns reads the current MySQL schema with a single joined query.
// Unknown/custom tables are intentionally included because Excel matching is
// configurable and may target tables that do not have a Go model yet.
func (dao *ExcelMatchJobDAO) ListModelColumns(ctx context.Context) ([]ExcelMatchModelColumn, error) {
	if dao.db == nil {
		return nil, errors.New("数据库未初始化")
	}

	var columns []ExcelMatchModelColumn
	err := dao.db.WithContext(ctx).Raw(`
		SELECT
			c.TABLE_NAME AS table_name,
			COALESCE(t.TABLE_COMMENT, '') AS table_comment,
			c.COLUMN_NAME AS column_name,
			c.DATA_TYPE AS data_type,
			c.COLUMN_TYPE AS column_type,
			COALESCE(c.COLUMN_COMMENT, '') AS column_comment,
			c.ORDINAL_POSITION AS ordinal_position,
			c.IS_NULLABLE AS is_nullable
		FROM information_schema.COLUMNS AS c
		INNER JOIN information_schema.TABLES AS t
			ON t.TABLE_SCHEMA = c.TABLE_SCHEMA
			AND t.TABLE_NAME = c.TABLE_NAME
		WHERE c.TABLE_SCHEMA = DATABASE()
			AND t.TABLE_TYPE = 'BASE TABLE'
		ORDER BY c.TABLE_NAME ASC, c.ORDINAL_POSITION ASC
	`).Scan(&columns).Error
	return columns, err
}

func (dao *ExcelMatchJobDAO) FindBojunFieldByKeys(ctx context.Context, matchField string, keys []string, valueField string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	if !isAllowedBojunExcelField(matchField) {
		return nil, fmt.Errorf("bojun match field is not allowed: %s", matchField)
	}
	if !isAllowedBojunExcelField(valueField) {
		return nil, fmt.Errorf("bojun field is not allowed: %s", valueField)
	}

	query := fmt.Sprintf(
		"SELECT CAST(%s AS CHAR) AS match_key, CAST(%s AS CHAR) AS matched_value FROM bojun_retail_orders WHERE %s IN ?",
		matchField,
		valueField,
		matchField,
	)
	rows, err := dao.db.WithContext(ctx).Raw(query, keys).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if key.Valid {
			if value.Valid {
				result[key.String] = value.String
			} else {
				result[key.String] = ""
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (dao *ExcelMatchJobDAO) ValidateTableColumns(ctx context.Context, tableName string, columns []string) error {
	if !isSafeExcelSQLIdentifier(tableName) {
		return fmt.Errorf("数据库表名不合法: %s", tableName)
	}
	for _, column := range columns {
		if !isSafeExcelSQLIdentifier(column) {
			return fmt.Errorf("数据库字段名不合法: %s", column)
		}
	}
	if dao.db == nil {
		return errors.New("数据库未初始化")
	}
	if !dao.db.WithContext(ctx).Migrator().HasTable(tableName) {
		return fmt.Errorf("数据库表不存在: %s", tableName)
	}
	columnTypes, err := dao.db.WithContext(ctx).Migrator().ColumnTypes(tableName)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(columnTypes))
	for _, columnType := range columnTypes {
		existing[columnType.Name()] = struct{}{}
	}
	for _, column := range columns {
		if _, ok := existing[column]; !ok {
			return fmt.Errorf("数据库表 %s 不存在字段: %s", tableName, column)
		}
	}
	return nil
}

func (dao *ExcelMatchJobDAO) FindTableFieldByKeys(ctx context.Context, tableName, matchField string, keys []string, valueField string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	if !isSafeExcelSQLIdentifier(tableName) || !isSafeExcelSQLIdentifier(matchField) || !isSafeExcelSQLIdentifier(valueField) {
		return nil, errors.New("数据库表名或字段名不合法")
	}
	query := fmt.Sprintf(
		"SELECT CAST(`%s` AS CHAR) AS match_key, CAST(`%s` AS CHAR) AS matched_value FROM `%s` WHERE `%s` IN ?",
		matchField,
		valueField,
		tableName,
		matchField,
	)
	rows, err := dao.db.WithContext(ctx).Raw(query, keys).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if key.Valid {
			result[key.String] = value.String
		}
	}
	return result, rows.Err()
}

func (dao *ExcelMatchJobDAO) FindBojunKeys(ctx context.Context, matchField string, keys []string) (map[string]struct{}, error) {
	return dao.FindWritableBojunKeys(ctx, matchField, "matched_docno", false, keys)
}

func (dao *ExcelMatchJobDAO) FindWritableBojunKeys(
	ctx context.Context,
	matchField string,
	writeField string,
	onlyEmpty bool,
	keys []string,
) (map[string]struct{}, error) {
	return dao.FindWritableBojunKeysForBatch(ctx, matchField, writeField, onlyEmpty, false, keys)
}

func (dao *ExcelMatchJobDAO) FindWritableBojunKeysForBatch(
	ctx context.Context,
	matchField string,
	writeField string,
	onlyEmpty bool,
	allowPendingToShop bool,
	keys []string,
) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	if !isAllowedBojunExcelField(matchField) {
		return nil, fmt.Errorf("bojun match field is not allowed: %s", matchField)
	}

	query := fmt.Sprintf(
		"SELECT CAST(`%s` AS CHAR) AS match_key FROM bojun_retail_orders WHERE `%s` IN ?",
		matchField,
		matchField,
	)
	if onlyEmpty {
		emptyCondition, ok := bojunExcelEmptyWriteCondition(writeField)
		if !ok {
			return nil, fmt.Errorf("bojun update field is not allowed")
		}
		if writeField == "oracle_retail_id" && allowPendingToShop {
			emptyCondition = "oracle_retail_id IS NULL AND (is_to_shop IN ('Y', 'N') OR is_to_shop IS NULL OR is_to_shop = '')"
		}
		query += " AND " + emptyCondition
	} else if writeField != "matched_docno" {
		return nil, fmt.Errorf("bojun update field is not allowed")
	}
	rows, err := dao.db.WithContext(ctx).Raw(query, keys).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key sql.NullString
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if key.Valid {
			result[key.String] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (dao *ExcelMatchJobDAO) BatchUpdateBojunFieldByKeys(ctx context.Context, matchField, writeField string, values map[string]string) (int64, error) {
	return dao.BatchUpdateBojunFieldsByKeys(ctx, matchField, map[string]map[string]string{writeField: values})
}

func (dao *ExcelMatchJobDAO) BatchUpdateBojunFieldsByKeys(
	ctx context.Context,
	matchField string,
	valuesByField map[string]map[string]string,
) (int64, error) {
	return dao.BatchUpdateBojunFieldsByKeysWithMode(ctx, matchField, valuesByField, false)
}

func (dao *ExcelMatchJobDAO) BatchUpdateBojunFieldsByKeysWithMode(
	ctx context.Context,
	matchField string,
	valuesByField map[string]map[string]string,
	clearMatchedDocNo bool,
) (int64, error) {
	if len(valuesByField) == 0 {
		return 0, nil
	}
	if !isAllowedBojunExcelField(matchField) {
		return 0, fmt.Errorf("bojun update field is not allowed")
	}
	writeFields := make([]string, 0, len(valuesByField))
	allKeys := make(map[string]struct{})
	for writeField, values := range valuesByField {
		if len(values) == 0 {
			continue
		}
		if _, allowed := bojunExcelEmptyWriteCondition(writeField); !allowed {
			return 0, fmt.Errorf("bojun update field is not allowed")
		}
		writeFields = append(writeFields, writeField)
		for key := range values {
			allKeys[key] = struct{}{}
		}
	}
	if len(writeFields) == 0 {
		return 0, nil
	}
	sort.Slice(writeFields, func(i, j int) bool {
		if writeFields[i] == "oracle_retail_id" {
			return false
		}
		if writeFields[j] == "oracle_retail_id" {
			return true
		}
		return writeFields[i] < writeFields[j]
	})
	assignments := make([]string, 0, len(writeFields)+1)
	args := make([]interface{}, 0, len(allKeys)*len(writeFields)*2+2)
	whereClauses := make([]string, 0, len(writeFields))
	whereArgs := make([]interface{}, 0, len(writeFields))
	_, updatingToShop := valuesByField["is_to_shop"]
	for _, writeField := range writeFields {
		values := valuesByField[writeField]
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		caseSQL := strings.Builder{}
		for _, key := range keys {
			writeValue, err := bojunExcelWriteValue(writeField, values[key])
			if err != nil {
				return 0, err
			}
			caseSQL.WriteString(" WHEN ? THEN ?")
			args = append(args, key, writeValue)
		}
		emptyCondition, _ := bojunExcelEmptyWriteCondition(writeField)
		if writeField == "oracle_retail_id" && updatingToShop {
			emptyCondition = "oracle_retail_id IS NULL AND (is_to_shop IN ('Y', 'N') OR is_to_shop IS NULL OR is_to_shop = '')"
		}
		clearField := clearMatchedDocNo && writeField == "matched_docno"
		for _, value := range values {
			if value != "" {
				clearField = false
				break
			}
		}
		caseExpression := fmt.Sprintf("CASE `%s`%s ELSE `%s` END", matchField, caseSQL.String(), writeField)
		if !clearField {
			caseExpression = fmt.Sprintf("CASE WHEN %s THEN %s ELSE `%s` END", emptyCondition, caseExpression, writeField)
		} else {
			emptyCondition = "1 = 1"
		}
		assignments = append(assignments, fmt.Sprintf("`%s` = %s", writeField, caseExpression))
		whereClauses = append(whereClauses, fmt.Sprintf("(`%s` IN ? AND %s)", matchField, emptyCondition))
		whereArgs = append(whereArgs, keys)
	}
	assignments = append(assignments, "updated_at = ?")
	args = append(args, time.Now().Unix())
	args = append(args, whereArgs...)
	query := fmt.Sprintf(
		"UPDATE bojun_retail_orders SET %s WHERE %s",
		strings.Join(assignments, ", "),
		strings.Join(whereClauses, " OR "),
	)
	result := dao.db.WithContext(ctx).Exec(query, args...)
	return result.RowsAffected, result.Error
}

func (dao *ExcelMatchJobDAO) UpdateBojunFieldByKey(ctx context.Context, matchField, key, writeField, value string) (int64, error) {
	if key == "" {
		return 0, nil
	}
	return dao.BatchUpdateBojunFieldByKeys(ctx, matchField, writeField, map[string]string{key: value})
}

func parseBojunExcelCompletedAt(value string) (time.Time, error) {
	const layout = "2006-01-02 15:04:05"
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	parsed, err := time.ParseInLocation(layout, value, location)
	if err != nil || parsed.Format(layout) != value {
		return time.Time{}, fmt.Errorf("bojun completed_at value must use yyyy-mm-dd hh:mm:ss")
	}
	return parsed, nil
}

func bojunExcelEmptyWriteCondition(writeField string) (string, bool) {
	switch writeField {
	case "matched_docno":
		return "(matched_docno IS NULL OR matched_docno = '')", true
	case "completed_at":
		return "completed_at IS NULL", true
	case "oracle_retail_id":
		return "oracle_retail_id IS NULL AND is_to_shop IN ('Y', 'N')", true
	case "order_phone":
		return "(order_phone IS NULL OR order_phone = '')", true
	case "paid_amount":
		return "paid_amount = 0", true
	case "push_amount":
		return "push_amount = 0", true
	case "is_to_shop":
		return "(is_to_shop IS NULL OR is_to_shop = '')", true
	default:
		return "", false
	}
}

func bojunExcelWriteValue(writeField, value string) (interface{}, error) {
	value = strings.TrimSpace(value)
	switch writeField {
	case "matched_docno":
		return value, nil
	case "completed_at":
		return parseBojunExcelCompletedAt(value)
	case "oracle_retail_id":
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return nil, fmt.Errorf("bojun oracle_retail_id value must be a positive integer")
		}
		return parsed, nil
	case "order_phone":
		if value == "" {
			return nil, fmt.Errorf("bojun order_phone value is required")
		}
		if utf8.RuneCountInString(value) > 64 {
			return nil, fmt.Errorf("bojun order_phone value exceeds 64 characters")
		}
		return value, nil
	case "paid_amount", "push_amount":
		if !bojunExcelDecimalValuePattern.MatchString(value) {
			return nil, fmt.Errorf("bojun %s value must fit decimal(18,2)", writeField)
		}
		return strings.TrimPrefix(value, "+"), nil
	case "is_to_shop":
		normalized := strings.ToUpper(value)
		if normalized != "Y" && normalized != "N" {
			return nil, fmt.Errorf("bojun is_to_shop value must be Y or N")
		}
		return normalized, nil
	default:
		return nil, fmt.Errorf("bojun update field is not allowed")
	}
}

func isAllowedBojunExcelField(field string) bool {
	_, ok := allowedBojunExcelFields[field]
	return ok
}

func isSafeExcelSQLIdentifier(value string) bool {
	return excelSQLIdentifierPattern.MatchString(value)
}
