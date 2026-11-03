package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func salesSummaryTestQuery() SalesSummaryQuery {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	return SalesSummaryQuery{Start: start, End: start.AddDate(0, 1, 0), StoreCode: "ABCN001P014"}
}

func salesSummaryTestRows(values ...[]driver.Value) *bojunQueryTestRows {
	return &bojunQueryTestRows{
		columns: []string{"SALES_AMOUNT", "SALES_QUANTITY", "RETURN_QUANTITY", "ORDER_COUNT"},
		values:  values,
	}
}

func TestQuerySalesSummaryBindsStoreDatesAndDeliveryEvidence(t *testing.T) {
	query := salesSummaryTestQuery()
	query.ConfirmedDocNos = []string{" SALE-1 ", "SALE-1", "SALE' OR 1=1 --"}
	query.SkippedDocNos = []string{"SKIP-1", " "}
	rows := salesSummaryTestRows([]driver.Value{380998.15, 2503.0, -66.0, int64(636)})
	calls := 0
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
		calls++
		if ctx != t.Context() {
			t.Fatal("query did not propagate request context")
		}
		for _, fragment := range []string{
			"r.STORE_CODE = :1", "r.STATUSTIME >= :2", "r.STATUSTIME < :3",
			"NVL(r.STATUS, '0') = '1' OR (r.DOCNO IN (:4, :5))",
			"AND (r.DOCNO NOT IN (:6))",
		} {
			if !strings.Contains(statement, fragment) {
				t.Fatalf("missing scoped evidence filter %q: %s", fragment, statement)
			}
		}
		want := []any{query.StoreCode, query.Start, query.End, "SALE-1", "SALE' OR 1=1 --", "SKIP-1"}
		if len(arguments) != len(want) {
			t.Fatalf("bind count=%d want=%d", len(arguments), len(want))
		}
		for index, value := range want {
			if arguments[index].Value != value || arguments[index].Ordinal != index+1 {
				t.Fatalf("bind %d=%#v want=%#v", index, arguments[index], value)
			}
		}
		if strings.Contains(statement, "ABCN001P014") || strings.Contains(statement, "SALE' OR 1=1 --") {
			t.Fatal("query interpolated store code or document number")
		}
		return rows, nil
	})
	got, err := adapter.QuerySalesSummary(t.Context(), query)
	want := SalesSummaryTotals{SalesAmount: 380998.15, SalesQuantity: 2503, ReturnQuantity: -66, OrderCount: 636}
	if err != nil || got != want || calls != 1 || !rows.closed {
		t.Fatalf("totals=%+v err=%v calls=%d closed=%v", got, err, calls, rows.closed)
	}
	if query.ConfirmedDocNos[0] != " SALE-1 " {
		t.Fatal("query mutated the caller's evidence")
	}
}

func TestSalesSummarySQLAggregatesItemsBeforeJoiningOrderAmounts(t *testing.T) {
	statement, _, err := buildSalesSummaryQuery(salesSummaryTestQuery())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"SELECT DISTINCT r.M_RETAIL_ID, r.DOCNO, r.RETAILSALETYPE, r.TOT_AMT_TS",
		"FROM " + BojunRetailTable + " r", "FROM " + bojunRetailSourceHeadTable + " h",
		"h.ID = r.M_RETAIL_ID", "h.STATUS = 2", "h.ISACTIVE = 'Y'",
		"FROM " + bojunRetailItemTable + " i", "i.ISACTIVE = 'Y'", "GROUP BY i.M_RETAIL_ID",
		"LEFT JOIN item_totals i ON i.M_RETAIL_ID = o.M_RETAIL_ID",
		"NVL(UPPER(TRIM(o.RETAILSALETYPE)), 'CMR') AS sale_type",
		"SUM(sales_amount)", "SUM(sales_quantity)", "SUM(return_quantity)",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("missing order-level aggregation %q: %s", fragment, statement)
		}
	}
	for _, forbidden := range []string{"JSON_ITEM", BojunProductView, bojunRetailPayItemTable, "MODIFIEDDATE", "TOT_QTY", " IN ()"} {
		if strings.Contains(statement, forbidden) {
			t.Fatalf("summary reads unnecessary or unknown source %q", forbidden)
		}
	}
}

