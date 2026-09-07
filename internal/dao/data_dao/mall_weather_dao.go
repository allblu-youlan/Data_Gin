package data_dao

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

const (
	defaultWeatherBatchSize = 100
	maxWeatherPageSize      = 500
)

var weatherColumnNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type FetchAttemptDisposition uint8

const (
	FetchAttemptDispositionUnknown FetchAttemptDisposition = iota
	FetchAttemptDispositionAcquired
	FetchAttemptDispositionBusy
	FetchAttemptDispositionTerminal
)

type UpsertResult struct {
	AffectedRows      int64
	ChecksumConflicts int64
}

type FetchAttemptLease struct {
	Disposition FetchAttemptDisposition
	Run         model.MallWeatherFetchRun
	Attempt     model.MallWeatherFetchAttempt
}

var ErrProviderRawSnapshotNotFound = errors.New("provider raw snapshot: not found")

type HourlyQuery struct {
	MallID                   uint
	StartUTC                 time.Time
	EndUTC                   time.Time
	AsOfUTC                  *time.Time
	Latest                   bool
	PreferNonNullTemperature bool
	QualityStatus            string
	AfterForecastTime        *time.Time
	AfterIssuedAtUTC         *time.Time
	AfterID                  uint
	Limit                    int
}

type MallWeatherDAO struct {
	db *gorm.DB
}

func NewMallWeatherDAO(databases ...*gorm.DB) *MallWeatherDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &MallWeatherDAO{db: db}
}

func (dao *MallWeatherDAO) WithDB(db *gorm.DB) *MallWeatherDAO {
	return &MallWeatherDAO{db: db}
}

func (dao *MallWeatherDAO) CreateRawSnapshot(ctx context.Context, snapshot *model.ProviderRawSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("mall weather: create nil raw snapshot")
	}
	if err := dao.db.WithContext(ctx).Create(snapshot).Error; err != nil {
		return fmt.Errorf("mall weather: create raw snapshot: %w", err)
	}
	return nil
}

func (dao *MallWeatherDAO) FindRawSnapshotByID(ctx context.Context, snapshotID uint) (*model.ProviderRawSnapshot, error) {
	if dao == nil || dao.db == nil || ctx == nil || snapshotID == 0 {
		return nil, fmt.Errorf("mall weather: invalid raw snapshot lookup")
	}
	var row model.ProviderRawSnapshot
	if err := dao.rawSnapshotByIDQuery(ctx, snapshotID, &row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProviderRawSnapshotNotFound
		}
		return nil, fmt.Errorf("mall weather: find raw snapshot: %w", err)
	}
	return &row, nil
}

func (dao *MallWeatherDAO) rawSnapshotByIDQuery(
	ctx context.Context,
	snapshotID uint,
	row *model.ProviderRawSnapshot,
) *gorm.DB {
	return dao.db.WithContext(ctx).Where("id = ?", snapshotID).First(row)
}

func (dao *MallWeatherDAO) GetOrCreateFetchRun(ctx context.Context, run *model.MallWeatherFetchRun) (*model.MallWeatherFetchRun, bool, error) {
	if run == nil {
		return nil, false, fmt.Errorf("mall weather: create nil fetch run")
	}
	query := dao.db.WithContext(ctx).Where(
		"mall_id = ? AND endpoint_kind = ? AND task_kind = ? AND task_window = ?",
		run.MallID,
		run.EndpointKind,
		run.TaskKind,
		run.TaskWindow,
	)
	var existing model.MallWeatherFetchRun
	err := query.First(&existing).Error
	if err == nil {
		return &existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, fmt.Errorf("mall weather: find fetch run: %w", err)
	}
	createErr := dao.db.WithContext(ctx).Create(run).Error
	if createErr == nil {
		return run, true, nil
	}
	// A concurrent worker may have won the unique-key race. Re-read the
	// logical run before surfacing the create error.
	if err := query.First(&existing).Error; err != nil {
		return nil, false, fmt.Errorf("mall weather: create fetch run: %w", createErr)
	}
	return &existing, false, nil
}

func (dao *MallWeatherDAO) CreateFetchAttempt(ctx context.Context, attempt *model.MallWeatherFetchAttempt) error {
	if attempt == nil {
		return fmt.Errorf("mall weather: create nil fetch attempt")
	}
	if err := dao.db.WithContext(ctx).Create(attempt).Error; err != nil {
		return fmt.Errorf("mall weather: create fetch attempt: %w", err)
	}
	return nil
}

