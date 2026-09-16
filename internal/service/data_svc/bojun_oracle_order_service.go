package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/internal/service/config_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"

	"github.com/google/uuid"
)

const (
	bojunOracleDatasourceCode   = "etl01"
	bojunOracleOrderSource      = "bojun_order_oracle"
	bojunOracleDefaultBatchSize = 100
	bojunOracleDefaultMaxPages  = 20
	bojunOracleDefaultLeaseTTL  = 2 * time.Minute
)

type bojunOracleDatasourceStore interface {
	FindEnabledReportDatasourceByCode(context.Context, string) (*model.ReportDatasource, error)
}

type bojunOracleCredentialDecryptor interface {
	Decrypt(version, ciphertext string) (string, error)
}

type bojunOracleConnection interface {
	QueryBojunRetailAfterID(context.Context, uint64, int) ([]reportoracle.BojunRetailRow, error)
	QueryBojunRetailByStatusTime(context.Context, time.Time, time.Time, uint64, int) ([]reportoracle.BojunRetailRow, error)
	MaxBojunRetailID(context.Context) (uint64, error)
	UpdateBojunRetailPushStatus(context.Context, uint64, bool, int) error
	Close() error
}

type bojunOracleConnectionOpener func(context.Context, reportoracle.Config) (bojunOracleConnection, error)

type bojunOracleSyncStateStore interface {
	Get(context.Context, string) (*model.BojunOracleSyncState, error)
	Initialize(context.Context, string, uint64, time.Time) (*model.BojunOracleSyncState, bool, error)
	AcquireLease(context.Context, string, string, time.Time, time.Duration) (*model.BojunOracleSyncState, bool, error)
	RenewLease(context.Context, string, string, time.Time, time.Duration) error
	Advance(context.Context, string, string, uint64, uint64, time.Time, time.Duration) error
	ReleaseLease(context.Context, string, string, time.Time) error
}

type bojunOracleRetailOrderStore interface {
	ExistsByDocNo(context.Context, string) (bool, error)
	FindByDocNo(context.Context, string) (*model.BojunRetailOrder, error)
	CreateIfNotExists(context.Context, *model.BojunRetailOrder) (bool, error)
	SupplementOracleFieldsIfMissing(context.Context, uint, *model.BojunRetailOrder) (bool, error)
	UpdateSyncStatus(context.Context, uint, int) error
	UpdateOracleBusinessFields(context.Context, *model.BojunRetailOrder) error
}

type bojunOracleExistingOrderMode uint8

const (
	bojunOracleExistingOrderSync bojunOracleExistingOrderMode = iota
	bojunOracleExistingOrderBackfillDetails
)

type BojunOracleOrderService struct {
	datasourceStore         bojunOracleDatasourceStore
	decryptor               bojunOracleCredentialDecryptor
	openOracle              bojunOracleConnectionOpener
	detailBackfillConfig    appConfig.ReportInputOracleConfig
	detailBackfillConfigErr error
	stateStore              bojunOracleSyncStateStore
	rawDataDAO              rawDataCreator
	retailOrderDAO          bojunOracleRetailOrderStore
	pushService             bojunOrderPusher
	skipPolicy              orderPushSkipConfigGetter
	now                     func() time.Time
	newLeaseToken           func() string
	batchSize               int
	maxPages                int
	leaseTTL                time.Duration
}

