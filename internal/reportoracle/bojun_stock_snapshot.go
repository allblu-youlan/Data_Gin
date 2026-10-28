package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/godror/godror"
)

const BojunStockSnapshotTable = "BOSNDS3.YL_RP_NC_HISSTORAGE01@link_to_bojun"

const bojunStockSnapshotGenerateSQL = `BEGIN YL_DBS.RP_NC_HISSTORAGE_GENERATE(:1, :2, :3, :4, :5); END;`

const bojunStockSnapshotMaxRows = 10000

type BojunStockSnapshotQuery struct {
	ChangeDate     int
	StoreID        int64
	ProductID      []int64
	ProductColorID []int64
}

type BojunStockSnapshotRow struct {
	ChangeDate   int
	StoreCode    string
	StoreName    *string
	ProductCode  string
	ProductName  *string
	ProductColor *string
	PriceList    *string
	Total        *string
	AmtList      *string
	QtyPreIn     *string
}

// The procedure truncates a shared result table. Callers must hold the datasource
// lock until these rows have been consumed; a transaction cannot isolate its DDL.
func (adapter *Adapter) QueryBojunStockSnapshot(ctx context.Context, query BojunStockSnapshotQuery) ([]BojunStockSnapshotRow, error) {
	if adapter == nil || adapter.db == nil || ctx == nil {
		return nil, fmt.Errorf("query bojun stock snapshot: invalid connection or context")
	}
	date := strconv.Itoa(query.ChangeDate)
	if parsed, err := time.Parse("20060102", date); err != nil || parsed.Format("20060102") != date {
		return nil, fmt.Errorf("query bojun stock snapshot: invalid date")
	}
	predicate, arguments, selections, err := buildBojunStockSnapshotQuery(query)
	if err != nil {
		return nil, err
	}
	connection, err := adapter.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open bojun stock snapshot session: %w", err)
	}
	defer connection.Close()

	var message string
	if _, err := connection.ExecContext(ctx, bojunStockSnapshotGenerateSQL,
		query.ChangeDate, selections[0], selections[1], selections[2], sql.Out{Dest: &message}); err != nil {
		return nil, fmt.Errorf("generate bojun stock snapshot: %w", err)
	}
	// The remote procedure catches exceptions, so Exec success alone is insufficient.
	if strings.TrimSpace(message) != "Y" {
		return nil, fmt.Errorf("generate bojun stock snapshot: procedure did not report success: %s", message)
	}
	statement := `SELECT h.CHANGEDATE, s.CODE, s.NAME AS STORE_NAME, pa.NO, p.NAME AS PRODUCT_NAME, pc.PROD_COLOR,
       h.PRICELIST, h.TOTAL, h.AMT_LIST, h.QTYPREIN
FROM ` + BojunStockSnapshotTable + ` h
JOIN BOSNDS3.C_STORE@link_to_bojun s ON s.ID = h.C_STORE_ID
JOIN BOSNDS3.M_PRODUCT_ALIAS@link_to_bojun pa ON pa.ID = h.M_PRODUCTALIAS_ID
JOIN BOSNDS3.M_PRODUCT@link_to_bojun p ON p.ID = h.M_PRODUCT_ID
JOIN BOSNDS3.M_PRODUCT_COLOR@link_to_bojun pc ON pc.ID = pa.M_PRODUCT_COLOR_ID
WHERE ` + predicate + fmt.Sprintf(` AND h.CHANGEDATE = :%d
ORDER BY h.C_STORE_ID, h.M_PRODUCTALIAS_ID, h.M_ATTRIBUTESETINSTANCE_ID
FETCH FIRST 10001 ROWS ONLY`, len(arguments)+1)
	arguments = append(arguments, query.ChangeDate, godror.NumberAsString(),
		godror.PrefetchCount(adapter.prefetchRows), godror.FetchArraySize(adapter.fetchArraySize))
	rows, err := connection.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("read bojun stock snapshot: %w", err)
	}
	defer rows.Close()
	result := make([]BojunStockSnapshotRow, 0)
	for rows.Next() {
		var row BojunStockSnapshotRow
		var storeName, productName, productColor, priceList, total, amtList, qtyPreIn sql.NullString
		if err := rows.Scan(&row.ChangeDate, &row.StoreCode, &storeName, &row.ProductCode, &productName, &productColor,
			&priceList, &total, &amtList, &qtyPreIn); err != nil {
			return nil, fmt.Errorf("scan bojun stock snapshot: %w", err)
		}
		if len(result) == bojunStockSnapshotMaxRows {
			return nil, fmt.Errorf("read bojun stock snapshot: too many rows; narrow product filters")
		}
		row.StoreCode, row.ProductCode = strings.TrimSpace(row.StoreCode), strings.TrimSpace(row.ProductCode)
		row.StoreName, row.ProductName, row.ProductColor = nullableBojunString(storeName), nullableBojunString(productName), nullableBojunString(productColor)
		row.PriceList, row.Total, row.AmtList, row.QtyPreIn = nullableBojunString(priceList), nullableBojunString(total), nullableBojunString(amtList), nullableBojunString(qtyPreIn)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bojun stock snapshot: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close bojun stock snapshot: %w", err)
	}
	return result, nil
}

