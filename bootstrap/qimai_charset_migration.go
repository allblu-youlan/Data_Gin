package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

const qimaiCharsetMigrationVersion = "2026-11-18-qimai-order-utf8mb4-v1"
const qimaiTableOptions = "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci"

var qimaiTextColumnTypePattern = regexp.MustCompile(`^(varchar\([1-9][0-9]*\)|text)$`)

type qimaiCharsetColumn struct {
	Name       string
	ColumnType string
	Nullable   string
	Charset    string
	Collation  string
	Default    sql.NullString
	Extra      string
	Comment    string
}

func buildQimaiCharsetSQL(tableCollation string, columns []qimaiCharsetColumn) (string, error) {
	if len(columns) == 0 {
		return "", fmt.Errorf("qimai charset migration: text columns are missing")
	}
	changes := make([]string, 0, len(columns)+1)
	if !strings.HasPrefix(tableCollation, "utf8mb4_") {
		if tableCollation != "utf8_general_ci" && tableCollation != "utf8mb3_general_ci" {
			return "", fmt.Errorf("qimai charset migration: table has an unexpected legacy collation")
		}
		changes = append(changes, "DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci")
	}
	for _, column := range columns {
		// UTF8MB4 already supports emoji; keep its existing comparison semantics.
		if column.Charset == "utf8mb4" {
			continue
		}
		// The deployed table has VARCHAR/TEXT, NULL defaults, and no extras or
		// comments. Stop on a different definition rather than silently removing it.
		if !mallWeatherIndexNamePattern.MatchString(column.Name) ||
			!qimaiTextColumnTypePattern.MatchString(column.ColumnType) ||
			(column.Charset != "utf8" && column.Charset != "utf8mb3") ||
			(column.Collation != "utf8_general_ci" && column.Collation != "utf8mb3_general_ci") ||
			(column.Nullable != "YES" && column.Nullable != "NO") ||
			column.Default.Valid || column.Extra != "" || column.Comment != "" {
			return "", fmt.Errorf("qimai charset migration: column %s has an unexpected definition", column.Name)
		}
		nullability := " NOT NULL"
		if column.Nullable == "YES" {
			nullability = " NULL DEFAULT NULL"
		}
		changes = append(changes, "MODIFY COLUMN `"+column.Name+"` "+column.ColumnType+
			" CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"+nullability)
	}
	if len(changes) == 0 {
		return "", nil
	}
	// MODIFY preserves TEXT capacity; CONVERT TO could widen it to MEDIUMTEXT.
	return "ALTER TABLE `qimai_order_data` " + strings.Join(changes, ", "), nil
}

func autoMigrateQimaiOrderTable(db *gorm.DB) error {
	if err := db.Set("gorm:table_options", qimaiTableOptions).AutoMigrate(&model.QIMAI_ORDER_DATA{}); err != nil {
		return fmt.Errorf("migrate qimai order table: %w", err)
	}
	return nil
}