func NewBojunOracleOrderService() *BojunOracleOrderService {
	defaultOracle, defaultOracleErr := appConfig.LoadReportInputQueryConfig()
	service := &BojunOracleOrderService{
		datasourceStore: reportrepo.New(),
		decryptor:       reportsecret.EnvironmentKeyring{},
		openOracle: func(ctx context.Context, oracleConfig reportoracle.Config) (bojunOracleConnection, error) {
			return reportoracle.Open(ctx, oracleConfig)
		},
		stateStore:     data_dao.NewBojunOracleSyncStateDAO(),
		rawDataDAO:     data_dao.NewRawDataDAO(),
		retailOrderDAO: data_dao.NewBojunRetailOrderDAO(),
		pushService:    NewBojunOrderPushService(),
		skipPolicy:     config_svc.NewOrderPushSkipConfigService(),
		now:            time.Now,
		newLeaseToken:  uuid.NewString,
		batchSize: positiveBojunInt(
			bojunEnvInt("BOJUN_ORACLE_BATCH_SIZE", config.GetInt("Bojun.OracleBatchSize", bojunOracleDefaultBatchSize)),
			bojunOracleDefaultBatchSize,
		),
		maxPages: positiveBojunInt(
			bojunEnvInt("BOJUN_ORACLE_MAX_PAGES", config.GetInt("Bojun.OracleMaxPages", bojunOracleDefaultMaxPages)),
			bojunOracleDefaultMaxPages,
		),
		leaseTTL: time.Duration(positiveBojunInt(
			bojunEnvInt("BOJUN_ORACLE_LEASE_SECONDS", config.GetInt("Bojun.OracleLeaseSeconds", int(bojunOracleDefaultLeaseTTL/time.Second))),
			int(bojunOracleDefaultLeaseTTL/time.Second),
		)) * time.Second,
	}
	if defaultOracleErr != nil {
		service.detailBackfillConfigErr = fmt.Errorf("load default Oracle configuration: %w", defaultOracleErr)
	} else if !validDefaultOracleConfig(defaultOracle.Oracle) {
		service.detailBackfillConfigErr = fmt.Errorf("default Oracle configuration is incomplete")
	} else {
		service.detailBackfillConfig = defaultOracle.Oracle
	}
	return service
}

func (service *BojunOracleOrderService) SyncIncremental(ctx context.Context) (result *BojunOrderSyncResult, resultErr error) {
	result = &BojunOrderSyncResult{PageSize: service.batchSize, MaxPages: service.maxPages}
	connection, datasource, err := service.open(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := connection.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close bojun Oracle connection: %w", closeErr))
		}
	}()

	state, err := service.stateStore.Get(ctx, bojunOracleDatasourceCode)
	if errors.Is(err, data_dao.ErrBojunOracleSyncStateNotInitialized) {
		queryCtx, cancel := reportOracleQueryContext(ctx, *datasource)
		maxRetailID, maxErr := connection.MaxBojunRetailID(queryCtx)
		cancel()
		if maxErr != nil {
			return result, maxErr
		}
		state, result.WatermarkInitialized, err = service.stateStore.Initialize(
			ctx, bojunOracleDatasourceCode, maxRetailID, service.now(),
		)
		if err != nil {
			return result, err
		}
		result.WatermarkBefore = state.LastRetailID
		result.WatermarkAfter = state.LastRetailID
		return result, nil
	}
	if err != nil {
		return result, err
	}

	result.WatermarkBefore = state.LastRetailID
	result.WatermarkAfter = state.LastRetailID
	token := service.newLeaseToken()
	state, acquired, err := service.stateStore.AcquireLease(
		ctx, bojunOracleDatasourceCode, token, service.now(), service.leaseTTL,
	)
	if err != nil {
		return result, err
	}
	result.LeaseAcquired = acquired
	if !acquired {
		return result, nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bojunOrderRunFinishTimeout)
		defer cancel()
		if releaseErr := service.stateStore.ReleaseLease(releaseCtx, bojunOracleDatasourceCode, token, service.now()); releaseErr != nil &&
			!errors.Is(releaseErr, data_dao.ErrBojunOracleSyncLeaseLost) {
			resultErr = errors.Join(resultErr, releaseErr)
		}
	}()

	pushSkipConfig, err := service.bojunPushSkipConfig(ctx, true)
	if err != nil {
		return result, err
	}
	watermark := state.LastRetailID
	for page := 1; page <= service.maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		queryCtx, cancel := reportOracleQueryContext(ctx, *datasource)
		rows, queryErr := connection.QueryBojunRetailAfterID(queryCtx, watermark, service.batchSize)
		cancel()
		if queryErr != nil {
			return result, queryErr
		}
		result.FetchPages++
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if err := service.processRow(
				ctx, connection, row, page, true, bojunOracleExistingOrderSync, result, pushSkipConfig,
			); err != nil {
				return result, err
			}
			if err := service.stateStore.RenewLease(
				ctx, bojunOracleDatasourceCode, token, service.now(), service.leaseTTL,
			); err != nil {
				return result, err
			}
		}
		nextWatermark := rows[len(rows)-1].RetailID
		if err := service.stateStore.Advance(
			ctx, bojunOracleDatasourceCode, token, watermark, nextWatermark, service.now(), service.leaseTTL,
		); err != nil {
			return result, err
		}
		watermark = nextWatermark
		result.WatermarkAfter = watermark
		if len(rows) < service.batchSize {
			break
		}
	}
	return result, nil
}

