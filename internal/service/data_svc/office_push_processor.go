package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	appconfig "gin-biz-web-api/config"
	"gin-biz-web-api/connector/feishu"
	webdavconnector "gin-biz-web-api/connector/webdav"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	projectredis "gin-biz-web-api/pkg/redis"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	officePushMaximumRows  = int64(100_000)
	officePushLeaseTTL     = 5 * time.Minute
	officePushHeartbeat    = time.Minute
	officePushStateTimeout = 5 * time.Second
	officeProcedureLockTTL = 2 * officePushLeaseTTL
)

var ErrOfficePushProcessNonRetryable = errors.New("office push processor: non-retryable")

var (
	errOfficePushLeaseLost     = errors.New("office push processor: lease lost")
	errOfficeProcedureLockBusy = errors.New("office push processor: procedure result table is busy")
)

type officeBot interface {
	UploadFile(context.Context, string, string) (string, error)
	SendText(context.Context, string, string, string, string) (string, error)
	SendFile(context.Context, string, string, string, string) (string, error)
}

type officeOracleConnection interface {
	InspectProcedure(context.Context, reportoracle.ProcedureRef) ([]reportoracle.ProcedureArgument, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	Execute(context.Context, *sql.Tx, reportoracle.CallPlan, map[string]interface{}) error
	QuerySelect(context.Context, string, ...interface{}) (*sql.Rows, error)
	Close() error
}

type officeWebDAVUploader interface {
	UploadFile(context.Context, webdavconnector.Config, string, string) error
}

type officeBotFactory func() (officeBot, error)
type officeOracleOpener func(context.Context, reportoracle.Config) (officeOracleConnection, error)

type OfficePushProcessor struct {
	db         *gorm.DB
	newBot     officeBotFactory
	botAppID   string
	webDAV     officeWebDAVUploader
	credential officePushCredentialCipher
	openOracle officeOracleOpener
	now        func() time.Time
	newToken   func() string
	leaseTTL   time.Duration
	heartbeat  time.Duration
	stateLimit time.Duration
}

func NewOfficePushProcessor() *OfficePushProcessor {
	var once sync.Once
	var bot officeBot
	var botErr error
	newBot := func() (officeBot, error) {
		once.Do(func() {
			redisInstance := projectredis.Instance()
			if redisInstance == nil || redisInstance.Client == nil {
				botErr = fmt.Errorf("office push processor: redis is unavailable")
				return
			}
			tokens, err := feishu.NewTenantTokenProvider(global.Credentials.FeishuAppID(), global.Credentials.FeishuAppSecret(), nil, redisInstance.Client)
			if err != nil {
				botErr = err
				return
			}
			bot, botErr = feishu.NewBotClient(tokens, nil)
		})
		return bot, botErr
	}
	return newOfficePushProcessor(database.DB, newBot, func(ctx context.Context, config reportoracle.Config) (officeOracleConnection, error) {
		return reportoracle.Open(ctx, config)
	}, global.Credentials.FeishuAppID())
}

func newOfficePushProcessor(db *gorm.DB, newBot officeBotFactory, openOracle officeOracleOpener, botAppIDs ...string) *OfficePushProcessor {
	if db == nil || newBot == nil || openOracle == nil {
		panic("office push processor: dependencies are required")
	}
	processor := &OfficePushProcessor{
		db: db, newBot: newBot, webDAV: webdavconnector.NewClient(nil), credential: reportsecret.EnvironmentKeyring{},
		openOracle: openOracle, now: func() time.Time { return time.Now().UTC() }, newToken: uuid.NewString,
		leaseTTL: officePushLeaseTTL, heartbeat: officePushHeartbeat, stateLimit: officePushStateTimeout,
	}
	if len(botAppIDs) > 0 {
		processor.botAppID = strings.TrimSpace(botAppIDs[0])
	}
	return processor
}

func (processor *OfficePushProcessor) Process(ctx context.Context, runID uint, retryAllowed bool) error {
	if processor == nil || processor.db == nil || processor.newBot == nil || processor.webDAV == nil || processor.credential == nil || processor.openOracle == nil || processor.newToken == nil ||
		processor.now == nil || processor.leaseTTL <= 0 || processor.heartbeat <= 0 || processor.stateLimit <= 0 || ctx == nil || runID == 0 {
		return fmt.Errorf("%w: invalid processor request", ErrOfficePushProcessNonRetryable)
	}
	leaseToken, err := canonicalOfficeUUID(processor.newToken())
	if err != nil {
		return fmt.Errorf("%w: invalid lease token", ErrOfficePushProcessNonRetryable)
	}
	claim, err := processor.claim(ctx, runID, leaseToken)
	if err != nil {
		return err
	}
	if !claim.acquired {
		return nil
	}
	run, target, message := claim.run, claim.target, claim.message
	executionCtx, cancelExecution := context.WithCancel(ctx)
	monitorDone := processor.startMonitor(executionCtx, cancelExecution, run.ID, leaseToken)
	defer func() {
		cancelExecution()
		<-monitorDone
	}()
	var messageID string
	var rowCount int64
	switch effectiveOfficePushChannel(target.Channel) {
	case model.OfficePushChannelFeishu:
		if !officePushBotMatches(target.BotAppID, processor.botAppID) {
			return processor.fail(ctx, run.ID, leaseToken, false, "FEISHU_BOT_MISMATCH", "飞书机器人配置已变更，请重新创建推送任务", ErrOfficePushProcessNonRetryable)
		}
		bot, botErr := processor.newBot()
		if botErr != nil {
			return processor.fail(ctx, run.ID, leaseToken, retryAllowed, "FEISHU_CONFIGURATION_INVALID", "飞书机器人配置不可用", botErr)
		}
		if message.SourceType == model.OfficeMessageSourceEdited {
			messageID, err = bot.SendText(executionCtx, target.ReceiveIDType, target.ReceiveID, message.Content, run.RunUUID)
			break
		}
		path, fileName, exportedRows, exportErr := processor.exportWorkbook(executionCtx, message, run.ParametersJSON, leaseToken)
		if exportErr != nil {
			return processor.fail(ctx, run.ID, leaseToken, retryAllowed, "ORACLE_EXPORT_FAILED", "Oracle 数据导出失败", exportErr)
		}
		defer os.Remove(path)
		rowCount = exportedRows
		fileKey, uploadErr := bot.UploadFile(executionCtx, path, fileName)
		if uploadErr != nil {
			return processor.fail(ctx, run.ID, leaseToken, retryAllowed, "FEISHU_FILE_UPLOAD_FAILED", "飞书文件上传失败", uploadErr)
		}
		messageID, err = bot.SendFile(executionCtx, target.ReceiveIDType, target.ReceiveID, fileKey, run.RunUUID)
	case model.OfficePushChannelWebDAV:
		if message.SourceType == model.OfficeMessageSourceEdited {
			return processor.fail(ctx, run.ID, leaseToken, false, "WEBDAV_MESSAGE_INVALID", "WebDAV 推送仅支持 Excel 消息", ErrOfficePushProcessNonRetryable)
		}
		path, fileName, exportedRows, exportErr := processor.exportWorkbook(executionCtx, message, run.ParametersJSON, leaseToken)
		if exportErr != nil {
			return processor.fail(ctx, run.ID, leaseToken, retryAllowed, "ORACLE_EXPORT_FAILED", "Oracle 数据导出失败", exportErr)
		}
		defer os.Remove(path)
		rowCount = exportedRows
		password, decryptErr := processor.credential.DecryptScoped(officeWebDAVCredentialPurpose, target.CredentialKeyVersion, target.WebDAVPasswordCiphertext)
		if decryptErr != nil {
			return processor.fail(ctx, run.ID, leaseToken, false, "WEBDAV_CREDENTIAL_INVALID", "WebDAV 应用密码不可用", decryptErr)
		}
		err = processor.webDAV.UploadFile(executionCtx, webdavconnector.Config{
			BaseURL: target.WebDAVURL, Username: target.WebDAVUsername, Password: password, Directory: target.WebDAVPath,
		}, path, fileName)
	default:
		return processor.fail(ctx, run.ID, leaseToken, false, "DELIVERY_CHANNEL_INVALID", "推送方式不受支持", ErrOfficePushProcessNonRetryable)
	}
	if err != nil {
		code, safeMessage := "FEISHU_MESSAGE_FAILED", "飞书消息发送失败"
		if effectiveOfficePushChannel(target.Channel) == model.OfficePushChannelWebDAV {
			code, safeMessage = "WEBDAV_UPLOAD_FAILED", "WebDAV 文件上传失败"
		}
		return processor.fail(ctx, run.ID, leaseToken, retryAllowed, code, safeMessage, err)
	}
	finishedAt := processor.now().UTC()
	stateCtx, cancelState := processor.stateContext(ctx)
	defer cancelState()
	result := processor.ownedRun(stateCtx, run.ID, leaseToken).
		Updates(map[string]interface{}{
			"status": model.OfficePushRunStatusSucceeded, "row_count": rowCount,
			"feishu_message_id": messageID, "error_code": "", "error_message_safe": "",
			"finished_at": finishedAt, "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": finishedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("office push processor: persist success: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return errOfficePushLeaseLost
	}
	return nil
}

type officePushClaim struct {
	run      model.OfficePushRun
	target   model.OfficePushTarget
	message  model.OfficeMessage
	acquired bool
}

func (processor *OfficePushProcessor) claim(ctx context.Context, runID uint, leaseToken string) (officePushClaim, error) {
	var claim officePushClaim
	err := processor.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", runID).First(&claim.run).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: push run does not exist", ErrOfficePushProcessNonRetryable)
		} else if err != nil {
			return fmt.Errorf("office push processor: load run: %w", err)
		}
		if claim.run.Status == model.OfficePushRunStatusSucceeded || claim.run.Status == model.OfficePushRunStatusUnknown || claim.run.Status == model.OfficePushRunStatusFailed {
			return nil
		}
		now := processor.now().UTC()
		if !officePushClaimable(claim.run, now) {
			return nil
		}
		snapshot, snapshotErr := decodeOfficePushSnapshot(claim.run.SnapshotJSON)
		if snapshotErr != nil {
			if strings.TrimSpace(string(claim.run.SnapshotJSON)) != "" {
				return processor.markInvalidSnapshot(tx, &claim.run, now)
			}
			var target model.OfficePushTarget
			if err := tx.Where("id = ? AND enabled = ?", claim.run.TargetID, true).First(&target).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return processor.markInvalidSnapshot(tx, &claim.run, now)
			} else if err != nil {
				return fmt.Errorf("office push processor: load legacy target: %w", err)
			}
			var message model.OfficeMessage
			if err := tx.Where("id = ? AND enabled = ?", claim.run.MessageID, true).First(&message).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return processor.markInvalidSnapshot(tx, &claim.run, now)
			} else if err != nil {
				return fmt.Errorf("office push processor: load legacy message: %w", err)
			}
			raw, err := newOfficePushSnapshot(target, message)
			if err != nil {
				return err
			}
			claim.run.SnapshotJSON = raw
			snapshot, snapshotErr = decodeOfficePushSnapshot(raw)
			if snapshotErr != nil {
				return snapshotErr
			}
		}
		claim.target, claim.message = snapshot.targetModel(), snapshot.messageModel()
		expiresAt := now.Add(processor.leaseTTL)
		if err := tx.Model(&claim.run).Updates(map[string]interface{}{
			"status": model.OfficePushRunStatusRunning, "attempt_count": gorm.Expr("attempt_count + 1"),
			"snapshot_json": claim.run.SnapshotJSON, "started_at": now, "finished_at": nil, "error_code": "", "error_message_safe": "",
			"lease_token": leaseToken, "lease_expires_at": expiresAt, "heartbeat_at": now, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("office push processor: claim run: %w", err)
		}
		claim.run.Status, claim.run.LeaseToken, claim.run.LeaseExpiresAt = model.OfficePushRunStatusRunning, leaseToken, &expiresAt
		claim.run.AttemptCount++
		claim.acquired = true
		return nil
	})
	return claim, err
}

