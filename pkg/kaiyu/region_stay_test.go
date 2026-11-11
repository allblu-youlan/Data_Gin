package kaiyu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestRegionStayByDayUsesInclusiveShanghaiDatesAndPreservesSourceMetrics(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/newretail/api/open/flow/getRegionStayByDay" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		assertAuthentication(t, r)
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(body, map[string]string{"shopId": "s1", "startDate": "20261101", "endDate": "20261130"}) {
			t.Errorf("body=%v", body)
		}
		fmt.Fprint(w, `{"status":1,"data":[{"dateline":"20261101","regions":[{"regionName":"入口","exposureCount":12,"stayCount":0,"stayRate":0,"avgStayTime":3.38}]}]}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	client.localLimiter = noWaitLimiter{}
	start := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
	items, err := client.RegionStayByDay(t.Context(), "s1", start, start.AddDate(0, 0, 29))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].Regions) != 1 || items[0].Regions[0].AvgStayTime != 3.38 || items[0].Regions[0].StayCount != 0 {
		t.Fatalf("items=%+v", items)
	}
}

func TestRegionStayByShopsSendsTwentyIDsAndAcceptsEmptyRegions(t *testing.T) {
	t.Parallel()
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprintf("s%d", i)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/newretail/api/open/flow/getRegionStayByShops" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		assertAuthentication(t, r)
		var body struct {
			Dateline string   `json:"dateline"`
			ShopIDs  []string `json:"shopIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Dateline != "20261103" || !reflect.DeepEqual(body.ShopIDs, ids) {
			t.Errorf("body=%+v", body)
		}
		fmt.Fprint(w, `{"status":1,"data":[{"shopid":"s0","shopname":"一店","regions":[]}]}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	client.localLimiter = noWaitLimiter{}
	items, err := client.RegionStayByShops(t.Context(), time.Date(2026, 11, 3, 0, 0, 0, 0, regionLocation), ids)
	if err != nil || len(items) != 1 || items[0].ShopID != "s0" || len(items[0].Regions) != 0 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestRegionStayRejectsInvalidRequestsBeforeHTTP(t *testing.T) {
	t.Parallel()
	client, err := NewClient(Config{SecurityID: "id", SecurityCode: "secret", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, regionLocation)
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"31 days", func() error {
			_, err := client.RegionStayByDay(t.Context(), "s1", day, day.AddDate(0, 0, 30))
			return err
		}},
		{"reverse range", func() error {
			_, err := client.RegionStayByDay(t.Context(), "s1", day, day.AddDate(0, 0, -1))
			return err
		}},
		{"missing shop", func() error { _, err := client.RegionStayByDay(t.Context(), "", day, day); return err }},
		{"missing date", func() error { _, err := client.RegionStayByDay(t.Context(), "s1", time.Time{}, day); return err }},
		{"21 shops", func() error { _, err := client.RegionStayByShops(t.Context(), day, make([]string, 21)); return err }},
		{"duplicate shops", func() error { _, err := client.RegionStayByShops(t.Context(), day, []string{"s1", "s1"}); return err }},
		{"empty shops", func() error { _, err := client.RegionStayByShops(t.Context(), day, nil); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("wanted validation error")
			}
		})
	}
}

func TestRegionStayRejectsUnrelatedDatesShopsAndMalformedMetrics(t *testing.T) {
	t.Parallel()
	metric := `{"regionName":"入口","exposureCount":10,"stayCount":1,"stayRate":0.1,"avgStayTime":2}`
	for _, tc := range []struct {
		name, body string
		byShops    bool
	}{
		{"unrequested date", `[{"dateline":"20261104","regions":[]}]`, false},
		{"duplicate date", `[{"dateline":"20261103","regions":[]},{"dateline":"20261103","regions":[]}]`, false},
		{"unrequested shop", `[{"shopid":"unrelated","regions":[]}]`, true},
		{"duplicate shop", `[{"shopid":"s1","regions":[]},{"shopid":"s1","regions":[]}]`, true},
		{"duplicate region", `[{"dateline":"20261103","regions":[` + metric + `,` + metric + `]}]`, false},
		{"missing metric", `[{"dateline":"20261103","regions":[{"regionName":"入口","exposureCount":10,"stayCount":1,"stayRate":0.1}]}]`, false},
		{"negative metric", `[{"dateline":"20261103","regions":[{"regionName":"入口","exposureCount":-1,"stayCount":1,"stayRate":0.1,"avgStayTime":2}]}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"status":1,"data":`+tc.body+`}`) }))
			defer server.Close()
			client := testClient(t, server)
			client.localLimiter = noWaitLimiter{}
			day := time.Date(2026, 11, 3, 0, 0, 0, 0, regionLocation)
			var err error
			if tc.byShops {
				_, err = client.RegionStayByShops(t.Context(), day, []string{"s1"})
			} else {
				_, err = client.RegionStayByDay(t.Context(), "s1", day, day)
			}
			if err == nil {
				t.Fatal("wanted response validation error")
			}
		})
	}
}

type noWaitLimiter struct{}

func (noWaitLimiter) Wait(ctx context.Context) error { return ctx.Err() }
