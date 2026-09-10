package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/storage"

	"github.com/google/uuid"
)

const (
	defaultReportExportLeaseTTL          = 90 * time.Second
	defaultReportExportHeartbeatInterval = 30 * time.Second
	defaultReportExportStateTimeout      = 10 * time.Second
	defaultReportExportRetention         = 72 * time.Hour
	defaultReportExportVerifyTimeout     = 10 * time.Second
	defaultReportExportPurgeBatchSize    = 5000
	reportExportWorkRootName             = "report-exports"
)

var (
	ErrReportExportProcessNonRetryable = errors.New("report export processor: non-retryable")
	errReportExportWorkerTemporary     = errors.New("report export processor: temporary failure")
	errReportExportCancelled           = errors.New("report export processor: cancelled")
)

type reportExportExecutionStore interface {
	BeginExport(context.Context, uint, string, string, time.Time, time.Duration) (*reportrepo.ExportLease, error)
	HeartbeatExport(context.Context, uint, string, time.Time, time.Duration) (reportrepo.ExportControl, error)
	InspectExport(context.Context, uint, string) (reportrepo.ExportControl, error)
	UpdateExportProgress(context.Context, uint, string, int64, string, model.JSONText, time.Time) error
	LoadExportRuntime(context.Context, uint, string) (*reportrepo.ExportRuntime, error)
	MarkExportReady(context.Context, uint, string, string, string, int64, int64, int, int64, time.Time, time.Time) error
	ConfirmExportReady(context.Context, uint, string, string, int64, int64) (bool, error)
	ReleaseExportForRetry(context.Context, uint, string, time.Time) error
	MarkExportFailed(context.Context, uint, string, string, string, time.Time) error
	MarkExportCancelled(context.Context, uint, string, time.Time) error
	ClaimResultPurge(context.Context, uint, string, time.Time) (*reportrepo.ExportRuntime, error)
	UpdateResultPurgeProgress(context.Context, uint, string, int64, time.Time) error
	MarkResultPurged(context.Context, uint, string, int64, time.Time) error
	ConfirmResultPurged(context.Context, uint) (bool, error)
	ReleaseResultPurge(context.Context, uint, string, time.Time) error
}

type reportExportWorkbookRenderer interface {
	Render(context.Context, ReportExportRenderRequest, func(ReportExportRenderProgress) error) (ReportExportRenderResult, error)
}

type reportExportObjectStore interface {
	UploadFile(context.Context, string, string, string) (storage.UploadResult, error)
	StatDownloadObject(context.Context, string) (storage.ObjectMetadata, error)
	DeleteObject(context.Context, string) error
}

type ReportExportProcessor struct {
	store             reportExportExecutionStore
	credential        reportDatasourceCredentialDecryptor
	oracle            reportExportOracleSessionFactory
	newObjectStore    func() (reportExportObjectStore, error)
	buildObjectKey    func(...string) string
	workerID          string
	workRoot          string
	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	stateTimeout      time.Duration
	retention         time.Duration
	verifyTimeout     time.Duration
	purgeBatchSize    int
	now               func() time.Time
	newToken          func() string
}

func NewReportExportProcessor() *ReportExportProcessor {
	return NewReportExportProcessorWithDependencies(reportrepo.New(), reportsecret.EnvironmentKeyring{}, oracleReportExportSessionFactory{})
}

func NewReportExportProcessorWithDependencies(store reportExportExecutionStore, credential reportDatasourceCredentialDecryptor, oracle reportExportOracleSessionFactory) *ReportExportProcessor {
	if store == nil || credential == nil || oracle == nil {
		panic("report export processor: dependencies are required")
	}
	return &ReportExportProcessor{
		store: store, credential: credential, oracle: oracle,
		newObjectStore: func() (reportExportObjectStore, error) { return storage.NewOSSClientFromConfig() },
		buildObjectKey: storage.BuildObjectKey, workerID: "report-export-" + uuid.NewString(),
		workRoot: excelTempRootDir(), leaseTTL: defaultReportExportLeaseTTL,
		heartbeatInterval: defaultReportExportHeartbeatInterval, stateTimeout: defaultReportExportStateTimeout,
		retention: defaultReportExportRetention, verifyTimeout: defaultReportExportVerifyTimeout,
		purgeBatchSize: defaultReportExportPurgeBatchSize, now: func() time.Time { return time.Now().UTC() }, newToken: uuid.NewString,
	}
}

