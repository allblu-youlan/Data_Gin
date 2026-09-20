package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"
	projectredis "gin-biz-web-api/pkg/redis"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrMallWeatherFeishuInvalid             = errors.New("mall weather feishu push: invalid input")
	ErrMallWeatherFeishuDestinationNotFound = errors.New("mall weather feishu push: destination not found")
	ErrMallWeatherFeishuDisabled            = errors.New("mall weather feishu push: disabled")
)

type mallWeatherFeishuDestinationReader interface {
	FindByID(context.Context, uint) (*model.DestinationDefinition, error)
}

type mallWeatherFeishuRunReader interface {
	FindByPipelineRunID(context.Context, uint) (*data_dao.MallWeatherFeishuRunRecord, error)
}

type mallWeatherFeishuSheetsReader interface {
	Inspect(context.Context, string, []string) (*feishu.SpreadsheetMetadata, error)
	ReadRange(context.Context, string, feishu.SheetRange) (*feishu.SheetValues, error)
}

type mallWeatherFeishuSheetsFactory func() (mallWeatherFeishuSheetsReader, error)

type mallWeatherFeishuPushDependencies struct {
	destinations  mallWeatherFeishuDestinationReader
	profiles      mallWeatherExportProfileReader
	permissions   mallPermissionChecker
	estimator     mallWeatherExportEstimator
	limits        mallWeatherExportLimitReader
	store         mallWeatherFeishuPushStore
	runs          mallWeatherFeishuRunReader
	resources     mallWeatherFeishuResourceResolver
	newSheets     mallWeatherFeishuSheetsFactory
	feishuEnabled func() bool
	now           func() time.Time
}

type MallWeatherFeishuPushService struct {
	destinations  mallWeatherFeishuDestinationReader
	profiles      mallWeatherExportProfileReader
	permissions   mallPermissionChecker
	estimator     mallWeatherExportEstimator
	limits        mallWeatherExportLimitReader
	store         mallWeatherFeishuPushStore
	runs          mallWeatherFeishuRunReader
	resources     mallWeatherFeishuResourceResolver
	newSheets     mallWeatherFeishuSheetsFactory
	feishuEnabled func() bool
	now           func() time.Time
	mallScope     *auth_svc.MallScopeService
}

func NewMallWeatherFeishuPushService() *MallWeatherFeishuPushService {
	var once sync.Once
	var sheets mallWeatherFeishuSheetsReader
	var sheetsErr error
	newSheets := func() (mallWeatherFeishuSheetsReader, error) {
		once.Do(func() {
			sheets, sheetsErr = newRuntimeMallWeatherFeishuSheets()
		})
		return sheets, sheetsErr
	}
	service, err := newMallWeatherFeishuPushService(mallWeatherFeishuPushDependencies{
		destinations: data_dao.NewDestinationDefinitionDAO(),
		profiles:     data_dao.NewMallWeatherExportProfileDAO(database.DB),
		permissions:  newAccountPermissionChecker(database.DB),
		estimator:    data_dao.NewMallWeatherExportJobDAO(database.DB),
		limits:       data_dao.NewRuntimeConfigDAO(),
		store:        gormMallWeatherFeishuPushStore{db: database.DB},
		runs:         data_dao.NewMallWeatherFeishuRunDAO(database.DB),
		resources:    global.Credentials,
		newSheets:    newSheets,
		feishuEnabled: func() bool {
			return config.GetBool("cfg.mall_weather.feishu_enabled")
		},
		now: time.Now,
	})
	if err != nil {
		panic(err)
	}
	service.mallScope = auth_svc.NewMallScopeService(database.DB)
	return service
}