func (service *BojunOracleOrderService) openDetailBackfill(
	ctx context.Context,
) (bojunOracleConnection, time.Duration, error) {
	if service == nil || ctx == nil || service.openOracle == nil || service.retailOrderDAO == nil ||
		service.batchSize <= 0 || service.maxPages <= 0 {
		return nil, 0, fmt.Errorf("bojun Oracle detail backfill dependencies are unavailable")
	}
	if service.detailBackfillConfigErr != nil {
		return nil, 0, service.detailBackfillConfigErr
	}
	if !validDefaultOracleConfig(service.detailBackfillConfig) {
		return nil, 0, fmt.Errorf("default Oracle configuration is incomplete")
	}
	connection, err := service.openOracle(ctx, defaultOracleAdapterConfig(service.detailBackfillConfig))
	if err != nil {
		return nil, 0, fmt.Errorf("open default Oracle for bojun detail backfill: %w", err)
	}
	queryTimeout := service.detailBackfillConfig.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = 30 * time.Second
	}
	return connection, queryTimeout, nil
}

func (service *BojunOracleOrderService) PreviewByStatusTime(ctx context.Context, startTime, endTime string) (*BojunOrderSyncResult, error) {
	return service.runByStatusTime(ctx, startTime, endTime, false)
}

func (service *BojunOracleOrderService) SyncByStatusTime(ctx context.Context, startTime, endTime string) (*BojunOrderSyncResult, error) {
	return service.runByStatusTime(ctx, startTime, endTime, true)
}

func (service *BojunOracleOrderService) runByStatusTime(
	ctx context.Context,
	startTime string,
	endTime string,
	confirmWrite bool,
) (result *BojunOrderSyncResult, resultErr error) {
	normalizedStart, normalizedEnd, err := normalizeBojunOrderTimeRange(startTime, endTime)
	result = &BojunOrderSyncResult{
		StartTime: normalizedStart, EndTime: normalizedEnd,
		PageSize: service.batchSize, MaxPages: service.maxPages,
	}
	if err != nil {
		return result, err
	}
	start, _ := parseBojunOrderTime(normalizedStart)
	end, _ := parseBojunOrderTime(normalizedEnd)
	connection, queryTimeout, err := service.openDetailBackfill(ctx)
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := connection.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close bojun Oracle connection: %w", closeErr))
		}
	}()

	pushSkipConfig, err := service.bojunPushSkipConfig(ctx, confirmWrite)
	if err != nil {
		return result, err
	}
	var afterID uint64
	for page := 1; page <= service.maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
		rows, queryErr := connection.QueryBojunRetailByStatusTime(
			queryCtx, start, end, afterID, service.batchSize,
		)
		cancel()
		if queryErr != nil {
			return result, queryErr
		}
		result.FetchPages++
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if err := service.processRow(
				ctx, connection, row, page, confirmWrite, bojunOracleExistingOrderBackfillDetails,
				result, pushSkipConfig,
			); err != nil {
				return result, err
			}
		}
		afterID = rows[len(rows)-1].RetailID
		if len(rows) < service.batchSize {
			break
		}
	}
	return result, nil
}

func (service *BojunOracleOrderService) open(ctx context.Context) (bojunOracleConnection, *model.ReportDatasource, error) {
	if service == nil || service.datasourceStore == nil || service.decryptor == nil || service.openOracle == nil ||
		service.stateStore == nil || service.rawDataDAO == nil || service.retailOrderDAO == nil || service.now == nil ||
		service.newLeaseToken == nil || service.batchSize <= 0 || service.maxPages <= 0 || service.leaseTTL <= 0 {
		return nil, nil, fmt.Errorf("bojun Oracle order service dependencies are unavailable")
	}
	datasource, err := service.datasourceStore.FindEnabledReportDatasourceByCode(ctx, bojunOracleDatasourceCode)
	if err != nil {
		return nil, nil, fmt.Errorf("load bojun Oracle datasource %s: %w", bojunOracleDatasourceCode, err)
	}
	password, err := service.decryptor.Decrypt(datasource.CredentialKeyVersion, datasource.PasswordCiphertext)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt bojun Oracle datasource credential: %w", err)
	}
	connection, err := service.openOracle(ctx, oracleConfigFromDatasource(*datasource, password))
	if err != nil {
		return nil, nil, fmt.Errorf("open bojun Oracle datasource: %w", err)
	}
	return connection, datasource, nil
}

