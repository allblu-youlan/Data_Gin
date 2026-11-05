package data_svc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
)

type bojunStatusRecordingStore struct {
	*fakeBojunOracleRetailStore
	statuses     []int
	failuresLeft int
}

func (store *bojunStatusRecordingStore) UpdateSyncStatus(ctx context.Context, id uint, synced int) error {
	store.statuses = append(store.statuses, synced)
	if store.failuresLeft > 0 {
		store.failuresLeft--
		return errors.New("local status update failed")
	}
	return store.fakeBojunOracleRetailStore.UpdateSyncStatus(ctx, id, synced)
}

func TestBojunOraclePushPersistsStatusOnceAndRetriesFailedWrites(t *testing.T) {
	setupBojunHenglongContextTestConfig(t, "https://henglong.test/sales")
	tests := []struct {
		name         string
		pushFails    bool
		writeBackErr error
		failuresLeft int
		wantStatuses []int
		wantSynced   int
		wantError    bool
	}{
		{name: "successful push", wantStatuses: []int{1}, wantSynced: 1},
		{name: "failed push", pushFails: true, wantStatuses: []int{2}, wantSynced: 2, wantError: true},
		{name: "retry successful push status", failuresLeft: 1, wantStatuses: []int{1, 1}, wantSynced: 1},
		{name: "retry failed push status", pushFails: true, failuresLeft: 1, wantStatuses: []int{2, 2}, wantSynced: 2, wantError: true},
		{name: "local status unavailable", failuresLeft: 2, wantStatuses: []int{1, 1}, wantError: true},
		{name: "Oracle write-back pending", writeBackErr: errors.New("Oracle update failed"), wantStatuses: []int{1, 3}, wantSynced: 3, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			pushCalls := 0
			http.DefaultTransport = bojunHenglongTestTransport(func(request *http.Request) (*http.Response, error) {
				pushCalls++
				response := "<responsecode>0</responsecode>"
				if tt.pushFails {
					response = "<responsecode>1</responsecode><responsemessage>rejected</responsemessage>"
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(response)),
					Request:    request,
				}, nil
			})
			connection := &fakeBojunOracleConnection{
				maxID: 11,
				rows: []reportoracle.BojunRetailRow{{
					RetailID: 11, DocNo: "STATUS-11", StoreCode: "ABCN002A001",
					StatusTime: time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local),
					PaidAmount: 88, PushAmount: 88, IsToShop: "Y",
				}},
				writeBackErr: tt.writeBackErr,
			}
			state := &fakeBojunOracleStateStore{
				state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 10, Initialized: true},
				leaseAcquired: true,
			}
			service := newTestBojunOracleOrderService(connection, state)
			store := &bojunStatusRecordingStore{
				fakeBojunOracleRetailStore: service.retailOrderDAO.(*fakeBojunOracleRetailStore),
				failuresLeft:               tt.failuresLeft,
			}
			service.retailOrderDAO = store
			service.pushService = &BojunOrderPushService{
				retailOrderDAO: store,
				logDAO:         &fakeDeliveryLogCreator{},
				pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
			}

			result, err := service.SyncIncremental(t.Context())
			if (err != nil) != tt.wantError {
				t.Fatalf("SyncIncremental() error = %v, want error %t", err, tt.wantError)
			}
			if !slices.Equal(store.statuses, tt.wantStatuses) || store.orders["STATUS-11"].Synced != tt.wantSynced {
				t.Fatalf("status writes = %v, stored = %d, want writes %v, stored %d", store.statuses, store.orders["STATUS-11"].Synced, tt.wantStatuses, tt.wantSynced)
			}
			if pushCalls != 1 || !slices.Equal(connection.writeBackOK, []bool{!tt.pushFails}) {
				t.Fatalf("push calls = %d, Oracle statuses = %v", pushCalls, connection.writeBackOK)
			}
			wantWatermark := uint64(11)
			if tt.wantError {
				wantWatermark = 10
			}
			if result.WatermarkAfter != wantWatermark {
				t.Fatalf("watermark = %d, want %d", result.WatermarkAfter, wantWatermark)
			}
		})
	}
}

func TestBojunOraclePushPersistsStatusForOtherPushers(t *testing.T) {
	for _, success := range []bool{true, false} {
		store := &bojunStatusRecordingStore{fakeBojunOracleRetailStore: &fakeBojunOracleRetailStore{}}
		service := &BojunOracleOrderService{
			retailOrderDAO: store,
			pushService:    &fakeBojunOraclePusher{result: bojunOrderPushResult{Success: success}},
			now:            time.Now,
		}
		err := service.pushAndWriteBack(t.Context(), &fakeBojunOracleConnection{}, reportoracle.BojunRetailRow{RetailID: 11},
			&model.BojunRetailOrder{BaseModel: model.BaseModel{ID: 11}, DocNo: "STATUS-11"},
			&BojunOrderSyncResult{}, OrderPushSkipConfig{})
		wantStatus := 1
		if !success {
			wantStatus = 2
		}
		if (err != nil) == success || !slices.Equal(store.statuses, []int{wantStatus}) {
			t.Fatalf("push success = %t, error = %v, status writes = %v", success, err, store.statuses)
		}
	}
}
