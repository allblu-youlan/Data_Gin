package reportrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxReportResultCleanupBatchSize = 500

var (
	ErrReportResultCleanupConflict  = errors.New("report result cleanup: state conflict")
	ErrReportResultCleanupLeaseLost = errors.New("report result cleanup: lease lost")
)

type ResultCleanupCandidate struct {
	RunID    uint `gorm:"column:run_id"`
	ExportID uint `gorm:"column:export_id"`
}

type ResultCleanupRuntime struct {
	Run        model.ReportRun
	Export     *model.ReportExport
	Version    model.ReportVersion
	Datasource model.ReportDatasource
	Columns    []model.ReportColumn
}

func (repository *Repository) ListReadyResultCleanupCandidates(ctx context.Context, now time.Time, afterExportID uint, limit int) ([]ResultCleanupCandidate, error) {
	if repository == nil || repository.db == nil || ctx == nil || now.IsZero() || limit < 1 || limit > maxReportResultCleanupBatchSize {
		return nil, fmt.Errorf("report result cleanup: invalid ready candidate query")
	}
	var candidates []ResultCleanupCandidate
	err := repository.db.WithContext(ctx).Table("report_exports AS exports").
		Select("exports.run_id AS run_id, exports.id AS export_id").
		Joins("JOIN report_runs AS runs ON runs.id = exports.run_id").
		Where("runs.data_scope_hash IS NULL OR runs.data_scope_hash = ? OR runs.result_expires_at <= ?", "", now.UTC().Truncate(time.Millisecond)).
		Where("exports.id > ? AND exports.status = ? AND exports.purged_at IS NULL", afterExportID, model.ReportExportStatusReady).
		Where("runs.result_purged_at IS NULL AND runs.status IN ?", []string{model.ReportRunStatusSucceeded, model.ReportRunStatusResultPurging}).
		Where("exports.lease_token IS NULL OR exports.lease_token = ? OR exports.lease_expires_at IS NULL OR exports.lease_expires_at <= ?", "", now.UTC().Truncate(time.Millisecond)).
		Order("exports.id ASC").Limit(limit).Find(&candidates).Error
	if err != nil {
		return nil, fmt.Errorf("report result cleanup: list ready candidates: %w", err)
	}
	return candidates, nil
}

func (repository *Repository) ListExpiredResultCleanupCandidates(ctx context.Context, now time.Time, afterRunID uint, limit int) ([]ResultCleanupCandidate, error) {
	if repository == nil || repository.db == nil || ctx == nil || now.IsZero() || limit < 1 || limit > maxReportResultCleanupBatchSize {
		return nil, fmt.Errorf("report result cleanup: invalid expired candidate query")
	}
	now = now.UTC().Truncate(time.Millisecond)
	var candidates []ResultCleanupCandidate
	err := repository.db.WithContext(ctx).Table("report_runs AS runs").
		Select("runs.id AS run_id, COALESCE(exports.id, 0) AS export_id").
		Joins("LEFT JOIN report_exports AS exports ON exports.run_id = runs.id").
		Where("runs.id > ? AND runs.result_expires_at IS NOT NULL AND runs.result_expires_at <= ? AND runs.result_purged_at IS NULL", afterRunID, now).
		Where("runs.status IN ?", []string{model.ReportRunStatusSucceeded, model.ReportRunStatusResultPurging}).
		Where("runs.lease_token IS NULL OR runs.lease_token = ? OR runs.lease_expires_at IS NULL OR runs.lease_expires_at <= ?", "", now).
		Where("exports.id IS NULL OR exports.status IN ?", []string{model.ReportExportStatusPending, model.ReportExportStatusRunning, model.ReportExportStatusFailed, model.ReportExportStatusCancelled, model.ReportExportStatusExpired}).
		Where("exports.id IS NULL OR exports.lease_token IS NULL OR exports.lease_token = ? OR exports.lease_expires_at IS NULL OR exports.lease_expires_at <= ?", "", now).
		Order("runs.id ASC").Limit(limit).Find(&candidates).Error
	if err != nil {
		return nil, fmt.Errorf("report result cleanup: list expired candidates: %w", err)
	}
	return candidates, nil
}