func buildBojunStockSnapshotQuery(query BojunStockSnapshotQuery) (string, []any, [3]string, error) {
	var selections [3]string
	arguments := []any{query.StoreID}
	predicate := "h.C_STORE_ID = :1"
	for index, filter := range []struct {
		table  string
		column string
		ids    []int64
	}{
		{"C_STORE", "h.C_STORE_ID", []int64{query.StoreID}},
		{"M_PRODUCT", "h.M_PRODUCT_ID", query.ProductID},
		{"M_PRODUCT_COLOR", "pa.M_PRODUCT_COLOR_ID", query.ProductColorID},
	} {
		if len(filter.ids) == 0 || len(filter.ids) > 100 {
			return "", nil, selections, fmt.Errorf("bojun stock snapshot: each ID list must contain 1 to 100 IDs")
		}
		ids := make(map[int64]struct{}, len(filter.ids))
		placeholders := make([]string, 0, len(filter.ids))
		for _, id := range filter.ids {
			if id <= 0 || id > 9999999999 {
				return "", nil, selections, fmt.Errorf("bojun stock snapshot: invalid ID")
			}
			if _, exists := ids[id]; exists {
				continue
			}
			ids[id] = struct{}{}
			if index > 0 {
				arguments = append(arguments, id)
				placeholders = append(placeholders, ":"+strconv.Itoa(len(arguments)))
			}
		}
		var err error
		selections[index], err = bojunStockSnapshotIDSelection(filter.table, ids)
		if err != nil {
			return "", nil, selections, err
		}
		if index > 0 {
			predicate += " AND " + filter.column + " IN (" + strings.Join(placeholders, ", ") + ")"
		}
	}
	return predicate, arguments, selections, nil
}

func bojunStockSnapshotIDSelection(table string, ids map[int64]struct{}) (string, error) {
	if table != "C_STORE" && table != "M_PRODUCT" && table != "M_PRODUCT_COLOR" {
		return "", fmt.Errorf("bojun stock snapshot: invalid ID table")
	}
	values := make([]int64, 0, len(ids))
	for id := range ids {
		if id <= 0 {
			return "", fmt.Errorf("bojun stock snapshot: invalid ID")
		}
		values = append(values, id)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	encoded := make([]string, 0, len(values))
	for _, id := range values {
		encoded = append(encoded, strconv.FormatInt(id, 10))
	}
	// f_fast_table executes this SQL on the remote database. Only allowlisted
	// table names and validated numeric IDs may enter the selector.
	if len(encoded) == 0 || len(encoded) > 100 {
		return "", fmt.Errorf("bojun stock snapshot: invalid ID selection size")
	}
	return "SELECT ID FROM " + table + " WHERE ID IN (" + strings.Join(encoded, ",") + ")", nil
}
