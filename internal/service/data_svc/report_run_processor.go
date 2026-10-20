package data_svc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportcontract"
	"gin-biz-web-api/internal/reporting"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultReportRunLeaseTTL          = 90 * time.Second
	defaultReportRunHeartbeatInterval = 30 * time.Second
	defaultReportRunStateTimeout      = 10 * time.Second
	defaultReportResultRetention      = 72 * time.Hour
)

var ErrReportRunProcessNonRetryable = errors.New("report run processor: non-retryable")

var errReportRunWorkerTemporary = errors.New("report run processor: temporary failure")

var ErrReportRunWaitingForSnapshot = reportRunWaitingError{}

type reportRunWaitingError struct {
	cleanupRunID    uint
	cleanupExportID uint
}

type ReportCleanupTarget struct {
	RunID    uint
	ExportID uint
}

func (reportRunWaitingError) Error() string             { return "report run processor: waiting for snapshot slot" }
func (reportRunWaitingError) RetryDelay() time.Duration { return job.ReportRunRetryDelay }
func (err reportRunWaitingError) CleanupRunID() uint    { return err.cleanupRunID }
func (err reportRunWaitingError) CleanupExportID() uint { return err.cleanupExportID }
func (reportRunWaitingError) Is(target error) bool {
	_, ok := target.(reportRunWaitingError)
	return ok
}

func ReportRunCleanupTarget(err error) (ReportCleanupTarget, bool) {
	var runWaiting interface{ CleanupRunID() uint }
	var exportWaiting interface{ CleanupExportID() uint }
	var target ReportCleanupTarget
	if errors.As(err, &runWaiting) {
		target.RunID = runWaiting.CleanupRunID()
	}
	if errors.As(err, &exportWaiting) {
		target.ExportID = exportWaiting.CleanupExportID()
	}
	if (target.RunID == 0) == (target.ExportID == 0) {
		return ReportCleanupTarget{}, false
	}
	return target, true
}

type reportExecutionStore interface {
	BeginExecution(context.Context, uint, string, string, time.Time, time.Duration) (*reportrepo.RunLease, error)
	HeartbeatExecution(context.Context, uint, string, time.Time, time.Duration) (reportrepo.RunControl, error)
	InspectExecution(context.Context, uint, string) (reportrepo.RunControl, error)
	MarkOracleExecutionStarted(context.Context, uint, string, time.Time) error
	MarkExecutionSucceeded(context.Context, uint, string, int64, time.Time, time.Time) error
	ConfirmExecutionSucceeded(context.Context, uint, int64) (bool, error)
	MarkExecutionFailed(context.Context, uint, string, string, string, time.Time) error
	MarkExecutionCancelled(context.Context, uint, string, time.Time) error
	MarkExecutionUnknown(context.Context, uint, string, string, string, time.Time) error
	ReleaseExecutionForRetry(context.Context, uint, string, time.Time) error
	MarkQueuedExecutionFailed(context.Context, uint, string, string, time.Time) error
	LoadRuntimeContract(context.Context, uint, string) (*reportrepo.RuntimeContract, error)
}

type reportDatasourceCredentialDecryptor interface {
	Decrypt(version, ciphertext string) (string, error)
}

type reportRunParameterDecryptor interface {
	Decrypt(version, ciphertext string) ([]byte, error)
}

type reportProcedureExecutionRequest struct {
	Runtime     reportrepo.RuntimeContract
	Definitions []reporting.ParameterDefinition
	Values      map[string]interface{}
	JSONPayload string
}

type reportProcedureExecutor interface {
	Execute(context.Context, reportProcedureExecutionRequest, string) (int64, error)
}

type ReportRunProcessor struct {
	store             reportExecutionStore
	credential        reportDatasourceCredentialDecryptor
	parameters        reportRunParameterDecryptor
	executor          reportProcedureExecutor
	workerID          string
	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	stateTimeout      time.Duration
	resultRetention   time.Duration
	now               func() time.Time
	reconcileOne      func(context.Context, uint) error
}