func (service *BojunOracleOrderService) processRow(
	ctx context.Context,
	connection bojunOracleConnection,
	row reportoracle.BojunRetailRow,
	page int,
	confirmWrite bool,
	existingMode bojunOracleExistingOrderMode,
	result *BojunOrderSyncResult,
	pushSkipConfig OrderPushSkipConfig,
) error {
	result.TotalCount++
	order, err := buildBojunRetailOrderFromOracle(row)
	if err != nil {
		result.FailedCount++
		return fmt.Errorf("build bojun Oracle retail order %d: %w", row.RetailID, err)
	}
	sample := bojunOraclePreviewItem(order)
	exists, err := service.retailOrderDAO.ExistsByDocNo(ctx, order.DocNo)
	if err != nil {
		result.FailedCount++
		return fmt.Errorf("check bojun Oracle order %s existence: %w", order.DocNo, err)
	}
	if exists {
		result.ExistingCount++
		if existingMode == bojunOracleExistingOrderBackfillDetails {
			return service.processExistingDetailBackfill(ctx, order, confirmWrite, result, sample)
		}
		existing, findErr := service.retailOrderDAO.FindByDocNo(ctx, order.DocNo)
		if findErr != nil {
			result.FailedCount++
			return fmt.Errorf("load existing bojun Oracle order %s: %w", order.DocNo, findErr)
		}
		if !confirmWrite {
			result.PreviewCount++
			sample.Status = "exists"
			if existing.OracleRetailID == nil {
				sample.Reason = "docno 已存在，将补充 Oracle 字段"
			} else {
				sample.Reason = "docno 已存在，不覆盖"
			}
			addBojunOrderSample(result, sample)
			return nil
		}
		return service.processExistingRow(ctx, connection, row, order, existing, result, pushSkipConfig, sample)
	}
	result.WritableCount++
	if !confirmWrite {
		result.PreviewCount++
		sample.Status = "pending"
		sample.Reason = "可写入，预览未落库"
		addBojunOrderSample(result, sample)
		return nil
	}

	rawData, err := buildBojunOracleRawData(row, page)
	if err != nil {
		result.FailedCount++
		return fmt.Errorf("build bojun Oracle raw data %d: %w", row.RetailID, err)
	}
	rawDataID, err := service.rawDataDAO.Create(ctx, rawData)
	if err != nil {
		result.FailedCount++
		return fmt.Errorf("create bojun Oracle order %s raw data: %w", order.DocNo, err)
	}
	result.SavedCount++
	order.RawDataID = rawDataID
	created, err := service.retailOrderDAO.CreateIfNotExists(ctx, order)
	if err != nil {
		result.FailedCount++
		return fmt.Errorf("create bojun Oracle retail order %s: %w", order.DocNo, err)
	}
	if !created {
		result.ExistingCount++
		result.SkippedCount++
		return nil
	}
	result.RetailCount++
	sample.Status = "created"
	sample.Reason = "已写入"
	addBojunOrderSample(result, sample)

	if order.IsToShop != "Y" || row.PushStatus == 1 || service.pushService == nil {
		return nil
	}
	return service.pushAndWriteBack(ctx, connection, row, order, result, pushSkipConfig)
}

func (service *BojunOracleOrderService) processExistingDetailBackfill(
	ctx context.Context,
	order *model.BojunRetailOrder,
	confirmWrite bool,
	result *BojunOrderSyncResult,
	sample BojunOrderPreviewItem,
) error {
	result.WritableCount++
	existing, err := service.retailOrderDAO.FindByDocNo(ctx, order.DocNo)
	if err != nil {
		result.FailedCount++
		return fmt.Errorf("load existing bojun Oracle order %s: %w", order.DocNo, err)
	}
	preserveMissingBojunOracleFields(order, existing)
	if !confirmWrite {
		result.PreviewCount++
		sample.Status = "exists"
		sample.Reason = "将更新 Oracle 订单业务字段"
		addBojunOrderSample(result, sample)
		return nil
	}
	if err := service.retailOrderDAO.UpdateOracleBusinessFields(ctx, order); err != nil {
		result.FailedCount++
		sample.Status = "failed"
		sample.Reason = "更新 Oracle 订单业务字段失败"
		addBojunOrderFailedSample(result, sample)
		return fmt.Errorf("update bojun Oracle business fields %s: %w", order.DocNo, err)
	}
	result.UpdatedCount++
	sample.Status = "updated"
	sample.Reason = "已更新 Oracle 订单业务字段"
	addBojunOrderSample(result, sample)
	return nil
}

