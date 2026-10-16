package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/godror/godror"
)

const BojunStockView = "YL_DBS.V_FA_V_STORAGE1_BJ"

const bojunStockSelectSQL = `
SELECT a.CODE,
       a.STORE_NAME,
       a.M_PRODUCT_ALIAS_NO,
       a.PROD_COLOR,
       a.M_PRODUCT_NAME,
       a.M_PRODUCT_VALUE,
       a.M_ATTR_VALUE1,
       a.M_ATTR_VALUE2_CODE,
       a.M_ATTR_VALUE2,
       a.PRICELIST,
       a.QTY,
       a.AMT_PRICEQTY,
       a.QTY_BAS,
       a.QTYPREOUT,
       a.QTY_BAS_PREOUT,
       a.AMT_PRICEQTYPREOUT,
       a.QTYPREIN,
       a.AMT_PRICEQTYPREIN,
       a.QTY_FREEZE,
       a.QTY_OMS,
       a.QTYVALID,
       a.AMT_PRICEQTYVALID,
       a.QTYCONSIGN,
       a.JST_PREOUT,
       a.JST_DIFF_QTY,
       a.QTYCAN,
       a.M_ATTRIBUTESETINSTANCE_ID,
       a.ISACTIVE
FROM ` + BojunStockView + ` a`

const bojunStockQuerySuffixSQL = `
ORDER BY a.C_STORE_ID,
         a.M_PRODUCTALIAS_ID,
         a.M_ATTRIBUTESETINSTANCE_ID
OFFSET :%d ROWS FETCH NEXT :%d ROWS ONLY`

type BojunStockQuery struct {
	ProductCode  []string
	ProductColor []string
	ProductName  []string
	StoreCode    string
	PageSize     int
	Offset       int
}

type BojunStockRow struct {
	StoreCode              string
	StoreName              *string
	ProductCode            string
	ProductColor           *string
	ProductName            *string
	Value                  *string
	Value1                 *string
	SizeCode               *string
	Value2                 *string
	PriceList              *string
	Qty                    *string
	AmtPriceQty            *string
	QtyBas                 *string
	QtyPreOut              *string
	QtyBasPreOut           *string
	AmtPriceQtyPreOut      *string
	QtyPreIn               *string
	AmtPriceQtyPreIn       *string
	QtyFreeze              *string
	QtyOms                 *string
	QtyValid               *string
	AmtPriceQtyValid       *string
	QtyConsign             *string
	JstPreOut              *string
	JstDiffQty             *string
	QtyCan                 *string
	AttributeSetInstanceID *int64
	IsActive               *string
}