func NewReportRunProcessor() *ReportRunProcessor {
	return NewReportRunProcessorWithDependencies(
		reportrepo.New(), reportsecret.EnvironmentKeyring{}, reportsecret.EnvironmentParameterCipher{}, oracleReportProcedureExecutor{},
	)
}

func NewReportRunProcessorWithDependencies(
	store reportExecutionStore,
	credential reportDatasourceCredentialDecryptor,
	parameters reportRunParameterDecryptor,
	executor reportProcedureExecutor,
) *ReportRunProcessor {
	if store == nil || credential == nil || parameters == nil || executor == nil {
		panic("report run processor: dependencies are required")
	}
	processor := &ReportRunProcessor{
		store: store, credential: credential, parameters: parameters, executor: executor,
		workerID: "report-run-" + uuid.NewString(), leaseTTL: defaultReportRunLeaseTTL,
		heartbeatInterval: defaultReportRunHeartbeatInterval, stateTimeout: defaultReportRunStateTimeout,
		resultRetention: defaultReportResultRetention, now: func() time.Time { return time.Now().UTC() },
	}
	if reconciliationStore, ok := store.(reportReconciliationStore); ok {
		reconciler := NewReportRunReconcilerWithDependencies(reconciliationStore, credential, oracleReportResultEvidenceReader{})
		processor.reconcileOne = reconciler.ReconcileOne
	}
	return processor
}