func (processor *OfficePushProcessor) markInvalidSnapshot(tx *gorm.DB, run *model.OfficePushRun, now time.Time) error {
	if err := tx.Model(run).Updates(map[string]interface{}{
		"status": model.OfficePushRunStatusFailed, "error_code": "EXECUTION_SNAPSHOT_INVALID", "error_message_safe": "推送任务执行快照不可用",
		"finished_at": now, "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now,
	}).Error; err != nil {
		return fmt.Errorf("office push processor: reject invalid snapshot: %w", err)
	}
	return nil
}

func (processor *OfficePushProcessor) fail(ctx context.Context, runID uint, leaseToken string, retryAllowed bool, code, safeMessage string, cause error) error {
	now := processor.now().UTC()
	status := model.OfficePushRunStatusFailed
	if retryAllowed && officePushRetryable(cause) {
		status = model.OfficePushRunStatusQueued
	}
	finishedAt := interface{}(now)
	if status == model.OfficePushRunStatusQueued {
		finishedAt = nil
	}
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	result := processor.ownedRun(stateCtx, runID, leaseToken).
		Updates(map[string]interface{}{
			"status": status, "error_code": code, "error_message_safe": safeMessage, "finished_at": finishedAt,
			"lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "updated_at": now,
		})
	if result.Error != nil {
		return errors.Join(cause, fmt.Errorf("office push processor: persist failure: %w", result.Error))
	}
	if result.RowsAffected != 1 {
		return nil
	}
	if status == model.OfficePushRunStatusQueued {
		return cause
	}
	return fmt.Errorf("%w: %s", ErrOfficePushProcessNonRetryable, safeMessage)
}