// The caller holds the existing schema migration advisory lock. DDL commits
// independently, so inspect each step again on retry and write the marker last.
func applyQimaiOrderUTF8MB4Migration(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("qimai charset migration: database is unavailable")
	}
	applied, err := schemaMigrationApplied(db, qimaiCharsetMigrationVersion)
	if err != nil || applied {
		return err
	}
	return db.Connection(func(tx *gorm.DB) (resultErr error) {
		conn, ok := tx.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return fmt.Errorf("qimai charset migration: dedicated connection is unavailable")
		}
		ctx := tx.Statement.Context
		var previousLockWait uint64
		if err := conn.QueryRowContext(ctx, "SELECT @@SESSION.lock_wait_timeout").Scan(&previousLockWait); err != nil {
			return fmt.Errorf("qimai charset migration: read metadata lock timeout: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "SET SESSION lock_wait_timeout = 5"); err != nil {
			return fmt.Errorf("qimai charset migration: set metadata lock timeout: %w", err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := conn.ExecContext(cleanupCtx, "SET SESSION lock_wait_timeout = ?", previousLockWait); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("qimai charset migration: restore metadata lock timeout: %w", err))
			}
		}()
		tableCollation, err := loadQimaiTableCollation(ctx, conn)
		if errors.Is(err, sql.ErrNoRows) {
			if err := autoMigrateQimaiOrderTable(tx); err != nil {
				return err
			}
			tableCollation, err = loadQimaiTableCollation(ctx, conn)
		}
		if err != nil {
			return fmt.Errorf("qimai charset migration: inspect table: %w", err)
		}
		columns, err := loadQimaiCharsetColumns(ctx, conn)
		if err != nil {
			return err
		}
		statement, err := buildQimaiCharsetSQL(tableCollation, columns)
		if err != nil {
			return err
		}
		if statement != "" {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("qimai charset migration: upgrade text columns: %w", err)
			}
		}
		tableCollation, err = loadQimaiTableCollation(ctx, conn)
		if err != nil {
			return fmt.Errorf("qimai charset migration: verify table: %w", err)
		}
		columns, err = loadQimaiCharsetColumns(ctx, conn)
		if err != nil {
			return err
		}
		remaining, err := buildQimaiCharsetSQL(tableCollation, columns)
		if err != nil {
			return err
		}
		if remaining != "" {
			return fmt.Errorf("qimai charset migration: text column verification failed")
		}
		if err := ensureQimaiRawDataIndex(ctx, conn); err != nil {
			return err
		}
		marker := schemaMigrationRecord{Version: qimaiCharsetMigrationVersion, AppliedAt: time.Now().UTC()}
		if err := tx.Create(&marker).Error; err != nil {
			return fmt.Errorf("qimai charset migration: write completion marker: %w", err)
		}
		return nil
	})
}

func loadQimaiTableCollation(ctx context.Context, conn *sql.Conn) (string, error) {
	var collation string
	err := conn.QueryRowContext(ctx, `SELECT TABLE_COLLATION FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'qimai_order_data'`).Scan(&collation)
	return collation, err
}

func loadQimaiCharsetColumns(ctx context.Context, conn *sql.Conn) ([]qimaiCharsetColumn, error) {
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, CHARACTER_SET_NAME,
COLLATION_NAME, COLUMN_DEFAULT, EXTRA, COLUMN_COMMENT FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'qimai_order_data' AND CHARACTER_SET_NAME IS NOT NULL
ORDER BY ORDINAL_POSITION`)
	if err != nil {
		return nil, fmt.Errorf("qimai charset migration: inspect text columns: %w", err)
	}
	defer rows.Close()
	var columns []qimaiCharsetColumn
	for rows.Next() {
		var column qimaiCharsetColumn
		if err := rows.Scan(&column.Name, &column.ColumnType, &column.Nullable, &column.Charset,
			&column.Collation, &column.Default, &column.Extra, &column.Comment); err != nil {
			return nil, fmt.Errorf("qimai charset migration: scan text column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("qimai charset migration: read text columns: %w", err)
	}
	return columns, nil
}

func ensureQimaiRawDataIndex(ctx context.Context, conn *sql.Conn) error {
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'qimai_order_data' AND COLUMN_NAME = 'raw_data_id'
AND SEQ_IN_INDEX = 1 AND NON_UNIQUE = 1 AND INDEX_TYPE = 'BTREE' AND IS_VISIBLE = 'YES' AND SUB_PART IS NULL`).Scan(&count); err != nil {
		return fmt.Errorf("qimai charset migration: inspect raw-data indexes: %w", err)
	}
	if count > 0 {
		return nil
	}
	return applyQueryIndex(ctx, conn, queryIndexSpec{
		TableName: "qimai_order_data", IndexName: "idx_qimai_raw_data_id",
		Columns: []queryIndexColumn{{Name: "raw_data_id"}},
	})
}