func (service *BojunOracleOrderService) processExistingRow(
	ctx context.Context,
	connection bojunOracleConnection,
	row reportoracle.BojunRetailRow,
	incoming *model.BojunRetailOrder,
	existing *model.BojunRetailOrder,
	result *BojunOrderSyncResult,
	pushSkipConfig OrderPushSkipConfig,
	sample BojunOrderPreviewItem,
) error {
	if existing == nil || incoming == nil {
		result.SkippedCount++
		return nil
	}
	businessUpdated := false
	preserveMissingBojunOracleFields(incoming, existing)
	if existing.OracleRetailID == nil {
		updated, err := service.retailOrderDAO.SupplementOracleFieldsIfMissing(ctx, existing.ID, incoming)
		if err != nil {
			result.FailedCount++
			return fmt.Errorf("supplement existing bojun Oracle order %s: %w", incoming.DocNo, err)
		}
		if !updated {
			reloaded, reloadErr := service.retailOrderDAO.FindByDocNo(ctx, incoming.DocNo)
			if reloadErr != nil {
				result.FailedCount++
				return fmt.Errorf("reload supplemented bojun Oracle order %s: %w", incoming.DocNo, reloadErr)
			}
			if reloaded == nil || reloaded.OracleRetailID == nil {
				result.FailedCount++
				return fmt.Errorf("supplement existing bojun Oracle order %s: Oracle retail ID remains empty", incoming.DocNo)
			}
			existing = reloaded
		} else {
			applyBojunOracleSupplement(existing, incoming)
			businessUpdated = true
		}
	}
	if existing.OracleRetailID == nil || *existing.OracleRetailID != row.RetailID {
		result.SkippedCount++
		sample.Status = "exists"
		sample.Reason = "docno 已存在，不覆盖"
		addBojunOrderSample(result, sample)
		return nil
	}
	if !sameBojunOracleBusinessFields(existing, incoming) {
		if err := service.retailOrderDAO.UpdateOracleBusinessFields(ctx, incoming); err != nil {
			result.FailedCount++
			return fmt.Errorf("reconcile existing bojun Oracle order %s: %w", incoming.DocNo, err)
		}
		applyBojunOracleBusinessFields(existing, incoming)
		businessUpdated = true
	}
	if businessUpdated {
		result.UpdatedCount++
		sample.Status = "updated"
		sample.Reason = "已同步 Oracle 订单业务字段"
		addBojunOrderSample(result, sample)
	}
	if strings.ToUpper(strings.TrimSpace(existing.IsToShop)) != "Y" || row.PushStatus == 1 {
		if existing.Synced != 1 {
			if err := service.retailOrderDAO.UpdateSyncStatus(ctx, existing.ID, 1); err != nil {
				return err
			}
		}
		result.SkippedCount++
		return nil
	}
	if existing.Synced == 1 || existing.Synced == 3 {
		return service.writeBackSuccessfulPush(ctx, connection, row.RetailID, existing.ID)
	}
	if service.pushService == nil {
		result.SkippedCount++
		return nil
	}
	return service.pushAndWriteBack(ctx, connection, row, existing, result, pushSkipConfig)
}

func preserveMissingBojunOracleFields(incoming, existing *model.BojunRetailOrder) {
	if incoming == nil || existing == nil {
		return
	}
	if incoming.StoreCode == "" {
		incoming.StoreCode = existing.StoreCode
	}
	if incoming.StoreName == "" {
		incoming.StoreName = existing.StoreName
	}
	if incoming.RetailBillType == "" {
		incoming.RetailBillType = existing.RetailBillType
		incoming.RetailSaleType = existing.RetailSaleType
		incoming.OrderTypeCode = existing.OrderTypeCode
		incoming.OrderTypeName = existing.OrderTypeName
	}
	if incoming.OrderPhone == "" {
		incoming.OrderPhone = existing.OrderPhone
	}
	if incoming.IsToShop == "" {
		incoming.IsToShop = existing.IsToShop
	}
}

