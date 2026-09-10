package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const queryIndexMigrationLockName = "data_gin_query_indexes_v1"

func queryIndexMigrationRetrySchedule() []time.Duration {
	return []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
	}
}

type queryIndexColumn struct {
	Name string
	Desc bool
}

type queryIndexSpec struct {
	TableName string
	IndexName string
	Columns   []queryIndexColumn
}

type queryIndexStateColumn struct {
	Name      string
	Desc      bool
	NonUnique int
	IndexType string
	Visible   bool
	FullWidth bool
}

func queryIndexSpecs() []queryIndexSpec {
	return []queryIndexSpec{
		{
			TableName: "mall_weather_realtime", IndexName: "idx_weather_realtime_query",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "snapshot_at_utc"}, {Name: "id"}},
		},
		{
			TableName: "mall_weather_minutely", IndexName: "idx_weather_minutely_query",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "forecast_minute_utc"}, {Name: "issued_at_utc", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "mall_weather_hourly", IndexName: "idx_weather_hourly_query",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "forecast_time_utc"}, {Name: "issued_at_utc", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "mall_weather_daily", IndexName: "idx_weather_daily_query",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "forecast_date_local"}, {Name: "issued_at_utc", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "mall_weather_life_indices", IndexName: "idx_weather_life_query",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "forecast_date_local"}, {Name: "source_api"}, {Name: "index_type"}, {Name: "issued_at_utc", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "mall_weather_latest", IndexName: "idx_weather_latest_current",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "data_kind"}, {Name: "fetched_at_utc", Desc: true}, {Name: "issued_at_utc", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "mall_weather_latest", IndexName: "idx_weather_latest_business_time",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "data_kind"}, {Name: "business_time"}, {Name: "id"}},
		},
		{
			TableName: "mall_weather_fetch_runs", IndexName: "idx_weather_fetch_runs_repair",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "endpoint_kind"}, {Name: "status"}, {Name: "task_kind"}, {Name: "id", Desc: true}},
		},
		{
			TableName: "mall_weather_alert_relations", IndexName: "idx_weather_alert_relation_active",
			Columns: []queryIndexColumn{{Name: "mall_id"}, {Name: "is_active"}, {Name: "last_seen_at"}, {Name: "alert_pk"}},
		},
		{
			TableName: "bojun_retail_orders", IndexName: "idx_bojun_open_query",
			Columns: []queryIndexColumn{{Name: "c_store_code"}, {Name: "billdate", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "bojun_retail_orders", IndexName: "idx_bojun_open_completed_query",
			Columns: []queryIndexColumn{{Name: "completed_at", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "bojun_retail_orders", IndexName: "idx_bojun_open_completed_mall_query",
			Columns: []queryIndexColumn{{Name: "c_store_code"}, {Name: "completed_at", Desc: true}, {Name: "id", Desc: true}},
		},
		{
			TableName: "malls", IndexName: "idx_malls_open_weather_query",
			Columns: []queryIndexColumn{{Name: "status"}, {Name: "weather_enabled"}, {Name: "geocode_status"}},
		},
	}
}

// ApplyQueryIndexes performs the one-shot online index migration. Callers must
// run this from a single migration process, never from the service startup path.
func ApplyQueryIndexes(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return fmt.Errorf("query index migration: database is unavailable")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("query index migration: get sql database: %w", err)
	}
	return retryQueryIndexMigration(ctx, queryIndexMigrationRetrySchedule(), func(ctx context.Context) error {
		return applyQueryIndexesOnce(ctx, sqlDB)
	})
}

func applyQueryIndexesOnce(ctx context.Context, sqlDB *sql.DB) (resultErr error) {
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("query index migration: acquire connection: %w", err)
	}
	defer conn.Close()

	locked, err := acquireQueryIndexMigrationLock(ctx, conn)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("query index migration: another migration is running")
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if releaseErr := releaseQueryIndexMigrationLock(releaseCtx, conn); releaseErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
		}
	}()

	if _, err := conn.ExecContext(ctx, "SET SESSION lock_wait_timeout = 5"); err != nil {
		return fmt.Errorf("query index migration: set metadata lock timeout: %w", err)
	}
	for _, spec := range queryIndexSpecs() {
		if err := applyQueryIndex(ctx, conn, spec); err != nil {
			return err
		}
	}
	return nil
}

func retryQueryIndexMigration(
	ctx context.Context,
	retryDelays []time.Duration,
	migrate func(context.Context) error,
) error {
	for attempt := 0; ; attempt++ {
		err := migrate(ctx)
		if err == nil || !isMetadataLockTimeout(err) {
			return err
		}
		if attempt >= len(retryDelays) {
			return fmt.Errorf(
				"query index migration: metadata lock retry exhausted after %d attempts: %w",
				attempt+1,
				err,
			)
		}
		if err := waitQueryIndexMigrationRetry(ctx, retryDelays[attempt]); err != nil {
			return fmt.Errorf("query index migration: metadata lock retry canceled: %w", err)
		}
	}
}

