package reportrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrReportRunNotFound  = errors.New("report execution: run not found")
	ErrReportRunLeaseLost = errors.New("report execution: lease lost")
)

type RunDisposition uint8

const (
	RunDispositionUnknown RunDisposition = iota
	RunDispositionAcquired
	RunDispositionBusy
	RunDispositionTerminal
	RunDispositionReconcile
)

type RunControl uint8

const (
	RunControlUnknown RunControl = iota
	RunControlContinue
	RunControlCancelRequested
	RunControlLeaseLost
)

type RunLease struct {
	Disposition RunDisposition
	LeaseToken  string
	Run         model.ReportRun
	Blocker     *RunExecutionBlocker
}

type RunExecutionBlocker struct {
	RunID                uint       `gorm:"column:run_id"`
	RequestedBy          uint       `gorm:"column:requested_by"`
	Status               string     `gorm:"column:status"`
	Attempt              int        `gorm:"column:attempt"`
	CancelRequested      bool       `gorm:"column:cancel_requested"`
	OracleStartedAt      *time.Time `gorm:"column:oracle_started_at"`
	LeaseExpiresAt       *time.Time `gorm:"column:lease_expires_at"`
	ResultExpiresAt      *time.Time `gorm:"column:result_expires_at"`
	ResultPurgedAt       *time.Time `gorm:"column:result_purged_at"`
	ExportID             *uint      `gorm:"column:export_id"`
	ExportStatus         *string    `gorm:"column:export_status"`
	ExportAttempt        *int       `gorm:"column:export_attempt"`
	ExportLeaseExpiresAt *time.Time `gorm:"column:export_lease_expires_at"`
	ExportPurgedAt       *time.Time `gorm:"column:export_purged_at"`
}

type RuntimeContract struct {
	Run        model.ReportRun
	Definition model.ReportDefinition
	Version    model.ReportVersion
	Datasource model.ReportDatasource
	Parameters []model.ReportParameter
	Columns    []model.ReportColumn
}

func (repository *Repository) ListReconciliationCandidates(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if repository == nil || repository.db == nil || ctx == nil || now.IsZero() || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("report reconciliation: invalid candidate query")
	}
	now = now.UTC().Truncate(time.Millisecond)
	var runIDs []uint
	err := buildReconciliationCandidateQuery(repository.db.WithContext(ctx), now, limit).Pluck("id", &runIDs).Error
	if err != nil {
		return nil, fmt.Errorf("report reconciliation: list candidates: %w", err)
	}
	return runIDs, nil
}

func (repository *Repository) RecoverLegacySnapshotStates(ctx context.Context, now time.Time, limit int) (int64, error) {
	if repository == nil || repository.db == nil || ctx == nil || now.IsZero() || limit < 1 || limit > 100 {
		return 0, fmt.Errorf("report execution recovery: invalid legacy state request")
	}
	now = now.UTC().Truncate(time.Millisecond)
	var candidates []struct {
		ID     uint   `gorm:"column:id"`
		Status string `gorm:"column:status"`
	}
	if err := legacySnapshotStateQuery(repository.db.WithContext(ctx), limit).Find(&candidates).Error; err != nil {
		return 0, fmt.Errorf("report execution recovery: list legacy states: %w", err)
	}
	var recovered int64
	for _, candidate := range candidates {
		updates := map[string]interface{}{
			"worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now,
		}
		switch candidate.Status {
		case model.ReportRunStatusExporting:
			updates["status"] = model.ReportRunStatusSucceeded
		case model.ReportRunStatusExported:
			updates["status"] = model.ReportRunStatusSucceeded
			updates["result_expires_at"] = now
		default:
			continue
		}
		result := repository.db.WithContext(ctx).Model(&model.ReportRun{}).
			Where("id = ? AND status = ? AND result_purged_at IS NULL", candidate.ID, candidate.Status).
			Updates(updates)
		if result.Error != nil {
			return recovered, fmt.Errorf("report execution recovery: recover legacy state: %w", result.Error)
		}
		recovered += result.RowsAffected
	}
	return recovered, nil
}

type legacySupersededDisposition uint8

const (
	legacySupersededWait legacySupersededDisposition = iota
	legacySupersededOverwritten
	legacySupersededNeedsPurge
)

