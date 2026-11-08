package data_svc

import (
	"context"
	"fmt"
	"testing"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/youzan"

	"go.uber.org/zap"
)

type fakeYouzanDistributionClient struct {
	pages        [][]map[string]any
	decryptSizes []int
	timeFilters  []youzan.OrderTimeFilter
}

func (f *fakeYouzanDistributionClient) FetchOrderPage(_ context.Context, _ string, timeFilter youzan.OrderTimeFilter, _, _ string, pageNo, _ int) ([]map[string]any, bool, error) {
	f.timeFilters = append(f.timeFilters, timeFilter)
	if pageNo > len(f.pages) {
		return nil, false, nil
	}
	return f.pages[pageNo-1], pageNo < len(f.pages), nil
}

func (f *fakeYouzanDistributionClient) DecryptBatch(_ context.Context, _ string, sources []string) ([]string, error) {
	f.decryptSizes = append(f.decryptSizes, len(sources))
	result := make([]string, len(sources))
	for i, source := range sources {
		result[i] = "decrypted:" + source
	}
	return result, nil
}

type fakeYouzanDistributionWriter struct {
	batches  [][]model.YouzanDistributionOrder
	existing map[string]bool
}

func (f *fakeYouzanDistributionWriter) FindExistingTIDs(_ context.Context, tids []string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, tid := range tids {
		if f.existing[tid] {
			result[tid] = true
		}
	}
	return result, nil
}

func (f *fakeYouzanDistributionWriter) CreateBatchIfNotExists(_ context.Context, orders []model.YouzanDistributionOrder) (int64, error) {
	batch := append([]model.YouzanDistributionOrder(nil), orders...)
	f.batches = append(f.batches, batch)
	return int64(len(orders)), nil
}

func TestYouzanDistributionOrderServiceSyncsEveryPageAndDecryptsNickname(t *testing.T) {
	initYouzanDistributionTestLogger()
	client := &fakeYouzanDistributionClient{pages: [][]map[string]any{
		{distributionOrderFixture("D100", "encrypted-1")},
		{distributionOrderFixture("D200", "encrypted-2")},
	}}
	writer := &fakeYouzanDistributionWriter{}
	service := newYouzanDistributionOrderService(client, writer, func(context.Context) (string, error) { return "token", nil })

	result, err := service.SyncRange(context.Background(), youzan.OrderTimeFilterCreated, "2026-07-05 00:00:00", "2026-07-05 23:59:59")
	if err != nil {
		t.Fatalf("SyncRange() error = %v", err)
	}
	if result.FetchPages != 2 || result.TotalCount != 2 || result.SavedCount != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(writer.batches) != 2 || writer.batches[1][0].TID != "D200" {
		t.Fatalf("expected one persisted batch per page, got %#v", writer.batches)
	}
	if got := writer.batches[0][0].FansNickname; got != "decrypted:encrypted-1" {
		t.Fatalf("FansNickname = %q", got)
	}
	if got := writer.batches[0][0].FansNicknameEncrypted; got != "encrypted-1" {
		t.Fatalf("FansNicknameEncrypted = %q", got)
	}
	if fmt.Sprint(client.timeFilters) != "[created created]" || result.TimeFilter != youzan.OrderTimeFilterCreated {
		t.Fatalf("time filters = %v, result time filter = %q", client.timeFilters, result.TimeFilter)
	}
}

func TestYouzanDistributionOrderServicePreviewDecryptsAndDoesNotWrite(t *testing.T) {
	initYouzanDistributionTestLogger()
	client := &fakeYouzanDistributionClient{pages: [][]map[string]any{{
		distributionOrderFixture("D100", "encrypted-1"),
		distributionOrderFixture("D200", "encrypted-2"),
	}}}
	writer := &fakeYouzanDistributionWriter{existing: map[string]bool{"D200": true}}
	service := newYouzanDistributionOrderService(client, writer, func(context.Context) (string, error) { return "token", nil })

	result, err := service.PreviewRange(context.Background(), youzan.OrderTimeFilterSuccess, "2026-07-05 00:00:00", "2026-07-05 23:59:59")
	if err != nil {
		t.Fatalf("PreviewRange() error = %v", err)
	}
	if len(writer.batches) != 0 {
		t.Fatalf("preview must not write, got %d batches", len(writer.batches))
	}
	if result.WritableCount != 1 || result.ExistingCount != 1 || result.PreviewCount != 1 {
		t.Fatalf("unexpected preview result: %+v", result)
	}
	if len(result.Samples) != 2 || result.Samples[0].FansNickname != "decrypted:encrypted-1" {
		t.Fatalf("preview samples must include decrypted nickname: %#v", result.Samples)
	}
	if result.Samples[1].Status != "exists" {
		t.Fatalf("existing order sample status = %q", result.Samples[1].Status)
	}
	if fmt.Sprint(client.timeFilters) != "[success]" || result.TimeFilter != youzan.OrderTimeFilterSuccess {
		t.Fatalf("time filters = %v, result time filter = %q", client.timeFilters, result.TimeFilter)
	}
}

func TestYouzanDistributionOrderServiceSplitsDecryptRequestsAtTenThousand(t *testing.T) {
	initYouzanDistributionTestLogger()
	orders := make([]map[string]any, 10001)
	for i := range orders {
		orders[i] = distributionOrderFixture(fmt.Sprintf("D%d", i), fmt.Sprintf("$encrypted-%d$", i))
	}
	client := &fakeYouzanDistributionClient{pages: [][]map[string]any{orders}}
	service := newYouzanDistributionOrderService(client, &fakeYouzanDistributionWriter{}, func(context.Context) (string, error) { return "token", nil })

	if _, err := service.SyncRange(context.Background(), youzan.OrderTimeFilterCreated, "2026-07-05 00:00:00", "2026-07-05 23:59:59"); err != nil {
		t.Fatalf("SyncRange() error = %v", err)
	}
	if fmt.Sprint(client.decryptSizes) != "[10000 1]" {
		t.Fatalf("decrypt batch sizes = %v, want [10000 1]", client.decryptSizes)
	}
}

func initYouzanDistributionTestLogger() {
	if logger.Logger == nil {
		logger.Logger = zap.NewNop()
	}
}

func distributionOrderFixture(tid, nickname string) map[string]any {
	return map[string]any{
		"order_info": map[string]any{
			"tid":          tid,
			"status":       "TRADE_SUCCESS",
			"success_time": "2026-07-05 12:00:00",
		},
		"buyer_info": map[string]any{"fans_nickname": nickname},
		"orders":     []any{map[string]any{"title": "test"}},
	}
}