func newMallWeatherFeishuPushService(
	dependencies mallWeatherFeishuPushDependencies,
) (*MallWeatherFeishuPushService, error) {
	if dependencies.destinations == nil || dependencies.profiles == nil || dependencies.permissions == nil ||
		dependencies.estimator == nil || dependencies.limits == nil || dependencies.store == nil || dependencies.runs == nil ||
		dependencies.resources == nil ||
		dependencies.newSheets == nil || dependencies.feishuEnabled == nil || dependencies.now == nil {
		return nil, errors.New("mall weather feishu push: invalid service configuration")
	}
	return &MallWeatherFeishuPushService{
		destinations:  dependencies.destinations,
		profiles:      dependencies.profiles,
		permissions:   dependencies.permissions,
		estimator:     dependencies.estimator,
		limits:        dependencies.limits,
		store:         dependencies.store,
		runs:          dependencies.runs,
		resources:     dependencies.resources,
		newSheets:     dependencies.newSheets,
		feishuEnabled: dependencies.feishuEnabled,
		now:           dependencies.now,
	}, nil
}

func (service *MallWeatherFeishuPushService) DryRun(
	ctx context.Context,
	actorUserID uint,
	request requestbody.MallWeatherFeishuPushRequest,
) (*MallWeatherFeishuDryRunResult, error) {
	prepared, err := service.prepare(ctx, actorUserID, request)
	if err != nil {
		return nil, err
	}
	sheets, err := service.newSheets()
	if err != nil || sheets == nil {
		return nil, errors.New("mall weather feishu push: sheets client is unavailable")
	}
	return service.inspectAndPlan(ctx, prepared.destination, prepared.profileDTO, prepared.estimatedRows, sheets)
}

type mallWeatherFeishuPreparedPush struct {
	destinationRow *model.DestinationDefinition
	destination    *MallWeatherFeishuResolvedDestination
	profileRow     *model.MallWeatherExportProfile
	profileDTO     MallWeatherExportProfileDTO
	filters        requestbody.MallWeatherExportFilters
	estimatedRows  map[string]int64
}

func (service *MallWeatherFeishuPushService) Create(
	ctx context.Context,
	actorUserID uint,
	idempotencyKey string,
	request requestbody.MallWeatherFeishuPushRequest,
) (*MallWeatherFeishuPushCreateResult, bool, error) {
	if !validIdempotencyKey(idempotencyKey) {
		return nil, false, fmt.Errorf("%w: idempotency key is required", ErrMallWeatherFeishuInvalid)
	}
	prepared, err := service.prepare(ctx, actorUserID, request)
	if err != nil {
		return nil, false, err
	}
	if err := validateMallWeatherFeishuPreparedPush(prepared); err != nil {
		return nil, false, err
	}
	profileSnapshot, filtersJSON, destinationSnapshot, err := encodeMallWeatherFeishuPushSnapshots(prepared)
	if err != nil {
		return nil, false, err
	}
	requestHash, err := hashJSON(mallWeatherFeishuPushRequestForHash(
		request.DestinationID,
		request.ProfileID,
		request.ExpectedProfileVersion,
		prepared.filters,
	))
	if err != nil {
		return nil, false, fmt.Errorf("mall weather feishu push: hash request: %w", err)
	}
	var totalEstimatedRows int64
	for _, rows := range prepared.estimatedRows {
		totalEstimatedRows += rows
	}
	command := mallWeatherFeishuPushCreateCommand{
		ActorUserID: actorUserID, DestinationID: prepared.destinationRow.ID,
		DestinationCode: prepared.destinationRow.Code, DestinationConfigJSON: prepared.destinationRow.ConfigJSON,
		ProfileID: prepared.profileRow.ID, ProfileVersion: prepared.profileRow.Version,
		ProfileCode: prepared.profileRow.Code, ProfileName: prepared.profileRow.Name,
		ProfileJSON: prepared.profileRow.ProfileJSON, ProfileSnapshotJSON: profileSnapshot,
		FiltersJSON: filtersJSON, DestinationSnapshotJSON: destinationSnapshot,
		KeyHash: sha256Hex([]byte(idempotencyKey)), RequestHash: requestHash,
		TraceID: uuid.NewString(), EstimatedRows: totalEstimatedRows, RequestedAt: service.now().UTC(),
	}
	return service.store.Create(ctx, command)
}