func sameBojunOracleBusinessFields(left, right *model.BojunRetailOrder) bool {
	if left == nil || right == nil {
		return false
	}
	return left.BillDate == right.BillDate && sameBojunTime(left.CompletedAt, right.CompletedAt) &&
		left.RetailBillType == right.RetailBillType && left.StoreCode == right.StoreCode &&
		left.StoreName == right.StoreName && left.RetailSaleType == right.RetailSaleType &&
		left.OrderTypeCode == right.OrderTypeCode && left.OrderTypeName == right.OrderTypeName &&
		left.OrderPhone == right.OrderPhone && left.PaidAmount == right.PaidAmount &&
		left.PushAmount == right.PushAmount && left.IsToShop == right.IsToShop &&
		left.TotalLines == right.TotalLines && left.TotalQty == right.TotalQty &&
		left.TotalAmtList == right.TotalAmtList && left.TotalAmtActual == right.TotalAmtActual &&
		left.TotalAmtAcc == right.TotalAmtAcc && left.TotalAmtAcc1 == right.TotalAmtAcc1 &&
		left.ItemsJSON == right.ItemsJSON && left.PayItemsJSON == right.PayItemsJSON
}

func applyBojunOracleBusinessFields(existing, incoming *model.BojunRetailOrder) {
	existing.BillDate = incoming.BillDate
	existing.CompletedAt = incoming.CompletedAt
	existing.RetailBillType = incoming.RetailBillType
	existing.StoreCode = incoming.StoreCode
	existing.StoreName = incoming.StoreName
	existing.RetailSaleType = incoming.RetailSaleType
	existing.OrderTypeCode = incoming.OrderTypeCode
	existing.OrderTypeName = incoming.OrderTypeName
	existing.OrderPhone = incoming.OrderPhone
	existing.PaidAmount = incoming.PaidAmount
	existing.PushAmount = incoming.PushAmount
	existing.IsToShop = incoming.IsToShop
	existing.TotalLines = incoming.TotalLines
	existing.TotalQty = incoming.TotalQty
	existing.TotalAmtList = incoming.TotalAmtList
	existing.TotalAmtActual = incoming.TotalAmtActual
	existing.TotalAmtAcc = incoming.TotalAmtAcc
	existing.TotalAmtAcc1 = incoming.TotalAmtAcc1
	existing.ItemsJSON = incoming.ItemsJSON
	existing.PayItemsJSON = incoming.PayItemsJSON
}

func applyBojunOracleSupplement(existing, incoming *model.BojunRetailOrder) {
	existing.OracleRetailID = incoming.OracleRetailID
	existing.RetailBillType = incoming.RetailBillType
	existing.StoreName = incoming.StoreName
	existing.OrderPhone = incoming.OrderPhone
	existing.PaidAmount = incoming.PaidAmount
	existing.PushAmount = incoming.PushAmount
	existing.IsToShop = incoming.IsToShop
	existing.TotalAmtList = incoming.TotalAmtList
	existing.TotalAmtActual = incoming.TotalAmtActual
	existing.TotalAmtAcc = incoming.TotalAmtAcc
	existing.TotalAmtAcc1 = incoming.TotalAmtAcc1
}