func (repository *Repository) ClaimExpiredResultCleanup(ctx context.Context, runID uint, leaseToken string, now time.Time, leaseTTL time.Duration) (*ResultCleanupRuntime, error) {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil || now.IsZero() || leaseTTL <= 0 {
		return nil, fmt.Errorf("report result cleanup: invalid claim")
	}
	now = now.UTC().Truncate(time.Millisecond)
	runtime := &ResultCleanupRuntime{}
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&runtime.Run, runID).Error; err != nil {
			return fmt.Errorf("report result cleanup: lock run: %w", err)
		}
		if runtime.Run.ResultPurgedAt != nil || runtime.Run.ResultExpiresAt == nil || now.Before(runtime.Run.ResultExpiresAt.UTC()) ||
			(runtime.Run.Status != model.ReportRunStatusSucceeded && runtime.Run.Status != model.ReportRunStatusResultPurging) ||
			(runtime.Run.LeaseToken != "" && runtime.Run.LeaseExpiresAt != nil && now.Before(runtime.Run.LeaseExpiresAt.UTC())) {
			return ErrReportResultCleanupConflict
		}
		var export model.ReportExport
		exportErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ?", runID).First(&export).Error
		if exportErr == nil {
			if (export.Status != model.ReportExportStatusPending && export.Status != model.ReportExportStatusRunning && export.Status != model.ReportExportStatusFailed && export.Status != model.ReportExportStatusCancelled && export.Status != model.ReportExportStatusExpired) ||
				(export.LeaseToken != "" && export.LeaseExpiresAt != nil && now.Before(export.LeaseExpiresAt.UTC())) {
				return ErrReportResultCleanupConflict
			}
			runtime.Export = &export
		} else if !errors.Is(exportErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("report result cleanup: lock export: %w", exportErr)
		}
		if err := tx.Where("run_id = ? AND expires_at <= ?", runID, now).Delete(&model.ReportResultReadLease{}).Error; err != nil {
			return fmt.Errorf("report result cleanup: delete expired read leases: %w", err)
		}
		var activeReaders int64
		if err := tx.Model(&model.ReportResultReadLease{}).Where("run_id = ? AND expires_at > ?", runID, now).Count(&activeReaders).Error; err != nil {
			return fmt.Errorf("report result cleanup: count active readers: %w", err)
		}
		if activeReaders > 0 {
			return ErrReportResultCleanupConflict
		}
		if err := tx.Where("id = ? AND definition_id = ?", runtime.Run.VersionID, runtime.Run.DefinitionID).First(&runtime.Version).Error; err != nil {
			return fmt.Errorf("report result cleanup: load version: %w", err)
		}
		if err := tx.Where("id = ? AND driver = ?", runtime.Version.DatasourceID, model.ReportDatasourceDriverOracle).First(&runtime.Datasource).Error; err != nil {
			return fmt.Errorf("report result cleanup: load datasource: %w", err)
		}
		if err := tx.Where("version_id = ?", runtime.Version.ID).Order("display_order ASC, id ASC").Find(&runtime.Columns).Error; err != nil {
			return fmt.Errorf("report result cleanup: load columns: %w", err)
		}
		expiresAt := now.Add(leaseTTL).UTC().Truncate(time.Millisecond)
		result := tx.Model(&model.ReportRun{}).Where("id = ? AND result_purged_at IS NULL", runID).Updates(map[string]interface{}{
			"status": model.ReportRunStatusResultPurging, "worker_id": "report-result-cleanup", "lease_token": leaseToken,
			"lease_expires_at": expiresAt, "heartbeat_at": now, "updated_at": now,
		})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return fmt.Errorf("report result cleanup: claim run: %w", result.Error)
			}
			return ErrReportResultCleanupConflict
		}
		if runtime.Export != nil {
			exportUpdates := map[string]interface{}{
				"lease_token": leaseToken, "lease_expires_at": expiresAt, "heartbeat_at": now, "purge_started_at": now, "updated_at": now,
			}
			if runtime.Export.Status == model.ReportExportStatusPending || runtime.Export.Status == model.ReportExportStatusRunning {
				exportUpdates["status"] = model.ReportExportStatusExpired
				runtime.Export.Status = model.ReportExportStatusExpired
			}
			result = tx.Model(&model.ReportExport{}).Where("id = ? AND purged_at IS NULL", runtime.Export.ID).Updates(exportUpdates)
			if result.Error != nil || result.RowsAffected != 1 {
				return ErrReportResultCleanupConflict
			}
		}
		runtime.Run.Status, runtime.Run.LeaseToken, runtime.Run.LeaseExpiresAt = model.ReportRunStatusResultPurging, leaseToken, &expiresAt
		detail := map[string]interface{}{"reasonCode": "RESULT_EXPIRED"}
		if runtime.Export != nil {
			detail["exportId"] = runtime.Export.ID
		}
		return repository.writeSystemAudit(ctx, tx, "REPORT_RESULT_PURGE_STARTED", "REPORT_RUN", runID, detail)
	})
	if err != nil {
		return nil, err
	}
	return runtime, nil
}

