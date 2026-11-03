package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type fakeSalesSummaryStore struct {
	called             bool
	code               string
	start, end         int
	confirmed, skipped []string
	evidenceErr        error
	weatherStart       time.Time
	weatherErr         error
}

func (store *fakeSalesSummaryStore) FindMall(_ context.Context, code string) (*model.Mall, error) {
	store.called = true
	return &model.Mall{BaseModel: model.BaseModel{ID: 3}, MallCode: code, NameCN: "上海浦东世博天地店", Timezone: "Asia/Shanghai"}, nil
}
func (store *fakeSalesSummaryStore) ReadPushEvidence(_ context.Context, code string, start, end int) ([]string, []string, error) {
	store.code, store.start, store.end = code, start, end
	return store.confirmed, store.skipped, store.evidenceErr
}
func (store *fakeSalesSummaryStore) ReadDailyWeather(_ context.Context, _ uint, start time.Time) ([]model.MallWeatherDaily, error) {
	store.weatherStart = start
	low, high := 16.0, 26.0
	return []model.MallWeatherDaily{{TemperatureMinC: &low, TemperatureMaxC: &high, Skycon: "LIGHT_RAIN"}}, store.weatherErr
}

type fakeSalesSummaryOracle struct {
	queries []reportoracle.SalesSummaryQuery
	totals  map[string]reportoracle.SalesSummaryTotals
	err     error
}

func (oracle *fakeSalesSummaryOracle) QuerySalesSummary(_ context.Context, query reportoracle.SalesSummaryQuery) (reportoracle.SalesSummaryTotals, error) {
	oracle.queries = append(oracle.queries, query)
	return oracle.totals[query.StoreCode], oracle.err
}

func TestSalesSummaryUsesOracleHistoryWithoutLocalOrders(t *testing.T) {
	store := &fakeSalesSummaryStore{}
	oracle := &fakeSalesSummaryOracle{totals: map[string]reportoracle.SalesSummaryTotals{
		"ABCN001P014": {SalesAmount: 380998, SalesQuantity: 2503, ReturnQuantity: -66, OrderCount: 636},
		"ABCN001A003": {SalesAmount: 480, SalesQuantity: 9, ReturnQuantity: -3, OrderCount: 3},
	}}
	scope := &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001P014"}}
	service := &SalesSummaryService{store: store, oracle: oracle, mallScope: scope,
		now: func() time.Time { return time.Date(2026, 11, 1, 17, 0, 0, 0, time.UTC) },
	}
	result, err := service.Query(t.Context(), 17, "abcn001p014", "2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.SalesAmount != 380998 || result.Metrics.OrderCount != 636 || result.Metrics.TotalQuantity != 2437 {
		t.Fatalf("Oracle historical totals were lost without local orders: %+v", result.Metrics)
	}
	if result.Metrics.AverageOrderValue != 599.05 || result.Metrics.AverageItemPrice != 599.05 || result.Metrics.ItemsPerOrder != 3.94 {
		t.Fatalf("incorrect derived metrics: %+v", result.Metrics)
	}
	if result.Metrics.DiscountRate < 80 || result.Metrics.DiscountRate > 83 {
		t.Fatalf("discount rate: %v", result.Metrics.DiscountRate)
	}
	if store.code != "ABCN001P014" || store.start != 20261001 || store.end != 20261031 {
		t.Fatalf("wrong local evidence filter: %+v", store)
	}
	query := oracle.queries[0]
	if query.StoreCode != "ABCN001P014" || query.Start.Format(time.RFC3339) != "2026-10-01T00:00:00+08:00" || query.End.Format(time.RFC3339) != "2026-11-01T00:00:00+08:00" {
		t.Fatalf("wrong source filter: %+v", query)
	}
	if result.Weather == nil || result.Weather.Date != "2026-11-02" {
		t.Fatalf("weather uses sales date: %+v", result.Weather)
	}
	scope.allowed = []string{"ABCN001A003"}
	result, err = service.Query(t.Context(), 17, "ABCN001A003", "2026-10-01", "2026-10-31")
	if err != nil || result.Metrics.SalesAmount != 480 || oracle.queries[1].StoreCode != "ABCN001A003" {
		t.Fatalf("switching malls reused another mall's totals: result=%+v err=%v", result, err)
	}
}