func (repository *Repository) RecoverLegacySupersededSnapshots(ctx context.Context, now time.Time, limit int) (int64, error) {
	if repository == nil || repository.db == nil || ctx == nil || now.IsZero() || limit < 1 || limit > 100 {
		return 0, fmt.Errorf("report execution recovery: invalid superseded snapshot request")
	}
	now = now.UTC().Truncate(time.Millisecond)
	var candidates []struct {
		ID           uint `gorm:"column:id"`
		DefinitionID uint `gorm:"column:definition_id"`
	}
	if err := legacySupersededSnapshotQuery(repository.db.WithContext(ctx), limit).
		Select("id", "definition_id").Find(&candidates).Error; err != nil {
		return 0, fmt.Errorf("report execution recovery: list superseded snapshots: %w", err)
	}
	var recovered int64
	for _, candidate := range candidates {
		err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var definition model.ReportDefinition
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&definition, candidate.DefinitionID).Error; err != nil {
				return fmt.Errorf("lock superseded snapshot scope: %w", err)
			}
			var run model.ReportRun
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "definition_id", "version_id", "status", "result_purged_at").First(&run, candidate.ID).Error; err != nil {
				return fmt.Errorf("lock superseded snapshot: %w", err)
			}
			if run.DefinitionID != candidate.DefinitionID || run.Status != model.ReportRunStatusSuperseded || run.ResultPurgedAt != nil {
				return nil
			}
			var version model.ReportVersion
			if err := tx.Select("id", "execution_mode", "result_table_owner", "result_table_name").
				Where("id = ? AND definition_id = ?", run.VersionID, run.DefinitionID).First(&version).Error; err != nil {
				return fmt.Errorf("load superseded snapshot version: %w", err)
			}
			var newerPurged int64
			var newerSnapshot int64
			if version.ExecutionMode == model.ReportExecutionModeTableSnapshot {
				newerSameTable := tx.Table("report_runs AS newer").
					Joins("JOIN report_versions AS versions ON versions.id = newer.version_id AND versions.definition_id = newer.definition_id").
					Where("newer.definition_id = ? AND newer.id > ?", run.DefinitionID, run.ID).
					Where("versions.execution_mode = ? AND versions.result_table_owner = ? AND versions.result_table_name = ?",
						model.ReportExecutionModeTableSnapshot, version.ResultTableOwner, version.ResultTableName)
				if err := newerSameTable.
					Where("newer.status = ? AND newer.result_purged_at IS NOT NULL", model.ReportRunStatusResultPurged).
					Count(&newerPurged).Error; err != nil {
					return fmt.Errorf("inspect newer purged snapshot: %w", err)
				}
				if newerPurged == 0 {
					if err := newerSameTable.
						Where("newer.status IN ? OR (newer.oracle_started_at IS NOT NULL AND newer.status IN ?)",
							unpurgedSnapshotStatuses(), []string{model.ReportRunStatusRunning, model.ReportRunStatusCancelRequested, model.ReportRunStatusUnknown, model.ReportRunStatusReconciling}).
						Count(&newerSnapshot).Error; err != nil {
						return fmt.Errorf("inspect newer snapshot ownership: %w", err)
					}
				}
			}
			disposition := classifyLegacySupersededSnapshot(newerPurged > 0, newerSnapshot > 0)
			updates := map[string]interface{}{
				"worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now,
			}
			action := ""
			detail := map[string]interface{}{}
			switch disposition {
			case legacySupersededWait:
				return nil
			case legacySupersededOverwritten:
				updates["status"] = model.ReportRunStatusResultPurged
				updates["result_purged_at"] = now
				action = "REPORT_RESULT_PURGED"
				detail["reasonCode"] = "SHARED_TABLE_PURGED_BY_NEWER_SNAPSHOT"
			case legacySupersededNeedsPurge:
				updates["status"] = model.ReportRunStatusSucceeded
				updates["result_expires_at"] = now
				action = "REPORT_RESULT_CLEANUP_QUEUED"
				detail["reasonCode"] = "LEGACY_SUPERSEDED_SNAPSHOT"
			}
			result := tx.Model(&model.ReportRun{}).
				Where("id = ? AND status = ? AND result_purged_at IS NULL", run.ID, model.ReportRunStatusSuperseded).
				Updates(updates)
			if result.Error != nil {
				return fmt.Errorf("recover superseded snapshot: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return nil
			}
			recovered++
			return repository.writeSystemAudit(ctx, tx, action, "REPORT_RUN", run.ID, detail)
		})
		if err != nil {
			return recovered, fmt.Errorf("report execution recovery: %w", err)
		}
	}
	return recovered, nil
}

func legacySupersededSnapshotQuery(db *gorm.DB, limit int) *gorm.DB {
	return db.Model(&model.ReportRun{}).
		Where("status = ? AND result_purged_at IS NULL", model.ReportRunStatusSuperseded).
		Order("id DESC").Limit(limit)
}

func unpurgedSnapshotStatuses() []string {
	return []string{
		model.ReportRunStatusSucceeded, model.ReportRunStatusExporting, model.ReportRunStatusExported,
		model.ReportRunStatusResultPurging, model.ReportRunStatusSuperseded,
	}
}

func classifyLegacySupersededSnapshot(newerPurged, newerSnapshot bool) legacySupersededDisposition {
	if newerPurged {
		return legacySupersededOverwritten
	}
	if newerSnapshot {
		return legacySupersededWait
	}
	return legacySupersededNeedsPurge
}

func legacySnapshotStateQuery(db *gorm.DB, limit int) *gorm.DB {
	return db.Model(&model.ReportRun{}).
		Select("id", "status").
		Where("status IN ? AND result_purged_at IS NULL", []string{
			model.ReportRunStatusExporting, model.ReportRunStatusExported,
		}).
		Order("id ASC").Limit(limit)
}

func buildReconciliationCandidateQuery(db *gorm.DB, now time.Time, limit int) *gorm.DB {
	return db.Model(&model.ReportRun{}).
		Where(
			"(status = ? AND (next_reconcile_at IS NULL OR next_reconcile_at <= ?)) OR "+
				"(status = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)) OR "+
				"(status = ? AND oracle_started_at IS NOT NULL AND (lease_expires_at IS NULL OR lease_expires_at <= ?))",
			model.ReportRunStatusUnknown, now,
			model.ReportRunStatusReconciling, now,
			model.ReportRunStatusRunning, now,
		).
		Order("id ASC").Limit(limit)
}

func (repository *Repository) ListQueuedRunsMissingDelivery(ctx context.Context, limit int) ([]uint, error) {
	if repository == nil || repository.db == nil || ctx == nil || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("report execution recovery: invalid queued query")
	}
	var runIDs []uint
	err := repository.db.WithContext(ctx).Model(&model.ReportRun{}).Where("status = ? AND updated_at <= ?", model.ReportRunStatusQueued, time.Now().UTC().Add(-30*time.Second).Truncate(time.Millisecond)).
		Order("id ASC").Limit(limit).Pluck("id", &runIDs).Error
	if err != nil {
		return nil, fmt.Errorf("report execution recovery: list queued runs: %w", err)
	}
	return runIDs, nil
}

func (repository *Repository) ListExpiredQueuedRuns(ctx context.Context, cutoff time.Time, limit int) ([]uint, error) {
	if repository == nil || repository.db == nil || ctx == nil || cutoff.IsZero() || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("report execution recovery: invalid expired queue query")
	}
	var runIDs []uint
	err := expiredQueuedRunQuery(repository.db.WithContext(ctx), cutoff, limit).Pluck("id", &runIDs).Error
	if err != nil {
		return nil, fmt.Errorf("report execution recovery: list expired queued runs: %w", err)
	}
	return runIDs, nil
}

func expiredQueuedRunQuery(db *gorm.DB, cutoff time.Time, limit int) *gorm.DB {
	return db.Model(&model.ReportRun{}).
		Where("status = ? AND created_at <= ?", model.ReportRunStatusQueued, cutoff.UTC().Truncate(time.Millisecond)).
		Order("id ASC").Limit(limit)
}