func (processor *OfficePushProcessor) startMonitor(ctx context.Context, cancelExecution context.CancelFunc, runID uint, leaseToken string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(processor.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeatAt := processor.now().UTC()
				stateCtx, cancel := processor.stateContext(ctx)
				result := processor.ownedActiveRun(stateCtx, runID, leaseToken, heartbeatAt).
					Updates(map[string]interface{}{"lease_expires_at": heartbeatAt.UTC().Add(processor.leaseTTL), "heartbeat_at": heartbeatAt.UTC(), "updated_at": heartbeatAt.UTC()})
				cancel()
				if result.Error != nil || result.RowsAffected != 1 {
					cancelExecution()
					return
				}
			}
		}
	}()
	return done
}

func (processor *OfficePushProcessor) stateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), processor.stateLimit)
}

func (processor *OfficePushProcessor) ownedRun(ctx context.Context, runID uint, leaseToken string) *gorm.DB {
	return processor.db.WithContext(ctx).Model(&model.OfficePushRun{}).
		Where("id = ? AND status = ? AND lease_token = ?", runID, model.OfficePushRunStatusRunning, leaseToken)
}

func (processor *OfficePushProcessor) ownedActiveRun(ctx context.Context, runID uint, leaseToken string, heartbeatAt time.Time) *gorm.DB {
	return processor.ownedRun(ctx, runID, leaseToken).Where("lease_expires_at > ?", heartbeatAt)
}

