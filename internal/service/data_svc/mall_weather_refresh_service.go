package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

const (
	MallWeatherRefreshKindV26Full = "V26_FULL"

	mallWeatherRefreshOperationScope = "weather.refresh"
	maxMallWeatherRefreshReasonRunes = 500
)

type MallWeatherRefreshKindResult struct {
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	OutboxJobID uint   `json:"outboxJobId,omitempty"`
}

type MallWeatherRefreshResult struct {
	JobID         uint                           `json:"jobId"`
	MallID        uint                           `json:"mallId"`
	CorrelationID string                         `json:"correlationId"`
	Kinds         []MallWeatherRefreshKindResult `json:"kinds"`
	Force         bool                           `json:"force"`
	Reason        string                         `json:"reason"`
	RequestedBy   uint                           `json:"requestedBy"`
	RequestedAt   time.Time                      `json:"requestedAt"`
}

type mallWeatherRefreshCommand struct {
	MallID      uint
	ActorUserID uint
	Kinds       []string
	Force       bool
	Reason      string
	KeyHash     string
	RequestHash string
	TaskWindow  string
	RequestedAt time.Time
}

type mallWeatherRefreshStore interface {
	Create(context.Context, mallWeatherRefreshCommand) (*MallWeatherRefreshResult, bool, error)
}

type gormMallWeatherRefreshStore struct {
	db *gorm.DB
}

type MallWeatherRefreshService struct {
	malls       mallWeatherQueryMallReader
	permissions mallPermissionChecker
	store       mallWeatherRefreshStore
	now         func() time.Time
}

func NewMallWeatherRefreshService() *MallWeatherRefreshService {
	return &MallWeatherRefreshService{
		malls: data_dao.NewMallDAO(database.DB), permissions: newAccountPermissionChecker(database.DB),
		store: gormMallWeatherRefreshStore{db: database.DB}, now: time.Now,
	}
}

func newMallWeatherRefreshService(malls mallWeatherQueryMallReader, permissions mallPermissionChecker, store mallWeatherRefreshStore, now func() time.Time) (*MallWeatherRefreshService, error) {
	if malls == nil || permissions == nil || store == nil || now == nil {
		return nil, fmt.Errorf("mall weather refresh: invalid service configuration")
	}
	return &MallWeatherRefreshService{malls: malls, permissions: permissions, store: store, now: now}, nil
}

func (service *MallWeatherRefreshService) Refresh(ctx context.Context, actorUserID, mallID uint, idempotencyKey string, request requestbody.MallWeatherRefreshRequest) (*MallWeatherRefreshResult, bool, error) {
	if service == nil || ctx == nil || actorUserID == 0 || mallID == 0 {
		return nil, false, fmt.Errorf("%w: invalid refresh request", ErrMallInvalidInput)
	}
	if err := service.authorize(ctx, actorUserID, PermissionWeatherRefresh); err != nil {
		return nil, false, err
	}
	if request.Force {
		if err := service.authorize(ctx, actorUserID, PermissionWeatherConfigManage); err != nil {
			return nil, false, err
		}
	}
	normalized, err := normalizeMallWeatherRefreshRequest(request)
	if err != nil {
		return nil, false, err
	}
	if !validIdempotencyKey(idempotencyKey) {
		return nil, false, fmt.Errorf("%w: idempotency key is required", ErrMallInvalidInput)
	}
	mall, err := service.malls.FindByID(ctx, mallID)
	if err != nil {
		return nil, false, err
	}
	if err := validateMallWeatherRefreshTarget(mall); err != nil {
		return nil, false, err
	}
	requestHash, err := hashJSON(struct {
		MallID  uint                                  `json:"mallId"`
		Request requestbody.MallWeatherRefreshRequest `json:"request"`
	}{MallID: mallID, Request: normalized})
	if err != nil {
		return nil, false, fmt.Errorf("mall weather refresh: hash request: %w", err)
	}
	identityHash := sha256.Sum256([]byte(strconv.FormatUint(uint64(actorUserID), 10) + "\x1f" + strconv.FormatUint(uint64(mallID), 10) + "\x1f" + idempotencyKey))
	command := mallWeatherRefreshCommand{
		MallID: mallID, ActorUserID: actorUserID, Kinds: normalized.Kinds, Force: normalized.Force, Reason: normalized.Reason,
		KeyHash: sha256Hex([]byte(idempotencyKey)), RequestHash: requestHash,
		TaskWindow: "manual:" + hex.EncodeToString(identityHash[:])[:48], RequestedAt: service.now().UTC(),
	}
	result, replayed, err := service.store.Create(ctx, command)
	if err != nil {
		return nil, false, err
	}
	if result != nil {
		// TaskWindow is derived deterministically from the idempotency identity, so
		// this also upgrades responses replayed from snapshots written before the
		// correlationId field existed.
		result.CorrelationID = command.TaskWindow
	}
	return result, replayed, nil
}