func (processor *ReportRunProcessor) Process(ctx context.Context, runID uint, failureRetryLimit int, queueRetryAllowed bool) error {
	if processor == nil || processor.store == nil || processor.executor == nil || ctx == nil || runID == 0 || failureRetryLimit < 0 {
		return fmt.Errorf("%w: invalid processor input", ErrReportRunProcessNonRetryable)
	}
	leaseToken := uuid.NewString()
	lease, err := processor.store.BeginExecution(ctx, runID, processor.workerID, leaseToken, processor.now().UTC(), processor.leaseTTL)
	if err != nil {
		if errors.Is(err, reportrepo.ErrReportRunNotFound) {
			return fmt.Errorf("%w: run does not exist", ErrReportRunProcessNonRetryable)
		}
		confirmCtx, confirmCancel := processor.stateContext(ctx)
		control, confirmErr := processor.store.InspectExecution(confirmCtx, runID, leaseToken)
		confirmCancel()
		if confirmErr == nil && control == reportrepo.RunControlContinue {
			lease = &reportrepo.RunLease{Disposition: reportrepo.RunDispositionAcquired, LeaseToken: leaseToken}
		} else {
			return errReportRunWorkerTemporary
		}
	}
	switch lease.Disposition {
	case reportrepo.RunDispositionBusy:
		fields := []zap.Field{zap.Uint("report_run_id", runID), zap.Bool("queue_retry_allowed", queueRetryAllowed)}
		if blocker := lease.Blocker; blocker != nil {
			fields = append(fields,
				zap.Uint("blocker_run_id", blocker.RunID), zap.Uint("blocker_requested_by", blocker.RequestedBy),
				zap.String("blocker_status", blocker.Status), zap.Int("blocker_attempt", blocker.Attempt),
				zap.Bool("blocker_cancel_requested", blocker.CancelRequested), zap.Timep("blocker_oracle_started_at", blocker.OracleStartedAt),
				zap.Timep("blocker_lease_expires_at", blocker.LeaseExpiresAt), zap.Timep("blocker_result_expires_at", blocker.ResultExpiresAt),
				zap.Timep("blocker_result_purged_at", blocker.ResultPurgedAt), zap.Any("blocker_export_id", blocker.ExportID),
				zap.Any("blocker_export_status", blocker.ExportStatus), zap.Any("blocker_export_attempt", blocker.ExportAttempt),
				zap.Timep("blocker_export_lease_expires_at", blocker.ExportLeaseExpiresAt), zap.Timep("blocker_export_purged_at", blocker.ExportPurgedAt),
			)
		}
		zap.L().Info("报表运行等待前序快照释放", fields...)
		if !queueRetryAllowed {
			stateCtx, cancel := processor.stateContext(ctx)
			defer cancel()
			err := processor.store.MarkQueuedExecutionFailed(stateCtx, runID, "SNAPSHOT_WAIT_TIMEOUT", "等待其他用户的报表文件生成超时，请重新提交", processor.now().UTC())
			if errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
				return nil
			}
			if err != nil {
				return errReportRunWorkerTemporary
			}
			return fmt.Errorf("%w: snapshot wait timeout", ErrReportRunProcessNonRetryable)
		}
		cleanupTarget := staleResultCleanupBlocker(lease.Blocker, processor.now().UTC())
		return reportRunWaitingError{cleanupRunID: cleanupTarget.RunID, cleanupExportID: cleanupTarget.ExportID}
	case reportrepo.RunDispositionTerminal:
		return nil
	case reportrepo.RunDispositionReconcile:
		if processor.reconcileOne == nil {
			return fmt.Errorf("%w: run requires reconciliation", ErrReportRunProcessNonRetryable)
		}
		if err := processor.reconcileOne(ctx, runID); err != nil {
			return errReportRunWorkerTemporary
		}
		return nil
	case reportrepo.RunDispositionAcquired:
	default:
		return fmt.Errorf("%w: unsupported run disposition", ErrReportRunProcessNonRetryable)
	}
	retryAllowed := lease.Run.Attempt > 0 && lease.Run.Attempt <= failureRetryLimit

	runCtx, cancel := context.WithCancel(ctx)
	monitorDone := processor.startMonitor(runCtx, cancel, runID, leaseToken)
	defer func() {
		cancel()
		<-monitorDone
	}()

	runtime, err := processor.store.LoadRuntimeContract(runCtx, runID, leaseToken)
	if err != nil {
		if errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
			return nil
		}
		return processor.finishPreOracleFailure(ctx, runID, leaseToken, retryAllowed, err)
	}

	if runtime.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot {
		budget, ok := processor.store.(interface {
			AcquireSelectDatasourceSlot(context.Context, uint) (func(), error)
			DeferSelectRun(context.Context, uint, string) error
		})
		if !ok {
			return processor.finishFailure(ctx, runID, leaseToken, "SOURCE_BUDGET_UNAVAILABLE", "查询并发控制不可用", nil)
		}
		release, budgetErr := budget.AcquireSelectDatasourceSlot(runCtx, runtime.Version.DatasourceID)
		if errors.Is(budgetErr, reportrepo.ErrReportRunBusy) {
			stateCtx, done := processor.stateContext(ctx)
			defer done()
			if err := budget.DeferSelectRun(stateCtx, runID, leaseToken); err != nil {
				return errReportRunWorkerTemporary
			}
			return reportRunWaitingError{}
		}
		if budgetErr != nil {
			return processor.finishPreOracleFailure(ctx, runID, leaseToken, retryAllowed, budgetErr)
		}
		defer release()
	}
	definitions := reportParameterDefinitions(runtime.Parameters)
	values := map[string]interface{}{}
	jsonPayload := ""
	if runtime.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot {
		// Parameters and runtime filters are consumed by the SELECT executor.
	} else if runtime.Version.ExecutionMode == model.ReportExecutionModeRefCursor {
		jsonPayload, err = buildRefCursorPayload(runtime.Run)
	} else if isJSONTableSnapshot(runtime.Version) {
		jsonPayload, err = buildJSONTablePayload(runtime.Run)
	} else {
		values, err = processor.restoreParameters(runtime.Run, definitions)
	}
	if err != nil {
		return processor.finishFailure(ctx, runID, leaseToken, "PARAMETER_SNAPSHOT_INVALID", "报表参数快照不可用", err)
	}
	password, err := processor.credential.Decrypt(runtime.Datasource.CredentialKeyVersion, runtime.Datasource.PasswordCiphertext)
	if err != nil {
		return processor.finishFailure(ctx, runID, leaseToken, "DATASOURCE_CREDENTIAL_INVALID", "Oracle数据源凭据不可用", err)
	}
	stateCtx, stateCancel := processor.stateContext(ctx)
	control, inspectErr := processor.store.InspectExecution(stateCtx, runID, leaseToken)
	stateCancel()
	if inspectErr != nil {
		return processor.finishPreOracleFailure(ctx, runID, leaseToken, retryAllowed, inspectErr)
	}
	if control == reportrepo.RunControlCancelRequested {
		return processor.finishCancelled(ctx, runID, leaseToken)
	}
	if control == reportrepo.RunControlLeaseLost {
		return nil
	}
	stateCtx, stateCancel = processor.stateContext(ctx)
	err = processor.store.MarkOracleExecutionStarted(stateCtx, runID, leaseToken, processor.now().UTC())
	stateCancel()
	if err != nil {
		if errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
			return nil
		}
		return processor.finishPreOracleFailure(ctx, runID, leaseToken, retryAllowed, err)
	}
	executionRequest := reportProcedureExecutionRequest{
		Runtime: *runtime, Definitions: definitions, Values: values, JSONPayload: jsonPayload,
	}
	rowCount, executeErr := processor.executor.Execute(runCtx, executionRequest, password)
	if executeErr != nil {
		logReportOracleExecutionFailure(executionRequest, executeErr)
		control, inspectErr := processor.inspect(ctx, runID, leaseToken)
		if errors.Is(executeErr, errOracleCommitOutcomeUnknown) || strings.Contains(executeErr.Error(), "SELECT_SNAPSHOT_COMMIT_UNKNOWN") {
			return processor.finishUnknown(ctx, runID, leaseToken, "ORACLE_COMMIT_OUTCOME_UNKNOWN", "Oracle提交结果需要对账", executeErr)
		}
		if inspectErr == nil && control == reportrepo.RunControlCancelRequested {
			return processor.finishCancelled(ctx, runID, leaseToken)
		}
		if errors.Is(executeErr, context.Canceled) && control == reportrepo.RunControlLeaseLost {
			return nil
		}
		if errors.Is(executeErr, reportoracle.ErrSelectLimitExceeded) {
			return processor.finishFailure(ctx, runID, leaseToken, "REPORT_QUERY_LIMIT_EXCEEDED", "结果超过行数或容量限制，请缩小查询范围", executeErr)
		}
		if runtime.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot && strings.Contains(executeErr.Error(), "REPORT_METADATA_CHANGED") {
			return processor.finishFailure(ctx, runID, leaseToken, "REPORT_METADATA_CHANGED", "依赖字段或授权范围已变化，请刷新字典后重新发布", executeErr)
		}
		return processor.finishFailure(ctx, runID, leaseToken, "ORACLE_EXECUTION_FAILED", safeReportOracleExecutionMessage(executeErr), executeErr)
	}
	finishedAt := processor.now().UTC()
	stateCtx, stateCancel = processor.stateContext(ctx)
	err = processor.store.MarkExecutionSucceeded(stateCtx, runID, leaseToken, rowCount, finishedAt, finishedAt.Add(reportResultRetention(runtime.Run, processor.resultRetention)))
	stateCancel()
	if err == nil {
		return nil
	}
	confirmCtx, confirmCancel := processor.stateContext(ctx)
	confirmed, confirmErr := processor.store.ConfirmExecutionSucceeded(confirmCtx, runID, rowCount)
	confirmCancel()
	if confirmErr == nil && confirmed {
		return nil
	}
	unknownErr := processor.finishUnknown(ctx, runID, leaseToken, "MYSQL_SUCCESS_OUTCOME_UNKNOWN", "运行成功状态需要对账", err)
	if errors.Is(unknownErr, reportrepo.ErrReportRunLeaseLost) {
		return fmt.Errorf("%w: success state could not be confirmed", ErrReportRunProcessNonRetryable)
	}
	return unknownErr
}