func (adapter *Adapter) QueryBojunStocks(ctx context.Context, query BojunStockQuery) ([]BojunStockRow, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query bojun stocks: adapter is closed")
	}
	if ctx == nil {
		return nil, fmt.Errorf("query bojun stocks: invalid query")
	}
	statement, arguments, err := buildBojunStockQuery(query)
	if err != nil {
		return nil, err
	}
	arguments = append(arguments, godror.NumberAsString())
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query bojun stocks: %w", err)
	}
	defer rows.Close()

	result := make([]BojunStockRow, 0, query.PageSize+1)
	for rows.Next() {
		var storeCode, storeName, productCode, productColor, productName sql.NullString
		var value, value1, sizeCode, value2, priceList sql.NullString
		var qty, amtPriceQty, qtyBas, qtyPreOut, qtyBasPreOut, amtPriceQtyPreOut sql.NullString
		var qtyPreIn, amtPriceQtyPreIn, qtyFreeze, qtyOms, qtyValid, amtPriceQtyValid sql.NullString
		var qtyConsign, jstPreOut, jstDiffQty, qtyCan, isActive sql.NullString
		var attributeSetInstanceID sql.NullInt64
		if err := rows.Scan(
			&storeCode, &storeName, &productCode, &productColor, &productName,
			&value, &value1, &sizeCode, &value2, &priceList,
			&qty, &amtPriceQty, &qtyBas, &qtyPreOut, &qtyBasPreOut, &amtPriceQtyPreOut,
			&qtyPreIn, &amtPriceQtyPreIn, &qtyFreeze, &qtyOms, &qtyValid, &amtPriceQtyValid,
			&qtyConsign, &jstPreOut, &jstDiffQty, &qtyCan, &attributeSetInstanceID, &isActive,
		); err != nil {
			return nil, fmt.Errorf("scan bojun stock: %w", err)
		}
		result = append(result, BojunStockRow{
			StoreCode: strings.TrimSpace(storeCode.String), StoreName: nullableBojunString(storeName),
			ProductCode: strings.TrimSpace(productCode.String), ProductColor: nullableBojunString(productColor),
			ProductName: nullableBojunString(productName), Value: nullableBojunString(value),
			Value1: nullableBojunString(value1), SizeCode: nullableBojunString(sizeCode), Value2: nullableBojunString(value2),
			PriceList: nullableBojunString(priceList), Qty: nullableBojunString(qty), AmtPriceQty: nullableBojunString(amtPriceQty),
			QtyBas: nullableBojunString(qtyBas), QtyPreOut: nullableBojunString(qtyPreOut), QtyBasPreOut: nullableBojunString(qtyBasPreOut),
			AmtPriceQtyPreOut: nullableBojunString(amtPriceQtyPreOut), QtyPreIn: nullableBojunString(qtyPreIn),
			AmtPriceQtyPreIn: nullableBojunString(amtPriceQtyPreIn), QtyFreeze: nullableBojunString(qtyFreeze), QtyOms: nullableBojunString(qtyOms),
			QtyValid: nullableBojunString(qtyValid), AmtPriceQtyValid: nullableBojunString(amtPriceQtyValid),
			QtyConsign: nullableBojunString(qtyConsign), JstPreOut: nullableBojunString(jstPreOut),
			JstDiffQty: nullableBojunString(jstDiffQty), QtyCan: nullableBojunString(qtyCan),
			AttributeSetInstanceID: nullableInt64(attributeSetInstanceID), IsActive: nullableBojunString(isActive),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bojun stocks: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close bojun stocks: %w", err)
	}
	return result, nil
}

func buildBojunStockQuery(query BojunStockQuery) (string, []any, error) {
	query.StoreCode = strings.TrimSpace(query.StoreCode)
	if query.StoreCode == "" || len(query.StoreCode) > 80 ||
		query.PageSize < 1 || query.PageSize > 100 || query.Offset < 0 {
		return "", nil, fmt.Errorf("query bojun stocks: invalid query")
	}
	var productValues []string
	var productColumn string
	productFilters := 0
	for _, filter := range []struct {
		values []string
		column string
	}{
		{query.ProductCode, "a.M_PRODUCT_ALIAS_NO"},
		{query.ProductColor, "a.PROD_COLOR"},
		{query.ProductName, "a.M_PRODUCT_NAME"},
	} {
		if len(filter.values) > 100 {
			return "", nil, fmt.Errorf("query bojun stocks: invalid query")
		}
		if len(filter.values) > 0 {
			productFilters++
			productValues, productColumn = filter.values, filter.column
		}
	}
	if productFilters != 1 {
		return "", nil, fmt.Errorf("query bojun stocks: invalid query")
	}

	placeholders := make([]string, 0, len(productValues))
	arguments := make([]any, 0, len(productValues)+3)
	arguments = append(arguments, query.StoreCode)
	seen := make(map[string]struct{}, len(productValues))
	for _, value := range productValues {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 80 {
			return "", nil, fmt.Errorf("query bojun stocks: invalid query")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		placeholders = append(placeholders, ":"+strconv.Itoa(len(arguments)+1))
		arguments = append(arguments, value)
	}
	// godror binds unnamed arguments by SQL occurrence, so pagination follows the products.
	statement := bojunStockSelectSQL + "\nWHERE a.CODE = :1\n  AND " + productColumn +
		" IN (" + strings.Join(placeholders, ", ") + ")" +
		fmt.Sprintf(bojunStockQuerySuffixSQL, len(arguments)+1, len(arguments)+2)
	arguments = append(arguments, query.Offset, query.PageSize+1)
	return statement, arguments, nil
}