func (service *BojunOracleOrderService) pushAndWriteBack(
	ctx context.Context,
	connection bojunOracleConnection,
	row reportoracle.BojunRetailRow,
	order *model.BojunRetailOrder,
	result *BojunOrderSyncResult,
	pushSkipConfig OrderPushSkipConfig,
) error {
	policy := OrderPushSkipPolicy{}
	if target, ok := bojunTargetForStore(order.StoreCode); ok {
		policy = pushSkipConfig.PolicyForTarget(target.Code)
	}
	pushResult := service.pushService.PushNewOrderWithPolicy(ctx, order, result.nextPushPosition(), policy)
	if pushResult.Skipped && !pushResult.Success {
		result.SkippedCount++
		return nil
	}
	pushDate := bojunOraclePushDate(service.now())
	if !pushResult.Success {
		result.FailedCount++
		pushErr := pushResult.Error
		if pushErr == nil {
			pushErr = errors.New("mall push returned an unsuccessful result")
		}
		statusErr := service.retailOrderDAO.UpdateSyncStatus(ctx, order.ID, 2)
		writeBackErr := connection.UpdateBojunRetailPushStatus(ctx, row.RetailID, false, pushDate)
		return errors.Join(
			fmt.Errorf("push bojun Oracle retail order %s: %w", order.DocNo, pushErr),
			statusErr,
			writeBackErr,
		)
	}
	if err := connection.UpdateBojunRetailPushStatus(ctx, row.RetailID, true, pushDate); err != nil {
		result.FailedCount++
		statusErr := service.retailOrderDAO.UpdateSyncStatus(ctx, order.ID, 3)
		return errors.Join(fmt.Errorf("write back successful bojun Oracle push %s: %w", order.DocNo, err), statusErr)
	}
	return service.retailOrderDAO.UpdateSyncStatus(ctx, order.ID, 1)
}

func (service *BojunOracleOrderService) writeBackSuccessfulPush(
	ctx context.Context,
	connection bojunOracleConnection,
	retailID uint64,
	localOrderID uint,
) error {
	if err := connection.UpdateBojunRetailPushStatus(ctx, retailID, true, bojunOraclePushDate(service.now())); err != nil {
		statusErr := service.retailOrderDAO.UpdateSyncStatus(ctx, localOrderID, 3)
		return errors.Join(fmt.Errorf("retry bojun Oracle successful push write-back: %w", err), statusErr)
	}
	return service.retailOrderDAO.UpdateSyncStatus(ctx, localOrderID, 1)
}

func bojunOraclePushDate(value time.Time) int {
	return value.Year()*10000 + int(value.Month())*100 + value.Day()
}

func (service *BojunOracleOrderService) bojunPushSkipConfig(ctx context.Context, confirmWrite bool) (OrderPushSkipConfig, error) {
	if !confirmWrite || service.skipPolicy == nil {
		return OrderPushSkipConfig{}, nil
	}
	return service.skipPolicy.Get(ctx)
}

func buildBojunRetailOrderFromOracle(row reportoracle.BojunRetailRow) (*model.BojunRetailOrder, error) {
	if row.RetailID == 0 || strings.TrimSpace(row.DocNo) == "" || row.StatusTime.IsZero() {
		return nil, fmt.Errorf("M_RETAIL_ID, DOCNO and STATUSTIME are required")
	}
	retailBillType := strings.TrimSpace(row.RetailSaleType)
	retailSaleType := strings.ToUpper(retailBillType)
	if retailSaleType == "" {
		retailSaleType = "CMR"
	}
	orderTypeCode, orderTypeName := bojunOrderType(retailSaleType)
	quantity := 1
	if orderTypeCode == "RET" {
		quantity = -1
	}
	itemsJSON := strings.TrimSpace(row.ItemsJSON)
	if itemsJSON == "" {
		itemsJSON = "[]"
	}
	var items []interface{}
	if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
		return nil, fmt.Errorf("JSON_ITEM must be a JSON array: %w", err)
	}
	payItemsJSON := strings.TrimSpace(row.PayItemsJSON)
	if payItemsJSON == "" {
		payItemsJSON = "[]"
	}
	var payItems []interface{}
	if err := json.Unmarshal([]byte(payItemsJSON), &payItems); err != nil {
		return nil, fmt.Errorf("PAY_ITEMS must be a JSON array: %w", err)
	}
	rawContent, err := json.Marshal(bojunOracleRawPayload(row))
	if err != nil {
		return nil, err
	}
	retailID := row.RetailID
	completedAt := row.StatusTime
	return &model.BojunRetailOrder{
		OracleRetailID: &retailID,
		DocNo:          row.DocNo, BillDate: statusTimeBillDate(row.StatusTime), CompletedAt: &completedAt,
		RetailBillType: retailBillType, StoreCode: strings.TrimSpace(row.StoreCode),
		StoreName: strings.TrimSpace(row.StoreName), RetailSaleType: retailSaleType,
		OrderTypeCode: orderTypeCode, OrderTypeName: orderTypeName,
		OrderPhone: strings.TrimSpace(row.OrderPhone), PaidAmount: row.PaidAmount, PushAmount: row.PushAmount,
		IsToShop:   strings.ToUpper(strings.TrimSpace(row.IsToShop)),
		TotalLines: len(items), TotalQty: quantity,
		TotalAmtList: row.PaidAmount, TotalAmtActual: row.PaidAmount,
		TotalAmtAcc: row.PaidAmount, TotalAmtAcc1: row.PaidAmount,
		ItemsJSON: itemsJSON, PayItemsJSON: payItemsJSON, RawContentJSON: string(rawContent),
		Synced: bojunOracleInitialSyncStatus(row),
	}, nil
}