func staleResultCleanupBlocker(blocker *reportrepo.RunExecutionBlocker, now time.Time) ReportCleanupTarget {
	if blocker == nil || blocker.RunID == 0 ||
		(blocker.Status != model.ReportRunStatusSucceeded && blocker.Status != model.ReportRunStatusResultPurging) ||
		blocker.ResultPurgedAt != nil || blocker.LeaseExpiresAt != nil && now.Before(blocker.LeaseExpiresAt.UTC()) {
		return ReportCleanupTarget{}
	}
	if blocker.ExportID != nil {
		if blocker.ExportStatus == nil || blocker.ExportPurgedAt != nil ||
			blocker.ExportLeaseExpiresAt != nil && now.Before(blocker.ExportLeaseExpiresAt.UTC()) {
			return ReportCleanupTarget{}
		}
		if *blocker.ExportStatus == model.ReportExportStatusReady {
			return ReportCleanupTarget{ExportID: *blocker.ExportID}
		}
		if !terminalResultCleanupExportStatus(*blocker.ExportStatus) {
			return ReportCleanupTarget{}
		}
	}
	if blocker.ResultExpiresAt == nil || now.Before(blocker.ResultExpiresAt.UTC()) {
		return ReportCleanupTarget{}
	}
	return ReportCleanupTarget{RunID: blocker.RunID}
}

