package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultReportReconcilePollInterval = 30 * time.Second
	defaultReportReconcileBatchSize    = 20
)

type reportReconciliationStore interface {
	RecoverLegacySnapshotStates(context.Context, time.Time, int) (int64, error)
	RecoverLegacySupersededSnapshots(context.Context, time.Time, int) (int64, error)
	ListReconciliationCandidates(context.Context, time.Time, int) ([]uint, error)
	BeginReconciliation(context.Context, uint, string, string, time.Time, time.Duration) (*reportrepo.RunLease, error)
	LoadRuntimeContract(context.Context, uint, string) (*reportrepo.RuntimeContract, error)
	MarkReconciliationSucceeded(context.Context, uint, string, int64, time.Time, time.Time) error
	MarkReconciliationPending(context.Context, uint, string, string, string, time.Time) error
	RecoverExpiredPreOracleRuns(context.Context, time.Time, int) (int64, error)
	ListExpiredQueuedRuns(context.Context, time.Time, int) ([]uint, error)
	MarkQueuedExecutionFailed(context.Context, uint, string, string, time.Time) error
	ListQueuedRunsMissingDelivery(context.Context, int) ([]uint, error)
	EnsureRunQueued(context.Context, uint, time.Time) error
	ListExportsMissingDelivery(context.Context, time.Time, int) ([]uint, error)
	EnsureExportQueued(context.Context, uint, time.Time) error
	ListSuccessfulRunsMissingExport(context.Context, time.Time, int) ([]uint, error)
	EnsureAutomaticExportQueued(context.Context, uint, time.Time) error
	ScheduleTerminalExportResultCleanup(context.Context, time.Time, int) (int64, error)
}

type reportResultEvidenceReader interface {
	CountCommittedRows(context.Context, reportrepo.RuntimeContract, string) (int64, error)
}

type ReportRunReconciler struct {
	store        reportReconciliationStore
	credential   reportDatasourceCredentialDecryptor
	results      reportResultEvidenceReader
	workerID     string
	leaseTTL     time.Duration
	pollInterval time.Duration
	batchSize    int
	stateTimeout time.Duration
	retention    time.Duration
	now          func() time.Time
	available    func(context.Context) bool
}

func NewReportRunReconciler() *ReportRunReconciler {
	reconciler := NewReportRunReconcilerWithDependencies(reportrepo.New(), reportsecretCredentialDecryptor{}, oracleReportResultEvidenceReader{})
	reconciler.available = database.CanServe
	return reconciler
}

type reportsecretCredentialDecryptor struct{}

func (reportsecretCredentialDecryptor) Decrypt(version, ciphertext string) (string, error) {
	return (reportsecret.EnvironmentKeyring{}).Decrypt(version, ciphertext)
}

func NewReportRunReconcilerWithDependencies(store reportReconciliationStore, credential reportDatasourceCredentialDecryptor, results reportResultEvidenceReader) *ReportRunReconciler {
	if store == nil || credential == nil || results == nil {
		panic("report run reconciliation: dependencies are required")
	}
	return &ReportRunReconciler{
		store: store, credential: credential, results: results, workerID: "report-reconcile-" + uuid.NewString(),
		leaseTTL: defaultReportRunLeaseTTL, pollInterval: defaultReportReconcilePollInterval,
		batchSize: defaultReportReconcileBatchSize, stateTimeout: defaultReportRunStateTimeout,
		retention: defaultReportResultRetention, now: func() time.Time { return time.Now().UTC() },
		available: func(context.Context) bool { return true },
	}
}

func (reconciler *ReportRunReconciler) Run(ctx context.Context) error {
	if reconciler == nil || reconciler.store == nil || ctx == nil {
		return fmt.Errorf("report run reconciliation: invalid runner")
	}
	reconciler.reconcileCycle(ctx)
	ticker := time.NewTicker(reconciler.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			reconciler.reconcileCycle(ctx)
		}
	}
}

func (reconciler *ReportRunReconciler) reconcileCycle(ctx context.Context) {
	if reconciler.available == nil || !reconciler.available(ctx) {
		return
	}
	if err := reconciler.Reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
		zap.L().Error("报表任务恢复周期失败", zap.String("worker_id", reconciler.workerID), zap.Error(err))
	}
}

