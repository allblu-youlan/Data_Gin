package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	projectredis "gin-biz-web-api/pkg/redis"

	redisv8 "github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

const (
	openBojunStockSnapshotCacheTTL = time.Minute
	openBojunStockSnapshotLockTTL  = 2 * time.Minute
	openBojunStockSnapshotWait     = 250 * time.Millisecond
)

var (
	ErrOpenBojunStockSnapshotForbidden    = errors.New("open bojun stock snapshot: forbidden")
	ErrOpenBojunStockSnapshotInvalidQuery = errors.New("open bojun stock snapshot: invalid query")
	ErrOpenBojunStockSnapshotUnavailable  = errors.New("open bojun stock snapshot: unavailable")
)

type openBojunStockSnapshotOracle interface {
	QueryBojunStockSnapshot(context.Context, reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error)
}

type openBojunStockSnapshotCache interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
}

type OpenBojunStockSnapshotQueryService struct {
	provider    *OpenBojunOracleProvider
	permissions openBojunQueryPermissionReader
	cache       openBojunStockSnapshotCache
	locker      weather.TaskLocker
	now         func() time.Time
}

type OpenBojunStockSnapshotDTO struct {
	ChangeDate   string  `json:"changeDate"`
	StoreCode    string  `json:"storeCode"`
	StoreName    *string `json:"storeName"`
	ProductCode  string  `json:"productCode"`
	ProductName  *string `json:"productName"`
	ProductColor *string `json:"productColor"`
	PriceList    *string `json:"priceList"`
	Total        *string `json:"total"`
	AmtList      *string `json:"amtList"`
	QtyPreIn     *string `json:"qtyPreIn"`
}

type OpenBojunStockSnapshotQueryDTO struct {
	Items       []OpenBojunStockSnapshotDTO `json:"items"`
	Pagination  OpenBojunQueryPagination    `json:"pagination"`
	GeneratedAt string                      `json:"generatedAt"`
}

type openBojunStockSnapshot struct {
	Items       []OpenBojunStockSnapshotDTO `json:"items"`
	GeneratedAt string                      `json:"generatedAt"`
}

func NewOpenBojunStockSnapshotQueryService(provider *OpenBojunOracleProvider) *OpenBojunStockSnapshotQueryService {
	store := openBojunStockSnapshotRedisStore{}
	return newOpenBojunStockSnapshotQueryService(provider, newAccountPermissionChecker(database.DB), store, store, time.Now)
}

func newOpenBojunStockSnapshotQueryService(provider *OpenBojunOracleProvider, permissions openBojunQueryPermissionReader, cache openBojunStockSnapshotCache, locker weather.TaskLocker, now func() time.Time) *OpenBojunStockSnapshotQueryService {
	if provider == nil || permissions == nil || cache == nil || locker == nil || now == nil {
		panic("open bojun stock snapshot query service: nil dependency")
	}
	return &OpenBojunStockSnapshotQueryService{provider: provider, permissions: permissions, cache: cache, locker: locker, now: now}
}