func terminalResultCleanupExportStatus(status string) bool {
	switch status {
	case model.ReportExportStatusFailed, model.ReportExportStatusCancelled, model.ReportExportStatusExpired:
		return true
	default:
		return false
	}
}

func buildRefCursorPayload(run model.ReportRun) (string, error) {
	if run.ID == 0 {
		return "", fmt.Errorf("report run id is missing")
	}
	conditions, err := decodeParameterSnapshot([]byte(run.NormalizedParametersJSON))
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		ReportID   uint                       `json:"report_id"`
		Conditions map[string]json.RawMessage `json:"conditions"`
	}{ReportID: run.ID, Conditions: conditions})
	if err != nil {
		return "", fmt.Errorf("encode JSON cursor payload: %w", err)
	}
	return string(payload), nil
}

func buildJSONTablePayload(run model.ReportRun) (string, error) {
	if run.ID == 0 {
		return "", fmt.Errorf("report run id is missing")
	}
	conditions, err := decodeParameterSnapshot([]byte(run.NormalizedParametersJSON))
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		ReportID   uint                       `json:"report_id"`
		Conditions map[string]json.RawMessage `json:"conditions"`
	}{ReportID: run.ID, Conditions: conditions})
	if err != nil {
		return "", fmt.Errorf("encode JSON result-table payload: %w", err)
	}
	return string(payload), nil
}

func (processor *ReportRunProcessor) finishPreOracleFailure(ctx context.Context, runID uint, leaseToken string, retryAllowed bool, cause error) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	if !retryAllowed {
		if err := processor.store.MarkExecutionFailed(stateCtx, runID, leaseToken, "PRE_ORACLE_RETRIES_EXHAUSTED", "报表运行暂时不可用", processor.now().UTC()); err != nil {
			if errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
				return nil
			}
			return fmt.Errorf("%w: failure state persistence failed", ErrReportRunProcessNonRetryable)
		}
		return fmt.Errorf("%w: 报表运行暂时不可用", ErrReportRunProcessNonRetryable)
	}
	if err := processor.store.ReleaseExecutionForRetry(stateCtx, runID, leaseToken, processor.now().UTC()); err != nil {
		if errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
			return nil
		}
		return errReportRunWorkerTemporary
	}
	return errReportRunWorkerTemporary
}