func (repository *Repository) EnsureRunQueued(ctx context.Context, runID uint, now time.Time) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || now.IsZero() {
		return fmt.Errorf("report execution recovery: invalid queue request")
	}
	now = now.UTC().Truncate(time.Millisecond)
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.ReportRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "run_uuid", "status").First(&run, runID).Error; err != nil {
			return fmt.Errorf("report execution recovery: lock queued run: %w", err)
		}
		if run.Status != model.ReportRunStatusQueued {
			return nil
		}
		if result := restoreJobOutbox(tx, recoveredRunOutbox(run, now), now); result.Error != nil {
			return fmt.Errorf("report execution recovery: restore queued outbox: %w", result.Error)
		}
		if result := touchQueuedRunRecovery(tx, run.ID, now); result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return fmt.Errorf("report execution recovery: rotate queued run: %w", result.Error)
			}
			return ErrReportRunLeaseLost
		}
		return nil
	})
}

func touchQueuedRunRecovery(tx *gorm.DB, runID uint, now time.Time) *gorm.DB {
	return tx.Model(&model.ReportRun{}).Where("id = ? AND status = ?", runID, model.ReportRunStatusQueued).
		Update("updated_at", now.UTC().Truncate(time.Millisecond))
}

func (repository *Repository) MarkQueuedExecutionFailed(ctx context.Context, runID uint, code, safeMessage string, now time.Time) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || !validRunError(code, safeMessage) || now.IsZero() {
		return fmt.Errorf("report execution: invalid queued failure")
	}
	now = now.UTC().Truncate(time.Millisecond)
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ReportRun{}).Where("id = ? AND status = ?", runID, model.ReportRunStatusQueued).Updates(map[string]interface{}{
			"status": model.ReportRunStatusFailed, "finished_at": now, "error_code": code,
			"error_message_safe": strings.TrimSpace(safeMessage), "updated_at": now,
		})
		if result.Error != nil {
			return fmt.Errorf("report execution: fail queued run: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrReportRunLeaseLost
		}
		return repository.writeSystemAudit(ctx, tx, "REPORT_RUN_FAILED", "REPORT_RUN", runID, map[string]interface{}{"reasonCode": code})
	})
}

func (repository *Repository) RecoverExpiredPreOracleRuns(ctx context.Context, now time.Time, limit int) (int64, error) {
	if repository == nil || repository.db == nil || ctx == nil || now.IsZero() || limit < 1 || limit > 100 {
		return 0, fmt.Errorf("report execution recovery: invalid request")
	}
	now = now.UTC().Truncate(time.Millisecond)
	var candidateIDs []uint
	err := expiredInterruptedRunQuery(repository.db.WithContext(ctx), now, limit).Pluck("id", &candidateIDs).Error
	if err != nil {
		return 0, fmt.Errorf("report execution recovery: list expired runs: %w", err)
	}
	var recovered int64
	for _, runID := range candidateIDs {
		err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var run model.ReportRun
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, runID).Error; err != nil {
				return fmt.Errorf("lock interrupted run: %w", err)
			}
			if (run.Status != model.ReportRunStatusRunning && run.Status != model.ReportRunStatusCancelRequested) ||
				run.LeaseExpiresAt != nil && now.Before(run.LeaseExpiresAt.UTC()) {
				return nil
			}
			if run.OracleStartedAt != nil {
				result := tx.Model(&model.ReportRun{}).Where("id = ? AND status IN ?", run.ID,
					[]string{model.ReportRunStatusRunning, model.ReportRunStatusCancelRequested}).
					Updates(unknownRunUpdates(now, "INTERRUPTED_AFTER_ORACLE_START", "执行结果需要对账"))
				if result.Error != nil {
					return fmt.Errorf("quarantine interrupted run: %w", result.Error)
				}
				if result.RowsAffected == 0 {
					return nil
				}
				recovered++
				return repository.writeSystemAudit(ctx, tx, "REPORT_RUN_UNKNOWN", "REPORT_RUN", run.ID, map[string]interface{}{"reasonCode": "INTERRUPTED_AFTER_ORACLE_START"})
			}
			if run.CancelRequested || run.Status == model.ReportRunStatusCancelRequested {
				result := tx.Model(&model.ReportRun{}).Where("id = ? AND status IN ?", run.ID,
					[]string{model.ReportRunStatusRunning, model.ReportRunStatusCancelRequested}).
					Updates(terminalRunUpdates(model.ReportRunStatusCancelled, "CANCELLED", "报表运行已取消", now))
				if result.Error != nil {
					return fmt.Errorf("cancel interrupted run: %w", result.Error)
				}
				if result.RowsAffected == 0 {
					return nil
				}
				recovered++
				return repository.writeSystemAudit(ctx, tx, "REPORT_RUN_CANCELLED", "REPORT_RUN", run.ID, map[string]interface{}{"reasonCode": "INTERRUPTED_BEFORE_ORACLE_START"})
			}
			result := tx.Model(&model.ReportRun{}).
				Where("id = ? AND status = ? AND oracle_started_at IS NULL", run.ID, model.ReportRunStatusRunning).
				Updates(map[string]interface{}{
					"status": model.ReportRunStatusQueued, "worker_id": "", "lease_token": "", "lease_expires_at": nil,
					"heartbeat_at": nil, "error_code": "", "error_message_safe": "", "updated_at": now,
				})
			if result.Error != nil {
				return fmt.Errorf("release run: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return nil
			}
			if result := restoreJobOutbox(tx, recoveredRunOutbox(run, now), now); result.Error != nil {
				return fmt.Errorf("restore outbox: %w", result.Error)
			}
			if err := repository.writeSystemAudit(ctx, tx, "REPORT_RUN_RETRY_QUEUED", "REPORT_RUN", run.ID, map[string]interface{}{"reasonCode": "PRE_ORACLE_LEASE_EXPIRED"}); err != nil {
				return err
			}
			recovered++
			return nil
		})
		if err != nil {
			return recovered, fmt.Errorf("report execution recovery: %w", err)
		}
	}
	return recovered, nil
}

func expiredInterruptedRunQuery(db *gorm.DB, now time.Time, limit int) *gorm.DB {
	return db.Model(&model.ReportRun{}).
		Where("status IN ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)",
			[]string{model.ReportRunStatusRunning, model.ReportRunStatusCancelRequested}, now.UTC().Truncate(time.Millisecond)).
		Order("id ASC").Limit(limit)
}