func (service *OpenBojunStockSnapshotQueryService) Query(ctx context.Context, actorUserID uint, request requestbody.OpenBojunStockSnapshotQueryRequest) (*OpenBojunStockSnapshotQueryDTO, error) {
	if ctx == nil {
		return nil, ErrOpenBojunStockSnapshotInvalidQuery
	}
	if actorUserID == 0 {
		return nil, ErrOpenBojunStockSnapshotForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, model.PermissionBojunStockRead, service.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %w", ErrOpenBojunStockSnapshotUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenBojunStockSnapshotForbidden
	}
	query, pagination, err := normalizeOpenBojunStockSnapshotRequest(request)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, service.provider.queryTimeout(30*time.Second))
	defer cancel()
	cacheKey, lockKey := service.keys(query)
	for {
		if err := queryCtx.Err(); err != nil {
			return nil, fmt.Errorf("%w: wait for snapshot: %w", ErrOpenBojunStockSnapshotUnavailable, err)
		}
		if snapshot := service.cached(queryCtx, cacheKey); snapshot != nil {
			return snapshot.page(pagination), nil
		}
		lock, acquired, err := service.locker.Acquire(queryCtx, lockKey)
		if err != nil {
			return nil, fmt.Errorf("%w: acquire result table lock: %w", ErrOpenBojunStockSnapshotUnavailable, err)
		}
		if acquired && lock != nil {
			return service.generate(queryCtx, query, pagination, cacheKey, lock)
		}
		timer := time.NewTimer(openBojunStockSnapshotWait)
		select {
		case <-queryCtx.Done():
			timer.Stop()
			return nil, fmt.Errorf("%w: wait for snapshot: %w", ErrOpenBojunStockSnapshotUnavailable, queryCtx.Err())
		case <-timer.C:
		}
	}
}

func (service *OpenBojunStockSnapshotQueryService) generate(ctx context.Context, query reportoracle.BojunStockSnapshotQuery, pagination OpenBojunQueryPagination, cacheKey string, lock weather.TaskLock) (*OpenBojunStockSnapshotQueryDTO, error) {
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil {
			zap.L().Warn("release bojun stock snapshot lock", zap.Error(err))
		}
	}()
	if snapshot := service.cached(ctx, cacheKey); snapshot != nil {
		return snapshot.page(pagination), nil
	}
	connection, err := service.provider.connection(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: open Oracle: %w", ErrOpenBojunStockSnapshotUnavailable, err)
	}
	oracle, ok := connection.(openBojunStockSnapshotOracle)
	if !ok {
		return nil, fmt.Errorf("%w: Oracle snapshot query is unavailable", ErrOpenBojunStockSnapshotUnavailable)
	}
	rows, err := oracle.QueryBojunStockSnapshot(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: generate snapshot: %w", ErrOpenBojunStockSnapshotUnavailable, err)
	}
	snapshot := &openBojunStockSnapshot{Items: make([]OpenBojunStockSnapshotDTO, 0, len(rows)), GeneratedAt: formatOpenBojunTime(service.now())}
	for _, row := range rows {
		date, err := time.Parse("20060102", fmt.Sprintf("%08d", row.ChangeDate))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid result date", ErrOpenBojunStockSnapshotUnavailable)
		}
		snapshot.Items = append(snapshot.Items, OpenBojunStockSnapshotDTO{
			ChangeDate: date.Format("2006-01-02"), StoreCode: row.StoreCode, StoreName: row.StoreName,
			ProductCode: row.ProductCode, ProductName: row.ProductName, ProductColor: row.ProductColor,
			PriceList: row.PriceList, Total: row.Total, AmtList: row.AmtList, QtyPreIn: row.QtyPreIn,
		})
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("%w: encode snapshot: %w", ErrOpenBojunStockSnapshotUnavailable, err)
	}
	if err := service.cache.Set(ctx, cacheKey, encoded, openBojunStockSnapshotCacheTTL); err != nil {
		zap.L().Warn("cache bojun stock snapshot", zap.Error(err))
	}
	return snapshot.page(pagination), nil
}

func (service *OpenBojunStockSnapshotQueryService) cached(ctx context.Context, key string) *openBojunStockSnapshot {
	encoded, err := service.cache.Get(ctx, key)
	if err != nil || len(encoded) == 0 {
		return nil
	}
	var snapshot openBojunStockSnapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil || snapshot.Items == nil || snapshot.GeneratedAt == "" {
		return nil
	}
	return &snapshot
}