func TestSalesSummaryPassesEvidenceAndToleratesWeatherFailure(t *testing.T) {
	store := &fakeSalesSummaryStore{confirmed: []string{"PUSHED"}, skipped: []string{"SKIPPED"}, weatherErr: errors.New("weather unavailable")}
	oracle := &fakeSalesSummaryOracle{totals: map[string]reportoracle.SalesSummaryTotals{"ABCN001P014": {SalesAmount: 80, SalesQuantity: 2, OrderCount: 1}}}
	service := &SalesSummaryService{store: store, oracle: oracle, mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001P014"}}, now: time.Now}
	result, err := service.Query(t.Context(), 17, "ABCN001P014", "2026-10-01", "2026-10-31")
	if err != nil || result.Weather != nil || result.Metrics.SalesAmount != 80 {
		t.Fatalf("weather failure blocked sales: result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(oracle.queries[0].ConfirmedDocNos, store.confirmed) || !reflect.DeepEqual(oracle.queries[0].SkippedDocNos, store.skipped) {
		t.Fatalf("evidence not forwarded: %+v", oracle.queries[0])
	}
}

func TestSalesSummaryDependencyFailuresDoNotReturnZeroTotals(t *testing.T) {
	for _, test := range []struct {
		name                   string
		oracleErr, evidenceErr error
	}{
		{name: "source unavailable", oracleErr: errors.New("Oracle unavailable")},
		{name: "evidence unavailable", evidenceErr: errors.New("MySQL unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeSalesSummaryStore{evidenceErr: test.evidenceErr}
			oracle := &fakeSalesSummaryOracle{err: test.oracleErr}
			service := &SalesSummaryService{store: store, oracle: oracle, mallScope: &fakeBusinessOverviewMallScope{allowed: []string{"ABCN001P014"}}, now: time.Now}
			result, err := service.Query(t.Context(), 17, "ABCN001P014", "2026-10-01", "2026-10-31")
			if !errors.Is(err, ErrSalesSummaryUnavailable) || result != nil {
				t.Fatalf("failure returned a successful zero summary: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestSalesSummaryRejectsUnauthorizedMallAndInvalidDate(t *testing.T) {
	store := &fakeSalesSummaryStore{}
	oracle := &fakeSalesSummaryOracle{}
	service := &SalesSummaryService{store: store, oracle: oracle, mallScope: &fakeBusinessOverviewMallScope{err: auth_svc.ErrMallScopeForbidden}, now: time.Now}
	_, err := service.Query(t.Context(), 17, "ABCN001P014", "2026-10-01", "2026-10-31")
	if !errors.Is(err, ErrSalesSummaryForbidden) || store.called || len(oracle.queries) > 0 {
		t.Fatalf("unauthorized mall queried: %v", err)
	}
	for _, test := range []struct{ name, start, end string }{{"invalid day", "2026-02-30", "2026-03-01"}, {"reversed range", "2026-10-31", "2026-10-01"}} {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Query(t.Context(), 17, "ABCN001P014", test.start, test.end)
			if !errors.Is(err, ErrSalesSummaryInvalid) || store.called || len(oracle.queries) > 0 {
				t.Fatalf("invalid dates queried: %v", err)
			}
		})
	}
}

func TestSalesSummaryEmptySourceTotalsKeepZeroRatios(t *testing.T) {
	metrics := SalesSummaryMetrics{}
	finishSalesSummaryMetrics(&metrics)
	if metrics.AverageOrderValue != 0 || metrics.AverageItemPrice != 0 || metrics.ItemsPerOrder != 0 {
		t.Fatalf("empty ratios: %+v", metrics)
	}
}

type salesSummaryEvidenceConnector struct {
	query *string
	args  *[]driver.NamedValue
}

func (c salesSummaryEvidenceConnector) Connect(context.Context) (driver.Conn, error) {
	return salesSummaryEvidenceConn{c}, nil
}
func (salesSummaryEvidenceConnector) Driver() driver.Driver { return salesSummaryEvidenceDriver{} }

type salesSummaryEvidenceDriver struct{}

func (salesSummaryEvidenceDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type salesSummaryEvidenceConn struct{ salesSummaryEvidenceConnector }

func (salesSummaryEvidenceConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (salesSummaryEvidenceConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }
func (salesSummaryEvidenceConn) Close() error              { return nil }
func (c salesSummaryEvidenceConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	*c.query = query
	*c.args = args
	return &salesSummaryEvidenceRows{rows: [][]driver.Value{{"CONFIRMED", int64(1), int64(0)}, {"SKIPPED", int64(0), int64(1)}, {"RETRIED", int64(1), int64(1)}}}, nil
}

type salesSummaryEvidenceRows struct {
	rows  [][]driver.Value
	index int
}

func (*salesSummaryEvidenceRows) Columns() []string { return []string{"docno", "confirmed", "skipped"} }
func (*salesSummaryEvidenceRows) Close() error      { return nil }
func (r *salesSummaryEvidenceRows) Next(dest []driver.Value) error {
	if r.index == len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func TestSalesSummaryReadPushEvidencePrefersRealSuccessOverPolicySkip(t *testing.T) {
	var query string
	var args []driver.NamedValue
	connection := sql.OpenDB(salesSummaryEvidenceConnector{query: &query, args: &args})
	t.Cleanup(func() { connection.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: connection, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	store := salesSummaryDBStore{db: db}
	confirmed, skipped, err := store.ReadPushEvidence(t.Context(), "ABCN001P014", 20261001, 20261031)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(confirmed, []string{"CONFIRMED", "RETRIED"}) || !reflect.DeepEqual(skipped, []string{"SKIPPED"}) {
		t.Fatalf("incorrect evidence classification: confirmed=%v skipped=%v", confirmed, skipped)
	}
	query = strings.ReplaceAll(query, "`", "")
	for _, part := range []string{"orders.c_store_code = ? AND orders.billdate >= ? AND orders.billdate <= ?", "sent.clean_record_id = orders.id", "sent.source_code = 'bojun_order'", "sent.destination_id = 0", "sent.dataset_kind = 'retail_order'", "sent.success = TRUE", "COALESCE(sent.response_body, '') <> 'skipped_by_order_push_policy'", "sent.response_body = 'skipped_by_order_push_policy'", "GROUP BY orders.docno"} {
		if !strings.Contains(query, part) {
			t.Fatalf("missing evidence constraint %q: %s", part, query)
		}
	}
	if len(args) != 3 || args[0].Value != "ABCN001P014" || args[1].Value != int64(20261001) || args[2].Value != int64(20261031) {
		t.Fatalf("wrong query bindings: %+v", args)
	}
}
