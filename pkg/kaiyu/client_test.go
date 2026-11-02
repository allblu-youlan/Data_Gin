package kaiyu

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListShopsReturnsCompleteArrayAndSignsRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/newretail/api/open/orgInfo/getShopList" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		assertAuthentication(t, r)
		if len(r.URL.Query()) != 5 {
			t.Errorf("unexpected query fields: %v", r.URL.Query())
		}
		fmt.Fprint(w, `{"status":1,"code":null,"msg":null,"data":[{"shopid":"s1","shopname":"杭州店","shopno":"001","fullname":"幼岚,浙江","shopstatus":"1"},{"shopid":"s2","shopname":"上海店","shopno":"","fullname":"野选,上海","shopstatus":"0"}]}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	shops, err := client.ListShops(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(shops) != 2 || shops[0].ID != "s1" || shops[0].Name != "杭州店" || shops[0].Number != "001" || shops[0].FullName != "幼岚,浙江" || shops[1].Status != "0" {
		t.Fatalf("shops = %+v", shops)
	}
}

func TestHourlyPassengersUsesLocalDayAndDecodesMetrics(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/newretail/api/open/flow/getPassengerInfoByHour" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		assertAuthentication(t, r)
		if r.URL.Query().Get("shopId") != "" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("business parameters must be JSON body only")
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["shopId"] != "s1" || payload["startDate"] != "20261028" || payload["endDate"] != "20261028" || len(payload) != 3 {
			t.Errorf("payload = %+v", payload)
		}
		fmt.Fprint(w, `{"status":1,"data":[{"shopid":"s1","shopname":"杭州店","dateline":"2026-10-28 09:00","passby":63,"traffic":37,"trafficcount":35,"trafficbatch":29,"capturerate":0.587,"avgvistiduration":28.5,"newvisitorsratio":0.25,"deepvisitratio":0.1,"netsales":15000,"salestransactions":3,"atv":5000,"shopperyield":0.081}]}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	day := time.Date(2026, 10, 28, 0, 5, 0, 0, time.FixedZone("CST", 8*3600))
	rows, err := client.HourlyPassengers(context.Background(), "s1", day)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	want := HourlyPassenger{ShopID: "s1", ShopName: "杭州店", Dateline: "2026-10-28 09:00", Passby: 63, Traffic: 37, TrafficCount: 35, TrafficBatch: 29, CaptureRate: 0.587, AvgVisitDuration: 28.5, NewVisitorsRatio: 0.25, DeepVisitRatio: 0.1, NetSales: 15000, SalesTransactions: 3, ATV: 5000, ShopperYield: 0.081}
	if rows[0] != want {
		t.Fatalf("row = %+v, want %+v", rows[0], want)
	}
}

func TestClientRejectsFailedAndMalformedResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"business failure", 200, `{"status":-1,"code":1003,"msg":"signature invalid"}`, "1003"},
		{"string code", 200, `{"status":-1,"code":"1202","msg":"busy"}`, "1202"},
		{"server failure", 503, "upstream error", "503"},
		{"malformed JSON", 200, "not JSON", "decode"},
		{"missing data", 200, `{"status":1}`, "data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := testClient(t, server).ListShops(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestClientRedactsCredentialsAndPreservesCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"status":-1,"code":1003,"msg":"securityId=test-id securityCode=test-secret signature=%s"}`, r.URL.Query().Get("signature"))
	}))
	defer server.Close()
	client := testClient(t, server)
	_, err := client.ListShops(context.Background())
	if err == nil || strings.Contains(err.Error(), "test-id") || strings.Contains(err.Error(), "test-secret") {
		t.Fatal("business error must redact credentials")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.ListShops(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), "test-id") || strings.Contains(err.Error(), "test-secret") {
		t.Fatal("transport error must redact credentials")
	}
}

func TestNewClientValidatesConfiguration(t *testing.T) {
	t.Parallel()
	for _, cfg := range []Config{{}, {SecurityID: "id"}, {SecurityID: "id", SecurityCode: "secret", BaseURL: ":bad"}} {
		if _, err := NewClient(cfg); err == nil {
			t.Fatal("expected configuration error")
		}
	}
	client, err := NewClient(Config{SecurityID: "id", SecurityCode: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout <= 0 {
		t.Fatal("default HTTP client must have a timeout")
	}
}

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(Config{BaseURL: server.URL, SecurityID: "test-id", SecurityCode: "test-secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertAuthentication(t *testing.T, r *http.Request) {
	t.Helper()
	q := r.URL.Query()
	if q.Get("securityId") != "test-id" || q.Get("appId") != "30001" || q.Get("version") != "v1" || q.Get("timestamp") == "" {
		t.Error("missing authentication parameters")
	}
	mac := hmac.New(md5.New, []byte("test-secret"))
	mac.Write([]byte("appId=30001&securityId=test-id&timestamp=" + q.Get("timestamp") + "&version=v1"))
	if q.Get("signature") != hex.EncodeToString(mac.Sum(nil)) {
		t.Error("request signature does not match the documented HmacMD5 contract")
	}
}