func recoveredRunOutbox(run model.ReportRun, now time.Time) model.AsyncJobOutbox {
	outbox := NewReportRunOutbox(run.RunUUID, now)
	outbox.PayloadJSON = model.JSONText(fmt.Sprintf(`{"run_id":%d}`, run.ID))
	return outbox
}

func restoreJobOutbox(tx *gorm.DB, outbox model.AsyncJobOutbox, now time.Time) *gorm.DB {
	now = now.UTC().Truncate(time.Millisecond)
	outbox.AvailableAt = now
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"task_type": outbox.TaskType, "payload_json": outbox.PayloadJSON, "queue_name": outbox.QueueName,
			"available_at": now, "published_at": nil, "attempts": 0, "last_error_safe": "",
			"locked_by": "", "locked_at": nil, "updated_at": now,
		}),
	}).Create(&outbox)
}

func (repository *Repository) BeginExecution(
	ctx context.Context,
	runID uint,
	workerID, leaseToken string,
	now time.Time,
	leaseTTL time.Duration,
) (*RunLease, error) {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 ||
		strings.TrimSpace(workerID) == "" || len(workerID) > 128 || uuid.Validate(leaseToken) != nil ||
		now.IsZero() || leaseTTL <= 0 {
		return nil, fmt.Errorf("report execution: invalid lease input")
	}
	now = now.UTC().Truncate(time.Millisecond)
	lease := &RunLease{}
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tableSnapshot, err := lockReportExecutionScope(tx, runID)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lease.Run, runID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrReportRunNotFound
		} else if err != nil {
			return fmt.Errorf("report execution: lock run: %w", err)
		}
		if cancellableRunBeforeOracle(lease.Run, now) {
			updates := terminalRunUpdates(model.ReportRunStatusCancelled, "CANCELLED", "报表运行已取消", now)
			if result := tx.Model(&model.ReportRun{}).Where("id = ?", runID).Updates(updates); result.Error != nil {
				return fmt.Errorf("report execution: cancel before Oracle start: %w", result.Error)
			} else if result.RowsAffected != 1 {
				return fmt.Errorf("report execution: cancel before Oracle start count changed")
			}
			lease.Disposition = RunDispositionTerminal
			lease.Run.Status = model.ReportRunStatusCancelled
			lease.Run.FinishedAt = &now
			return repository.writeSystemAudit(ctx, tx, "REPORT_RUN_CANCELLED", "REPORT_RUN", runID, map[string]interface{}{"reasonCode": "CANCELLED_BEFORE_ORACLE_START"})
		}
		disposition, err := classifyRunStart(&lease.Run, now)
		if err != nil {
			return err
		}
		lease.Disposition = disposition
		if disposition == RunDispositionReconcile && lease.Run.Status == model.ReportRunStatusRunning {
			updates := unknownRunUpdates(now, "WORKER_LEASE_EXPIRED_AFTER_ORACLE_START", "执行结果需要对账")
			if result := tx.Model(&model.ReportRun{}).Where("id = ? AND status = ?", runID, model.ReportRunStatusRunning).Updates(updates); result.Error != nil {
				return fmt.Errorf("report execution: quarantine stale run: %w", result.Error)
			} else if result.RowsAffected != 1 {
				return fmt.Errorf("report execution: quarantine stale run count changed")
			}
			lease.Run.Status = model.ReportRunStatusUnknown
			lease.Run.UnknownAt = &now
			lease.Run.UnknownReasonCode = "WORKER_LEASE_EXPIRED_AFTER_ORACLE_START"
			lease.Run.LeaseToken = ""
			lease.Run.WorkerID = ""
			lease.Run.LeaseExpiresAt = nil
			return repository.writeSystemAudit(ctx, tx, "REPORT_RUN_UNKNOWN", "REPORT_RUN", runID, map[string]interface{}{"reasonCode": "WORKER_LEASE_EXPIRED_AFTER_ORACLE_START"})
		}
		if disposition != RunDispositionAcquired {
			return nil
		}
		if tableSnapshot {
			blocker, err := tableSnapshotExecutionBlocker(tx, lease.Run)
			if err != nil {
				return err
			}
			if blocker != nil {
				lease.Disposition = RunDispositionBusy
				lease.Blocker = blocker
				return nil
			}
		}
		expiresAt := now.Add(leaseTTL).UTC().Truncate(time.Millisecond)
		updates := map[string]interface{}{
			"status": model.ReportRunStatusRunning, "worker_id": strings.TrimSpace(workerID),
			"lease_token": leaseToken, "lease_expires_at": expiresAt, "heartbeat_at": now,
			"attempt": gorm.Expr("attempt + 1"), "finished_at": nil,
			"unknown_at": nil, "unknown_reason_code": "", "next_reconcile_at": nil,
			"error_code": "", "error_message_safe": "", "updated_at": now,
		}
		if lease.Run.StartedAt == nil {
			updates["started_at"] = now
			lease.Run.StartedAt = &now
		}
		if result := tx.Model(&model.ReportRun{}).Where("id = ?", runID).Updates(updates); result.Error != nil {
			return fmt.Errorf("report execution: claim run: %w", result.Error)
		} else if result.RowsAffected != 1 {
			return fmt.Errorf("report execution: claim run count changed")
		}
		lease.LeaseToken = leaseToken
		lease.Run.Status = model.ReportRunStatusRunning
		lease.Run.WorkerID = strings.TrimSpace(workerID)
		lease.Run.LeaseToken = leaseToken
		lease.Run.LeaseExpiresAt = &expiresAt
		lease.Run.HeartbeatAt = &now
		lease.Run.Attempt++
		return repository.writeSystemAudit(ctx, tx, "REPORT_RUN_STARTED", "REPORT_RUN", runID, map[string]interface{}{"attempt": lease.Run.Attempt})
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func lockReportExecutionScope(tx *gorm.DB, runID uint) (bool, error) {
	var identity struct {
		DefinitionID uint `gorm:"column:definition_id"`
		VersionID    uint `gorm:"column:version_id"`
	}
	if err := tx.Model(&model.ReportRun{}).Select("definition_id", "version_id").Where("id = ?", runID).Take(&identity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return false, ErrReportRunNotFound
	} else if err != nil {
		return false, fmt.Errorf("report execution: load run identity: %w", err)
	}
	var version model.ReportVersion
	if err := tx.Select("execution_mode").Where("id = ? AND definition_id = ?", identity.VersionID, identity.DefinitionID).Take(&version).Error; err != nil {
		return false, fmt.Errorf("report execution: load execution mode: %w", err)
	}
	if version.ExecutionMode != model.ReportExecutionModeTableSnapshot {
		return false, nil
	}
	var definition model.ReportDefinition
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", identity.DefinitionID).Take(&definition).Error; err != nil {
		return false, fmt.Errorf("report execution: lock table snapshot scope: %w", err)
	}
	return true, nil
}