func officePushClaimable(run model.OfficePushRun, now time.Time) bool {
	if run.Status == model.OfficePushRunStatusQueued {
		return true
	}
	return run.Status == model.OfficePushRunStatusRunning && (run.LeaseToken == "" || run.LeaseExpiresAt == nil || !now.Before(run.LeaseExpiresAt.UTC()))
}

func officePushRetryable(err error) bool {
	var botError *feishu.BotError
	if errors.As(err, &botError) {
		return botError.Retryable
	}
	var retryableError interface{ Retryable() bool }
	if errors.As(err, &retryableError) {
		return retryableError.Retryable()
	}
	return true
}

func officePushBotMatches(targetAppID, configuredAppID string) bool {
	targetAppID = strings.TrimSpace(targetAppID)
	configuredAppID = strings.TrimSpace(configuredAppID)
	return targetAppID == "" || configuredAppID != "" && targetAppID == configuredAppID
}

func (processor *OfficePushProcessor) exportWorkbook(ctx context.Context, message model.OfficeMessage, rawParameters model.JSONText, leaseToken string) (string, string, int64, error) {
	mappings, _, err := normalizeOfficeColumnMappings(message.ColumnMappingJSON, message.SourceType)
	if err != nil {
		return "", "", 0, err
	}
	fileName, err := renderOfficeWorkbookFileName(message.FileNameTemplate, message.Name, processor.now())
	if err != nil {
		return "", "", 0, err
	}
	procedureLockKey := ""
	if message.SourceType == model.OfficeMessageSourceOracleProcedure {
		procedureLockKey = officeProcedureLockKey(message)
		if err := processor.acquireProcedureLock(ctx, procedureLockKey, leaseToken); err != nil {
			return "", "", 0, err
		}
		procedureCtx, cancelProcedure := context.WithCancel(ctx)
		procedureMonitorDone := processor.startProcedureLockMonitor(procedureCtx, cancelProcedure, procedureLockKey, leaseToken)
		defer func() {
			cancelProcedure()
			<-procedureMonitorDone
			processor.releaseProcedureLock(ctx, procedureLockKey, leaseToken)
		}()
		ctx = procedureCtx
	}
	oracleConfig, queryTimeout, err := officeOracleConfigFromEnvironment()
	if err != nil {
		return "", "", 0, err
	}
	openCtx, cancel := context.WithTimeout(ctx, oracleConfig.ConnectTimeout)
	connection, err := processor.openOracle(openCtx, oracleConfig)
	cancel()
	if err != nil {
		return "", "", 0, err
	}
	defer connection.Close()

	queryCtx, queryCancel := context.WithTimeout(ctx, queryTimeout)
	defer queryCancel()
	var pager interface {
		reportExportPageReader
		Close() error
	}
	switch message.SourceType {
	case model.OfficeMessageSourceOracleProcedure:
		pager, err = newOfficeProcedurePager(queryCtx, connection, message, mappings)
	case model.OfficeMessageSourceOracleQuery:
		pager, err = newOfficeSelectPager(queryCtx, connection, message, mappings, rawParameters)
	default:
		err = fmt.Errorf("office push processor: unsupported message source")
	}
	if err != nil {
		return "", "", 0, err
	}
	defer pager.Close()

	tempDir, err := os.MkdirTemp("", "office-push-*")
	if err != nil {
		return "", "", 0, fmt.Errorf("office push processor: create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	outputPath := filepath.Join(tempDir, fileName)
	renderer := NewReportExportRenderer(pager)
	renderer.maxSheets = 1
	result, err := renderer.Render(queryCtx, officeWorkbookRenderRequest(mappings, outputPath), nil)
	if err != nil {
		return "", "", 0, err
	}
	stablePath := filepath.Join(os.TempDir(), "office-push-"+strconv.FormatUint(uint64(message.ID), 10)+"-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".xlsx")
	if err := os.Rename(outputPath, stablePath); err != nil {
		return "", "", 0, fmt.Errorf("office push processor: move workbook: %w", err)
	}
	return stablePath, fileName, result.ProcessedRows, nil
}

func (processor *OfficePushProcessor) acquireProcedureLock(ctx context.Context, lockKey, leaseToken string) error {
	if lockKey == "." || len(lockKey) > 255 {
		return fmt.Errorf("office push processor: invalid procedure lock")
	}
	canonicalLeaseToken, err := canonicalOfficeUUID(leaseToken)
	if err != nil || canonicalLeaseToken != leaseToken {
		return fmt.Errorf("office push processor: invalid procedure lock")
	}
	for {
		err := processor.tryAcquireProcedureLock(ctx, lockKey, leaseToken)
		if !errors.Is(err, errOfficeProcedureLockBusy) {
			return err
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (processor *OfficePushProcessor) startProcedureLockMonitor(ctx context.Context, cancelExecution context.CancelFunc, lockKey, leaseToken string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(processor.heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeatAt := processor.now().UTC()
				stateCtx, cancel := processor.stateContext(ctx)
				result := processor.ownedProcedureLock(stateCtx, lockKey, leaseToken, heartbeatAt.UTC()).
					Updates(map[string]interface{}{"lease_expires_at": heartbeatAt.UTC().Add(officeProcedureLockTTL), "updated_at": heartbeatAt.UTC()})
				cancel()
				if result.Error != nil || result.RowsAffected != 1 {
					cancelExecution()
					return
				}
			}
		}
	}()
	return done
}

func (processor *OfficePushProcessor) ownedProcedureLock(ctx context.Context, lockKey, leaseToken string, heartbeatAt time.Time) *gorm.DB {
	return processor.db.WithContext(ctx).Model(&model.OfficeProcedureExportLock{}).
		Where("lock_key = ? AND lease_token = ? AND lease_expires_at > ?", lockKey, leaseToken, heartbeatAt)
}

func (processor *OfficePushProcessor) tryAcquireProcedureLock(ctx context.Context, lockKey, leaseToken string) error {
	now := processor.now().UTC()
	return processor.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lock model.OfficeProcedureExportLock
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("lock_key = ?", lockKey).First(&lock).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lock = model.OfficeProcedureExportLock{LockKey: lockKey, LeaseToken: leaseToken, LeaseExpiresAt: now.Add(officeProcedureLockTTL), UpdatedAt: now}
			if err := tx.Create(&lock).Error; err != nil {
				return fmt.Errorf("%w: %v", errOfficeProcedureLockBusy, err)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("office push processor: load procedure lock: %w", err)
		}
		if lock.LeaseToken != leaseToken && now.Before(lock.LeaseExpiresAt.UTC()) {
			return errOfficeProcedureLockBusy
		}
		if err := tx.Model(&lock).Updates(map[string]interface{}{"lease_token": leaseToken, "lease_expires_at": now.Add(officeProcedureLockTTL), "updated_at": now}).Error; err != nil {
			return fmt.Errorf("office push processor: acquire procedure lock: %w", err)
		}
		return nil
	})
}

func officeProcedureLockKey(message model.OfficeMessage) string {
	if message.SourceType != model.OfficeMessageSourceOracleProcedure {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(message.ResultTableOwner)) + "." + strings.ToUpper(strings.TrimSpace(message.ResultTableName))
}

func (processor *OfficePushProcessor) releaseProcedureLock(ctx context.Context, lockKey, leaseToken string) {
	stateCtx, cancel := processor.stateContext(ctx)
	defer cancel()
	_ = processor.db.WithContext(stateCtx).Where("lock_key = ? AND lease_token = ?", lockKey, leaseToken).Delete(&model.OfficeProcedureExportLock{}).Error
}

func officeOracleConfigFromEnvironment() (reportoracle.Config, time.Duration, error) {
	configured, err := appconfig.LoadReportInputQueryConfig()
	if err != nil {
		return reportoracle.Config{}, 0, err
	}
	oracle := configured.Oracle
	if oracle.Host == "" || oracle.Username == "" || oracle.Password == "" || (oracle.ServiceName == "" && oracle.SID == "") || (oracle.ServiceName != "" && oracle.SID != "") {
		return reportoracle.Config{}, 0, fmt.Errorf("office push processor: default Oracle environment is incomplete")
	}
	return reportoracle.Config{
		Host: oracle.Host, Port: oracle.Port, ServiceName: oracle.ServiceName, SID: oracle.SID,
		Username: oracle.Username, Password: oracle.Password, Timezone: oracle.Timezone,
		ConnectTimeout: oracle.ConnectTimeout, MaxOpenConnections: oracle.MaxOpenConnections,
		MaxIdleConnections: oracle.MaxIdleConnections, ConnectionLifetime: oracle.ConnectionLifetime,
		ConnectionIdleTime: oracle.ConnectionIdleTime, PrefetchRows: oracle.PrefetchRows, FetchArraySize: oracle.ArraySize,
	}, oracle.QueryTimeout, nil
}

func newOfficeProcedurePager(ctx context.Context, connection officeOracleConnection, message model.OfficeMessage, mappings []OfficeColumnMapping) (*officeSelectPager, error) {
	ref := reportoracle.ProcedureRef{Owner: message.ProcedureOwner, Package: message.PackageName, Name: message.ProcedureName, Overload: message.ProcedureOverload}
	arguments, err := connection.InspectProcedure(ctx, ref)
	if err != nil {
		return nil, err
	}
	if len(arguments) != 0 {
		return nil, fmt.Errorf("office push processor: configured procedure must not require arguments")
	}
	plan, err := reportoracle.BuildCallPlan(ref, nil)
	if err != nil {
		return nil, err
	}
	tx, err := connection.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	if err := connection.Execute(ctx, tx, plan, map[string]interface{}{}); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("office push processor: commit Oracle procedure: %w", err)
	}
	table, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: message.ResultTableOwner, Name: message.ResultTableName})
	if err != nil {
		return nil, err
	}
	columns := officeSourceColumns(mappings)
	quotedColumns := make([]string, len(columns))
	for index, column := range columns {
		quotedColumns[index] = `"` + column + `"`
	}
	statement := `SELECT ` + strings.Join(quotedColumns, ", ") + ` FROM "` + table.Owner + `"."` + table.Name + `"`
	rows, err := connection.QuerySelect(ctx, statement)
	if err != nil {
		return nil, err
	}
	return newOfficeRowsPager(rows, mappings)
}