func (service *MallWeatherFeishuPushService) prepare(
	ctx context.Context,
	actorUserID uint,
	request requestbody.MallWeatherFeishuPushRequest,
) (*mallWeatherFeishuPreparedPush, error) {
	if service == nil || ctx == nil || actorUserID == 0 || request.DestinationID == 0 || request.ProfileID == 0 ||
		(request.ExpectedProfileVersion != nil && *request.ExpectedProfileVersion == 0) {
		return nil, ErrMallWeatherFeishuInvalid
	}
	if !service.feishuEnabled() {
		return nil, ErrMallWeatherFeishuDisabled
	}
	allowed, err := service.permissions.HasPermission(
		ctx, actorUserID, PermissionWeatherFeishuPush, service.now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("mall weather feishu push: authorize: %w", err)
	}
	if !allowed {
		return nil, ErrMallForbidden
	}
	destination, err := service.destinations.FindByID(ctx, request.DestinationID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMallWeatherFeishuDestinationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mall weather feishu push: load destination: %w", err)
	}
	resolved, err := resolveMallWeatherFeishuDestination(destination, service.resources)
	if err != nil {
		return nil, fmt.Errorf("%w: destination configuration", ErrMallWeatherFeishuInvalid)
	}
	profile, err := service.profiles.FindByID(ctx, request.ProfileID)
	if err != nil {
		return nil, err
	}
	if profile == nil || !profile.Enabled ||
		(request.ExpectedProfileVersion != nil && *request.ExpectedProfileVersion != profile.Version) {
		return nil, ErrMallWeatherExportProfileConflict
	}
	profileDTO, err := mallWeatherExportProfileDTO(profile)
	if err != nil {
		return nil, err
	}
	filters := profileDTO.Filters
	if request.Filters != nil {
		filters, err = normalizeMallWeatherExportFilters(*request.Filters)
		if err != nil {
			return nil, ErrMallWeatherFeishuInvalid
		}
	}
	if service.mallScope != nil {
		filters.MallIDs, err = service.mallScope.ConstrainMallIDs(ctx, actorUserID, filters.MallIDs)
		if err != nil {
			if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
				return nil, ErrMallForbidden
			}
			return nil, fmt.Errorf("mall weather feishu push: constrain mall scope: %w", err)
		}
	}
	limits, err := loadMallWeatherExportLimits(ctx, service.limits)
	if err != nil {
		return nil, err
	}
	if err := validateMallWeatherExportJobRange(profileDTO.Datasets, filters, profileDTO.TimeZone, limits); err != nil {
		return nil, ErrMallWeatherFeishuInvalid
	}
	estimatedRows, err := service.estimateDryRunRows(ctx, profileDTO, filters, limits)
	if err != nil {
		return nil, err
	}
	return &mallWeatherFeishuPreparedPush{
		destinationRow: destination, destination: resolved, profileRow: profile,
		profileDTO: profileDTO, filters: filters, estimatedRows: estimatedRows,
	}, nil
}

func (service *MallWeatherFeishuPushService) estimateDryRunRows(
	ctx context.Context,
	profile MallWeatherExportProfileDTO,
	filters requestbody.MallWeatherExportFilters,
	limits mallWeatherExportLimits,
) (map[string]int64, error) {
	estimateCtx, cancel := context.WithTimeout(
		ctx,
		time.Duration(limits.EstimateTimeoutSeconds)*time.Second,
	)
	defer cancel()
	result := make(map[string]int64, len(profile.Datasets))
	var total int64
	for _, dataset := range profile.Datasets {
		single := profile
		single.Datasets = []requestbody.MallWeatherExportDataset{dataset}
		remaining := limits.MaxEstimatedRows - total
		stopAfter := remaining
		if stopAfter < 1 {
			stopAfter = 1
		}
		estimateRequest, err := mallWeatherExportEstimateRequest(single, filters, stopAfter)
		if err != nil {
			return nil, err
		}
		rows, err := service.estimator.EstimateRows(estimateCtx, estimateRequest)
		if err != nil {
			return nil, fmt.Errorf("mall weather feishu push: estimate %s: %w", dataset.Kind, err)
		}
		if rows < 0 || rows > remaining {
			return nil, ErrMallWeatherExportTooLarge
		}
		result[dataset.Kind] = rows
		total += rows
	}
	return result, nil
}