func tableSnapshotExecutionBlocker(tx *gorm.DB, run model.ReportRun) (*RunExecutionBlocker, error) {
	query := tableSnapshotExecutionBlockerQuery(tx, run)
	var blocker RunExecutionBlocker
	result := query.
		Select(`report_runs.id AS run_id, report_runs.requested_by, report_runs.status, report_runs.attempt,
			report_runs.cancel_requested, report_runs.oracle_started_at, report_runs.lease_expires_at,
			report_runs.result_expires_at, report_runs.result_purged_at, exports.id AS export_id,
			exports.status AS export_status, exports.attempt AS export_attempt,
			exports.lease_expires_at AS export_lease_expires_at, exports.purged_at AS export_purged_at`).
		Joins("LEFT JOIN report_exports AS exports ON exports.run_id = report_runs.id").
		Order("report_runs.id ASC").Limit(1).Scan(&blocker)
	if result.Error != nil {
		return nil, fmt.Errorf("report execution: inspect table snapshot scope: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &blocker, nil
}

func tableSnapshotExecutionBlockerQuery(tx *gorm.DB, run model.ReportRun) *gorm.DB {
	blockingStatuses := []string{
		model.ReportRunStatusRunning, model.ReportRunStatusCancelRequested, model.ReportRunStatusUnknown, model.ReportRunStatusReconciling,
		model.ReportRunStatusExporting, model.ReportRunStatusResultPurging,
	}
	query := tx.Model(&model.ReportRun{}).
		Where("report_runs.definition_id = ? AND report_runs.id <> ?", run.DefinitionID, run.ID).
		Where("report_runs.status IN ? OR (report_runs.status IN ? AND report_runs.result_purged_at IS NULL)", blockingStatuses,
			[]string{model.ReportRunStatusSucceeded, model.ReportRunStatusSuperseded})
	if run.Status == model.ReportRunStatusQueued {
		query = query.Or("report_runs.definition_id = ? AND report_runs.status = ? AND report_runs.id < ?", run.DefinitionID, model.ReportRunStatusQueued, run.ID)
	}
	return query
}

func cancellableRunBeforeOracle(run model.ReportRun, now time.Time) bool {
	status := strings.ToUpper(strings.TrimSpace(run.Status))
	if status == model.ReportRunStatusQueued || status == model.ReportRunStatusCancelRequested {
		return run.CancelRequested || status == model.ReportRunStatusCancelRequested
	}
	return status == model.ReportRunStatusRunning && run.CancelRequested && run.OracleStartedAt == nil &&
		(run.LeaseExpiresAt == nil || !now.Before(run.LeaseExpiresAt.UTC()))
}

func classifyRunStart(run *model.ReportRun, now time.Time) (RunDisposition, error) {
	if run == nil || run.ID == 0 || now.IsZero() {
		return RunDispositionUnknown, fmt.Errorf("report execution: invalid stored run")
	}
	switch strings.ToUpper(strings.TrimSpace(run.Status)) {
	case model.ReportRunStatusQueued:
		return RunDispositionAcquired, nil
	case model.ReportRunStatusRunning:
		if run.LeaseExpiresAt != nil && now.Before(run.LeaseExpiresAt.UTC()) {
			return RunDispositionBusy, nil
		}
		if run.OracleStartedAt != nil {
			return RunDispositionReconcile, nil
		}
		return RunDispositionAcquired, nil
	case model.ReportRunStatusUnknown, model.ReportRunStatusReconciling:
		return RunDispositionReconcile, nil
	case model.ReportRunStatusSucceeded, model.ReportRunStatusFailed, model.ReportRunStatusCancelled,
		model.ReportRunStatusExported, model.ReportRunStatusResultPurged, model.ReportRunStatusSuperseded:
		return RunDispositionTerminal, nil
	case model.ReportRunStatusCancelRequested:
		return RunDispositionTerminal, nil
	default:
		return RunDispositionUnknown, fmt.Errorf("report execution: unsupported run status %q", run.Status)
	}
}

func (repository *Repository) HeartbeatExecution(
	ctx context.Context,
	runID uint,
	leaseToken string,
	now time.Time,
	leaseTTL time.Duration,
) (RunControl, error) {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil || now.IsZero() || leaseTTL <= 0 {
		return RunControlUnknown, fmt.Errorf("report execution: invalid heartbeat")
	}
	now = now.UTC().Truncate(time.Millisecond)
	result := repository.ownedExecution(ctx, runID, leaseToken).Where("cancel_requested = ?", false).Updates(map[string]interface{}{
		"heartbeat_at": now, "lease_expires_at": now.Add(leaseTTL).UTC().Truncate(time.Millisecond), "updated_at": now,
	})
	if result.Error != nil {
		return RunControlUnknown, fmt.Errorf("report execution: heartbeat: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return RunControlContinue, nil
	}
	return repository.InspectExecution(ctx, runID, leaseToken)
}

func (repository *Repository) BeginReconciliation(ctx context.Context, runID uint, workerID, leaseToken string, now time.Time, leaseTTL time.Duration) (*RunLease, error) {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || strings.TrimSpace(workerID) == "" ||
		len(workerID) > 128 || uuid.Validate(leaseToken) != nil || now.IsZero() || leaseTTL <= 0 {
		return nil, fmt.Errorf("report reconciliation: invalid lease input")
	}
	now = now.UTC().Truncate(time.Millisecond)
	lease := &RunLease{}
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lease.Run, runID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrReportRunNotFound
		} else if err != nil {
			return fmt.Errorf("report reconciliation: lock run: %w", err)
		}
		lease.Disposition = classifyReconciliationStart(lease.Run, now)
		if lease.Disposition != RunDispositionAcquired {
			return nil
		}
		expiresAt := now.Add(leaseTTL).UTC().Truncate(time.Millisecond)
		updates := map[string]interface{}{
			"status": model.ReportRunStatusReconciling, "worker_id": strings.TrimSpace(workerID), "lease_token": leaseToken,
			"lease_expires_at": expiresAt, "heartbeat_at": now, "last_reconciled_at": now,
			"reconcile_attempts": gorm.Expr("reconcile_attempts + 1"), "updated_at": now,
		}
		result := tx.Model(&model.ReportRun{}).Where("id = ? AND status IN ?", runID, []string{
			model.ReportRunStatusUnknown,
			model.ReportRunStatusReconciling,
			model.ReportRunStatusRunning,
		}).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("report reconciliation: claim run: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("report reconciliation: claim count changed")
		}
		lease.LeaseToken = leaseToken
		lease.Run.Status = model.ReportRunStatusReconciling
		lease.Run.LeaseToken = leaseToken
		lease.Run.WorkerID = strings.TrimSpace(workerID)
		lease.Run.LeaseExpiresAt = &expiresAt
		return repository.writeSystemAudit(ctx, tx, "REPORT_RUN_RECONCILIATION_STARTED", "REPORT_RUN", runID, nil)
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func classifyReconciliationStart(run model.ReportRun, now time.Time) RunDisposition {
	switch run.Status {
	case model.ReportRunStatusUnknown:
		return RunDispositionAcquired
	case model.ReportRunStatusReconciling:
		if run.LeaseExpiresAt != nil && now.Before(run.LeaseExpiresAt.UTC()) {
			return RunDispositionBusy
		}
		return RunDispositionAcquired
	case model.ReportRunStatusRunning:
		if run.OracleStartedAt == nil {
			return RunDispositionTerminal
		}
		if run.LeaseExpiresAt != nil && now.Before(run.LeaseExpiresAt.UTC()) {
			return RunDispositionBusy
		}
		return RunDispositionAcquired
	default:
		return RunDispositionTerminal
	}
}

func (repository *Repository) InspectExecution(ctx context.Context, runID uint, leaseToken string) (RunControl, error) {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil {
		return RunControlUnknown, fmt.Errorf("report execution: invalid inspection")
	}
	var run model.ReportRun
	err := repository.db.WithContext(ctx).Select("status", "cancel_requested", "lease_token").First(&run, runID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RunControlLeaseLost, nil
	}
	if err != nil {
		return RunControlUnknown, fmt.Errorf("report execution: inspect: %w", err)
	}
	if run.Status != model.ReportRunStatusRunning || run.LeaseToken != leaseToken {
		return RunControlLeaseLost, nil
	}
	if run.CancelRequested {
		return RunControlCancelRequested, nil
	}
	return RunControlContinue, nil
}

func (repository *Repository) MarkOracleExecutionStarted(ctx context.Context, runID uint, leaseToken string, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("report execution: invalid oracle start time")
	}
	return repository.updateOwnedExecutionWithAudit(ctx, runID, leaseToken, map[string]interface{}{
		"oracle_started_at": now.UTC().Truncate(time.Millisecond), "updated_at": now.UTC().Truncate(time.Millisecond),
	}, true, "REPORT_RUN_ORACLE_STARTED", nil)
}

func (repository *Repository) MarkExecutionSucceeded(ctx context.Context, runID uint, leaseToken string, rowCount int64, finishedAt, expiresAt time.Time) error {
	if rowCount < 0 || finishedAt.IsZero() || !expiresAt.After(finishedAt) {
		return fmt.Errorf("report execution: invalid success update")
	}
	finishedAt = finishedAt.UTC().Truncate(time.Millisecond)
	updates := map[string]interface{}{
		"status": model.ReportRunStatusSucceeded, "row_count": rowCount,
		"finished_at": finishedAt, "result_expires_at": expiresAt.UTC().Truncate(time.Millisecond),
		"error_code": "", "error_message_safe": "", "unknown_at": nil, "unknown_reason_code": "",
		"worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil,
		"updated_at": finishedAt,
	}
	return repository.markSucceededAndQueueExport(ctx, runID, leaseToken, model.ReportRunStatusRunning, true, updates, "REPORT_RUN_SUCCEEDED", rowCount, finishedAt)
}

func buildAutomaticReportExport(run model.ReportRun, exportUUID string, now time.Time) (model.ReportExport, model.AsyncJobOutbox, error) {
	if run.ID == 0 || run.RequestedBy == 0 || uuid.Validate(exportUUID) != nil || now.IsZero() || !json.Valid([]byte(run.PresentationSnapshotJSON)) {
		return model.ReportExport{}, model.AsyncJobOutbox{}, fmt.Errorf("report execution: invalid automatic export")
	}
	export := model.ReportExport{
		ExportUUID: exportUUID, RunID: run.ID, Status: model.ReportExportStatusPending,
		FrozenFiltersJSON: model.JSONText(`[]`), FrozenSortJSON: model.JSONText(`[]`),
		FrozenColumnsJSON: run.PresentationSnapshotJSON, CreatedBy: run.RequestedBy,
	}
	return export, NewReportExportOutbox(exportUUID, now), nil
}

func (repository *Repository) ConfirmExecutionSucceeded(ctx context.Context, runID uint, rowCount int64) (bool, error) {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || rowCount < 0 {
		return false, fmt.Errorf("report execution: invalid success confirmation")
	}
	var run model.ReportRun
	err := repository.db.WithContext(ctx).Select("status", "row_count").First(&run, runID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("report execution: confirm success: %w", err)
	}
	return run.Status == model.ReportRunStatusSucceeded && run.RowCount == rowCount, nil
}

func (repository *Repository) MarkExecutionFailed(ctx context.Context, runID uint, leaseToken, code, safeMessage string, finishedAt time.Time) error {
	if !validRunError(code, safeMessage) || finishedAt.IsZero() {
		return fmt.Errorf("report execution: invalid failure update")
	}
	return repository.updateOwnedExecutionWithAudit(ctx, runID, leaseToken, terminalRunUpdates(model.ReportRunStatusFailed, code, safeMessage, finishedAt), false, "REPORT_RUN_FAILED", map[string]interface{}{"errorCode": code})
}

func (repository *Repository) MarkExecutionCancelled(ctx context.Context, runID uint, leaseToken string, finishedAt time.Time) error {
	if finishedAt.IsZero() {
		return fmt.Errorf("report execution: invalid cancellation update")
	}
	return repository.updateOwnedExecutionWithAudit(ctx, runID, leaseToken, terminalRunUpdates(model.ReportRunStatusCancelled, "CANCELLED", "报表运行已取消", finishedAt), false, "REPORT_RUN_CANCELLED", nil)
}

func (repository *Repository) MarkExecutionUnknown(ctx context.Context, runID uint, leaseToken, reasonCode, safeMessage string, now time.Time) error {
	if !validRunError(reasonCode, safeMessage) || now.IsZero() {
		return fmt.Errorf("report execution: invalid unknown update")
	}
	return repository.updateOwnedExecutionWithAudit(ctx, runID, leaseToken, unknownRunUpdates(now.UTC().Truncate(time.Millisecond), reasonCode, safeMessage), false, "REPORT_RUN_UNKNOWN", map[string]interface{}{"reasonCode": reasonCode})
}

func (repository *Repository) ReleaseExecutionForRetry(ctx context.Context, runID uint, leaseToken string, now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("report execution: invalid retry release")
	}
	now = now.UTC().Truncate(time.Millisecond)
	return repository.updateOwnedExecutionWithAudit(ctx, runID, leaseToken, map[string]interface{}{
		"status": model.ReportRunStatusQueued, "worker_id": "", "lease_token": "", "lease_expires_at": nil,
		"heartbeat_at": nil, "finished_at": nil, "error_code": "", "error_message_safe": "", "updated_at": now,
	}, true, "REPORT_RUN_RETRY_QUEUED", nil)
}

func (repository *Repository) LoadRuntimeContract(ctx context.Context, runID uint, leaseToken string) (*RuntimeContract, error) {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil {
		return nil, fmt.Errorf("report execution: invalid runtime contract request")
	}
	var runtime RuntimeContract
	if err := repository.ownedRuntime(ctx, runID, leaseToken).First(&runtime.Run).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrReportRunLeaseLost
	} else if err != nil {
		return nil, fmt.Errorf("report execution: load run: %w", err)
	}
	if err := repository.db.WithContext(ctx).Where("id = ?", runtime.Run.DefinitionID).First(&runtime.Definition).Error; err != nil {
		return nil, fmt.Errorf("report execution: load definition: %w", err)
	}
	if err := repository.db.WithContext(ctx).Where("id = ? AND definition_id = ? AND status = ?", runtime.Run.VersionID, runtime.Run.DefinitionID, model.ReportVersionStatusPublished).First(&runtime.Version).Error; err != nil {
		return nil, fmt.Errorf("report execution: load published version: %w", err)
	}
	if !runtimeContractMatches(runtime.Run, runtime.Version) {
		return nil, fmt.Errorf("report execution: immutable contract hash mismatch")
	}
	if err := repository.db.WithContext(ctx).Where("id = ? AND driver = ?", runtime.Version.DatasourceID, model.ReportDatasourceDriverOracle).First(&runtime.Datasource).Error; err != nil {
		return nil, fmt.Errorf("report execution: load Oracle datasource: %w", err)
	}
	if err := repository.db.WithContext(ctx).Where("version_id = ?", runtime.Version.ID).Order("position ASC, id ASC").Find(&runtime.Parameters).Error; err != nil {
		return nil, fmt.Errorf("report execution: load parameters: %w", err)
	}
	if err := repository.db.WithContext(ctx).Where("version_id = ?", runtime.Version.ID).Order("display_order ASC, id ASC").Find(&runtime.Columns).Error; err != nil {
		return nil, fmt.Errorf("report execution: load columns: %w", err)
	}
	return &runtime, nil
}