type officeSelectPager struct {
	rows          *sql.Rows
	requested     []string
	columnIndexes []int
	pending       []interface{}
	readRows      int64
}

func newOfficeSelectPager(ctx context.Context, connection officeOracleConnection, message model.OfficeMessage, mappings []OfficeColumnMapping, rawParameters model.JSONText) (*officeSelectPager, error) {
	schema, _, err := normalizeOfficeQueryParameters(message.SelectSQL, message.ParameterSchemaJSON)
	if err != nil {
		return nil, err
	}
	arguments, err := officeParameterArguments(schema, rawParameters)
	if err != nil {
		return nil, err
	}
	rows, err := connection.QuerySelect(ctx, message.SelectSQL, arguments...)
	if err != nil {
		return nil, err
	}
	return newOfficeRowsPager(rows, mappings)
}

func newOfficeRowsPager(rows *sql.Rows, mappings []OfficeColumnMapping) (*officeSelectPager, error) {
	if rows == nil {
		return nil, fmt.Errorf("office push processor: SELECT rows are unavailable")
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("office push processor: read SELECT columns: %w", err)
	}
	byName := make(map[string]int, len(columns))
	for index, column := range columns {
		key := strings.ToUpper(strings.TrimSpace(column))
		if _, duplicate := byName[key]; duplicate {
			rows.Close()
			return nil, fmt.Errorf("office push processor: SELECT returned duplicate columns")
		}
		byName[key] = index
	}
	requested := officeSourceColumns(mappings)
	indexes := make([]int, len(requested))
	for index, column := range requested {
		position, exists := byName[column]
		if !exists {
			rows.Close()
			return nil, fmt.Errorf("office push processor: configured SELECT column %s is missing", column)
		}
		indexes[index] = position
	}
	return &officeSelectPager{rows: rows, requested: requested, columnIndexes: indexes}, nil
}

