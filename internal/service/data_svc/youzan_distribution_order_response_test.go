package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/pkg/youzan"
)

func TestYouzanDistributionOrderServicePreservesNumericIDsFromHTTP(t *testing.T) {
	initYouzanDistributionTestLogger()
	for _, test := range []struct {
		name      string
		nodeKdtID int64
		rootKdtID int64
	}{
		{name: "eight digit shop identifiers", nodeKdtID: 12345678, rootKdtID: 87654321},
		{name: "integer precision above float64 exact range", nodeKdtID: 9007199254740993, rootKdtID: 9007199254740995},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/orders" {
					t.Errorf("unexpected request path %q", r.URL.Path)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"success":true,"code":200,"data":{"full_order_info_list":[{"full_order_info":{"order_info":{"tid":"D_NUMERIC_ID","node_kdt_id":%d,"root_kdt_id":%d},"buyer_info":{},"orders":[]}}],"paginator":{"has_next":false}}}`,
					test.nodeKdtID, test.rootKdtID)
			}))
			defer server.Close()
			client := youzan.NewDistributionClient(server.URL+"/orders", server.URL+"/decrypt", server.Client())
			writer := &fakeYouzanDistributionWriter{}
			service := newYouzanDistributionOrderService(client, writer, func(context.Context) (string, error) { return "test-token", nil })

			result, err := service.SyncRange(t.Context(), youzan.OrderTimeFilterCreated,
				"2026-11-05 00:00:00", "2026-11-05 23:59:59")
			if err != nil {
				t.Fatal(err)
			}
			if result.SavedCount != 1 || len(writer.batches) != 1 || len(writer.batches[0]) != 1 {
				t.Fatalf("SyncRange() result=%+v batches=%+v, want one written order", result, writer.batches)
			}
			row := writer.batches[0][0]
			if row.NodeKdtID != test.nodeKdtID || row.RootKdtID != test.rootKdtID {
				t.Errorf("stored shop IDs=(%d,%d), want (%d,%d)", row.NodeKdtID, row.RootKdtID, test.nodeKdtID, test.rootKdtID)
			}
			var raw struct {
				OrderInfo struct {
					NodeKdtID int64 `json:"node_kdt_id"`
					RootKdtID int64 `json:"root_kdt_id"`
				} `json:"order_info"`
			}
			if err := json.Unmarshal([]byte(row.RawContentJSON), &raw); err != nil {
				t.Fatal(err)
			}
			if raw.OrderInfo.NodeKdtID != test.nodeKdtID || raw.OrderInfo.RootKdtID != test.rootKdtID {
				t.Errorf("retained JSON shop IDs=(%d,%d), want (%d,%d)", raw.OrderInfo.NodeKdtID, raw.OrderInfo.RootKdtID, test.nodeKdtID, test.rootKdtID)
			}
		})
	}
}

func TestYouzanDistributionOrderServicePropagatesGatewayRateLimit(t *testing.T) {
	initYouzanDistributionTestLogger()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"gw_err_resp":{"err_code":4101,"err_msg":"请求太频繁，请控制请求频率或检查应用API账单是否欠费","trace_id":"yz-service-rate-limit"}}`)
	}))
	defer server.Close()
	client := youzan.NewDistributionClient(server.URL+"/orders", server.URL+"/decrypt", server.Client())
	writer := &fakeYouzanDistributionWriter{}
	service := newYouzanDistributionOrderService(client, writer, func(context.Context) (string, error) { return "test-token", nil })

	result, err := service.SyncRange(t.Context(), youzan.OrderTimeFilterCreated,
		"2026-11-05 00:00:00", "2026-11-05 23:59:59")
	if err == nil {
		t.Fatal("SyncRange() accepted a gateway rate limit as a successful empty page")
	}
	var hint interface{ RetryDelay() time.Duration }
	var delay time.Duration
	if errors.As(err, &hint) {
		delay = hint.RetryDelay()
	}
	if delay != time.Minute {
		t.Errorf("SyncRange() retry delay=%s, want 60s", delay)
	}
	for _, want := range []string{"code=4101", "trace_id=yz-service-rate-limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("SyncRange() error=%v, want %q", err, want)
		}
	}
	if result == nil || result.FetchPages != 0 || result.SavedCount != 0 || len(writer.batches) != 0 {
		t.Fatalf("rate limited result=%+v batches=%+v, want no fetched pages or writes", result, writer.batches)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("SyncRange() made %d HTTP requests, want one attempt", got)
	}
}
