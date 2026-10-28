package data_ctrl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gin-biz-web-api/constant"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/data_svc"

	"github.com/gin-gonic/gin"
)

type fakeOpenBojunStockSnapshotService struct {
	request requestbody.OpenBojunStockSnapshotQueryRequest
	actor   uint
	calls   int
	err     error
}

func (service *fakeOpenBojunStockSnapshotService) Query(_ context.Context, actor uint, request requestbody.OpenBojunStockSnapshotQueryRequest) (*data_svc.OpenBojunStockSnapshotQueryDTO, error) {
	service.request, service.actor = request, actor
	service.calls++
	if service.err != nil {
		return nil, service.err
	}
	total := "9007199254740993.123456789"
	return &data_svc.OpenBojunStockSnapshotQueryDTO{
		Items:      []data_svc.OpenBojunStockSnapshotDTO{{ChangeDate: request.ChangeDate, StoreCode: "001", ProductCode: "SKU", Total: &total}},
		Pagination: data_svc.OpenBojunQueryPagination{Page: 1, PageSize: 50}, GeneratedAt: "2026-10-27 15:00:00",
	}, nil
}

func newBojunStockSnapshotControllerRouter(service *fakeOpenBojunStockSnapshotService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(constant.CurrentUserID, "17"); c.Next() })
	router.POST("/snapshots", NewOpenBojunStockSnapshotControllerWithService(service).Query)
	return router
}

func TestOpenBojunStockSnapshotControllerStrictJSONAndPublicFields(t *testing.T) {
	for _, test := range []struct{ query, body string }{
		{"?page=1", `{}`}, {"?", `{}`}, {"", `{"unknown":1}`}, {"", `{} {}`},
		{"", `null`}, {"", `[]`}, {"", `{"changeDate":20261027}`}, {"", `{"productId":["1) OR 1=1"]}`},
	} {
		service := &fakeOpenBojunStockSnapshotService{}
		response := performBojunQuery(newBojunStockSnapshotControllerRouter(service), "/snapshots"+test.query, test.body)
		if response.Code != 422 || service.calls != 0 {
			t.Fatalf("query=%s body=%s status=%d calls=%d", test.query, test.body, response.Code, service.calls)
		}
	}
	service := &fakeOpenBojunStockSnapshotService{}
	response := performBojunQuery(newBojunStockSnapshotControllerRouter(service), "/snapshots", `{"changeDate":"2026-10-27","storeId":10,"productId":[20],"productColorId":[30],"page":2,"pageSize":10}`)
	if response.Code != 200 || service.actor != 17 || service.request.ChangeDate != "2026-10-27" || service.request.StoreID != 10 || service.request.ProductID[0] != 20 || service.request.ProductColorID[0] != 30 || *service.request.Page != 2 || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("response=%s service=%+v", response.Body.String(), service)
	}
	var body struct {
		Data struct {
			Items       []map[string]any `json:"items"`
			Pagination  map[string]any   `json:"pagination"`
			GeneratedAt string           `json:"generatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	fields := []string{"changeDate", "storeCode", "storeName", "productCode", "productName", "productColor", "priceList", "total", "amtList", "qtyPreIn"}
	if len(body.Data.Items) != 1 || len(body.Data.Items[0]) != len(fields) || len(body.Data.Pagination) != 3 || body.Data.GeneratedAt == "" {
		t.Fatalf("unexpected public result: %s", response.Body.String())
	}
	for _, field := range fields {
		if _, exists := body.Data.Items[0][field]; !exists {
			t.Fatalf("missing %s", field)
		}
	}
	if body.Data.Items[0]["total"] != "9007199254740993.123456789" || body.Data.Items[0]["storeName"] != nil {
		t.Fatalf("decimal or null changed: %s", response.Body.String())
	}
}

func TestOpenBojunStockSnapshotControllerSafeErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{data_svc.ErrOpenBojunStockSnapshotForbidden, http.StatusForbidden},
		{data_svc.ErrOpenBojunStockSnapshotInvalidQuery, http.StatusUnprocessableEntity},
		{fmt.Errorf("%w: private Oracle detail", data_svc.ErrOpenBojunStockSnapshotUnavailable), http.StatusServiceUnavailable},
		{errors.New("private Oracle detail"), http.StatusServiceUnavailable},
	} {
		response := performBojunQuery(newBojunStockSnapshotControllerRouter(&fakeOpenBojunStockSnapshotService{err: test.err}), "/snapshots", `{}`)
		if response.Code != test.status || strings.Contains(response.Body.String(), "private Oracle") || !strings.Contains(response.Body.String(), "库存快照") {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
}