func (pager *officeSelectPager) Read(ctx context.Context, columns []string, after *reportoracle.ResultCursor, limit int) (reportoracle.ResultPage, error) {
	if pager == nil || pager.rows == nil || ctx == nil || limit < 1 || len(columns) != len(pager.requested) {
		return reportoracle.ResultPage{}, fmt.Errorf("office push processor: invalid SELECT page request")
	}
	for index := range columns {
		if !strings.EqualFold(columns[index], pager.requested[index]) {
			return reportoracle.ResultPage{}, fmt.Errorf("office push processor: SELECT column contract changed")
		}
	}
	if after != nil && after.Key != strconv.FormatInt(pager.readRows, 10) {
		return reportoracle.ResultPage{}, fmt.Errorf("office push processor: SELECT cursor changed")
	}
	page := reportoracle.ResultPage{Columns: append([]string(nil), pager.requested...), Rows: make([]reportoracle.ResultRow, 0, limit)}
	for len(page.Rows) < limit {
		values, ok, err := pager.nextRow()
		if err != nil {
			return reportoracle.ResultPage{}, err
		}
		if !ok {
			return page, nil
		}
		pager.readRows++
		if pager.readRows > officePushMaximumRows {
			return reportoracle.ResultPage{}, fmt.Errorf("office push processor: Oracle result exceeds %d rows", officePushMaximumRows)
		}
		rowValues := make([]interface{}, len(pager.columnIndexes))
		for index, position := range pager.columnIndexes {
			rowValues[index] = values[position]
		}
		key := strconv.FormatInt(pager.readRows, 10)
		page.Rows = append(page.Rows, reportoracle.ResultRow{Key: key, Values: rowValues})
		page.NextKey = key
	}
	values, ok, err := pager.nextRow()
	if err != nil {
		return reportoracle.ResultPage{}, err
	}
	if ok {
		pager.pending = values
		page.HasNext = true
	}
	return page, nil
}

