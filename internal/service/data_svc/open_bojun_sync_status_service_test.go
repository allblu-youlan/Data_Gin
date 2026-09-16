package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
)

type fakeOpenBojunSyncStateReader struct {
	state      *model.BojunOracleSyncState
	err        error
	sourceCode string
	calls      int
}

func (reader *fakeOpenBojunSyncStateReader) Get(_ context.Context, sourceCode string) (*model.BojunOracleSyncState, error) {
	reader.sourceCode = sourceCode
	reader.calls++
	return reader.state, reader.err
}

func TestOpenBojunSyncStatusServiceReturnsCommittedOracleWatermark(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 9, 16, 10, 30, 0, 0, location)
	lastSucceededAt := now.Add(-time.Minute)
	states := &fakeOpenBojunSyncStateReader{state: &model.BojunOracleSyncState{
		SourceCode: bojunOracleDatasourceCode, LastRetailID: 45077, Initialized: true,
		LastSucceededAt:       &lastSucceededAt,
		CommonTimestampsField: model.CommonTimestampsField{UpdatedAt: int(now.Add(-30 * time.Second).Unix())},
	}}
	permissions := &fakeOpenBojunPermissionReader{allowed: true}
	service := newOpenBojunSyncStatusService(states, permissions, func() time.Time { return now })
	result, err := service.Query(t.Context(), 17)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if states.calls != 1 || states.sourceCode != bojunOracleDatasourceCode || permissions.permission != model.PermissionBojunOrderRead ||
		!result.Initialized || result.Source != "ORACLE" || result.WatermarkField != "oracle_retail_id" ||
		result.WatermarkOracleRetailID != 45077 || result.StateUpdatedAt == nil || *result.StateUpdatedAt != "2026-09-16 10:29:30" ||
		result.LastSuccessfulSyncAt == nil || *result.LastSuccessfulSyncAt != "2026-09-16 10:29:00" || result.CheckedAt != "2026-09-16 10:30:00" {
		t.Fatalf("states=%+v permissions=%+v result=%+v", states, permissions, result)
	}
}

func TestOpenBojunSyncStatusServiceReportsNotInitialized(t *testing.T) {
	service := newOpenBojunSyncStatusService(
		&fakeOpenBojunSyncStateReader{err: data_dao.ErrBojunOracleSyncStateNotInitialized},
		&fakeOpenBojunPermissionReader{allowed: true},
		func() time.Time { return time.Date(2026, 9, 16, 2, 30, 0, 0, time.UTC) },
	)
	result, err := service.Query(t.Context(), 17)
	if err != nil || result.Initialized || result.WatermarkOracleRetailID != 0 || result.StateUpdatedAt != nil || result.LastSuccessfulSyncAt != nil {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestOpenBojunSyncStatusServiceFailsClosed(t *testing.T) {
	states := &fakeOpenBojunSyncStateReader{err: errors.New("database unavailable")}
	service := newOpenBojunSyncStatusService(states, &fakeOpenBojunPermissionReader{allowed: false}, time.Now)
	if _, err := service.Query(t.Context(), 17); !errors.Is(err, ErrOpenBojunOrderForbidden) || states.calls != 0 {
		t.Fatalf("forbidden error=%v calls=%d", err, states.calls)
	}
	service = newOpenBojunSyncStatusService(states, &fakeOpenBojunPermissionReader{allowed: true}, time.Now)
	if _, err := service.Query(t.Context(), 17); !errors.Is(err, ErrOpenBojunSyncStatusUnavailable) || states.calls != 1 {
		t.Fatalf("unavailable error=%v calls=%d", err, states.calls)
	}
}
