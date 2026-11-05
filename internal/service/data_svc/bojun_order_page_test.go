package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/orderpush"
)

func TestBojunAPIPagePrefetchQueryCounts(t *testing.T) {
	for _, existingCount := range []int{100, 50, 0} {
		t.Run(fmt.Sprintf("existing_%d", existingCount), func(t *testing.T) {
			store := &fakeBojunRetailOrderWriter{
				existing: map[string]bool{}, orders: map[string]*model.BojunRetailOrder{},
			}
			records := make([]map[string]interface{}, 100)
			for i := range records {
				docNo := fmt.Sprintf("ORDER-%03d", i)
				records[i] = map[string]interface{}{
					"docno": docNo, "extendedFields1": "2026-11-05 10:00:00", "totQty": float64(1),
				}
				if i < existingCount {
					order, err := buildBojunRetailOrder(0, records[i])
					if err != nil {
						t.Fatal(err)
					}
					store.orders[docNo] = order
					store.existing[docNo] = true
				}
			}
			service := &BojunOrderService{
				retailOrderDAO: store, rawDataDAO: &fakeBojunRawDataCreator{},
				pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
			}

			result, err := runBojunAPIPageForTest(t, service, records)
			if err != nil {
				t.Fatal(err)
			}
			if store.batchFindCalls != 1 || store.findCalls != 100-existingCount {
				t.Fatalf("batch reads=%d single reads=%d, want 1 and %d", store.batchFindCalls, store.findCalls, 100-existingCount)
			}
			if result.TotalCount != 100 || result.ExistingCount != existingCount || result.SkippedCount != existingCount ||
				result.RetailCount != 100-existingCount || result.SavedCount != 100-existingCount || result.FailedCount != 0 {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestBojunAPIPagePrefetchReadsDuplicateAfterUpdate(t *testing.T) {
	first := map[string]interface{}{"docno": "ORDER-1", "cStoreName": "updated", "totQty": float64(1)}
	second := map[string]interface{}{"docno": "ORDER-1", "totQty": float64(2)}
	store := &fakeBojunRetailOrderWriter{
		existing: map[string]bool{"ORDER-1": true},
		orders: map[string]*model.BojunRetailOrder{
			"ORDER-1": {DocNo: "ORDER-1", StoreName: "old", TotalQty: 1},
		},
	}
	service := &BojunOrderService{
		retailOrderDAO: store, rawDataDAO: &fakeBojunRawDataCreator{},
		pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
	}

	result, err := runBojunAPIPageForTest(t, service, []map[string]interface{}{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if store.batchFindCalls != 1 || store.findCalls != 1 || result.UpdatedCount != 2 {
		t.Fatalf("batch reads=%d single reads=%d result=%+v", store.batchFindCalls, store.findCalls, result)
	}
	if order := store.orders["ORDER-1"]; order.StoreName != "updated" || order.TotalQty != 2 {
		t.Fatalf("duplicate order did not observe the preceding update: %+v", order)
	}
}

func TestBojunAPIPagePrefetchFailureUsesRecordFailureStatistics(t *testing.T) {
	queryFailure := errors.New("batch query failed")
	store := &fakeBojunRetailOrderWriter{existing: map[string]bool{}, batchFindErr: queryFailure}
	rawStore := &fakeBojunRawDataCreator{}
	service := &BojunOrderService{
		retailOrderDAO: store, rawDataDAO: rawStore,
		pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
	}

	result, err := runBojunAPIPageForTest(t, service, []map[string]interface{}{{"docno": "ORDER-1"}})
	if !errors.Is(err, queryFailure) {
		t.Fatalf("error=%v, want batch query failure", err)
	}
	if result.TotalCount != 1 || result.FailedCount != 1 || len(result.FailedSamples) != 1 ||
		result.FailedSamples[0].DocNo != "ORDER-1" || rawStore.created != 0 || store.findCalls != 0 {
		t.Fatalf("result=%+v raw writes=%d single reads=%d", result, rawStore.created, store.findCalls)
	}
}

type bojunPageTestSkipConfig struct {
	config OrderPushSkipConfig
}

func (getter bojunPageTestSkipConfig) Get(context.Context) (OrderPushSkipConfig, error) {
	return getter.config, nil
}

type bojunPageTestPusher func(*model.BojunRetailOrder, int, OrderPushSkipPolicy) bojunOrderPushResult

func (push bojunPageTestPusher) PushNewOrderWithPolicy(_ context.Context, order *model.BojunRetailOrder, position int, policy OrderPushSkipPolicy) bojunOrderPushResult {
	return push(order, position, policy)
}

func TestBojunAPIPagePrefetchPreservesPushPositionsAcrossPages(t *testing.T) {
	first := map[string]interface{}{"docno": "ORDER-1", "cStoreCode": "ABCN001P012"}
	second := map[string]interface{}{"docno": "ORDER-2", "cStoreCode": "ABCN001P012"}
	store := &fakeBojunRetailOrderWriter{existing: map[string]bool{}}
	var positions []int
	var skipped []bool
	service := &BojunOrderService{
		retailOrderDAO: store, rawDataDAO: &fakeBojunRawDataCreator{},
		pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
		skipPolicy: bojunPageTestSkipConfig{config: OrderPushSkipConfig{Targets: []orderpush.TargetSkipPolicy{
			{TargetCode: "qiantan", Cycle: 2, Skip: 1},
		}}},
		pushService: bojunPageTestPusher(func(order *model.BojunRetailOrder, position int, policy OrderPushSkipPolicy) bojunOrderPushResult {
			positions = append(positions, position)
			skipped = append(skipped, policy.ShouldSkip(position))
			if order.DocNo == "ORDER-1" {
				return bojunOrderPushResult{Error: errors.New("push failed")}
			}
			return bojunOrderPushResult{Success: true, Skipped: policy.ShouldSkip(position)}
		}),
	}

	result, err := runBojunAPIPageForTest(t, service, []map[string]interface{}{first}, []map[string]interface{}{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(positions, []int{1, 2}) || !reflect.DeepEqual(skipped, []bool{false, true}) {
		t.Fatalf("positions=%v skipped=%v", positions, skipped)
	}
	if result.TotalCount != 3 || result.RetailCount != 2 || result.ExistingCount != 1 || result.FailedCount != 1 ||
		len(result.FailedSamples) != 1 || result.FailedSamples[0].Status != "push_failed" || store.batchFindCalls != 2 {
		t.Fatalf("result=%+v batch reads=%d", result, store.batchFindCalls)
	}
}

func runBojunAPIPageForTest(t *testing.T, service *BojunOrderService, pages ...[]map[string]interface{}) (*BojunOrderSyncResult, error) {
	t.Helper()
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", configDir+string(os.PathSeparator))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Current int `json:"current"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Current < 1 || body.Current > len(pages) {
			http.Error(writer, "invalid page request", http.StatusBadRequest)
			return
		}
		records := pages[body.Current-1]
		_ = json.NewEncoder(writer).Encode(map[string]interface{}{
			"code": 200,
			"data": map[string]interface{}{
				"current": body.Current, "totalPage": len(pages), "total": len(records), "records": records,
			},
		})
	}))
	t.Cleanup(server.Close)
	t.Setenv("BOJUN_BASE_URL", server.URL)
	t.Setenv("BOJUN_APP_KEY", "test-app")
	t.Setenv("BOJUN_SECRET", "test-secret")
	return service.SyncOrders(t.Context(), "2026-11-05 10:00:00", "2026-11-05 11:00:00")
}
