package data_ctrl

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
)

func TestOpenTmallSycmOverviewHourlyControllerMapsRequest(t *testing.T) {
	page, size := 2, 25
	want := requestbody.OpenTmallQueryRequest{
		BatchNo: "202611131700", Filters: map[string]string{"SECTION_NAME": "首页", "DIMENSION_NAME": "实时"},
		Page: &page, PageSize: &size,
	}
	service := &fakeOpenTmallQueryService{}
	response := performOpenTmallControllerRequest(service, "/api/open/tmall/sycm/overview/hourly/query",
		`{"batchNo":"202611131700","filters":{"SECTION_NAME":"首页","DIMENSION_NAME":"实时"},"page":2,"pageSize":25}`)
	if response.Code != http.StatusOK || service.calls != 1 || service.actor != 17 ||
		service.dataset != reportoracle.TmallDataset("sycm_overview_hourly") || !reflect.DeepEqual(service.request, want) {
		t.Fatalf("status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"items":[]`) || strings.Contains(response.Body.String(), `"columns"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestOpenTmallSycmOverviewHourlyControllerPreservesSourceValues(t *testing.T) {
	amount, change, rate := "123456789012345678.90", "-0.29980000", "0.06050000"
	average, visitors, exported := "123.4567890123456789", "0", "2026-11-13 17:41:48"
	service := &fakeOpenTmallQueryService{rows: []reportoracle.TmallRow{{
		"PAYMENT_AMOUNT": &amount, "PAYMENT_AMOUNT_CHG": &change, "PAY_CONVERSION_RATE": &rate,
		"AVG_ORDER_VALUE": &average, "VISITOR_COUNT": &visitors, "EXPORT_TIME": &exported,
		"REFUND_AMOUNT": nil, "DATE_RANGE": nil,
	}}}
	response := performOpenTmallControllerRequest(service, "/api/open/tmall/sycm/overview/hourly/query", `{"batchNo":"202611131700"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || result.Code != 0 || len(result.Data.Items) != 1 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	want := map[string]any{
		"PAYMENT_AMOUNT": amount, "PAYMENT_AMOUNT_CHG": change, "PAY_CONVERSION_RATE": rate,
		"AVG_ORDER_VALUE": average, "VISITOR_COUNT": visitors, "EXPORT_TIME": exported,
		"REFUND_AMOUNT": "", "DATE_RANGE": "",
	}
	if !reflect.DeepEqual(result.Data.Items[0], want) {
		t.Fatalf("row=%#v want=%#v", result.Data.Items[0], want)
	}
}

func TestOpenTmallSycmOverviewHourlyControllerRejectsInvalidJSON(t *testing.T) {
	for _, test := range []struct{ name, query, body string }{
		{"daily dates", "", `{"batchNo":"202611131700","startDate":"2026-11-13","endDate":"2026-11-13"}`},
		{"realtime range", "", `{"batchNo":"202611131700","startTime":"2026-11-13 17:00:00"}`},
		{"numeric batch", "", `{"batchNo":202611131700}`},
		{"numeric filter", "", `{"batchNo":"202611131700","filters":{"SECTION_NAME":123}}`},
		{"unknown field", "", `{"batchNo":"202611131700","unknown":true}`},
		{"query parameters", "?batchNo=202611131700", `{}`},
		{"empty query parameters", "?", `{}`},
		{"null body", "", `null`},
		{"array body", "", `[]`},
		{"empty body", "", ``},
		{"malformed object", "", `{"batchNo":`},
		{"trailing object", "", `{} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenTmallQueryService{}
			response := performOpenTmallControllerRequest(service, "/api/open/tmall/sycm/overview/hourly/query"+test.query, test.body)
			if response.Code != http.StatusUnprocessableEntity || service.calls != 0 || !strings.Contains(response.Body.String(), `"code":100422`) {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
			}
		})
	}
}
