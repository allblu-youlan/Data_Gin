package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

var ErrOpenBojunSyncStatusUnavailable = errors.New("open bojun sync status: unavailable")

type openBojunSyncStateReader interface {
	Get(context.Context, string) (*model.BojunOracleSyncState, error)
}

type OpenBojunSyncStatusResult struct {
	Source                  string  `json:"source"`
	WatermarkField          string  `json:"watermarkField"`
	Initialized             bool    `json:"initialized"`
	WatermarkOracleRetailID uint64  `json:"watermarkOracleRetailId"`
	StateUpdatedAt          *string `json:"stateUpdatedAt,omitempty"`
	LastSuccessfulSyncAt    *string `json:"lastSuccessfulSyncAt,omitempty"`
	CheckedAt               string  `json:"checkedAt"`
}

type OpenBojunSyncStatusService struct {
	states      openBojunSyncStateReader
	permissions openBojunOrderPermissionReader
	now         func() time.Time
}

func NewOpenBojunSyncStatusService() *OpenBojunSyncStatusService {
	return newOpenBojunSyncStatusService(
		data_dao.NewBojunOracleSyncStateDAO(database.DB),
		newAccountPermissionChecker(database.DB),
		time.Now,
	)
}

func newOpenBojunSyncStatusService(
	states openBojunSyncStateReader,
	permissions openBojunOrderPermissionReader,
	now func() time.Time,
) *OpenBojunSyncStatusService {
	if states == nil || permissions == nil || now == nil {
		panic("open bojun sync status service: nil dependency")
	}
	return &OpenBojunSyncStatusService{states: states, permissions: permissions, now: now}
}

func (service *OpenBojunSyncStatusService) Query(ctx context.Context, actorUserID uint) (*OpenBojunSyncStatusResult, error) {
	if ctx == nil || actorUserID == 0 {
		return nil, ErrOpenBojunOrderForbidden
	}
	now := service.now().UTC()
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, model.PermissionBojunOrderRead, now)
	if err != nil {
		return nil, fmt.Errorf("%w: authorize: %v", ErrOpenBojunSyncStatusUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenBojunOrderForbidden
	}
	result := &OpenBojunSyncStatusResult{
		Source: "ORACLE", WatermarkField: "oracle_retail_id", CheckedAt: formatOpenBojunTime(now),
	}
	state, err := service.states.Get(ctx, bojunOracleDatasourceCode)
	if errors.Is(err, data_dao.ErrBojunOracleSyncStateNotInitialized) {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read committed watermark: %v", ErrOpenBojunSyncStatusUnavailable, err)
	}
	result.Initialized = true
	result.WatermarkOracleRetailID = state.LastRetailID
	if state.UpdatedAt > 0 {
		value := formatOpenBojunUpdatedAt(state.UpdatedAt)
		result.StateUpdatedAt = &value
	}
	if state.LastSucceededAt != nil {
		result.LastSuccessfulSyncAt = formatOpenBojunCompletedAt(state.LastSucceededAt)
	}
	return result, nil
}

func formatOpenBojunTime(value time.Time) string {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	return value.In(location).Format(openBojunOrderDateTimeFormat)
}