func (dao *MallWeatherDAO) BeginFetchAttempt(ctx context.Context, runID uint, startedAt time.Time, staleAfter time.Duration) (*FetchAttemptLease, error) {
	if dao == nil || dao.db == nil || ctx == nil || runID == 0 || startedAt.IsZero() || staleAfter <= 0 {
		return nil, fmt.Errorf("mall weather: invalid fetch attempt lease input")
	}
	startedAt = startedAt.UTC()
	lease := &FetchAttemptLease{}
	err := dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lease.Run, runID).Error; err != nil {
			return fmt.Errorf("mall weather: lock fetch run: %w", err)
		}

		var latestAttempt *model.MallWeatherFetchAttempt
		if lease.Run.AttemptCount > 0 {
			var row model.MallWeatherFetchAttempt
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("fetch_run_id = ? AND attempt_no = ?", lease.Run.ID, lease.Run.AttemptCount).
				First(&row).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("mall weather: lock latest fetch attempt: %w", err)
			}
			if err == nil {
				latestAttempt = &row
			}
		}

		disposition, recoverInterrupted, err := classifyFetchAttemptStart(&lease.Run, latestAttempt, startedAt, staleAfter)
		if err != nil {
			return err
		}
		lease.Disposition = disposition
		if disposition != FetchAttemptDispositionAcquired {
			return nil
		}
		if recoverInterrupted {
			finishedAt := startedAt
			durationMS := finishedAt.Sub(latestAttempt.StartedAt).Milliseconds()
			if durationMS < 0 {
				durationMS = 0
			}
			if err := NewMallWeatherDAO(tx).UpdateFetchAttempt(ctx, latestAttempt.ID, map[string]interface{}{
				"status": "persist_failed", "finished_at": &finishedAt, "duration_ms": durationMS,
				"error_class": "internal", "error_code": "WORKER_INTERRUPTED",
				"error_message_safe": "weather worker attempt was interrupted",
			}); err != nil {
				return err
			}
		}

		lease.Attempt = model.MallWeatherFetchAttempt{
			FetchRunID: lease.Run.ID, AttemptNo: lease.Run.AttemptCount + 1,
			StartedAt: startedAt, Status: "running",
		}
		if err := NewMallWeatherDAO(tx).CreateFetchAttempt(ctx, &lease.Attempt); err != nil {
			return err
		}
		updates := map[string]interface{}{
			"attempt_count": lease.Attempt.AttemptNo, "status": "running", "finished_at": nil,
			"error_class": "", "error_code": "", "error_message_safe": "",
		}
		if lease.Run.StartedAt == nil {
			updates["started_at"] = &startedAt
			lease.Run.StartedAt = &startedAt
		}
		if err := NewMallWeatherDAO(tx).UpdateFetchRun(ctx, lease.Run.ID, updates); err != nil {
			return err
		}
		lease.Run.AttemptCount = lease.Attempt.AttemptNo
		lease.Run.Status = "running"
		lease.Run.FinishedAt = nil
		lease.Run.ErrorClass = ""
		lease.Run.ErrorCode = ""
		lease.Run.ErrorMessageSafe = ""
		return nil
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func classifyFetchAttemptStart(run *model.MallWeatherFetchRun, latestAttempt *model.MallWeatherFetchAttempt, now time.Time, staleAfter time.Duration) (FetchAttemptDisposition, bool, error) {
	if run == nil || run.ID == 0 || now.IsZero() || staleAfter <= 0 || run.AttemptCount < 0 {
		return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: invalid fetch run state")
	}
	if run.AttemptCount == 0 && latestAttempt != nil {
		return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: unexpected fetch attempt for pending run")
	}
	if run.AttemptCount > 0 && (latestAttempt == nil || latestAttempt.FetchRunID != run.ID || latestAttempt.AttemptNo != run.AttemptCount) {
		return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: inconsistent latest fetch attempt")
	}
	if run.Status != "pending" && latestAttempt == nil {
		return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: fetch run status requires an attempt")
	}
	switch run.Status {
	case "success", "partial_success":
		if latestAttempt.Status != run.Status {
			return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: inconsistent terminal fetch attempt")
		}
		return FetchAttemptDispositionTerminal, false, nil
	case "pending":
		if run.AttemptCount != 0 {
			return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: pending fetch run has attempts")
		}
		return FetchAttemptDispositionAcquired, false, nil
	case "failed":
		if latestAttempt.Status == "running" || latestAttempt.Status == "success" || latestAttempt.Status == "partial_success" {
			return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: inconsistent failed fetch attempt")
		}
		return FetchAttemptDispositionAcquired, false, nil
	case "running":
		if latestAttempt.Status != "running" || latestAttempt.StartedAt.IsZero() {
			return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: inconsistent running fetch attempt")
		}
		if now.UTC().Before(latestAttempt.StartedAt.UTC().Add(staleAfter)) {
			return FetchAttemptDispositionBusy, false, nil
		}
		return FetchAttemptDispositionAcquired, true, nil
	default:
		return FetchAttemptDispositionUnknown, false, fmt.Errorf("mall weather: unsupported fetch run status %q", run.Status)
	}
}

