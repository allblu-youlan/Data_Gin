package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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

func TestQuerySalesSummaryBindsStoreAndDatesUsingToShopFlag(t *testing.T) {
	query := salesSummaryTestQuery()
	rows := salesSummaryTestRows([]driver.Value{380998.15, 2503.0, -66.0, int64(636)})
	calls := 0
	adapter := newBojunQueryTestAdapter(t, func(ctx context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
		calls++
		if ctx != t.Context() {
			t.Fatal("query did not propagate request context")
		}
		for _, fragment := range []string{
			"r.STORE_CODE = :1", "r.STATUSTIME >= :2", "r.STATUSTIME < :3",
			"r.IS_TOSHOP = 'Y'",
		} {
			if !strings.Contains(statement, fragment) {
				t.Fatalf("missing scoped sales filter %q: %s", fragment, statement)
			}
		}
		want := []any{query.StoreCode, query.Start, query.End}
		if len(arguments) != len(want) {
			t.Fatalf("bind count=%d want=%d", len(arguments), len(want))
		}
		for index, value := range want {
			if arguments[index].Value != value || arguments[index].Ordinal != index+1 {
				t.Fatalf("bind %d=%#v want=%#v", index, arguments[index], value)
			}
		}
		if strings.Contains(statement, query.StoreCode) {
			t.Fatal("query interpolated store code")
		}
		return rows, nil
	})
	got, err := adapter.QuerySalesSummary(t.Context(), query)
	want := SalesSummaryTotals{SalesAmount: 380998.15, SalesQuantity: 2503, ReturnQuantity: -66, OrderCount: 636}
	if err != nil || got != want || calls != 1 || !rows.closed {
		t.Fatalf("totals=%+v err=%v calls=%d closed=%v", got, err, calls, rows.closed)
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

func TestSalesSummaryDoesNotRestrictToCompletedPushesOrLocalDocuments(t *testing.T) {
	query := salesSummaryTestQuery()
	statement, arguments, err := buildSalesSummaryQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`\br\.STATUS\b`, `\br\.DOCNO\s+(NOT\s+)?IN\b`, `\bdelivery_logs\b`} {
		if regexp.MustCompile(forbidden).MatchString(statement) {
			t.Fatalf("summary still restricts IS_TOSHOP=Y orders using %q: %s", forbidden, statement)
		}
	}
	if len(arguments) != 3 {
		t.Fatalf("query has %d bindings, want only store and date range", len(arguments))
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