func (snapshot *openBojunStockSnapshot) page(pagination OpenBojunQueryPagination) *OpenBojunStockSnapshotQueryDTO {
	start := (pagination.Page - 1) * pagination.PageSize
	items := make([]OpenBojunStockSnapshotDTO, 0)
	if start < len(snapshot.Items) {
		end := min(start+pagination.PageSize, len(snapshot.Items))
		items = snapshot.Items[start:end]
		pagination.HasMore = end < len(snapshot.Items)
	}
	return &OpenBojunStockSnapshotQueryDTO{Items: items, Pagination: pagination, GeneratedAt: snapshot.GeneratedAt}
}

func normalizeOpenBojunStockSnapshotRequest(request requestbody.OpenBojunStockSnapshotQueryRequest) (reportoracle.BojunStockSnapshotQuery, OpenBojunQueryPagination, error) {
	query := reportoracle.BojunStockSnapshotQuery{StoreID: request.StoreID}
	invalid := func() (reportoracle.BojunStockSnapshotQuery, OpenBojunQueryPagination, error) {
		return reportoracle.BojunStockSnapshotQuery{}, OpenBojunQueryPagination{}, ErrOpenBojunStockSnapshotInvalidQuery
	}
	date, err := time.Parse("2006-01-02", request.ChangeDate)
	if err != nil || date.Format("2006-01-02") != request.ChangeDate || date.Year() < 1 || query.StoreID <= 0 || query.StoreID > 9999999999 {
		return invalid()
	}
	query.ChangeDate, _ = strconv.Atoi(date.Format("20060102"))
	for _, filter := range []struct {
		input  []int64
		output *[]int64
	}{{request.ProductID, &query.ProductID}, {request.ProductColorID, &query.ProductColorID}} {
		if len(filter.input) == 0 || len(filter.input) > 100 {
			return invalid()
		}
		values := make([]int64, 0, len(filter.input))
		seen := make(map[int64]struct{}, len(filter.input))
		for _, id := range filter.input {
			if id <= 0 || id > 9999999999 {
				return invalid()
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			values = append(values, id)
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		*filter.output = values
	}

	pagination, err := normalizeOpenBojunQueryPagination(request.Page, request.PageSize)
	if err != nil {
		return invalid()
	}
	return query, pagination, nil
}

func (service *OpenBojunStockSnapshotQueryService) keys(query reportoracle.BojunStockSnapshotQuery) (string, string) {
	config := service.provider.config
	datasource, _ := json.Marshal([]any{config.Host, config.Port, config.ServiceName, config.SID})
	datasourceHash := sha256.Sum256(datasource)
	encoded, _ := json.Marshal(query)
	queryHash := sha256.Sum256(append(datasource, encoded...))
	return "open:bojun:stock-snapshot:v1:" + hex.EncodeToString(queryHash[:]), hex.EncodeToString(datasourceHash[:])
}

// Resolve Redis at request time so route registration can precede Redis startup.
type openBojunStockSnapshotRedisStore struct{}

func (openBojunStockSnapshotRedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	client := projectredis.Instance()
	if client == nil || client.Client == nil {
		return nil, errors.New("snapshot Redis is unavailable")
	}
	value, err := client.Client.Get(ctx, projectredis.GenNamespace(key)).Bytes()
	if errors.Is(err, redisv8.Nil) {
		return nil, nil
	}
	return value, err
}

func (openBojunStockSnapshotRedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	client := projectredis.Instance()
	if client == nil || client.Client == nil {
		return errors.New("snapshot Redis is unavailable")
	}
	return client.Client.Set(ctx, projectredis.GenNamespace(key), value, ttl).Err()
}

func (openBojunStockSnapshotRedisStore) Acquire(ctx context.Context, key string) (weather.TaskLock, bool, error) {
	client := projectredis.Instance()
	if client == nil || client.Client == nil {
		return nil, false, errors.New("snapshot Redis is unavailable")
	}
	locker, err := weather.NewRedisTaskLocker(client.Client, projectredis.GenNamespace("lock:open-bojun-stock-snapshot:"), openBojunStockSnapshotLockTTL)
	if err != nil {
		return nil, false, err
	}
	return locker.Acquire(ctx, key)
}