func (dao *MallWeatherDAO) UpdateFetchAttempt(ctx context.Context, id uint, updates map[string]interface{}) error {
	safeUpdates, err := sanitizeFetchAttemptUpdates(updates)
	if err != nil {
		return err
	}
	result := dao.db.WithContext(ctx).Model(&model.MallWeatherFetchAttempt{}).
		Where("id = ?", id).
		Updates(safeUpdates)
	if result.Error != nil {
		return fmt.Errorf("mall weather: update fetch attempt: %w", result.Error)
	}
	if id == 0 || result.RowsAffected != 1 {
		return fmt.Errorf("mall weather: fetch attempt not found")
	}
	return nil
}

func (dao *MallWeatherDAO) UpdateFetchRun(ctx context.Context, id uint, updates map[string]interface{}) error {
	safeUpdates, err := sanitizeFetchRunUpdates(updates)
	if err != nil {
		return err
	}
	result := dao.db.WithContext(ctx).Model(&model.MallWeatherFetchRun{}).
		Where("id = ?", id).
		Updates(safeUpdates)
	if result.Error != nil {
		return fmt.Errorf("mall weather: update fetch run: %w", result.Error)
	}
	if id == 0 || result.RowsAffected != 1 {
		return fmt.Errorf("mall weather: fetch run not found")
	}
	return nil
}

func sanitizeFetchAttemptUpdates(updates map[string]interface{}) (map[string]interface{}, error) {
	allowed := map[string]struct{}{
		"finished_at": {}, "duration_ms": {}, "http_status": {}, "provider_status": {},
		"raw_snapshot_id": {}, "response_checksum": {}, "status": {}, "error_class": {},
		"error_code": {}, "error_message_safe": {},
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("mall weather: no fetch attempt fields to update")
	}
	result := make(map[string]interface{}, len(updates))
	for field, value := range updates {
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("mall weather: fetch attempt field %q is not allowed", field)
		}
		result[field] = value
	}
	return result, nil
}

func sanitizeFetchRunUpdates(updates map[string]interface{}) (map[string]interface{}, error) {
	allowed := map[string]struct{}{
		"attempt_count": {}, "status": {}, "started_at": {}, "finished_at": {}, "duration_ms": {},
		"http_status": {}, "provider_status": {}, "provider_server_time": {}, "response_checksum": {},
		"raw_snapshot_id": {}, "row_counts_json": {}, "parse_warnings_json": {}, "error_class": {},
		"error_code": {}, "error_message_safe": {}, "parser_version": {},
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("mall weather: no fetch run fields to update")
	}
	result := make(map[string]interface{}, len(updates))
	for field, value := range updates {
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("mall weather: fetch run field %q is not allowed", field)
		}
		result[field] = value
	}
	return result, nil
}

func (dao *MallWeatherDAO) UpsertRealtime(ctx context.Context, rows []model.MallWeatherRealtime) (UpsertResult, error) {
	return upsertChecksumAwareWeatherRows(ctx, dao.db, rows, []string{"mall_id", "provider", "snapshot_at_utc"}, nil, 1)
}

func (dao *MallWeatherDAO) UpsertMinutely(ctx context.Context, rows []model.MallWeatherMinutely) (UpsertResult, error) {
	return upsertChecksumAwareWeatherRows(ctx, dao.db, rows, []string{"mall_id", "provider", "forecast_minute_utc", "issued_at_utc"}, nil, 120)
}