func (service *MallWeatherRefreshService) authorize(ctx context.Context, actorUserID uint, permission string) error {
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, permission, service.now().UTC())
	if err != nil {
		return fmt.Errorf("mall weather refresh: authorize: %w", err)
	}
	if !allowed {
		return ErrMallForbidden
	}
	return nil
}

func normalizeMallWeatherRefreshRequest(request requestbody.MallWeatherRefreshRequest) (requestbody.MallWeatherRefreshRequest, error) {
	if len(request.Kinds) != 1 {
		return request, fmt.Errorf("%w: refresh kinds are required", ErrMallInvalidInput)
	}
	seen := make(map[string]struct{}, len(request.Kinds))
	kinds := make([]string, 0, len(request.Kinds))
	for _, value := range request.Kinds {
		kind := strings.ToUpper(strings.TrimSpace(value))
		if kind != MallWeatherRefreshKindV26Full {
			return request, fmt.Errorf("%w: unsupported refresh kind", ErrMallInvalidInput)
		}
		if _, exists := seen[kind]; exists {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	request.Kinds = kinds
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || utf8.RuneCountInString(request.Reason) > maxMallWeatherRefreshReasonRunes || strings.ContainsAny(request.Reason, "\x00\r\n") {
		return request, fmt.Errorf("%w: invalid refresh reason", ErrMallInvalidInput)
	}
	return request, nil
}

func validateMallWeatherRefreshTarget(mall *model.Mall) error {
	if mall == nil || mall.ID == 0 || mall.Status != "active" || !mall.WeatherEnabled || mall.WeatherProvider != weatherdomain.ProviderCaiyun ||
		mall.GeocodeStatus != "confirmed" || mall.WeatherLongitude == nil || mall.WeatherLatitude == nil ||
		*mall.WeatherLongitude < -180 || *mall.WeatherLongitude > 180 || *mall.WeatherLatitude < -90 || *mall.WeatherLatitude > 90 {
		return fmt.Errorf("%w: mall is not eligible for weather refresh", ErrMallInvalidInput)
	}
	return nil
}

func (store gormMallWeatherRefreshStore) Create(ctx context.Context, command mallWeatherRefreshCommand) (*MallWeatherRefreshResult, bool, error) {
	if store.db == nil || ctx == nil || command.MallID == 0 || command.ActorUserID == 0 || len(command.Kinds) == 0 ||
		len(command.KeyHash) != 64 || len(command.RequestHash) != 64 || command.TaskWindow == "" || command.RequestedAt.IsZero() {
		return nil, false, fmt.Errorf("mall weather refresh: invalid store command")
	}
	var result *MallWeatherRefreshResult
	var replayed bool
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		idempotencyDAO := data_dao.NewAPIIdempotencyDAO(tx)
		record := &model.APIIdempotencyRecord{
			OperationScope: mallWeatherRefreshOperationScope, ActorUserID: command.ActorUserID,
			KeyHash: command.KeyHash, RequestHash: command.RequestHash, ResourceType: "weather_refresh",
			ResponseJSON: model.JSONText(`{}`),
		}
		reserved, err := idempotencyDAO.Reserve(ctx, record)
		if err != nil {
			return err
		}
		if !reserved {
			existing, err := idempotencyDAO.FindForUpdate(ctx, mallWeatherRefreshOperationScope, command.ActorUserID, command.KeyHash)
			if err != nil {
				return err
			}
			if existing.RequestHash != command.RequestHash {
				return ErrMallIdempotencyConflict
			}
			if existing.ResourceID == 0 || existing.HTTPStatus == 0 || existing.ResponseJSON == "" || existing.ResponseJSON == model.JSONText(`{}`) {
				return ErrMallIdempotencyPending
			}
			var snapshot MallWeatherRefreshResult
			if err := json.Unmarshal([]byte(existing.ResponseJSON), &snapshot); err != nil {
				return fmt.Errorf("mall weather refresh: decode idempotency response: %w", err)
			}
			result, replayed = &snapshot, true
			return nil
		}

		created := &MallWeatherRefreshResult{
			JobID: record.ID, MallID: command.MallID, CorrelationID: command.TaskWindow,
			Force: command.Force, Reason: command.Reason,
			RequestedBy: command.ActorUserID, RequestedAt: command.RequestedAt.UTC(),
			Kinds: make([]MallWeatherRefreshKindResult, 0, len(command.Kinds)),
		}
		weatherDAO := data_dao.NewMallWeatherDAO(tx)
		for _, kind := range command.Kinds {
			fresh := false
			if !command.Force {
				fresh, err = mallWeatherRefreshKindFresh(ctx, weatherDAO, command.MallID, kind, command.RequestedAt)
				if err != nil {
					return err
				}
			}
			kindResult := MallWeatherRefreshKindResult{Kind: kind}
			if fresh && !command.Force {
				kindResult.Status = "SKIPPED_FRESH"
				created.Kinds = append(created.Kinds, kindResult)
				continue
			}
			outbox, err := newMallWeatherManualRefreshOutbox(command.MallID, command.TaskWindow, kind, command.RequestedAt)
			if err != nil {
				return err
			}
			if err := data_dao.NewAsyncJobOutboxDAO(tx).Create(ctx, &outbox); err != nil {
				return fmt.Errorf("mall weather refresh: create outbox: %w", err)
			}
			kindResult.Status, kindResult.OutboxJobID = "QUEUED", outbox.ID
			created.Kinds = append(created.Kinds, kindResult)
		}
		responseJSON, err := json.Marshal(created)
		if err != nil {
			return fmt.Errorf("mall weather refresh: encode response: %w", err)
		}
		if err := idempotencyDAO.Complete(ctx, record.ID, record.ID, http.StatusAccepted, model.JSONText(responseJSON)); err != nil {
			return err
		}
		result = created
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return result, replayed, nil
}

type mallWeatherRefreshLatestReader interface {
	FindCurrentLatest(context.Context, uint, string) (*model.MallWeatherLatest, error)
	FindCurrentLatestLifeSource(context.Context, uint, string) (*model.MallWeatherLatest, error)
}

func mallWeatherRefreshKindFresh(ctx context.Context, latest mallWeatherRefreshLatestReader, mallID uint, kind string, now time.Time) (bool, error) {
	if ctx == nil || latest == nil || mallID == 0 || now.IsZero() {
		return false, fmt.Errorf("mall weather refresh: invalid freshness request")
	}
	if kind != MallWeatherRefreshKindV26Full {
		return false, fmt.Errorf("mall weather refresh: unsupported freshness kind")
	}
	for _, dataKind := range []string{
		model.MallWeatherDataKindRealtime, model.MallWeatherDataKindMinutely,
		model.MallWeatherDataKindHourly, model.MallWeatherDataKindDaily,
	} {
		row, err := latest.FindCurrentLatest(ctx, mallID, dataKind)
		fresh, err := mallWeatherRefreshPointerFresh(dataKind, row, now, err)
		if err != nil {
			return false, err
		}
		if !fresh {
			return false, nil
		}
	}
	row, err := latest.FindCurrentLatestLifeSource(ctx, mallID, weatherdomain.SourceAPIV26Daily)
	return mallWeatherRefreshPointerFresh(model.MallWeatherDataKindLife, row, now, err)
}

func mallWeatherRefreshPointerFresh(dataKind string, row *model.MallWeatherLatest, now time.Time, lookupErr error) (bool, error) {
	if errors.Is(lookupErr, data_dao.ErrMallWeatherLatestNotFound) || (lookupErr == nil && row == nil) {
		return false, nil
	}
	if lookupErr != nil {
		return false, fmt.Errorf("mall weather refresh: find freshness: %w", lookupErr)
	}
	status, _, err := currentWeatherFreshness(dataKind, row, now.UTC())
	if err != nil {
		return false, err
	}
	return status == model.MallWeatherFreshnessFresh, nil
}

func newMallWeatherManualRefreshOutbox(mallID uint, taskWindow, kind string, availableAt time.Time) (model.AsyncJobOutbox, error) {
	if mallID == 0 || taskWindow == "" || availableAt.IsZero() {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather refresh: invalid outbox identity")
	}
	if kind != MallWeatherRefreshKindV26Full {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather refresh: unsupported outbox kind")
	}
	endpointKind := caiyun.EndpointWeatherV26
	payload, err := json.Marshal(job.MallTaskPayload{MallID: mallID, TaskWindow: taskWindow, EndpointKind: endpointKind})
	if err != nil {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather refresh: encode outbox payload: %w", err)
	}
	if _, err := job.DecodeMallWeatherTaskPayload(job.TypeMallWeatherManual, payload); err != nil {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather refresh: validate outbox payload: %w", err)
	}
	return model.AsyncJobOutbox{
		TaskKey:  "mall:weather:" + taskWindow + ":" + endpointKind,
		TaskType: job.TypeMallWeatherManual, PayloadJSON: model.JSONText(payload),
		QueueName: job.MallWeatherQueueName, AvailableAt: availableAt.UTC(),
	}, nil
}