func (pager *officeSelectPager) nextRow() ([]interface{}, bool, error) {
	if pager.pending != nil {
		values := pager.pending
		pager.pending = nil
		return values, true, nil
	}
	if !pager.rows.Next() {
		if err := pager.rows.Err(); err != nil {
			return nil, false, fmt.Errorf("office push processor: iterate SELECT rows: %w", err)
		}
		return nil, false, nil
	}
	columns, err := pager.rows.Columns()
	if err != nil {
		return nil, false, fmt.Errorf("office push processor: read SELECT columns: %w", err)
	}
	values := make([]interface{}, len(columns))
	destinations := make([]interface{}, len(columns))
	for index := range values {
		destinations[index] = &values[index]
	}
	if err := pager.rows.Scan(destinations...); err != nil {
		return nil, false, fmt.Errorf("office push processor: scan SELECT row: %w", err)
	}
	return values, true, nil
}

func (pager *officeSelectPager) Close() error {
	if pager == nil || pager.rows == nil {
		return nil
	}
	return pager.rows.Close()
}

func officeFrozenColumns(mappings []OfficeColumnMapping) []frozenResultColumn {
	columns := make([]frozenResultColumn, len(mappings))
	for index, mapping := range mappings {
		columns[index] = frozenResultColumn{
			FieldID: strings.ToLower(mapping.SourceColumn), LogicalCode: strings.ToLower(mapping.SourceColumn),
			DatabaseColumn: mapping.SourceColumn, ValueType: mapping.ValueType,
			ExcelHeader: mapping.Header, PreviewHeader: mapping.Header, ExportOrder: mapping.Order,
			DisplayOrder: mapping.Order, PreviewVisible: true, ExportVisible: true, ExportAllowed: true, ExcelWidth: mapping.Width,
		}
	}
	return columns
}

func officeWorkbookRenderRequest(mappings []OfficeColumnMapping, outputPath string) ReportExportRenderRequest {
	return ReportExportRenderRequest{
		Columns:             officeFrozenColumns(mappings),
		OutputPath:          outputPath,
		decimalNumberFormat: reportExcelTwoDecimalNumberFormat,
	}
}

func officeSourceColumns(mappings []OfficeColumnMapping) []string {
	columns := make([]string, len(mappings))
	for index, mapping := range mappings {
		columns[index] = mapping.SourceColumn
	}
	return columns
}

func officeWorkbookFileName(name string) string {
	name = strings.Map(func(character rune) rune {
		if character == '/' || character == '\\' || unicode.IsControl(character) {
			return '-'
		}
		return character
	}, strings.TrimSpace(name))
	if name == "" {
		name = "办公消息"
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	return name + ".xlsx"
}