func (dao *MallWeatherDAO) UpsertHourly(ctx context.Context, rows []model.MallWeatherHourly) (UpsertResult, error) {
	return upsertChecksumAwareWeatherRows(ctx, dao.db, rows, []string{"mall_id", "provider", "forecast_time_utc", "issued_at_utc"}, nil, 200)
}

func (dao *MallWeatherDAO) UpsertDaily(ctx context.Context, rows []model.MallWeatherDaily) (UpsertResult, error) {
	return upsertChecksumAwareWeatherRows(ctx, dao.db, rows, []string{"mall_id", "provider", "forecast_date_local", "issued_at_utc"}, nil, 15)
}

func (dao *MallWeatherDAO) UpsertAlerts(ctx context.Context, rows []model.MallWeatherAlert) (UpsertResult, error) {
	return upsertChecksumAwareWeatherRows(ctx, dao.db, rows, []string{"provider", "alert_id"}, []string{"first_seen_at"}, defaultWeatherBatchSize)
}

func (dao *MallWeatherDAO) UpsertAlertRelations(ctx context.Context, rows []model.MallWeatherAlertRelation) (UpsertResult, error) {
	if len(rows) == 0 {
		return UpsertResult{}, nil
	}
	result := dao.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "mall_id"}, {Name: "alert_pk"}},
			DoUpdates: clause.Set{
				{Column: clause.Column{Name: "relation_reason"}, Value: clause.Expr{SQL: "VALUES(`relation_reason`)"}},
				{Column: clause.Column{Name: "last_seen_at"}, Value: clause.Expr{SQL: "GREATEST(`last_seen_at`, VALUES(`last_seen_at`))"}},
				{Column: clause.Column{Name: "is_active"}, Value: clause.Expr{SQL: "VALUES(`is_active`)"}},
				{Column: clause.Column{Name: "updated_at"}, Value: clause.Expr{SQL: "VALUES(`updated_at`)"}},
			},
		}).
		CreateInBatches(&rows, defaultWeatherBatchSize)
	if result.Error != nil {
		return UpsertResult{}, fmt.Errorf("mall weather: batch upsert alert relations: %w", result.Error)
	}
	return UpsertResult{AffectedRows: result.RowsAffected}, nil
}

func (dao *MallWeatherDAO) DeactivateMissingAlertRelations(
	ctx context.Context,
	mallID uint,
	provider string,
	seenAlertPKs []uint,
	missingBefore time.Time,
) (int64, error) {
	provider = strings.TrimSpace(provider)
	if dao == nil || dao.db == nil || ctx == nil || mallID == 0 || provider == "" ||
		missingBefore.IsZero() || len(seenAlertPKs) > 500 {
		return 0, fmt.Errorf("mall weather: invalid missing alert relation reconciliation")
	}
	for _, alertPK := range seenAlertPKs {
		if alertPK == 0 {
			return 0, fmt.Errorf("mall weather: invalid missing alert relation reconciliation")
		}
	}
	result := dao.deactivateMissingAlertRelationsQuery(
		ctx,
		mallID,
		provider,
		seenAlertPKs,
		missingBefore.UTC(),
	)
	if result.Error != nil {
		return 0, fmt.Errorf("mall weather: deactivate missing alert relations: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (dao *MallWeatherDAO) deactivateMissingAlertRelationsQuery(
	ctx context.Context,
	mallID uint,
	provider string,
	seenAlertPKs []uint,
	missingBefore time.Time,
) *gorm.DB {
	providerAlerts := dao.db.WithContext(ctx).
		Model(&model.MallWeatherAlert{}).
		Select("id").
		Where("provider = ?", provider)
	query := dao.db.WithContext(ctx).
		Model(&model.MallWeatherAlertRelation{}).
		Where("mall_id = ? AND is_active = ? AND last_seen_at <= ?", mallID, true, missingBefore).
		Where("alert_pk IN (?)", providerAlerts)
	if len(seenAlertPKs) > 0 {
		query = query.Where("alert_pk NOT IN ?", seenAlertPKs)
	}
	return query.Update("is_active", false)
}

func (dao *MallWeatherDAO) UpsertLifeIndices(ctx context.Context, rows []model.MallWeatherLifeIndex) (UpsertResult, error) {
	return upsertChecksumAwareWeatherRows(ctx, dao.db, rows, []string{"mall_id", "provider", "source_api", "forecast_date_local", "index_type", "issued_at_utc"}, nil, 200)
}

func (dao *MallWeatherDAO) UpsertLatest(ctx context.Context, rows []model.MallWeatherLatest) (UpsertResult, error) {
	return upsertLatestRows(ctx, dao.db, rows)
}

func (dao *MallWeatherDAO) FindAlertsByProviderIDs(ctx context.Context, provider string, alertIDs []string) ([]model.MallWeatherAlert, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" || len(alertIDs) == 0 || len(alertIDs) > 500 {
		return nil, fmt.Errorf("mall weather: invalid alert identity query")
	}
	for _, alertID := range alertIDs {
		if strings.TrimSpace(alertID) == "" {
			return nil, fmt.Errorf("mall weather: invalid alert identity query")
		}
	}
	var rows []model.MallWeatherAlert
	if err := dao.db.WithContext(ctx).
		Where("provider = ? AND alert_id IN ?", provider, alertIDs).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("mall weather: find alerts by provider ids: %w", err)
	}
	return rows, nil
}

