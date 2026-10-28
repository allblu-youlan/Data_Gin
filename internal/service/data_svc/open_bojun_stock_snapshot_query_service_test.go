package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
)

type snapshotTestPermission struct {
	allowed bool
	err     error
}

func (permission snapshotTestPermission) HasPermission(_ context.Context, _ uint, code string, _ time.Time) (bool, error) {
	if code != model.PermissionBojunStockRead {
		return false, errors.New("unexpected permission")
	}
	return permission.allowed, permission.err
}

type snapshotTestOracle struct {
	*fakeOpenBojunOracle
	run func(context.Context, reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error)
}

func (oracle *snapshotTestOracle) QueryBojunStockSnapshot(ctx context.Context, query reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error) {
	return oracle.run(ctx, query)
}

type snapshotTestCache struct {
	mu       sync.Mutex
	data     map[string][]byte
	sets     int
	gets     int
	ttl      time.Duration
	getErr   error
	setErr   error
	getValue func(int) []byte
}

func (cache *snapshotTestCache) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.gets++
	if cache.getValue != nil {
		return cache.getValue(cache.gets), cache.getErr
	}
	return append([]byte(nil), cache.data[key]...), cache.getErr
}

func (cache *snapshotTestCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.sets++
	cache.ttl = ttl
	if cache.setErr != nil {
		return cache.setErr
	}
	if cache.data == nil {
		cache.data = make(map[string][]byte)
	}
	cache.data[key] = append([]byte(nil), value...)
	return nil
}

type snapshotTestLocker struct {
	mu       sync.Mutex
	held     bool
	err      error
	releases int
	keys     []string
}

func (locker *snapshotTestLocker) Acquire(_ context.Context, key string) (weather.TaskLock, bool, error) {
	locker.mu.Lock()
	defer locker.mu.Unlock()
	locker.keys = append(locker.keys, key)
	if locker.err != nil {
		return nil, false, locker.err
	}
	if locker.held {
		return nil, false, nil
	}
	locker.held = true
	return snapshotTestLock{locker}, true, nil
}

type snapshotTestLock struct{ locker *snapshotTestLocker }

func (lock snapshotTestLock) Release(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock.locker.mu.Lock()
	defer lock.locker.mu.Unlock()
	lock.locker.held = false
	lock.locker.releases++
	return nil
}

func newSnapshotTestService(run func(context.Context, reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error)) (*OpenBojunStockSnapshotQueryService, *snapshotTestCache, *snapshotTestLocker, *atomic.Int32) {
	opened := &atomic.Int32{}
	oracle := &snapshotTestOracle{fakeOpenBojunOracle: &fakeOpenBojunOracle{}, run: run}
	provider := newOpenBojunOracleProvider(openBojunProductOracleConfig(), func(context.Context, reportoracle.Config) (openBojunOracle, error) {
		opened.Add(1)
		return oracle, nil
	})
	cache, locker := &snapshotTestCache{}, &snapshotTestLocker{}
	service := newOpenBojunStockSnapshotQueryService(provider, snapshotTestPermission{allowed: true}, cache, locker, func() time.Time { return time.Date(2026, 10, 27, 7, 0, 0, 0, time.UTC) })
	return service, cache, locker, opened
}

func validSnapshotRequest() requestbody.OpenBojunStockSnapshotQueryRequest {
	return requestbody.OpenBojunStockSnapshotQueryRequest{ChangeDate: "2026-10-26", StoreID: 10, ProductID: []int64{21, 20, 21}, ProductColorID: []int64{31, 30}}
}

func TestOpenBojunStockSnapshotsCacheCompleteResultsAndPageWithoutRegeneration(t *testing.T) {
	calls := 0
	amount := "9007199254740993.123456789"
	service, cache, locker, opened := newSnapshotTestService(func(ctx context.Context, query reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error) {
		calls++
		if !reflect.DeepEqual(query, reportoracle.BojunStockSnapshotQuery{ChangeDate: 20261026, StoreID: 10, ProductID: []int64{20, 21}, ProductColorID: []int64{30, 31}}) {
			t.Fatalf("query=%+v", query)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("Oracle deadline is missing")
		}
		return []reportoracle.BojunStockSnapshotRow{
			{ChangeDate: 20261026, StoreCode: "001", ProductCode: "SKU1", Total: &amount},
			{ChangeDate: 20261026, StoreCode: "001", ProductCode: "SKU2"},
		}, nil
	})
	request := validSnapshotRequest()
	size, page := 1, 1
	request.Page, request.PageSize = &page, &size
	first, err := service.Query(t.Context(), 17, request)
	if err != nil || len(first.Items) != 1 || !first.Pagination.HasMore || first.GeneratedAt != "2026-10-27 15:00:00" || *first.Items[0].Total != amount || first.Items[0].ChangeDate != "2026-10-26" {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	page = 2
	request.ProductID = []int64{20, 21}
	second, err := service.Query(t.Context(), 17, request)
	if err != nil || len(second.Items) != 1 || second.Pagination.HasMore || second.Items[0].ProductCode != "SKU2" || second.GeneratedAt != first.GeneratedAt {
		t.Fatalf("second=%+v error=%v", second, err)
	}
	page = 3
	empty, err := service.Query(t.Context(), 17, request)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.Pagination.HasMore {
		t.Fatalf("empty=%+v error=%v", empty, err)
	}
	if calls != 1 || opened.Load() != 1 || cache.sets != 1 || cache.ttl != time.Minute || locker.releases != 1 {
		t.Fatalf("calls=%d opened=%d sets=%d ttl=%v releases=%d", calls, opened.Load(), cache.sets, cache.ttl, locker.releases)
	}
}

func TestOpenBojunStockSnapshotsRejectInvalidRequestsAndPermissionsBeforeOracle(t *testing.T) {
	service, cache, _, opened := newSnapshotTestService(nil)
	for _, mutate := range []func(*requestbody.OpenBojunStockSnapshotQueryRequest){
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.ChangeDate = "2026-02-29" },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.ChangeDate = "20261026" },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.ChangeDate = "2026-1-01" },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.StoreID = 0 },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.ProductColorID = []int64{0} },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.ProductID = nil },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.ProductID = []int64{-1} },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { r.ProductID = make([]int64, 101) },
		func(r *requestbody.OpenBojunStockSnapshotQueryRequest) { value := 101; r.PageSize = &value },
	} {
		request := validSnapshotRequest()
		mutate(&request)
		if _, err := service.Query(t.Context(), 17, request); !errors.Is(err, ErrOpenBojunStockSnapshotInvalidQuery) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
	for _, permission := range []snapshotTestPermission{{}, {err: errors.New("permission unavailable")}} {
		service.permissions = permission
		_, err := service.Query(t.Context(), 17, validSnapshotRequest())
		want := ErrOpenBojunStockSnapshotForbidden
		if permission.err != nil {
			want = ErrOpenBojunStockSnapshotUnavailable
		}
		if !errors.Is(err, want) {
			t.Fatalf("error=%v want=%v", err, want)
		}
	}
	if opened.Load() != 0 || cache.gets != 0 {
		t.Fatalf("opened=%d cache reads=%d", opened.Load(), cache.gets)
	}
}

