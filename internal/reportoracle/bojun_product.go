package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const BojunProductView = "YL_DBS.V_BJ_PRODUCT"

const bojunProductByCodeSQL = `
SELECT PRICELIST,
       PRODUCT_NAME,
       VALUE,
       NO,
       PROD_COLOR,
       VALUE1,
       VALUE2
FROM ` + BojunProductView + `
WHERE NO = :1`

type BojunProductRow struct {
	PriceList   string
	ProductName string
	Value       string
	ProductCode string
	ProdColor   string
	Value1      string
	Value2      string
}

func (adapter *Adapter) QueryBojunProductByCode(
	ctx context.Context,
	productCode string,
) (*BojunProductRow, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query bojun product: adapter is closed")
	}
	productCode = strings.TrimSpace(productCode)
	if ctx == nil || productCode == "" {
		return nil, fmt.Errorf("query bojun product: invalid product code")
	}

	var priceList, productName, value, rowProductCode sql.NullString
	var prodColor, value1, value2 sql.NullString
	err := adapter.db.QueryRowContext(ctx, bojunProductByCodeSQL, productCode).Scan(
		&priceList,
		&productName,
		&value,
		&rowProductCode,
		&prodColor,
		&value1,
		&value2,
	)
	if err != nil {
		return nil, fmt.Errorf("query bojun product: %w", err)
	}
	return &BojunProductRow{
		PriceList: strings.TrimSpace(priceList.String), ProductName: strings.TrimSpace(productName.String),
		Value: strings.TrimSpace(value.String), ProductCode: strings.TrimSpace(rowProductCode.String),
		ProdColor: strings.TrimSpace(prodColor.String), Value1: strings.TrimSpace(value1.String),
		Value2: strings.TrimSpace(value2.String),
	}, nil
}