func upsertWeatherRows[T any](ctx context.Context, db *gorm.DB, rows []T, conflictColumns []string, batchSize int) (UpsertResult, error) {
	if len(rows) == 0 {
		return UpsertResult{}, nil
	}
	columns := make([]clause.Column, 0, len(conflictColumns))
	for _, name := range conflictColumns {
		columns = append(columns, clause.Column{Name: name})
	}
	result := db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: columns, UpdateAll: true}).
		CreateInBatches(&rows, batchSize)
	if result.Error != nil {
		return UpsertResult{}, fmt.Errorf("mall weather: batch upsert: %w", result.Error)
	}
	return UpsertResult{AffectedRows: result.RowsAffected}, nil
}

func upsertChecksumAwareWeatherRows[T any](ctx context.Context, db *gorm.DB, rows []T, conflictColumns, immutableColumns []string, batchSize int) (UpsertResult, error) {
	if len(rows) == 0 {
		return UpsertResult{}, nil
	}
	columns := make([]clause.Column, 0, len(conflictColumns))
	for _, name := range conflictColumns {
		columns = append(columns, clause.Column{Name: name})
	}
	updates, err := checksumAwareUpdateSet(db, &rows[0], conflictColumns, immutableColumns)
	if err != nil {
		return UpsertResult{}, err
	}
	checksumConflicts, err := countChecksumConflicts(ctx, db, rows, conflictColumns)
	if err != nil {
		return UpsertResult{}, err
	}
	result := db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: columns, DoUpdates: updates}).
		CreateInBatches(&rows, batchSize)
	if result.Error != nil {
		return UpsertResult{}, fmt.Errorf("mall weather: checksum-aware batch upsert: %w", result.Error)
	}
	return UpsertResult{AffectedRows: result.RowsAffected, ChecksumConflicts: checksumConflicts}, nil
}