func (processor *ReportExportProcessor) Process(ctx context.Context, exportID uint, retryAllowed bool) error {
	if err := processor.validate(ctx, exportID); err != nil {
		return fmt.Errorf("%w: %v", ErrReportExportProcessNonRetryable, err)
	}
	leaseToken := processor.newToken()
	lease, err := processor.store.BeginExport(ctx, exportID, processor.workerID, leaseToken, processor.now(), processor.leaseTTL)
	if err != nil {
		if errors.Is(err, reportrepo.ErrReportExportExecutionNotFound) || errors.Is(err, reportrepo.ErrReportExportResultUnavailable) {
			return fmt.Errorf("%w: export cannot start", ErrReportExportProcessNonRetryable)
		}
		return errReportExportWorkerTemporary
	}
	switch lease.Disposition {
	case reportrepo.ExportDispositionBusy:
		return errReportExportWorkerTemporary
	case reportrepo.ExportDispositionTerminal:
		if lease.Export.Status == model.ReportExportStatusReady && lease.Export.PurgedAt == nil {
			return processor.purgeReadyExport(ctx, exportID, true)
		}
		return nil
	case reportrepo.ExportDispositionAcquired:
	default:
		return fmt.Errorf("%w: unsupported export disposition", ErrReportExportProcessNonRetryable)
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	monitor := processor.startMonitor(runCtx, cancelRun, exportID, leaseToken)
	defer func() {
		cancelRun()
		<-monitor
	}()
	runtime, err := processor.store.LoadExportRuntime(runCtx, exportID, leaseToken)
	if err != nil {
		return processor.finishExportFailure(ctx, exportID, leaseToken, retryAllowed, err, "导出契约不可用")
	}
	columns, err := frozenExportColumns(runtime.Export.FrozenColumnsJSON)
	if err != nil {
		return processor.finishExportFailure(ctx, exportID, leaseToken, false, err, "导出字段配置无效")
	}
	password, err := processor.credential.Decrypt(runtime.Datasource.CredentialKeyVersion, runtime.Datasource.PasswordCiphertext)
	if err != nil {
		return processor.finishExportFailure(ctx, exportID, leaseToken, false, err, "Oracle数据源凭据不可用")
	}
	session, err := processor.oracle.Open(runCtx, *runtime, password)
	if err != nil {
		return processor.finishExportFailure(ctx, exportID, leaseToken, retryAllowed, err, "Oracle结果读取失败")
	}
	workDir, err := createReportExportWorkDir(processor.workRoot, exportID, leaseToken)
	if err != nil {
		_ = session.Close()
		return processor.finishExportFailure(ctx, exportID, leaseToken, retryAllowed, err, "导出工作目录创建失败")
	}
	defer os.RemoveAll(workDir)
	fileName := "report-" + runtime.Run.RunUUID + ".xlsx"
	outputPath := filepath.Join(workDir, fileName)
	renderer := NewReportExportRenderer(session)
	renderResult, renderErr := renderer.Render(runCtx, reportExportWorkbookRenderRequest(columns, outputPath), func(progress ReportExportRenderProgress) error {
		checkpoint, encodeErr := reportExportCheckpoint(progress)
		if encodeErr != nil {
			return encodeErr
		}
		stateCtx, cancel := processor.stateContext(runCtx)
		defer cancel()
		return processor.store.UpdateExportProgress(stateCtx, exportID, leaseToken, progress.ProcessedRows, progress.CurrentSheet, checkpoint, processor.now())
	})
	closeErr := session.Close()
	if renderErr != nil || closeErr != nil {
		if processor.exportCancelled(ctx, exportID, leaseToken) {
			return processor.finishExportCancelled(ctx, exportID, leaseToken)
		}
		return processor.finishExportFailure(ctx, exportID, leaseToken, retryAllowed, errors.Join(renderErr, closeErr), "Excel生成失败")
	}
	checksum, fileSize, err := inspectReportExportArtifact(outputPath)
	if err != nil {
		return processor.finishExportFailure(ctx, exportID, leaseToken, retryAllowed, err, "导出文件校验失败")
	}
	objectSuffix, err := reportExportObjectKey(runtime.Export.ExportUUID, leaseToken, processor.now())
	if err != nil {
		return processor.finishExportFailure(ctx, exportID, leaseToken, false, err, "导出对象标识无效")
	}
	objectKey := processor.buildObjectKey(objectSuffix)
	objectStore, err := processor.newObjectStore()
	if err != nil || objectStore == nil || objectKey == "" {
		return processor.finishExportFailure(ctx, exportID, leaseToken, retryAllowed, errors.Join(err, errors.New("object store unavailable")), "导出文件上传失败")
	}
	uploadAttempted := true
	upload, err := objectStore.UploadFile(runCtx, objectKey, outputPath, fileName)
	if err == nil {
		if upload.ObjectKey != objectKey {
			err = fmt.Errorf("report export processor: uploaded object key mismatch")
		}
	}
	if err == nil {
		verifyCtx, cancel := context.WithTimeout(runCtx, processor.verifyTimeout)
		metadata, verifyErr := objectStore.StatDownloadObject(verifyCtx, objectKey)
		cancel()
		if verifyErr != nil {
			err = fmt.Errorf("report export processor: verify uploaded object: %w", verifyErr)
		} else if metadata.Size != fileSize {
			err = fmt.Errorf("report export processor: uploaded object size mismatch")
		} else if !strings.EqualFold(metadata.ChecksumSHA256, checksum) {
			err = fmt.Errorf("report export processor: uploaded object checksum mismatch")
		}
	}
	if err != nil {
		if uploadAttempted {
			err = errors.Join(err, processor.deleteObject(ctx, objectStore, objectKey))
		}
		if processor.exportCancelled(ctx, exportID, leaseToken) {
			return processor.finishExportCancelled(ctx, exportID, leaseToken)
		}
		return processor.finishExportFailure(ctx, exportID, leaseToken, retryAllowed, err, "导出文件上传失败")
	}
	if processor.exportCancelled(ctx, exportID, leaseToken) {
		err = processor.deleteObject(ctx, objectStore, objectKey)
		return errors.Join(processor.finishExportCancelled(ctx, exportID, leaseToken), err)
	}
	readyAt := processor.now()
	stateCtx, stateCancel := processor.stateContext(ctx)
	err = processor.store.MarkExportReady(stateCtx, exportID, leaseToken, objectKey, checksum, fileSize, renderResult.ProcessedRows, renderResult.SheetCount, renderResult.TruncatedCellCount, readyAt, readyAt.Add(processor.retention))
	stateCancel()
	if err != nil {
		confirmCtx, confirmCancel := processor.stateContext(ctx)
		confirmed, confirmErr := processor.store.ConfirmExportReady(confirmCtx, exportID, objectKey, checksum, fileSize, renderResult.ProcessedRows)
		confirmCancel()
		if !confirmed {
			if confirmErr == nil {
				err = errors.Join(err, processor.deleteObject(ctx, objectStore, objectKey))
			}
			if processor.exportCancelled(ctx, exportID, leaseToken) {
				if confirmErr != nil {
					err = errors.Join(err, processor.deleteObject(ctx, objectStore, objectKey))
				}
				return errors.Join(processor.finishExportCancelled(ctx, exportID, leaseToken), err, confirmErr)
			}
			return errors.Join(errReportExportWorkerTemporary, err, confirmErr)
		}
	}
	cancelRun()
	<-monitor
	return processor.purgeReadyExport(ctx, exportID, true)
}

func reportExportWorkbookRenderRequest(columns []frozenResultColumn, outputPath string) ReportExportRenderRequest {
	return ReportExportRenderRequest{
		Columns:             columns,
		OutputPath:          outputPath,
		decimalNumberFormat: reportExcelTwoDecimalNumberFormat,
	}
}

func (processor *ReportExportProcessor) CleanupReadyResult(ctx context.Context, exportID uint) error {
	if processor == nil || ctx == nil || exportID == 0 {
		return fmt.Errorf("report export processor: invalid ready result cleanup")
	}
	return processor.purgeReadyExport(ctx, exportID, false)
}

func (processor *ReportExportProcessor) purgeReadyExport(ctx context.Context, exportID uint, waitForLease bool) error {
	var runtime *reportrepo.ExportRuntime
	var purgeToken string
	for {
		purgeToken = processor.newToken()
		stateCtx, cancel := processor.stateContext(ctx)
		var err error
		runtime, err = processor.store.ClaimResultPurge(stateCtx, exportID, purgeToken, processor.now())
		cancel()
		if err == nil {
			break
		}
		if !errors.Is(err, reportrepo.ErrReportResultPurgeConflict) {
			return errReportExportWorkerTemporary
		}
		confirmed, confirmErr := processor.confirmResultPurged(ctx, exportID)
		if confirmErr == nil && confirmed {
			return nil
		}
		if !waitForLease {
			return errors.Join(errReportExportWorkerTemporary, err, confirmErr)
		}
		if waitErr := waitForReportExportPurge(ctx, processor.heartbeatInterval); waitErr != nil {
			return errors.Join(errReportExportWorkerTemporary, confirmErr, waitErr)
		}
	}
	if runtime.Version.ExecutionMode == model.ReportExecutionModeTableSnapshot {
		stateCtx, cancel := processor.stateContext(ctx)
		err := processor.store.MarkResultPurged(stateCtx, exportID, purgeToken, runtime.Run.RowCount, processor.now())
		cancel()
		if err != nil {
			confirmed, confirmErr := processor.confirmResultPurged(ctx, exportID)
			if confirmed {
				return nil
			}
			return errors.Join(errReportExportWorkerTemporary, err, confirmErr)
		}
		return nil
	}
	password, err := processor.credential.Decrypt(runtime.Datasource.CredentialKeyVersion, runtime.Datasource.PasswordCiphertext)
	if err != nil {
		return processor.releasePurge(ctx, exportID, purgeToken, err)
	}
	session, err := processor.oracle.Open(ctx, *runtime, password)
	if err != nil {
		return processor.releasePurge(ctx, exportID, purgeToken, err)
	}
	defer session.Close()
	cumulative := runtime.Export.PurgedRows
	for {
		deleted, purgeErr := session.Purge(ctx, processor.purgeBatchSize)
		if purgeErr != nil {
			return processor.releasePurge(ctx, exportID, purgeToken, purgeErr)
		}
		cumulative += deleted
		stateCtx, cancel := processor.stateContext(ctx)
		progressErr := processor.store.UpdateResultPurgeProgress(stateCtx, exportID, purgeToken, cumulative, processor.now())
		cancel()
		if progressErr != nil {
			return processor.releasePurge(ctx, exportID, purgeToken, progressErr)
		}
		if deleted < int64(processor.purgeBatchSize) {
			break
		}
	}
	stateCtx, cancel := processor.stateContext(ctx)
	err = processor.store.MarkResultPurged(stateCtx, exportID, purgeToken, runtime.Run.RowCount, processor.now())
	cancel()
	if err != nil {
		confirmed, confirmErr := processor.confirmResultPurged(ctx, exportID)
		if confirmed {
			return nil
		}
		return errors.Join(errReportExportWorkerTemporary, err, confirmErr)
	}
	return nil
}

func (processor *ReportExportProcessor) confirmResultPurged(ctx context.Context, exportID uint) (bool, error) {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	return processor.store.ConfirmResultPurged(stateCtx, exportID)
}

func waitForReportExportPurge(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("report export processor: invalid purge wait interval")
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (processor *ReportExportProcessor) finishExportFailure(ctx context.Context, exportID uint, leaseToken string, retryAllowed bool, cause error, safeMessage string) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	if retryAllowed {
		if err := processor.store.ReleaseExportForRetry(stateCtx, exportID, leaseToken, processor.now()); err != nil && !errors.Is(err, reportrepo.ErrReportExportLeaseLost) {
			return errors.Join(errReportExportWorkerTemporary, cause, err)
		}
		return errors.Join(errReportExportWorkerTemporary, cause)
	}
	if err := processor.store.MarkExportFailed(stateCtx, exportID, leaseToken, "EXPORT_FAILED", safeMessage, processor.now()); err != nil && !errors.Is(err, reportrepo.ErrReportExportLeaseLost) {
		return errors.Join(ErrReportExportProcessNonRetryable, cause, err)
	}
	return errors.Join(ErrReportExportProcessNonRetryable, cause)
}

func (processor *ReportExportProcessor) exportCancelled(ctx context.Context, exportID uint, leaseToken string) bool {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	control, err := processor.store.InspectExport(stateCtx, exportID, leaseToken)
	return err == nil && control == reportrepo.ExportControlCancelRequested
}

func (processor *ReportExportProcessor) finishExportCancelled(ctx context.Context, exportID uint, leaseToken string) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	err := processor.store.MarkExportCancelled(stateCtx, exportID, leaseToken, processor.now())
	if errors.Is(err, reportrepo.ErrReportExportLeaseLost) {
		return nil
	}
	return err
}

