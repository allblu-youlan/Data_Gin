package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/godror/godror"
)

const BojunStockView = "YL_DBS.V_FA_V_STORAGE1_BJ"

const bojunStockQuerySQL = `
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
FROM ` + BojunStockView + ` a
WHERE a.M_PRODUCT_ALIAS_NO = :1
  AND a.CODE = :2
ORDER BY a.C_STORE_ID,
         a.M_PRODUCTALIAS_ID,
         a.M_ATTRIBUTESETINSTANCE_ID
OFFSET :3 ROWS FETCH NEXT :4 ROWS ONLY`

type BojunStockQuery struct {
	ProductCode string
	StoreCode   string
	PageSize    int
	Offset      int
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
	query.ProductCode = strings.TrimSpace(query.ProductCode)
	query.StoreCode = strings.TrimSpace(query.StoreCode)
	if ctx == nil || query.ProductCode == "" || query.StoreCode == "" ||
		len(query.ProductCode) > 80 || len(query.StoreCode) > 80 ||
		query.PageSize < 1 || query.PageSize > 100 || query.Offset < 0 {
		return nil, fmt.Errorf("query bojun stocks: invalid query")
	}

	rows, err := adapter.db.QueryContext(ctx, bojunStockQuerySQL,
		query.ProductCode, query.StoreCode, query.Offset, query.PageSize+1, godror.NumberAsString(),
	)
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