func runtimeContractMatches(run model.ReportRun, version model.ReportVersion) bool {
	return version.DatasourceID != 0 && version.ContractHash == run.ContractHash &&
		version.ProcedureSignatureHash == run.ProcedureSignatureHash && version.ResultSchemaHash == run.ResultSchemaHash
}

func (repository *Repository) MarkReconciliationSucceeded(ctx context.Context, runID uint, leaseToken string, rowCount int64, finishedAt, expiresAt time.Time) error {
	if rowCount < 0 || finishedAt.IsZero() || !expiresAt.After(finishedAt) {
		return fmt.Errorf("report reconciliation: invalid success update")
	}
	finishedAt = finishedAt.UTC().Truncate(time.Millisecond)
	updates := map[string]interface{}{
		"status": model.ReportRunStatusSucceeded, "row_count": rowCount, "finished_at": finishedAt.UTC().Truncate(time.Millisecond),
		"result_expires_at": expiresAt.UTC().Truncate(time.Millisecond), "unknown_at": nil, "unknown_reason_code": "",
		"next_reconcile_at": nil, "error_code": "", "error_message_safe": "", "worker_id": "", "lease_token": "",
		"lease_expires_at": nil, "heartbeat_at": nil, "updated_at": finishedAt.UTC().Truncate(time.Millisecond),
	}
	return repository.markSucceededAndQueueExport(ctx, runID, leaseToken, model.ReportRunStatusReconciling, false, updates, "REPORT_RUN_RECONCILED", rowCount, finishedAt)
}