func (processor *ReportExportProcessor) releasePurge(ctx context.Context, exportID uint, purgeToken string, cause error) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	err := processor.store.ReleaseResultPurge(stateCtx, exportID, purgeToken, processor.now())
	if errors.Is(err, reportrepo.ErrReportExportLeaseLost) {
		err = nil
	}
	return errors.Join(errReportExportWorkerTemporary, cause, err)
}

func (processor *ReportExportProcessor) startMonitor(ctx context.Context, cancelRun context.CancelFunc, exportID uint, leaseToken string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(processor.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case heartbeatAt := <-ticker.C:
				stateCtx, cancel := processor.stateContext(ctx)
				control, err := processor.store.HeartbeatExport(stateCtx, exportID, leaseToken, heartbeatAt, processor.leaseTTL)
				cancel()
				if err != nil || control != reportrepo.ExportControlContinue {
					cancelRun()
					return
				}
			}
		}
	}()
	return done
}

func (processor *ReportExportProcessor) stateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), processor.stateTimeout)
}

func (processor *ReportExportProcessor) deleteObject(ctx context.Context, store reportExportObjectStore, objectKey string) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	if err := store.DeleteObject(stateCtx, objectKey); err != nil {
		return fmt.Errorf("report export processor: delete object: %w", err)
	}
	return nil
}