func (service *MallWeatherFeishuPushService) inspectAndPlan(
	ctx context.Context,
	destination *MallWeatherFeishuResolvedDestination,
	profile MallWeatherExportProfileDTO,
	estimatedRows map[string]int64,
	sheets mallWeatherFeishuSheetsReader,
) (*MallWeatherFeishuDryRunResult, error) {
	inspectCtx, cancel := context.WithTimeout(
		ctx,
		time.Duration(destination.Config.TimeoutSeconds)*time.Second,
	)
	defer cancel()
	requiredSheetIDs := make([]string, 0, len(destination.SheetIDs))
	for _, sheetID := range destination.SheetIDs {
		requiredSheetIDs = append(requiredSheetIDs, sheetID)
	}
	metadata, err := sheets.Inspect(inspectCtx, destination.SpreadsheetToken, requiredSheetIDs)
	if err != nil {
		return nil, err
	}
	metadataByID := make(map[string]feishu.SheetMetadata, len(metadata.Sheets))
	for _, sheet := range metadata.Sheets {
		metadataByID[sheet.SheetID] = sheet
	}
	headers := make(map[string]*feishu.SheetValues, len(profile.Datasets))
	for _, dataset := range profile.Datasets {
		columns, err := mallWeatherExportRenderColumns(dataset)
		if err != nil || len(columns) == 0 {
			return nil, ErrMallWeatherFeishuInvalid
		}
		sheetID, exists := destination.SheetIDs[dataset.Kind]
		metadata, hasMetadata := metadataByID[sheetID]
		if !exists || !hasMetadata {
			return nil, ErrMallWeatherFeishuInvalid
		}
		endColumn := int64(len(columns))
		if metadata.GridProperties.ColumnCount < endColumn {
			endColumn = metadata.GridProperties.ColumnCount
		}
		headers[dataset.Kind], err = sheets.ReadRange(inspectCtx, destination.SpreadsheetToken, feishu.SheetRange{
			SheetID: sheetID, StartRow: 1, EndRow: 1, StartColumn: 1, EndColumn: endColumn,
		})
		if err != nil {
			return nil, err
		}
	}
	return buildMallWeatherFeishuDryRunPlan(mallWeatherFeishuDryRunPlanInput{
		Destination: destination, Profile: profile, Metadata: metadata, Headers: headers, EstimatedRows: estimatedRows,
	})
}

func newRuntimeMallWeatherFeishuSheets() (mallWeatherFeishuRuntimeSheets, error) {
	redisInstance := projectredis.Instance()
	if redisInstance == nil || redisInstance.Client == nil {
		return nil, errors.New("mall weather feishu push: redis is unavailable")
	}
	tokens, err := feishu.NewTenantTokenProvider(
		global.Credentials.FeishuAppID(),
		global.Credentials.FeishuAppSecret(),
		nil,
		redisInstance.Client,
	)
	if err != nil {
		return nil, err
	}
	return feishu.NewSheetsClient(tokens, nil)
}

func newMallWeatherFeishuOutbox(
	pipelineRunID uint,
	traceID string,
	availableAt time.Time,
) (model.AsyncJobOutbox, error) {
	if pipelineRunID == 0 || len(traceID) != 36 || uuid.Validate(traceID) != nil || availableAt.IsZero() {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather feishu push: invalid outbox identity")
	}
	payload, err := json.Marshal(job.MallWeatherFeishuTaskPayload{PipelineRunID: pipelineRunID})
	if err != nil {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather feishu push: encode outbox payload: %w", err)
	}
	if _, err := job.DecodeMallWeatherFeishuTaskPayload(payload); err != nil {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather feishu push: validate outbox payload: %w", err)
	}
	return model.AsyncJobOutbox{
		TaskKey:     "mall:weather:feishu:" + traceID,
		TaskType:    job.TypeMallWeatherFeishu,
		PayloadJSON: model.JSONText(payload),
		QueueName:   job.MallDeliveryQueueName,
		AvailableAt: availableAt.UTC(),
	}, nil
}