func (reconciler *ReportRunReconciler) Reconcile(ctx context.Context) error {
	now := reconciler.now().UTC()
	var recoveryErrors []error
	legacyRecoveryCount, err := reconciler.store.RecoverLegacySnapshotStates(ctx, now, reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("recover legacy snapshot states: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else if legacyRecoveryCount > 0 {
		zap.L().Info("已恢复历史报表快照状态", zap.Int64("report_run_count", legacyRecoveryCount))
	}
	supersededRecoveryCount, err := reconciler.store.RecoverLegacySupersededSnapshots(ctx, now, reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("recover superseded snapshots: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else if supersededRecoveryCount > 0 {
		zap.L().Info("已恢复历史替代报表快照", zap.Int64("report_run_count", supersededRecoveryCount))
	}
	if _, err := reconciler.store.RecoverExpiredPreOracleRuns(ctx, now, reconciler.batchSize); err != nil {
		recoveryErr := fmt.Errorf("recover interrupted report runs: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	}
	runIDs, err := reconciler.store.ListReconciliationCandidates(ctx, now, reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("list report reconciliation candidates: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else {
		for _, runID := range runIDs {
			if err := reconciler.reconcileOne(ctx, runID); err != nil && !errors.Is(err, context.Canceled) {
				recoveryErr := fmt.Errorf("reconcile report run %d: %w", runID, err)
				if reconciler.available == nil || !reconciler.available(ctx) {
					return recoveryErr
				}
				recoveryErrors = append(recoveryErrors, recoveryErr)
			}
		}
	}
	expiredQueuedIDs, err := reconciler.store.ListExpiredQueuedRuns(ctx, now.Add(-job.ReportRunSnapshotWaitLimit), reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("list expired queued report runs: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else {
		for _, runID := range expiredQueuedIDs {
			if err := reconciler.store.MarkQueuedExecutionFailed(ctx, runID, "SNAPSHOT_WAIT_TIMEOUT", "等待其他用户的报表文件生成超时，请重新提交", now); err != nil && !errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
				recoveryErr := fmt.Errorf("fail expired queued report run %d: %w", runID, err)
				if database.IsConnectivityError(err) {
					return recoveryErr
				}
				recoveryErrors = append(recoveryErrors, recoveryErr)
			}
		}
	}
	queuedIDs, err := reconciler.store.ListQueuedRunsMissingDelivery(ctx, reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("list undelivered report runs: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else {
		for _, runID := range queuedIDs {
			if err := reconciler.store.EnsureRunQueued(ctx, runID, reconciler.now().UTC()); err != nil {
				recoveryErr := fmt.Errorf("restore report run delivery %d: %w", runID, err)
				if database.IsConnectivityError(err) {
					return recoveryErr
				}
				recoveryErrors = append(recoveryErrors, recoveryErr)
				continue
			}
			zap.L().Info("已恢复报表查询任务投递", zap.Uint("report_run_id", runID))
		}
	}
	terminalCleanupCount, err := reconciler.store.ScheduleTerminalExportResultCleanup(ctx, now, reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("schedule terminal export result cleanup: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else if terminalCleanupCount > 0 {
		zap.L().Info("已恢复终态导出的报表结果清理", zap.Int64("report_run_count", terminalCleanupCount))
	}
	missingExportRunIDs, err := reconciler.store.ListSuccessfulRunsMissingExport(ctx, now, reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("list report runs missing exports: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else {
		for _, runID := range missingExportRunIDs {
			if err := reconciler.store.EnsureAutomaticExportQueued(ctx, runID, reconciler.now().UTC()); err != nil {
				recoveryErr := fmt.Errorf("restore automatic export for run %d: %w", runID, err)
				if database.IsConnectivityError(err) {
					return recoveryErr
				}
				recoveryErrors = append(recoveryErrors, recoveryErr)
				continue
			}
			zap.L().Info("已补建历史报表自动导出", zap.Uint("report_run_id", runID))
		}
	}
	exportIDs, err := reconciler.store.ListExportsMissingDelivery(ctx, now, reconciler.batchSize)
	if err != nil {
		recoveryErr := fmt.Errorf("list undelivered report exports: %w", err)
		if database.IsConnectivityError(err) {
			return recoveryErr
		}
		recoveryErrors = append(recoveryErrors, recoveryErr)
	} else {
		for _, exportID := range exportIDs {
			if err := reconciler.store.EnsureExportQueued(ctx, exportID, reconciler.now().UTC()); err != nil {
				recoveryErr := fmt.Errorf("restore report export delivery %d: %w", exportID, err)
				if database.IsConnectivityError(err) {
					return recoveryErr
				}
				recoveryErrors = append(recoveryErrors, recoveryErr)
				continue
			}
			zap.L().Info("已恢复报表导出任务投递", zap.Uint("report_export_id", exportID))
		}
	}
	return errors.Join(recoveryErrors...)
}

func (reconciler *ReportRunReconciler) ReconcileOne(ctx context.Context, runID uint) error {
	if reconciler == nil || reconciler.store == nil || ctx == nil || runID == 0 {
		return fmt.Errorf("report run reconciliation: invalid run")
	}
	return reconciler.reconcileOne(ctx, runID)
}

func (reconciler *ReportRunReconciler) reconcileOne(ctx context.Context, runID uint) error {
	leaseToken := uuid.NewString()
	lease, err := reconciler.store.BeginReconciliation(ctx, runID, reconciler.workerID, leaseToken, reconciler.now().UTC(), reconciler.leaseTTL)
	if err != nil {
		return err
	}
	if lease.Disposition != reportrepo.RunDispositionAcquired {
		return nil
	}
	runtime, err := reconciler.store.LoadRuntimeContract(ctx, runID, leaseToken)
	if err != nil {
		return reconciler.pending(ctx, runID, leaseToken, "RECONCILE_CONTRACT_UNAVAILABLE", err)
	}
	password, err := reconciler.credential.Decrypt(runtime.Datasource.CredentialKeyVersion, runtime.Datasource.PasswordCiphertext)
	if err != nil {
		return reconciler.pending(ctx, runID, leaseToken, "RECONCILE_CREDENTIAL_UNAVAILABLE", err)
	}
	rowCount, err := reconciler.results.CountCommittedRows(ctx, *runtime, password)
	if err != nil {
		return reconciler.pending(ctx, runID, leaseToken, "ORACLE_RESULT_UNAVAILABLE", err)
	}
	finishedAt := reconciler.now().UTC()
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reconciler.stateTimeout)
	defer cancel()
	return reconciler.store.MarkReconciliationSucceeded(stateCtx, runID, leaseToken, rowCount, finishedAt, finishedAt.Add(reconciler.retention))
}

func (reconciler *ReportRunReconciler) pending(ctx context.Context, runID uint, leaseToken, code string, cause error) error {
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reconciler.stateTimeout)
	defer cancel()
	if err := reconciler.store.MarkReconciliationPending(stateCtx, runID, leaseToken, code, "运行结果等待Oracle结果表确认", reconciler.now().UTC()); err != nil {
		return errors.Join(
			fmt.Errorf("report run reconciliation: persist pending state: %w", err),
			fmt.Errorf("report run reconciliation: pending cause: %w", cause),
		)
	}
	return nil
}

type oracleReportResultEvidenceReader struct{}

func (oracleReportResultEvidenceReader) CountCommittedRows(ctx context.Context, runtime reportrepo.RuntimeContract, password string) (int64, error) {
	queryCtx, cancel := reportOracleQueryContext(ctx, runtime.Datasource)
	defer cancel()
	adapter, err := reportoracle.Open(queryCtx, oracleConfigFromDatasource(runtime.Datasource, password))
	if err != nil {
		return 0, err
	}
	defer func() { _ = adapter.Close() }()
	if runtime.Version.ExecutionMode == model.ReportExecutionModeRefCursor {
		if err := adapter.ValidateJSONSnapshotStore(queryCtx); err != nil {
			return 0, err
		}
		return adapter.CountJSONSnapshotRows(queryCtx, runtime.Run.RunUUID)
	}
	configuredColumns := make([]string, 0, len(runtime.Columns))
	for _, column := range runtime.Columns {
		configuredColumns = append(configuredColumns, column.DatabaseColumn)
	}
	contract, err := adapter.InspectResultSnapshotContract(queryCtx, reportoracle.ResultTableSnapshotRef(
		reportoracle.ResultTableRef{Owner: runtime.Version.ResultTableOwner, Name: runtime.Version.ResultTableName}, configuredColumns,
	))
	if err != nil {
		return 0, err
	}
	plan, err := reportoracle.BuildResultCountPlan(contract)
	if err != nil {
		return 0, err
	}
	return adapter.CountResultRows(queryCtx, plan)
}