func (processor *ReportRunProcessor) restoreParameters(run model.ReportRun, definitions []reporting.ParameterDefinition) (map[string]interface{}, error) {
	publicValues, err := decodeParameterSnapshot([]byte(run.NormalizedParametersJSON))
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		if definition.SystemInjected {
			delete(publicValues, definition.Code)
		}
	}
	if run.SensitiveParametersCipher != "" || run.SensitiveParametersKeyVersion != "" {
		if run.SensitiveParametersCipher == "" || run.SensitiveParametersKeyVersion == "" {
			return nil, fmt.Errorf("sensitive parameter snapshot is incomplete")
		}
		plaintext, err := processor.parameters.Decrypt(run.SensitiveParametersKeyVersion, run.SensitiveParametersCipher)
		if err != nil {
			return nil, err
		}
		sensitive, err := decodeParameterSnapshot(plaintext)
		if err != nil {
			return nil, err
		}
		for code, value := range sensitive {
			if _, duplicate := publicValues[code]; duplicate {
				return nil, fmt.Errorf("parameter %q appears in two snapshots", code)
			}
			publicValues[code] = value
		}
	}
	systemValues, err := reportSystemValues(definitions, strconv.FormatUint(uint64(run.DefinitionID), 10), run.RequestedBy)
	if err != nil {
		return nil, err
	}
	normalized, err := reporting.NormalizeParameters(definitions, publicValues, systemValues)
	if err != nil {
		return nil, err
	}
	return normalized.DatabaseValues, nil
}

func decodeParameterSnapshot(raw []byte) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode report parameter snapshot: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode report parameter snapshot: trailing data")
	}
	return result, nil
}

func logReportOracleProcedureInput(request reportProcedureExecutionRequest) {
	zap.L().Info("调用Oracle报表存储过程", reportOracleExecutionLogFields(request, true)...)
}

func logReportOracleExecutionFailure(request reportProcedureExecutionRequest, cause error) {
	fields := append(reportOracleExecutionLogFields(request, false), zap.Error(cause))
	zap.L().Error("Oracle报表执行失败", fields...)
}

func reportOracleExecutionLogFields(request reportProcedureExecutionRequest, includeInput bool) []zap.Field {
	version := request.Runtime.Version
	parts := []string{version.ProcedureOwner}
	if version.PackageName != "" {
		parts = append(parts, version.PackageName)
	}
	parts = append(parts, version.ProcedureName)
	fields := []zap.Field{
		zap.Uint("report_run_id", request.Runtime.Run.ID),
		zap.String("report_run_uuid", request.Runtime.Run.RunUUID),
		zap.Uint("report_definition_id", request.Runtime.Run.DefinitionID),
		zap.String("oracle_procedure", strings.Join(parts, ".")),
		zap.String("execution_mode", version.ExecutionMode),
	}
	if !includeInput {
		return fields
	}
	if request.JSONPayload != "" {
		fields = append(fields,
			zap.String("input_argument", version.JSONInputArgName),
			zap.String("actual_input_json", request.JSONPayload),
		)
	} else {
		fields = append(fields, zap.Any("actual_input_parameters", reportProcedureLogValues(request.Definitions, request.Values)))
	}
	return fields
}

func reportProcedureLogValues(definitions []reporting.ParameterDefinition, values map[string]interface{}) map[string]interface{} {
	logged := make(map[string]interface{}, len(values))
	for key, value := range values {
		logged[key] = value
	}
	for _, definition := range definitions {
		if definition.Sensitive {
			if _, exists := logged[definition.Code]; exists {
				logged[definition.Code] = "[REDACTED]"
			}
		}
	}
	return logged
}

func (processor *ReportRunProcessor) startMonitor(ctx context.Context, cancel context.CancelFunc, runID uint, leaseToken string) <-chan struct{} {
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
				monitorCtx, monitorCancel := context.WithTimeout(context.WithoutCancel(ctx), processor.stateTimeout)
				control, err := processor.store.HeartbeatExecution(monitorCtx, runID, leaseToken, heartbeatAt.UTC(), processor.leaseTTL)
				monitorCancel()
				if err != nil || control == reportrepo.RunControlCancelRequested || control == reportrepo.RunControlLeaseLost {
					cancel()
					return
				}
			}
		}
	}()
	return done
}

func (processor *ReportRunProcessor) inspect(ctx context.Context, runID uint, leaseToken string) (reportrepo.RunControl, error) {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	return processor.store.InspectExecution(stateCtx, runID, leaseToken)
}