func TestOpenBojunStockSnapshotsGlobalLockCacheKeysAndDoubleCheck(t *testing.T) {
	service, cache, locker, opened := newSnapshotTestService(nil)
	query, _, err := normalizeOpenBojunStockSnapshotRequest(validSnapshotRequest())
	if err != nil {
		t.Fatal(err)
	}
	key, lockKey := service.keys(query)
	query.ProductID = []int64{99}
	otherKey, otherLock := service.keys(query)
	if key == otherKey || lockKey != otherLock || strings.Contains(key, "productId") || strings.Contains(key, service.provider.config.Password) && service.provider.config.Password != "" {
		t.Fatalf("cache or global lock key does not isolate queries: %s %s %s %s", key, otherKey, lockKey, otherLock)
	}
	service.provider.config.Host += "-other"
	differentKey, differentLock := service.keys(query)
	if differentKey == otherKey || differentLock == otherLock {
		t.Fatal("datasource must isolate both cache and result table lock")
	}
	encoded, err := json.Marshal(openBojunStockSnapshot{Items: []OpenBojunStockSnapshotDTO{}, GeneratedAt: "2026-10-27 15:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	cache.getValue = func(calls int) []byte {
		if calls < 2 {
			return nil
		}
		return encoded
	}
	result, err := service.Query(t.Context(), 17, validSnapshotRequest())
	if err != nil || result.Items == nil || opened.Load() != 0 || locker.releases != 1 {
		t.Fatalf("result=%+v error=%v opened=%d releases=%d", result, err, opened.Load(), locker.releases)
	}
}

func TestOpenBojunStockSnapshotsFailClosedAndDoNotCacheFailures(t *testing.T) {
	service, cache, locker, opened := newSnapshotTestService(func(context.Context, reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error) {
		return nil, errors.New("Oracle generation failed")
	})
	locker.err = errors.New("Redis unavailable")
	if _, err := service.Query(t.Context(), 17, validSnapshotRequest()); !errors.Is(err, ErrOpenBojunStockSnapshotUnavailable) || opened.Load() != 0 {
		t.Fatalf("lock failure error=%v opened=%d", err, opened.Load())
	}
	locker.err = nil
	if _, err := service.Query(t.Context(), 17, validSnapshotRequest()); !errors.Is(err, ErrOpenBojunStockSnapshotUnavailable) || cache.sets != 0 || locker.releases != 1 {
		t.Fatalf("Oracle failure error=%v sets=%d releases=%d", err, cache.sets, locker.releases)
	}
	service, cache, locker, _ = newSnapshotTestService(func(context.Context, reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error) {
		return nil, nil
	})
	cache.setErr = errors.New("cache write failed")
	result, err := service.Query(t.Context(), 17, validSnapshotRequest())
	if err != nil || result.Items == nil || locker.releases != 1 {
		t.Fatalf("cache write failure result=%+v error=%v releases=%d", result, err, locker.releases)
	}
}

func TestOpenBojunStockSnapshotsWaitersCancelAndConcurrentRequestsShareGeneration(t *testing.T) {
	service, _, locker, opened := newSnapshotTestService(nil)
	locker.held = true
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := service.Query(ctx, 17, validSnapshotRequest()); !errors.Is(err, context.DeadlineExceeded) || opened.Load() != 0 {
		t.Fatalf("canceled wait error=%v opened=%d", err, opened.Load())
	}
	var calls atomic.Int32
	service, _, locker, _ = newSnapshotTestService(func(context.Context, reportoracle.BojunStockSnapshotQuery) ([]reportoracle.BojunStockSnapshotRow, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return []reportoracle.BojunStockSnapshotRow{{ChangeDate: 20261026, StoreCode: "001", ProductCode: "SKU1"}}, nil
	})
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.Query(t.Context(), 17, validSnapshotRequest())
			if err != nil || len(result.Items) != 1 {
				t.Errorf("result=%+v error=%v", result, err)
			}
		}()
	}
	wait.Wait()
	if calls.Load() != 1 || locker.releases != 1 {
		t.Fatalf("generations=%d releases=%d", calls.Load(), locker.releases)
	}
}
