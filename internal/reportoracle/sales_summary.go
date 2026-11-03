package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SalesSummaryQuery struct {
	Start           time.Time
	End             time.Time
	StoreCode       string
	ConfirmedDocNos []string
	SkippedDocNos   []string
}

type SalesSummaryTotals struct {
	SalesAmount    float64
	SalesQuantity  float64
	ReturnQuantity float64
	OrderCount     int64
}

func (adapter *Adapter) QuerySalesSummary(ctx context.Context, query SalesSummaryQuery) (SalesSummaryTotals, error) {
	if adapter == nil || adapter.db == nil {
		return SalesSummaryTotals{}, fmt.Errorf("query sales summary: adapter is closed")
	}
	statement, arguments, err := buildSalesSummaryQuery(query)
	if err != nil {
		return SalesSummaryTotals{}, err
	}
	var amount, sales, returns sql.NullFloat64
	var count sql.NullInt64
	if err := adapter.db.QueryRowContext(ctx, statement, arguments...).Scan(&amount, &sales, &returns, &count); err != nil {
		return SalesSummaryTotals{}, fmt.Errorf("query sales summary: %w", err)
	}
	return SalesSummaryTotals{
		SalesAmount: amount.Float64, SalesQuantity: sales.Float64,
		ReturnQuantity: returns.Float64, OrderCount: count.Int64,
	}, nil
}

func buildSalesSummaryQuery(query SalesSummaryQuery) (string, []any, error) {
	storeCode := strings.TrimSpace(query.StoreCode)
	if storeCode == "" || query.Start.IsZero() || !query.End.After(query.Start) {
		return "", nil, fmt.Errorf("query sales summary: invalid store or date range")
	}
	arguments := []any{storeCode, query.Start, query.End}
	confirmed := salesSummaryDocNoPredicate(query.ConfirmedDocNos, false, &arguments)
	skipped := salesSummaryDocNoPredicate(query.SkippedDocNos, true, &arguments)
	pushed := "NVL(r.STATUS, '0') = '1'"
	if confirmed != "" {
		pushed += " OR " + confirmed
	}
	filter := "(" + pushed + ")"
	if skipped != "" {
		filter += "\n      AND " + skipped
	}
	return salesSummaryOrdersSQL + filter + salesSummaryTotalsSQL, arguments, nil
}

// Oracle permits at most 1000 expressions in one IN list. Multiple confirmed
// groups are alternatives; every skipped group must be excluded.
func salesSummaryDocNoPredicate(values []string, exclude bool, arguments *[]any) string {
	seen := make(map[string]struct{}, len(values))
	docNos := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		docNos = append(docNos, value)
	}
	if len(docNos) == 0 {
		return ""
	}
	operator, joiner := " IN (", " OR "
	if exclude {
		operator, joiner = " NOT IN (", " AND "
	}
	groups := make([]string, 0, (len(docNos)+999)/1000)
	for start := 0; start < len(docNos); start += 1000 {
		end := min(start+1000, len(docNos))
		placeholders := make([]string, 0, end-start)
		for _, docNo := range docNos[start:end] {
			*arguments = append(*arguments, docNo)
			placeholders = append(placeholders, ":"+strconv.Itoa(len(*arguments)))
		}
		groups = append(groups, "r.DOCNO"+operator+strings.Join(placeholders, ", ")+")")
	}
	return "(" + strings.Join(groups, joiner) + ")"
}

const salesSummaryOrdersSQL = `WITH orders AS (
    SELECT DISTINCT r.M_RETAIL_ID, r.DOCNO, r.RETAILSALETYPE, r.TOT_AMT_TS
    FROM ` + BojunRetailTable + ` r
    WHERE r.STORE_CODE = :1
      AND r.STATUSTIME >= :2
      AND r.STATUSTIME < :3
      AND EXISTS (
          SELECT 1 FROM ` + bojunRetailSourceHeadTable + ` h
          WHERE h.ID = r.M_RETAIL_ID AND h.STATUS = 2 AND h.ISACTIVE = 'Y'
      )
      AND `

// Item totals remain one row per order, so an order's pushed amount is never
// multiplied by its number of items. A missing quantity retains the existing
// Oracle order import's +1/-1 fallback without assuming another source column.
const salesSummaryTotalsSQL = `
), order_values AS (
    SELECT o.M_RETAIL_ID,
           NVL(UPPER(TRIM(o.RETAILSALETYPE)), 'CMR') AS sale_type,
           CASE WHEN UPPER(TRIM(o.RETAILSALETYPE)) = 'RET'
                THEN -ABS(NVL(o.TOT_AMT_TS, 0)) ELSE NVL(o.TOT_AMT_TS, 0) END AS sales_amount,
           CASE WHEN UPPER(TRIM(o.RETAILSALETYPE)) = 'RET' OR NVL(o.TOT_AMT_TS, 0) < 0
                THEN 1 ELSE 0 END AS is_refund
    FROM orders o
), item_totals AS (
    SELECT i.M_RETAIL_ID,
           COUNT(i.QTY) AS quantity_count,
           SUM(CASE WHEN i.QTY > 0 THEN i.QTY ELSE 0 END) AS positive_qty,
           SUM(CASE WHEN i.QTY < 0 THEN i.QTY ELSE 0 END) AS negative_qty
    FROM ` + bojunRetailItemTable + ` i
    WHERE i.ISACTIVE = 'Y'
      AND EXISTS (SELECT 1 FROM orders o WHERE o.M_RETAIL_ID = i.M_RETAIL_ID)
    GROUP BY i.M_RETAIL_ID
), per_order AS (
    SELECT o.sales_amount, o.is_refund,
           CASE WHEN NVL(i.quantity_count, 0) = 0
                THEN CASE WHEN o.is_refund = 1 THEN 0 ELSE 1 END
                WHEN o.is_refund = 1 AND o.sale_type <> 'EXP' THEN 0
                ELSE i.positive_qty END AS sales_quantity,
           CASE WHEN NVL(i.quantity_count, 0) = 0
                THEN CASE WHEN o.is_refund = 1 THEN -1 ELSE 0 END
                WHEN o.is_refund = 1 AND o.sale_type <> 'EXP'
                THEN -i.positive_qty + i.negative_qty
                ELSE i.negative_qty END AS return_quantity
    FROM order_values o
    LEFT JOIN item_totals i ON i.M_RETAIL_ID = o.M_RETAIL_ID
)
SELECT NVL(SUM(sales_amount), 0) AS sales_amount,
       NVL(SUM(sales_quantity), 0) AS sales_quantity,
       NVL(SUM(return_quantity), 0) AS return_quantity,
       NVL(SUM(CASE WHEN sales_quantity > 0 OR (is_refund = 0 AND return_quantity = 0)
                    THEN 1 ELSE 0 END), 0) AS order_count
FROM per_order`