func (processor *ReportRunProcessor) finishFailure(ctx context.Context, runID uint, leaseToken, code, safeMessage string, cause error) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	if err := processor.store.MarkExecutionFailed(stateCtx, runID, leaseToken, code, safeMessage, processor.now().UTC()); err != nil {
		if errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
			return nil
		}
		return errReportRunWorkerTemporary
	}
	return errors.Join(
		ErrReportRunProcessNonRetryable,
		fmt.Errorf("%s: %w", safeMessage, cause),
	)
}

func safeReportOracleExecutionMessage(err error) string {
	const fallback = "Oracle存储过程执行失败"
	if err == nil {
		return fallback
	}
	var oracleErr interface {
		error
		Code() int
		Message() string
	}
	if !errors.As(err, &oracleErr) || oracleErr.Code() < 20000 || oracleErr.Code() > 20999 {
		return fallback
	}
	message := oracleErr.Message()
	if lineEnd := strings.IndexAny(message, "\r\n"); lineEnd >= 0 {
		message = message[:lineEnd]
	}
	message = strings.TrimSpace(message)
	message = strings.Map(func(value rune) rune {
		if value < ' ' || value == '\u007f' {
			return -1
		}
		return value
	}, message)
	if message == "" {
		return fallback
	}
	runes := []rune(message)
	if len(runes) > 450 {
		message = string(runes[:450])
	}
	return fmt.Sprintf("ORA-%05d：%s", oracleErr.Code(), message)
}

func (processor *ReportRunProcessor) finishCancelled(ctx context.Context, runID uint, leaseToken string) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	if err := processor.store.MarkExecutionCancelled(stateCtx, runID, leaseToken, processor.now().UTC()); err != nil && !errors.Is(err, reportrepo.ErrReportRunLeaseLost) {
		return errReportRunWorkerTemporary
	}
	return nil
}

func (processor *ReportRunProcessor) finishUnknown(ctx context.Context, runID uint, leaseToken, code, safeMessage string, cause error) error {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	if err := processor.store.MarkExecutionUnknown(stateCtx, runID, leaseToken, code, safeMessage, processor.now().UTC()); err != nil {
		return fmt.Errorf("%w: state persistence failed", ErrReportRunProcessNonRetryable)
	}
	return fmt.Errorf("%w: %s", ErrReportRunProcessNonRetryable, safeMessage)
}

func (processor *ReportRunProcessor) stateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), processor.stateTimeout)
}

var errOracleCommitOutcomeUnknown = errors.New("oracle report commit outcome unknown")

type oracleReportProcedureExecutor struct{}