func (processor *ReportExportProcessor) validate(ctx context.Context, exportID uint) error {
	if processor == nil || processor.store == nil || processor.credential == nil || processor.oracle == nil || processor.newObjectStore == nil ||
		processor.buildObjectKey == nil || processor.now == nil || processor.newToken == nil || ctx == nil || exportID == 0 ||
		strings.TrimSpace(processor.workerID) == "" || strings.TrimSpace(processor.workRoot) == "" || processor.leaseTTL <= 0 ||
		processor.heartbeatInterval <= 0 || processor.stateTimeout <= 0 || processor.retention <= 0 || processor.verifyTimeout <= 0 ||
		processor.purgeBatchSize < 1 || processor.purgeBatchSize > 20000 {
		return fmt.Errorf("report export processor: invalid configuration")
	}
	return nil
}

func createReportExportWorkDir(root string, exportID uint, leaseToken string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || exportID == 0 || uuid.Validate(leaseToken) != nil {
		return "", fmt.Errorf("report export processor: invalid work directory")
	}
	workDir := filepath.Join(root, reportExportWorkRootName, strconv.FormatUint(uint64(exportID), 10), leaseToken)
	if !isPathInside(root, workDir) {
		return "", fmt.Errorf("report export processor: unsafe work directory")
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", fmt.Errorf("report export processor: create work directory: %w", err)
	}
	return workDir, nil
}

func inspectReportExportArtifact(filePath string) (string, int64, error) {
	return inspectMallWeatherExportArtifact(filePath)
}

func reportExportObjectKey(exportUUID, leaseToken string, now time.Time) (string, error) {
	if uuid.Validate(exportUUID) != nil || uuid.Validate(leaseToken) != nil || now.IsZero() {
		return "", fmt.Errorf("report export processor: invalid object identity")
	}
	return path.Join(reportExportWorkRootName, now.UTC().Format("2006/01/02"), exportUUID, leaseToken, "result.xlsx"), nil
}

func reportExportCheckpoint(progress ReportExportRenderProgress) (model.JSONText, error) {
	if progress.ProcessedRows < 0 || progress.SheetCount < 1 || progress.CurrentSheet == "" {
		return "", fmt.Errorf("report export processor: invalid progress checkpoint")
	}
	encoded, err := json.Marshal(map[string]interface{}{"afterKey": progress.AfterKey, "sheetCount": progress.SheetCount})
	if err != nil {
		return "", fmt.Errorf("report export processor: encode progress checkpoint: %w", err)
	}
	return model.JSONText(encoded), nil
}
