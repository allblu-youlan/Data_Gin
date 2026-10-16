package data_svc

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	pkgConfig "gin-biz-web-api/pkg/config"
)

type bojunHenglongTestTransport func(*http.Request) (*http.Response, error)

func (transport bojunHenglongTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestBojunOracleHenglongRetry(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("HengLong:\n  SalesAPIURL: https://henglong.test/sales\n  TillID: '01'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgConfig.NewConfig("", configDir+string(os.PathSeparator))

	const retryDocNo = "ABCN002A001P12610151236520008"
	const nextDocNo = "ABCN002A001P12610151236520009"
	const duplicateResponse = `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><postsalescreateResponse xmlns="http://tempurl.org"><postsalescreateResult><header><responsecode>1</responsecode><responsemessage>交易保存到MPOS失败，POS机41620101，已存在交易流水ABCN002A001P12610151236520008</responsemessage></header><salestotal><storecode>416201</storecode><txdocno>ABCN002A001P12610151236520008</txdocno><paidamount>536.40</paidamount></salestotal></postsalescreateResult></postsalescreateResponse></soap:Body></soap:Envelope>`
	tests := []struct {
		name     string
		response string
		wantErr  bool
	}{
		{name: "existing transaction completes retry and processes next order", response: duplicateResponse},
		{
			name:     "ordinary rejection remains failed and stops the page",
			response: strings.Replace(duplicateResponse, "已存在交易流水"+retryDocNo, "门店未启用", 1),
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			var pushedDocNos []string
			http.DefaultTransport = bojunHenglongTestTransport(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				response := "<responsecode>0</responsecode>"
				switch {
				case strings.Contains(string(body), "<txdocno>"+retryDocNo+"</txdocno>"):
					pushedDocNos = append(pushedDocNos, retryDocNo)
					response = tt.response
				case strings.Contains(string(body), "<txdocno>"+nextDocNo+"</txdocno>"):
					pushedDocNos = append(pushedDocNos, nextDocNo)
				default:
					return nil, fmt.Errorf("unexpected Henglong request: %s", body)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(response)),
					Request:    request,
				}, nil
			})

			statusTime := time.Date(2026, 10, 15, 12, 36, 52, 0, time.Local)
			connection := &fakeBojunOracleConnection{
				maxID: 13,
				rows: []reportoracle.BojunRetailRow{
					{RetailID: 12, StoreCode: "ABCN002A001", DocNo: retryDocNo, StatusTime: statusTime, PaidAmount: 536.40, PushAmount: 536.40, IsToShop: "Y"},
					{RetailID: 13, StoreCode: "ABCN002A001", DocNo: nextDocNo, StatusTime: statusTime, PaidAmount: 88, PushAmount: 88, IsToShop: "Y"},
				},
			}
			state := &fakeBojunOracleStateStore{
				state:         model.BojunOracleSyncState{SourceCode: bojunOracleDatasourceCode, LastRetailID: 11, Initialized: true},
				leaseAcquired: true,
			}
			service := newTestBojunOracleOrderService(connection, state)
			store := service.retailOrderDAO.(*fakeBojunOracleRetailStore)
			existing, err := buildBojunRetailOrderFromOracle(connection.rows[0])
			if err != nil {
				t.Fatal(err)
			}
			existing.ID, existing.Synced = 77, 2
			store.orders[retryDocNo] = existing
			logs := &fakeDeliveryLogCreator{}
			runs := &fakeBojunPipelineRunRecorder{}
			service.pushService = &BojunOrderPushService{retailOrderDAO: store, logDAO: logs, pipelineRunDAO: runs}

			result, err := service.SyncIncremental(t.Context())
			if (err != nil) != tt.wantErr {
				t.Fatalf("SyncIncremental() error = %v, want error %t", err, tt.wantErr)
			}
			if tt.wantErr {
				if !slices.Equal(pushedDocNos, []string{retryDocNo}) || store.orders[nextDocNo] != nil {
					t.Fatalf("failed push processed subsequent order: pushed=%v next=%+v", pushedDocNos, store.orders[nextDocNo])
				}
				if existing.Synced != 2 || !slices.Equal(connection.writeBackOK, []bool{false}) || !slices.Equal(connection.writeBackIDs, []uint64{12}) {
					t.Fatalf("failure status: synced=%d Oracle IDs=%v statuses=%v", existing.Synced, connection.writeBackIDs, connection.writeBackOK)
				}
				if state.advancedTo != 0 || result.WatermarkAfter != 11 || result.FailedCount != 1 {
					t.Fatalf("failed push advanced watermark: state=%+v result=%+v", state, result)
				}
			} else {
				if !slices.Equal(pushedDocNos, []string{retryDocNo, nextDocNo}) {
					t.Fatalf("pushed orders = %v, want retry followed by next order", pushedDocNos)
				}
				if existing.Synced != 1 || store.orders[nextDocNo] == nil || store.orders[nextDocNo].Synced != 1 {
					t.Fatalf("local sync states: retry=%d next=%+v", existing.Synced, store.orders[nextDocNo])
				}
				if !slices.Equal(connection.writeBackIDs, []uint64{12, 13}) || !slices.Equal(connection.writeBackOK, []bool{true, true}) {
					t.Fatalf("Oracle write-backs: IDs=%v statuses=%v", connection.writeBackIDs, connection.writeBackOK)
				}
				if state.advancedFrom != 11 || state.advancedTo != 13 || result.WatermarkAfter != 13 || result.FailedCount != 0 || !result.SourceCaughtUp {
					t.Fatalf("retry did not complete page: state=%+v result=%+v", state, result)
				}
			}
			if len(logs.logs) != len(pushedDocNos) || logs.logs[0].Success == tt.wantErr || logs.logs[0].ResponseBody != tt.response {
				t.Fatalf("delivery logs did not preserve response and status: %+v", logs.logs)
			}
			wantRunStatus := "success"
			if tt.wantErr {
				wantRunStatus = "failed"
			}
			if runs.finishStatus != wantRunStatus {
				t.Fatalf("pipeline status = %q, want %q", runs.finishStatus, wantRunStatus)
			}
		})
	}
}