func bojunOracleInitialSyncStatus(row reportoracle.BojunRetailRow) int {
	if strings.ToUpper(strings.TrimSpace(row.IsToShop)) != "Y" || row.PushStatus == 1 {
		return 1
	}
	return 0
}

func statusTimeBillDate(value time.Time) int {
	return value.Year()*10000 + int(value.Month())*100 + value.Day()
}

type bojunOracleRawRecord struct {
	RetailID       uint64          `json:"M_RETAIL_ID"`
	StoreCode      string          `json:"STORE_CODE"`
	StoreName      string          `json:"STORE_NAME"`
	DocNo          string          `json:"DOCNO"`
	RetailSaleType string          `json:"RETAILSALETYPE"`
	StatusTime     time.Time       `json:"STATUSTIME"`
	OrderPhone     string          `json:"DM_VP_C_VIP_MOBILE"`
	PaidAmount     float64         `json:"TOT_AMT_SF"`
	PushAmount     float64         `json:"TOT_AMT_TS"`
	IsToShop       string          `json:"IS_TOSHOP"`
	PushStatus     int             `json:"STATUS"`
	ItemsJSON      json.RawMessage `json:"JSON_ITEM"`
	PayItemsJSON   json.RawMessage `json:"PAY_ITEMS"`
}

func bojunOracleRawPayload(row reportoracle.BojunRetailRow) bojunOracleRawRecord {
	itemsJSON := strings.TrimSpace(row.ItemsJSON)
	if itemsJSON == "" {
		itemsJSON = "[]"
	}
	payItemsJSON := strings.TrimSpace(row.PayItemsJSON)
	if payItemsJSON == "" {
		payItemsJSON = "[]"
	}
	return bojunOracleRawRecord{
		RetailID: row.RetailID, StoreCode: row.StoreCode, StoreName: row.StoreName, DocNo: row.DocNo,
		RetailSaleType: row.RetailSaleType, StatusTime: row.StatusTime, OrderPhone: row.OrderPhone,
		PaidAmount: row.PaidAmount, PushAmount: row.PushAmount, IsToShop: row.IsToShop,
		PushStatus: row.PushStatus, ItemsJSON: json.RawMessage(itemsJSON),
		PayItemsJSON: json.RawMessage(payItemsJSON),
	}
}

func buildBojunOracleRawData(row reportoracle.BojunRetailRow, page int) (*model.RawData, error) {
	rawContent, err := json.Marshal(bojunOracleRawPayload(row))
	if err != nil {
		return nil, err
	}
	ingestedAt := time.Now()
	metadata, err := json.Marshal(map[string]interface{}{
		"source": bojunOracleOrderSource, "datasource_code": bojunOracleDatasourceCode,
		"table": reportoracle.BojunRetailTable, "m_retail_id": row.RetailID,
		"page": page, "ingested_at": ingestedAt.Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	return &model.RawData{
		DataSourceID: 0, ExternalID: row.DocNo, DataType: "order",
		RawContent: string(rawContent), Metadata: string(metadata), Status: "pending",
		Remark: bojunOracleOrderSource, Source: bojunOracleOrderSource, IngestedAt: &ingestedAt,
	}, nil
}

func bojunOraclePreviewItem(order *model.BojunRetailOrder) BojunOrderPreviewItem {
	return BojunOrderPreviewItem{
		DocNo: order.DocNo, StoreCode: order.StoreCode,
		OrderTypeCode: order.OrderTypeCode, OrderTypeName: order.OrderTypeName,
		BillDate: order.BillDate, TotalQty: order.TotalQty, TotalAmtActual: order.TotalAmtActual,
	}
}