func (repository *Repository) UpdateExpiredResultCleanupProgress(ctx context.Context, runID uint, leaseToken string, purgedRows int64, now time.Time, leaseTTL time.Duration) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil || purgedRows < 0 || now.IsZero() || leaseTTL <= 0 {
		return fmt.Errorf("report result cleanup: invalid progress")
	}
	now = now.UTC().Truncate(time.Millisecond)
	result := repository.db.WithContext(ctx).Model(&model.ReportRun{}).
		Where("id = ? AND status = ? AND result_purged_at IS NULL AND lease_token = ?", runID, model.ReportRunStatusResultPurging, leaseToken).
		Updates(map[string]interface{}{"heartbeat_at": now, "lease_expires_at": now.Add(leaseTTL).UTC().Truncate(time.Millisecond), "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("report result cleanup: update progress: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrReportResultCleanupLeaseLost
	}
	if err := repository.db.WithContext(ctx).Model(&model.ReportExport{}).Where("run_id = ? AND lease_token = ?", runID, leaseToken).
		Updates(map[string]interface{}{"purged_rows": purgedRows, "heartbeat_at": now, "lease_expires_at": now.Add(leaseTTL).UTC().Truncate(time.Millisecond), "updated_at": now}).Error; err != nil {
		return fmt.Errorf("report result cleanup: update export progress: %w", err)
	}
	return nil
}

func (repository *Repository) MarkExpiredResultPurged(ctx context.Context, runID uint, leaseToken string, purgedRows int64, now time.Time) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil || purgedRows < 0 || now.IsZero() {
		return fmt.Errorf("report result cleanup: invalid completion")
	}
	now = now.UTC().Truncate(time.Millisecond)
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ReportRun{}).Where("id = ? AND status = ? AND result_purged_at IS NULL AND lease_token = ?", runID, model.ReportRunStatusResultPurging, leaseToken).
			Updates(map[string]interface{}{"status": model.ReportRunStatusResultPurged, "result_purged_at": now, "worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return fmt.Errorf("report result cleanup: finish run: %w", result.Error)
			}
			return ErrReportResultCleanupLeaseLost
		}
		var export model.ReportExport
		if err := tx.Where("run_id = ?", runID).First(&export).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return repository.writeSystemAudit(ctx, tx, "REPORT_RESULT_PURGED", "REPORT_RUN", runID, map[string]interface{}{"purgedRows": purgedRows})
		} else if err != nil {
			return fmt.Errorf("report result cleanup: load export completion: %w", err)
		}
		updates := map[string]interface{}{"purged_at": now, "purged_rows": purgedRows, "worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now}
		if export.Status == model.ReportExportStatusPending || export.Status == model.ReportExportStatusRunning {
			updates["status"] = model.ReportExportStatusExpired
		}
		if result := tx.Model(&model.ReportExport{}).Where("id = ? AND purged_at IS NULL AND lease_token = ?", export.ID, leaseToken).Updates(updates); result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return fmt.Errorf("report result cleanup: finish export: %w", result.Error)
			}
			return ErrReportResultCleanupLeaseLost
		}
		return repository.writeSystemAudit(ctx, tx, "REPORT_RESULT_PURGED", "REPORT_RUN", runID, map[string]interface{}{"exportId": export.ID, "purgedRows": purgedRows})
	})
}

func (repository *Repository) ReleaseExpiredResultCleanup(ctx context.Context, runID uint, leaseToken string, now time.Time) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil || now.IsZero() {
		return fmt.Errorf("report result cleanup: invalid release")
	}
	now = now.UTC().Truncate(time.Millisecond)
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ReportRun{}).Where("id = ? AND status = ? AND lease_token = ?", runID, model.ReportRunStatusResultPurging, leaseToken).
			Updates(map[string]interface{}{"worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return fmt.Errorf("report result cleanup: release run: %w", result.Error)
			}
			return ErrReportResultCleanupLeaseLost
		}
		if err := tx.Model(&model.ReportExport{}).Where("run_id = ? AND lease_token = ?", runID, leaseToken).
			Updates(map[string]interface{}{"worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("report result cleanup: release export: %w", err)
		}
		return repository.writeSystemAudit(ctx, tx, "REPORT_RESULT_PURGE_RETRY", "REPORT_RUN", runID, map[string]interface{}{"reasonCode": "RESULT_EXPIRED"})
	})
}
