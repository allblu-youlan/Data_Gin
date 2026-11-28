package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const qimaiSalesSyncIndexMigrationVersion = "2026-11-28-qimai-sales-sync-index-v1"

func qimaiSalesSyncQueryIndex() queryIndexSpec {
	return queryIndexSpec{
		TableName: "qimai_order_data", IndexName: "idx_qimai_sales_sync",
		Columns: []queryIndexColumn{{Name: "shop_code"}, {Name: "status"}, {Name: "synced"}},
	}
}

// The schema migration lock is already held by the caller. Keep a separate
// marker so installations with the older charset/schema migrations still upgrade.
func applyQimaiSalesSyncIndexMigration(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("qimai sales sync index migration: database is unavailable")
	}
	applied, err := schemaMigrationApplied(db, qimaiSalesSyncIndexMigrationVersion)
	if err != nil || applied {
		return err
	}
	return db.Connection(func(tx *gorm.DB) (resultErr error) {
		conn, ok := tx.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return fmt.Errorf("qimai sales sync index migration: dedicated connection is unavailable")
		}
		ctx := tx.Statement.Context
		var previousLockWait uint64
		if err := conn.QueryRowContext(ctx, "SELECT @@SESSION.lock_wait_timeout").Scan(&previousLockWait); err != nil {
			return fmt.Errorf("qimai sales sync index migration: read metadata lock timeout: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "SET SESSION lock_wait_timeout = 5"); err != nil {
			return fmt.Errorf("qimai sales sync index migration: set metadata lock timeout: %w", err)
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := conn.ExecContext(cleanupCtx, "SET SESSION lock_wait_timeout = ?", previousLockWait); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("qimai sales sync index migration: restore metadata lock timeout: %w", err))
			}
		}()
		if err := applyQueryIndex(ctx, conn, qimaiSalesSyncQueryIndex()); err != nil {
			return err
		}
		// Online DDL commits independently; record completion only after verification.
		marker := schemaMigrationRecord{Version: qimaiSalesSyncIndexMigrationVersion, AppliedAt: time.Now().UTC()}
		if err := tx.Create(&marker).Error; err != nil {
			return fmt.Errorf("qimai sales sync index migration: write completion marker: %w", err)
		}
		return nil
	})
}