func (repository *Repository) markSucceededAndQueueExport(
	ctx context.Context,
	runID uint,
	leaseToken, expectedStatus string,
	requireNotCancelled bool,
	updates map[string]interface{},
	auditAction string,
	rowCount int64,
	finishedAt time.Time,
) error {
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ? AND lease_token = ?", runID, expectedStatus, leaseToken)
		if requireNotCancelled {
			query = query.Where("cancel_requested = ?", false)
		}
		var run model.ReportRun
		if err := query.First(&run).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrReportRunLeaseLost
		} else if err != nil {
			return fmt.Errorf("report execution: lock successful run: %w", err)
		}
		updateQuery := tx.Model(&model.ReportRun{}).Where("id = ? AND status = ? AND lease_token = ?", runID, expectedStatus, leaseToken)
		if requireNotCancelled {
			updateQuery = updateQuery.Where("cancel_requested = ?", false)
		}
		result := updateQuery.Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("report execution: persist success: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrReportRunLeaseLost
		}
		if err := repository.writeSystemAudit(ctx, tx, auditAction, "REPORT_RUN", runID, map[string]interface{}{"rowCount": rowCount}); err != nil {
			return err
		}
		if run.RetainsQuerySnapshot() || !frozenRunAllowsAction(run.PermissionSnapshotJSON, run.RequestedBy, ReportActionExport) {
			return nil
		}
		export, outbox, err := buildAutomaticReportExport(run, uuid.NewString(), finishedAt)
		if err != nil {
			return err
		}
		if err := tx.Create(&export).Error; err != nil {
			return fmt.Errorf("report execution: create automatic export: %w", err)
		}
		outbox.PayloadJSON = model.JSONText(fmt.Sprintf(`{"export_id":%d}`, export.ID))
		if err := tx.Create(&outbox).Error; err != nil {
			return fmt.Errorf("report execution: create automatic export outbox: %w", err)
		}
		return repository.writeSystemAudit(ctx, tx, "REPORT_EXPORT_AUTO_QUEUED", "REPORT_EXPORT", export.ID, map[string]interface{}{"runId": runID})
	})
}