func checksumAwareUpdateSet(db *gorm.DB, row interface{}, conflictColumns, immutableColumns []string) (clause.Set, error) {
	if db == nil || row == nil {
		return nil, fmt.Errorf("mall weather: checksum-aware upsert is not configured")
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(row); err != nil {
		return nil, fmt.Errorf("mall weather: parse checksum-aware model: %w", err)
	}
	excluded := map[string]struct{}{"created_at": {}}
	for _, name := range append(append([]string(nil), conflictColumns...), immutableColumns...) {
		excluded[name] = struct{}{}
	}
	updates := make(clause.Set, 0, len(statement.Schema.Fields))
	hasChecksum := false
	hasLastSeen := false
	for _, field := range statement.Schema.Fields {
		name := field.DBName
		if name == "" || field.PrimaryKey {
			continue
		}
		if !weatherColumnNamePattern.MatchString(name) {
			return nil, fmt.Errorf("mall weather: unsafe model column name")
		}
		if _, skip := excluded[name]; skip {
			continue
		}
		if name == "raw_checksum" {
			hasChecksum = true
			continue
		}
		quoted := "`" + name + "`"
		if name == "last_seen_at" {
			hasLastSeen = true
			updates = append(updates, clause.Assignment{
				Column: clause.Column{Name: name}, Value: clause.Expr{SQL: "GREATEST(" + quoted + ", VALUES(" + quoted + "))"},
			})
			continue
		}
		updates = append(updates, clause.Assignment{
			Column: clause.Column{Name: name},
			Value:  clause.Expr{SQL: "IF(`raw_checksum` = VALUES(`raw_checksum`), " + quoted + ", VALUES(" + quoted + "))"},
		})
	}
	if !hasChecksum || !hasLastSeen {
		return nil, fmt.Errorf("mall weather: model does not support checksum-aware upsert")
	}
	// MySQL evaluates ON DUPLICATE KEY assignments from left to right. Keep the
	// new checksum last so every preceding IF compares against the stored value.
	updates = append(updates, clause.Assignment{
		Column: clause.Column{Name: "raw_checksum"}, Value: clause.Expr{SQL: "VALUES(`raw_checksum`)"},
	})
	return updates, nil
}

func countChecksumConflicts[T any](ctx context.Context, db *gorm.DB, rows []T, conflictColumns []string) (int64, error) {
	identityPredicate, identityArgs, err := checksumIdentityPredicate(ctx, db, rows, conflictColumns)
	if err != nil {
		return 0, err
	}
	var lockedRows []struct {
		ID uint `gorm:"column:id"`
	}
	if err := db.WithContext(ctx).
		Model(&rows[0]).
		Select("id").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(identityPredicate, identityArgs...).
		Find(&lockedRows).Error; err != nil {
		return 0, fmt.Errorf("mall weather: lock checksum identities: %w", err)
	}
	predicate, args, err := checksumConflictPredicate(ctx, db, rows, conflictColumns)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := db.WithContext(ctx).Model(&rows[0]).Where(predicate, args...).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("mall weather: count checksum conflicts: %w", err)
	}
	return count, nil
}

func checksumConflictPredicate[T any](ctx context.Context, db *gorm.DB, rows []T, conflictColumns []string) (string, []interface{}, error) {
	return checksumRowPredicate(ctx, db, rows, conflictColumns, true)
}

func checksumIdentityPredicate[T any](ctx context.Context, db *gorm.DB, rows []T, conflictColumns []string) (string, []interface{}, error) {
	return checksumRowPredicate(ctx, db, rows, conflictColumns, false)
}

func checksumRowPredicate[T any](ctx context.Context, db *gorm.DB, rows []T, conflictColumns []string, includeChecksumDifference bool) (string, []interface{}, error) {
	if ctx == nil || db == nil || len(rows) == 0 || len(conflictColumns) == 0 {
		return "", nil, fmt.Errorf("mall weather: invalid checksum conflict query")
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&rows[0]); err != nil {
		return "", nil, fmt.Errorf("mall weather: parse checksum conflict model: %w", err)
	}
	var checksumField *schema.Field
	if includeChecksumDifference {
		checksumField = statement.Schema.LookUpField("raw_checksum")
		if checksumField == nil {
			return "", nil, fmt.Errorf("mall weather: checksum field is unavailable")
		}
	}
	fields := make([]*schema.Field, len(conflictColumns))
	for index, name := range conflictColumns {
		if !weatherColumnNamePattern.MatchString(name) {
			return "", nil, fmt.Errorf("mall weather: unsafe checksum conflict column")
		}
		fields[index] = statement.Schema.LookUpField(name)
		if fields[index] == nil {
			return "", nil, fmt.Errorf("mall weather: checksum conflict column is unavailable")
		}
	}
	groups := make([]string, 0, len(rows))
	conditionCount := len(fields)
	if includeChecksumDifference {
		conditionCount++
	}
	args := make([]interface{}, 0, len(rows)*conditionCount)
	for index := range rows {
		value := reflect.ValueOf(&rows[index])
		conditions := make([]string, 0, conditionCount)
		for fieldIndex, field := range fields {
			fieldValue, _ := field.ValueOf(ctx, value)
			conditions = append(conditions, "`"+conflictColumns[fieldIndex]+"` = ?")
			args = append(args, normalizeWeatherIdentityFieldValue(field, fieldValue))
		}
		if includeChecksumDifference {
			checksum, _ := checksumField.ValueOf(ctx, value)
			conditions = append(conditions, "`raw_checksum` <> ?")
			args = append(args, checksum)
		}
		groups = append(groups, "("+strings.Join(conditions, " AND ")+")")
	}
	return "(" + strings.Join(groups, " OR ") + ")", args, nil
}