func (oracleReportProcedureExecutor) Execute(ctx context.Context, request reportProcedureExecutionRequest, password string) (rowCount int64, resultErr error) {
	if request.Runtime.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot {
		return executeSelectReport(ctx, request, password)
	}
	queryCtx, cancel := reportOracleQueryContext(ctx, request.Runtime.Datasource)
	defer cancel()
	adapter, err := reportoracle.Open(queryCtx, oracleConfigFromDatasource(request.Runtime.Datasource, password))
	if err != nil {
		return 0, err
	}
	defer func() { _ = adapter.Close() }()
	procedureRef := reportoracle.ProcedureRef{Owner: request.Runtime.Version.ProcedureOwner, Package: request.Runtime.Version.PackageName, Name: request.Runtime.Version.ProcedureName, Overload: request.Runtime.Version.ProcedureOverload}
	procedure, err := adapter.InspectProcedure(queryCtx, procedureRef)
	if err != nil {
		return 0, err
	}
	if request.Runtime.Version.ExecutionMode == model.ReportExecutionModeRefCursor {
		return executeRefCursorReport(queryCtx, adapter, request, procedureRef, procedure)
	}
	resultRef := reportoracle.ResultTableRef{Owner: request.Runtime.Version.ResultTableOwner, Name: request.Runtime.Version.ResultTableName}
	resultColumns, err := adapter.InspectResultTable(queryCtx, resultRef)
	if err != nil {
		return 0, err
	}
	if err := reportcontract.VerifyRuntimeMetadata([]byte(request.Runtime.Version.CompiledSpecJSON), request.Runtime.Run.ContractHash, request.Runtime.Run.ProcedureSignatureHash, request.Runtime.Run.ResultSchemaHash, procedure, resultColumns); err != nil {
		return 0, err
	}
	configuredColumns := make([]string, 0, len(request.Runtime.Columns))
	for _, column := range request.Runtime.Columns {
		configuredColumns = append(configuredColumns, column.DatabaseColumn)
	}
	snapshot, err := adapter.InspectResultSnapshotContract(queryCtx, reportoracle.ResultTableSnapshotRef(resultRef, configuredColumns))
	if err != nil {
		return 0, err
	}
	tx, err := adapter.BeginTx(queryCtx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	// 结果表由存储过程在写入前自行清空，执行器不再提前删除数据。
	var executionErr error
	if isJSONTableSnapshot(request.Runtime.Version) {
		plan, planErr := reportoracle.BuildJSONTableCallPlan(procedureRef, procedure, request.Runtime.Version.JSONInputArgName)
		if planErr != nil {
			_ = tx.Rollback()
			return 0, planErr
		}
		logReportOracleProcedureInput(request)
		executionErr = adapter.ExecuteJSONTable(queryCtx, tx, plan, request.JSONPayload)
	} else {
		plan, planErr := reportoracle.BuildCallPlan(procedureRef, request.Definitions)
		if planErr != nil {
			_ = tx.Rollback()
			return 0, planErr
		}
		logReportOracleProcedureInput(request)
		executionErr = adapter.Execute(queryCtx, tx, plan, request.Values)
	}
	if executionErr != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return 0, fmt.Errorf("%w: rollback after execution failure: %v", errOracleCommitOutcomeUnknown, rollbackErr)
		}
		return 0, executionErr
	}
	countPlan, err := reportoracle.BuildResultCountPlan(snapshot)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	rowCount, err = adapter.CountResultRowsTx(queryCtx, tx, countPlan)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("%w: %v", errOracleCommitOutcomeUnknown, err)
	}
	return rowCount, nil
}

func executeRefCursorReport(
	ctx context.Context,
	adapter *reportoracle.Adapter,
	request reportProcedureExecutionRequest,
	procedureRef reportoracle.ProcedureRef,
	procedure []reportoracle.ProcedureArgument,
) (rowCount int64, resultErr error) {
	if err := reportcontract.VerifyRuntimeProcedureMetadata(
		[]byte(request.Runtime.Version.CompiledSpecJSON), request.Runtime.Run.ContractHash,
		request.Runtime.Run.ProcedureSignatureHash, procedure,
	); err != nil {
		return 0, err
	}
	if err := adapter.ValidateJSONSnapshotStore(ctx); err != nil {
		return 0, err
	}
	plan, err := reportoracle.BuildJSONCursorCallPlan(
		procedureRef, procedure, request.Runtime.Version.JSONInputArgName, request.Runtime.Version.ResultCursorArgName,
	)
	if err != nil {
		return 0, err
	}
	expectedColumns := make([]string, 0, len(request.Runtime.Columns))
	for _, column := range request.Runtime.Columns {
		expectedColumns = append(expectedColumns, column.DatabaseColumn)
	}
	tx, err := adapter.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() {
		if resultErr != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				resultErr = errors.Join(resultErr, fmt.Errorf("rollback JSON cursor report: %w", rollbackErr))
			}
		}
	}()
	logReportOracleProcedureInput(request)
	snapshot, err := adapter.ExecuteJSONCursorSnapshot(ctx, tx, plan, request.Runtime.Run.RunUUID, request.JSONPayload, expectedColumns)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("%w: %v", errOracleCommitOutcomeUnknown, err)
	}
	return snapshot.RowCount, nil
}
