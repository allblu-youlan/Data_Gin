package bootstrap

import (
	"context"
	"fmt"
	"time"

	"gin-biz-web-api/internal/service/auth_svc"

	"gorm.io/gorm"
)

const openMonitoringPermissionMigrationVersion = "2026-11-12-open-monitoring-permissions-v1"

// The caller holds the existing schema migration lock. Permission writes and
// their marker share a transaction so failed upgrades remain retryable.
func applyOpenMonitoringPermissionMigration(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate monitoring permissions: database is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		applied, err := schemaMigrationApplied(tx, openMonitoringPermissionMigrationVersion)
		if err != nil || applied {
			return err
		}
		if err := auth_svc.SyncOpenMonitoringPermissionSeeds(ctx, tx); err != nil {
			return err
		}
		marker := schemaMigrationRecord{
			Version: openMonitoringPermissionMigrationVersion, AppliedAt: time.Now().UTC(),
		}
		if err := tx.Create(&marker).Error; err != nil {
			return fmt.Errorf("write monitoring permission migration marker: %w", err)
		}
		return nil
	})
}
