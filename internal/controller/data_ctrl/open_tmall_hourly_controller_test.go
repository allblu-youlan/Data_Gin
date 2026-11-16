package data_ctrl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"

	"github.com/gin-gonic/gin"
)

func TestOpenTmallQztgHourlyControllerReturnsEmptyStringsWithoutChangingValues(t *testing.T) {
	roi, cost, productID := "0", "16.02", "90071992547409931234567890"
	service := &fakeOpenTmallQueryService{items: []reportoracle.TmallRow{{
		"ADD_CART_COST": nil,
		"BOOST_CLICK":   nil,
		"ACTUAL_ROI":    &roi,
		"COST":          &cost,
		"PRODUCT_ID":    &productID,
	}}}
	recorder := performOpenTmallControllerRequest(service, "/api/open/tmall/qztg/hourly/query", `{"batchNo":"202611041300"}`)
	var response struct {
		Code int `json:"code"`
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || response.Code != 0 || len(response.Data.Items) != 1 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	want := map[string]any{
		"ADD_CART_COST": "",
		"BOOST_CLICK":   "",
		"ACTUAL_ROI":    "0",
		"COST":          "16.02",
		"PRODUCT_ID":    productID,
	}
	if !reflect.DeepEqual(response.Data.Items[0], want) {
		t.Fatalf("row=%#v want=%#v", response.Data.Items[0], want)
	}
}

func TestOpenTmallHourlyControllerMapsBatchAndPagination(t *testing.T) {
	page, size := 2, 25
	want := requestbody.OpenTmallQueryRequest{
		BatchNo: "202611041300", Filters: map[string]string{"PRODUCT_ID": "9007199254740993"},
		Page: &page, PageSize: &size,
	}
	for _, test := range []struct {
		path    string
		dataset reportoracle.TmallDataset
	}{
		{"qztg", reportoracle.TmallQztgHourly},
		{"sycm", reportoracle.TmallSycmHourly},
	} {
		t.Run(test.path, func(t *testing.T) {
			service := &fakeOpenTmallQueryService{}
			response := performOpenTmallControllerRequest(service, "/api/open/tmall/"+test.path+"/hourly/query",
				`{"batchNo":"202611041300","filters":{"PRODUCT_ID":"9007199254740993"},"page":2,"pageSize":25}`)
			if response.Code != http.StatusOK || service.calls != 1 || service.actor != 17 || service.dataset != test.dataset || !reflect.DeepEqual(service.request, want) {
				t.Fatalf("status=%d service=%+v body=%s", response.Code, service, response.Body.String())
			}
			var payload struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(response.Body.String(), `"items":[]`) || len(payload.Data) != 2 || payload.Data["pagination"] == nil || payload.Data["columns"] != nil {
				t.Fatalf("unexpected response: %s", response.Body.String())
			}
		})
	}
}

func TestOpenTmallHourlyControllerRejectsWrongRequestShape(t *testing.T) {
	for _, test := range []struct{ name, query, body string }{
		{"daily dates", "", `{"batchNo":"202611041300","startDate":"2026-11-04","endDate":"2026-11-04"}`},
		{"numeric batch", "", `{"batchNo":202611041300}`},
		{"numeric product", "", `{"batchNo":"202611041300","filters":{"PRODUCT_ID":123}}`},
		{"query parameters", "?batchNo=202611041300", `{}`},
		{"null", "", `null`},
		{"trailing object", "", `{} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeOpenTmallQueryService{}
			response := performOpenTmallControllerRequest(service, "/api/open/tmall/sycm/hourly/query"+test.query, test.body)
			if response.Code != http.StatusUnprocessableEntity || service.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
			}
		})
	}
}

func TestOpenTmallHourlyControllerRequiresJSONContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		t.Run(contentType, func(t *testing.T) {
			service := &fakeOpenTmallQueryService{}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, "17"); c.Next() })
			router.POST("/query", NewOpenTmallControllerWithService(service).QuerySycmHourly)
			request := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(`{"batchNo":"202611041300"}`))
			request.Header.Set("Content-Type", contentType)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity || service.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
			}
		})
	}
}