func (repository *Repository) MarkReconciliationPending(ctx context.Context, runID uint, leaseToken, code, safeMessage string, now time.Time) error {
	if !validRunError(code, safeMessage) || now.IsZero() {
		return fmt.Errorf("report reconciliation: invalid pending update")
	}
	updates := unknownRunUpdates(now.UTC().Truncate(time.Millisecond), code, safeMessage)
	updates["next_reconcile_at"] = now.UTC().Add(time.Minute).Truncate(time.Millisecond)
	return repository.updateOwnedReconciliationWithAudit(ctx, runID, leaseToken, updates, "REPORT_RUN_RECONCILE_PENDING", map[string]interface{}{"reasonCode": code})
}

func (repository *Repository) ownedExecution(ctx context.Context, runID uint, leaseToken string) *gorm.DB {
	return repository.db.WithContext(ctx).Model(&model.ReportRun{}).
		Where("id = ? AND status = ? AND lease_token = ?", runID, model.ReportRunStatusRunning, leaseToken)
}

func (repository *Repository) ownedRuntime(ctx context.Context, runID uint, leaseToken string) *gorm.DB {
	return repository.db.WithContext(ctx).Model(&model.ReportRun{}).
		Where("id = ? AND status IN ? AND lease_token = ?", runID, []string{model.ReportRunStatusRunning, model.ReportRunStatusReconciling}, leaseToken)
}

func (repository *Repository) updateOwnedReconciliation(ctx context.Context, runID uint, leaseToken string, updates map[string]interface{}) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil {
		return fmt.Errorf("report reconciliation: invalid fenced update")
	}
	result := repository.db.WithContext(ctx).Model(&model.ReportRun{}).
		Where("id = ? AND status = ? AND lease_token = ?", runID, model.ReportRunStatusReconciling, leaseToken).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("report reconciliation: fenced update: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrReportRunLeaseLost
	}
	return nil
}

func (repository *Repository) updateOwnedExecution(ctx context.Context, runID uint, leaseToken string, updates map[string]interface{}, requireActive bool) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil {
		return fmt.Errorf("report execution: invalid fenced update")
	}
	query := repository.ownedExecution(ctx, runID, leaseToken)
	if requireActive {
		query = query.Where("cancel_requested = ?", false)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("report execution: fenced update: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrReportRunLeaseLost
	}
	return nil
}

func (repository *Repository) updateOwnedExecutionWithAudit(ctx context.Context, runID uint, leaseToken string, updates map[string]interface{}, requireActive bool, action string, detail map[string]interface{}) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil {
		return fmt.Errorf("report execution: invalid audited update")
	}
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&model.ReportRun{}).Where("id = ? AND status = ? AND lease_token = ?", runID, model.ReportRunStatusRunning, leaseToken)
		if requireActive {
			query = query.Where("cancel_requested = ?", false)
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("report execution: audited update: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrReportRunLeaseLost
		}
		return repository.writeSystemAudit(ctx, tx, action, "REPORT_RUN", runID, detail)
	})
}

func (repository *Repository) updateOwnedReconciliationWithAudit(ctx context.Context, runID uint, leaseToken string, updates map[string]interface{}, action string, detail map[string]interface{}) error {
	if repository == nil || repository.db == nil || ctx == nil || runID == 0 || uuid.Validate(leaseToken) != nil {
		return fmt.Errorf("report reconciliation: invalid audited update")
	}
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ReportRun{}).Where("id = ? AND status = ? AND lease_token = ?", runID, model.ReportRunStatusReconciling, leaseToken).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("report reconciliation: audited update: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrReportRunLeaseLost
		}
		return repository.writeSystemAudit(ctx, tx, action, "REPORT_RUN", runID, detail)
	})
}

func terminalRunUpdates(status, code, message string, finishedAt time.Time) map[string]interface{} {
	finishedAt = finishedAt.UTC().Truncate(time.Millisecond)
	return map[string]interface{}{
		"status": status, "finished_at": finishedAt, "error_code": code, "error_message_safe": strings.TrimSpace(message),
		"worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": finishedAt,
	}
}

func unknownRunUpdates(now time.Time, code, message string) map[string]interface{} {
	now = now.UTC().Truncate(time.Millisecond)
	return map[string]interface{}{
		"status": model.ReportRunStatusUnknown, "unknown_at": now, "unknown_reason_code": code,
		"next_reconcile_at": now, "error_code": code, "error_message_safe": strings.TrimSpace(message),
		"worker_id": "", "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now,
	}
}

func validRunError(code, message string) bool {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	return code != "" && len(code) <= 64 && message != "" && len([]byte(message)) <= 2000
}