func normalizeWeatherIdentityFieldValue(field *schema.Field, value interface{}) interface{} {
	if field == nil || !strings.EqualFold(strings.TrimSpace(field.TagSettings["TYPE"]), "date") {
		return value
	}
	switch typed := value.(type) {
	case time.Time:
		return typed.Format("2006-01-02")
	case *time.Time:
		if typed != nil {
			return typed.Format("2006-01-02")
		}
	}
	return value
}

func (dao *MallWeatherDAO) QueryHourly(ctx context.Context, query HourlyQuery) ([]model.MallWeatherHourly, error) {
	statement, args, err := buildHourlyQuery(query)
	if err != nil {
		return nil, err
	}
	var rows []model.MallWeatherHourly
	if err := dao.db.WithContext(ctx).Raw(statement, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("mall weather: query hourly: %w", err)
	}
	return rows, nil
}

func buildHourlyQuery(query HourlyQuery) (string, []interface{}, error) {
	if query.MallID == 0 {
		return "", nil, fmt.Errorf("mall weather: mall id is required")
	}
	if query.StartUTC.IsZero() || query.EndUTC.IsZero() || !query.StartUTC.Before(query.EndUTC) {
		return "", nil, fmt.Errorf("mall weather: invalid hourly time range")
	}

	where := []string{
		"w.mall_id = ?",
		"w.forecast_time_utc >= ?",
		"w.forecast_time_utc < ?",
	}
	args := []interface{}{query.MallID, query.StartUTC.UTC(), query.EndUTC.UTC()}
	if query.AsOfUTC != nil {
		where = append(where, "w.issued_at_utc <= ?")
		args = append(args, query.AsOfUTC.UTC())
	}
	if query.QualityStatus != "" {
		where = append(where, "w.quality_status = ?")
		args = append(args, query.QualityStatus)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 200
	} else if limit > maxWeatherPageSize {
		limit = maxWeatherPageSize
	}
	args = append(args, limit)

	if query.Latest || query.AsOfUTC != nil {
		versionOrder := "w.issued_at_utc DESC, w.id DESC"
		if query.PreferNonNullTemperature {
			versionOrder = "(w.temperature_c IS NULL) ASC, " + versionOrder
		}
		outerWhere := []string{"ranked.version_rank = 1"}
		if query.AfterForecastTime != nil {
			outerWhere = append(outerWhere, "(ranked.forecast_time_utc > ? OR (ranked.forecast_time_utc = ? AND ranked.id > ?))")
			cursor := query.AfterForecastTime.UTC()
			args = append(args[:len(args)-1], cursor, cursor, query.AfterID, limit)
		}
		return `SELECT weather.* FROM (
	SELECT w.id, w.forecast_time_utc, ROW_NUMBER() OVER (
	  PARTITION BY w.forecast_time_utc
	  ORDER BY ` + versionOrder + `
	) AS version_rank
	FROM mall_weather_hourly AS w
	WHERE ` + strings.Join(where, " AND ") + `
) AS ranked
	INNER JOIN mall_weather_hourly AS weather ON weather.id = ranked.id
	WHERE ` + strings.Join(outerWhere, " AND ") + `
	ORDER BY ranked.forecast_time_utc ASC, ranked.id ASC
	LIMIT ?`, args, nil
	}
	if query.AfterForecastTime != nil {
		if query.AfterIssuedAtUTC == nil {
			return "", nil, fmt.Errorf("mall weather: issued-at cursor is required for version history")
		}
		where = append(where, `(w.forecast_time_utc > ?
OR (w.forecast_time_utc = ? AND w.issued_at_utc < ?)
OR (w.forecast_time_utc = ? AND w.issued_at_utc = ? AND w.id > ?))`)
		cursor := query.AfterForecastTime.UTC()
		issuedCursor := query.AfterIssuedAtUTC.UTC()
		args = append(args[:len(args)-1], cursor, cursor, issuedCursor, cursor, issuedCursor, query.AfterID, limit)
	}

	return `SELECT w.*
FROM mall_weather_hourly AS w
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY w.forecast_time_utc ASC, w.issued_at_utc DESC, w.id ASC
LIMIT ?`, args, nil
}