func TestSalesSummaryDocumentFiltersSplitAtOracleInLimit(t *testing.T) {
	query := salesSummaryTestQuery()
	for index := range 1001 {
		query.ConfirmedDocNos = append(query.ConfirmedDocNos, fmt.Sprintf("CONFIRMED-%04d", index))
		query.SkippedDocNos = append(query.SkippedDocNos, fmt.Sprintf("SKIPPED-%04d", index))
	}
	statement, arguments, err := buildSalesSummaryQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	groups := regexp.MustCompile(`r\.DOCNO (NOT )?IN \(([^)]*)\)`).FindAllStringSubmatch(statement, -1)
	if len(groups) != 4 || len(arguments) != 2005 {
		t.Fatalf("groups=%d arguments=%d", len(groups), len(arguments))
	}
	for index, group := range groups {
		want := 1000
		if index%2 == 1 {
			want = 1
		}
		if count := strings.Count(group[2], ":"); count != want {
			t.Fatalf("IN group %d has %d bindings, want %d", index, count, want)
		}
	}
	if !strings.Contains(statement, ") OR r.DOCNO IN (") || !strings.Contains(statement, ") AND r.DOCNO NOT IN (") {
		t.Fatal("split inclusion/exclusion filters changed their boolean meaning")
	}
	placeholders := regexp.MustCompile(`:[0-9]+`).FindAllString(statement, -1)
	if len(placeholders) != len(arguments) {
		t.Fatalf("placeholders=%d arguments=%d", len(placeholders), len(arguments))
	}
	for index, placeholder := range placeholders {
		if placeholder != fmt.Sprintf(":%d", index+1) {
			t.Fatalf("placeholder %d=%s", index, placeholder)
		}
	}
}

func TestQuerySalesSummaryEmptyAggregateReturnsZeroTotals(t *testing.T) {
	rows := salesSummaryTestRows([]driver.Value{nil, nil, nil, nil})
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return rows, nil
	})
	got, err := adapter.QuerySalesSummary(t.Context(), salesSummaryTestQuery())
	if err != nil || got != (SalesSummaryTotals{}) || !rows.closed {
		t.Fatalf("empty totals=%+v error=%v closed=%v", got, err, rows.closed)
	}
}

func TestQuerySalesSummaryDoesNotTurnDatabaseErrorsIntoZeroSales(t *testing.T) {
	queryErr := errors.New("source unavailable")
	adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return nil, queryErr
	})
	if _, err := adapter.QuerySalesSummary(t.Context(), salesSummaryTestQuery()); !errors.Is(err, queryErr) {
		t.Fatalf("query error not preserved: %v", err)
	}
	for _, rows := range []*bojunQueryTestRows{
		salesSummaryTestRows(),
		salesSummaryTestRows([]driver.Value{"not-a-number", 2.0, -1.0, int64(1)}),
		{columns: []string{"SALES_AMOUNT", "SALES_QUANTITY", "RETURN_QUANTITY", "ORDER_COUNT"}, nextErr: queryErr},
	} {
		adapter := newBojunQueryTestAdapter(t, func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return rows, nil
		})
		if _, err := adapter.QuerySalesSummary(t.Context(), salesSummaryTestQuery()); err == nil || !rows.closed {
			t.Fatalf("missing/invalid result accepted: error=%v closed=%v", err, rows.closed)
		}
	}
}

func TestQuerySalesSummaryRejectsInvalidFilterBeforeQuery(t *testing.T) {
	query := salesSummaryTestQuery()
	var closed *Adapter
	if _, err := closed.QuerySalesSummary(t.Context(), query); err == nil {
		t.Fatal("closed adapter accepted")
	}
	adapter := &Adapter{db: &sql.DB{}}
	for _, invalid := range []SalesSummaryQuery{
		{Start: query.Start, End: query.End},
		{StoreCode: query.StoreCode, End: query.End},
		{StoreCode: query.StoreCode, Start: query.End, End: query.Start},
		{StoreCode: query.StoreCode, Start: query.Start, End: query.Start},
	} {
		if _, err := adapter.QuerySalesSummary(t.Context(), invalid); err == nil {
			t.Fatalf("invalid query accepted: %+v", invalid)
		}
	}
}