func isMetadataLockTimeout(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1205
}

func waitQueryIndexMigrationRetry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func acquireQueryIndexMigrationLock(ctx context.Context, conn *sql.Conn) (bool, error) {
	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", queryIndexMigrationLockName).Scan(&locked); err != nil {
		return false, fmt.Errorf("query index migration: acquire advisory lock: %w", err)
	}
	return locked.Valid && locked.Int64 == 1, nil
}

func releaseQueryIndexMigrationLock(ctx context.Context, conn *sql.Conn) error {
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", queryIndexMigrationLockName).Scan(&released); err != nil {
		return fmt.Errorf("query index migration: release advisory lock: %w", err)
	}
	if !released.Valid || released.Int64 != 1 {
		return fmt.Errorf("query index migration: advisory lock was not released")
	}
	return nil
}

func applyQueryIndex(ctx context.Context, conn *sql.Conn, spec queryIndexSpec) error {
	state, exists, err := loadQueryIndexState(ctx, conn, spec)
	if err != nil {
		return err
	}
	if exists {
		if !queryIndexStateMatches(state, spec.Columns) {
			return fmt.Errorf("query index migration: %s.%s has an unexpected definition", spec.TableName, spec.IndexName)
		}
		return nil
	}
	statement, err := buildAddQueryIndexSQL(spec)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("query index migration: create %s.%s: %w", spec.TableName, spec.IndexName, err)
	}
	state, exists, err = loadQueryIndexState(ctx, conn, spec)
	if err != nil {
		return err
	}
	if !exists || !queryIndexStateMatches(state, spec.Columns) {
		return fmt.Errorf("query index migration: %s.%s verification failed", spec.TableName, spec.IndexName)
	}
	return nil
}

func loadQueryIndexState(ctx context.Context, conn *sql.Conn, spec queryIndexSpec) ([]queryIndexStateColumn, bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT NON_UNIQUE, COLUMN_NAME, COLLATION, INDEX_TYPE, IS_VISIBLE, SUB_PART
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?
ORDER BY SEQ_IN_INDEX ASC`, spec.TableName, spec.IndexName)
	if err != nil {
		return nil, false, fmt.Errorf("query index migration: inspect %s.%s: %w", spec.TableName, spec.IndexName, err)
	}
	defer rows.Close()
	state := make([]queryIndexStateColumn, 0, len(spec.Columns))
	for rows.Next() {
		var nonUnique int
		var columnName, collation, indexType, visible sql.NullString
		var subPart sql.NullInt64
		if err := rows.Scan(&nonUnique, &columnName, &collation, &indexType, &visible, &subPart); err != nil {
			return nil, false, fmt.Errorf("query index migration: scan %s.%s: %w", spec.TableName, spec.IndexName, err)
		}
		state = append(state, queryIndexStateColumn{
			Name: columnName.String, Desc: strings.EqualFold(collation.String, "D"), NonUnique: nonUnique,
			IndexType: indexType.String, Visible: strings.EqualFold(visible.String, "YES"), FullWidth: !subPart.Valid,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("query index migration: iterate %s.%s: %w", spec.TableName, spec.IndexName, err)
	}
	return state, len(state) > 0, nil
}

func queryIndexStateMatches(actual []queryIndexStateColumn, expected []queryIndexColumn) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if actual[index].NonUnique != 1 || !strings.EqualFold(actual[index].IndexType, "BTREE") ||
			!actual[index].Visible || !actual[index].FullWidth ||
			!strings.EqualFold(actual[index].Name, expected[index].Name) ||
			actual[index].Desc != expected[index].Desc {
			return false
		}
	}
	return true
}

func buildAddQueryIndexSQL(spec queryIndexSpec) (string, error) {
	if !mallWeatherIndexNamePattern.MatchString(spec.TableName) ||
		!mallWeatherIndexNamePattern.MatchString(spec.IndexName) || len(spec.Columns) == 0 {
		return "", fmt.Errorf("query index migration: unsafe index specification")
	}
	columns := make([]string, len(spec.Columns))
	for index, column := range spec.Columns {
		if !mallWeatherIndexNamePattern.MatchString(column.Name) {
			return "", fmt.Errorf("query index migration: unsafe index column")
		}
		direction := " ASC"
		if column.Desc {
			direction = " DESC"
		}
		columns[index] = "`" + column.Name + "`" + direction
	}
	return "ALTER TABLE `" + spec.TableName + "` ADD INDEX `" + spec.IndexName + "` (" +
		strings.Join(columns, ", ") + "), ALGORITHM=INPLACE, LOCK=NONE", nil
}
