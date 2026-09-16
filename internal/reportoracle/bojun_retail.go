package reportoracle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/godror/godror"
)

const (
	BojunRetailTable           = "YL_DBS.BJ_REPORT_RETAIL_SF"
	bojunRetailItemTable       = "BOSNDS3.M_RETAILITEM@LINK_TO_BOJUN"
	bojunRetailPayItemTable    = "BOSNDS3.M_RETAILPAYITEM@LINK_TO_BOJUN"
	bojunRetailPaywayTable     = "BOSNDS3.C_PAYWAY@LINK_TO_BOJUN"
	bojunRetailSourceHeadTable = "BOSNDS3.M_RETAIL@LINK_TO_BOJUN"
	maxBojunRetailBatchSize    = 1000
)

type BojunRetailRow struct {
	RetailID       uint64
	StoreCode      string
	StoreName      string
	DocNo          string
	RetailSaleType string
	StatusTime     time.Time
	OrderPhone     string
	PaidAmount     float64
	PushAmount     float64
	IsToShop       string
	PushStatus     int
	ItemsJSON      string
	PayItemsJSON   string
}

const bojunRetailProjectedColumns = `
	M_RETAIL_ID, STORE_CODE, STORE_NAME, DOCNO, RETAILSALETYPE, STATUSTIME,
	DM_VP_C_VIP_MOBILE, TOT_AMT_SF, TOT_AMT_TS, IS_TOSHOP,
	STATUS`

const bojunRetailSourceColumns = `
	r.M_RETAIL_ID, r.STORE_CODE, r.STORE_NAME, r.DOCNO,
	r.RETAILSALETYPE, r.STATUSTIME, r.DM_VP_C_VIP_MOBILE,
	r.TOT_AMT_SF, r.TOT_AMT_TS, r.IS_TOSHOP,
	NVL(r.STATUS, '0') AS STATUS`

const bojunRetailAfterIDSQL = `
SELECT ` + bojunRetailProjectedColumns + `
FROM (
    SELECT ` + bojunRetailSourceColumns + `
    FROM ` + BojunRetailTable + ` r
    JOIN ` + bojunRetailSourceHeadTable + ` h ON h.ID = r.M_RETAIL_ID
    WHERE h.STATUS = 2
      AND h.ISACTIVE = 'Y'
      AND r.M_RETAIL_ID > :1
    ORDER BY r.M_RETAIL_ID
)
WHERE ROWNUM <= :2
ORDER BY M_RETAIL_ID`

const bojunRetailStatusTimeRangeSQL = `
SELECT ` + bojunRetailProjectedColumns + `
FROM (
    SELECT ` + bojunRetailSourceColumns + `
    FROM ` + BojunRetailTable + ` r
    JOIN ` + bojunRetailSourceHeadTable + ` h ON h.ID = r.M_RETAIL_ID
    WHERE h.STATUS = 2
      AND h.ISACTIVE = 'Y'
      AND r.STATUSTIME >= :1
      AND r.STATUSTIME < :2
      AND r.M_RETAIL_ID > :3
    ORDER BY r.M_RETAIL_ID
)
WHERE ROWNUM <= :4
ORDER BY M_RETAIL_ID`

const bojunRetailMaxIDSQL = `
SELECT NVL(MAX(r.M_RETAIL_ID), 0)
FROM ` + BojunRetailTable + ` r
JOIN ` + bojunRetailSourceHeadTable + ` h ON h.ID = r.M_RETAIL_ID
WHERE h.STATUS = 2
  AND h.ISACTIVE = 'Y'`

const bojunRetailPushStatusSQL = `
UPDATE ` + BojunRetailTable + `
SET STATUS = :1, PUSH_DATE = :2
WHERE M_RETAIL_ID = :3`

const bojunRetailItemsSelectSQL = `
SELECT a.M_RETAIL_ID,
       a.DOCNO,
       b.TYPE,
       b.MARKDIS,
       c.PRODUCT_NAME,
       c.VALUE AS PRODUCT_VALUE,
       c.PROD_COLOR,
       c.NO,
       c.VALUE1,
       c.VALUE2,
       b.QTY,
       b.DM_AMT_RETAIL,
       b.TOT_AMT_ACTUAL,
       b.TOT_AMT_LIST,
       b.AMT_ACC,
       b.TOT_AMT_ACC,
       b.DISCOUNT,
       b.PRICELIST,
       b.PRICEACTUAL
FROM ` + BojunRetailTable + ` a
JOIN ` + bojunRetailItemTable + ` b ON a.M_RETAIL_ID = b.M_RETAIL_ID
JOIN ` + BojunProductView + ` c ON b.M_PRODUCTALIAS_ID = c.M_PRODUCTALIAS_ID`

const bojunRetailItemsSQLPrefix = bojunRetailItemsSelectSQL + `
WHERE b.ISACTIVE = 'Y'
  AND a.M_RETAIL_ID IN (`

const bojunRetailItemsSQLSuffix = `)
ORDER BY a.M_RETAIL_ID, c.NO`

const bojunRetailPayItemsFromSQL = `
FROM ` + bojunRetailPayItemTable + ` a
JOIN ` + bojunRetailPaywayTable + ` k ON k.ID = a.C_PAYWAY_ID
LEFT JOIN ` + bojunRetailSourceHeadTable + ` b ON a.M_RETAIL_ID = b.ID
WHERE a.ISACTIVE = 'Y'
  AND b.STATUS = 2
  AND b.ISACTIVE = 'Y'`

const bojunRetailPayItemsSQLPrefix = `
SELECT b.ID AS M_RETAIL_ID,
       a.C_PAYWAY_ID,
       k.NAME AS C_PAYWAY_NAME,
       SUM(a.PAYAMOUNT) AS TOT_AMT_CX` + bojunRetailPayItemsFromSQL + `
  AND b.ID IN (`

const bojunRetailPayItemsSQLSuffix = `)
GROUP BY b.ID, a.C_PAYWAY_ID, k.NAME
ORDER BY b.ID, a.C_PAYWAY_ID`

type bojunRetailItem struct {
	RetailID     uint64   `json:"-"`
	DocNo        string   `json:"docno"`
	Type         *float64 `json:"type"`
	MarkDis      *float64 `json:"markdis"`
	ProductName  string   `json:"mProductName"`
	ProductValue string   `json:"productValue"`
	ProductColor string   `json:"prodColor"`
	No           string   `json:"no"`
	Value1       string   `json:"value1"`
	Value2       string   `json:"value2"`
	Qty          *float64 `json:"qty"`
	DMAmtRetail  *float64 `json:"dmAmtRetail"`
	TotAmtActual *float64 `json:"totAmtActual"`
	TotAmtList   *float64 `json:"totAmtList"`
	AmtAcc       *float64 `json:"amtAcc"`
	TotAmtAcc    *float64 `json:"totAmtAcc"`
	Discount     *float64 `json:"discount"`
	PriceList    *float64 `json:"pricelist"`
	PriceActual  *float64 `json:"priceactual"`
}

type bojunRetailPayItem struct {
	RetailID   uint64   `json:"-"`
	PaywayID   int64    `json:"cPaywayId"`
	PaywayName string   `json:"cPaywayName"`
	PayAmount  *float64 `json:"payamount"`
}

func (adapter *Adapter) QueryBojunRetailAfterID(ctx context.Context, afterID uint64, limit int) ([]BojunRetailRow, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query bojun Oracle retail orders: adapter is closed")
	}
	if err := validateBojunRetailBatchSize(limit); err != nil {
		return nil, err
	}
	return adapter.queryBojunRetailRows(ctx, bojunRetailAfterIDSQL, afterID, limit)
}

func (adapter *Adapter) QueryBojunRetailByStatusTime(
	ctx context.Context,
	start time.Time,
	end time.Time,
	afterID uint64,
	limit int,
) ([]BojunRetailRow, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query bojun Oracle retail orders by time: adapter is closed")
	}
	if start.IsZero() || !end.After(start) {
		return nil, fmt.Errorf("query bojun Oracle retail orders by time: invalid time range")
	}
	if err := validateBojunRetailBatchSize(limit); err != nil {
		return nil, err
	}
	return adapter.queryBojunRetailRows(ctx, bojunRetailStatusTimeRangeSQL, start, end, afterID, limit)
}

func (adapter *Adapter) MaxBojunRetailID(ctx context.Context) (uint64, error) {
	if adapter == nil || adapter.db == nil {
		return 0, fmt.Errorf("query bojun Oracle maximum retail id: adapter is closed")
	}
	var value int64
	if err := adapter.db.QueryRowContext(ctx, bojunRetailMaxIDSQL).Scan(&value); err != nil {
		return 0, fmt.Errorf("query bojun Oracle maximum retail id: %w", err)
	}
	if value < 0 {
		return 0, fmt.Errorf("query bojun Oracle maximum retail id: negative value")
	}
	return uint64(value), nil
}

func (adapter *Adapter) UpdateBojunRetailPushStatus(ctx context.Context, retailID uint64, success bool, pushDate int) error {
	if adapter == nil || adapter.db == nil {
		return fmt.Errorf("update bojun Oracle push status: adapter is closed")
	}
	if retailID == 0 {
		return fmt.Errorf("update bojun Oracle push status: retail id is required")
	}
	if pushDate < 10000101 || pushDate > 99991231 {
		return fmt.Errorf("update bojun Oracle push status: push date must use yyyyMMdd")
	}
	status := 0
	if success {
		status = 1
	}
	result, err := adapter.db.ExecContext(ctx, bojunRetailPushStatusSQL, status, pushDate, retailID)
	if err != nil {
		return fmt.Errorf("update bojun Oracle push status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update bojun Oracle push status rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("update bojun Oracle push status: retail id %d not found", retailID)
	}
	return nil
}

func (adapter *Adapter) queryBojunRetailRows(ctx context.Context, statement string, arguments ...interface{}) ([]BojunRetailRow, error) {
	arguments = append(arguments,
		godror.PrefetchCount(adapter.prefetchRows),
		godror.FetchArraySize(adapter.fetchArraySize),
	)
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query bojun Oracle retail orders: %w", err)
	}
	defer rows.Close()

	result := make([]BojunRetailRow, 0)
	for rows.Next() {
		row, err := scanBojunRetailRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bojun Oracle retail orders: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close bojun Oracle retail orders: %w", err)
	}
	if err := adapter.attachBojunRetailItems(ctx, result); err != nil {
		return nil, err
	}
	if err := adapter.attachBojunRetailPayItems(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

type bojunRetailScanner interface {
	Scan(dest ...interface{}) error
}

func scanBojunRetailRow(scanner bojunRetailScanner) (BojunRetailRow, error) {
	var (
		retailID                                int64
		storeCode, storeName, docNo, retailType sql.NullString
		statusTime                              sql.NullTime
		orderPhone, isToShop                    sql.NullString
		paidAmount, pushAmount                  sql.NullFloat64
		pushStatus                              sql.NullInt64
	)
	if err := scanner.Scan(
		&retailID, &storeCode, &storeName, &docNo, &retailType, &statusTime,
		&orderPhone, &paidAmount, &pushAmount, &isToShop,
		&pushStatus,
	); err != nil {
		return BojunRetailRow{}, fmt.Errorf("scan bojun Oracle retail order: %w", err)
	}
	if retailID <= 0 || strings.TrimSpace(docNo.String) == "" || !statusTime.Valid {
		return BojunRetailRow{}, fmt.Errorf("scan bojun Oracle retail order: required field is empty")
	}
	return BojunRetailRow{
		RetailID: uint64(retailID), StoreCode: strings.TrimSpace(storeCode.String), StoreName: strings.TrimSpace(storeName.String),
		DocNo:          strings.TrimSpace(docNo.String),
		RetailSaleType: strings.TrimSpace(retailType.String), StatusTime: statusTime.Time,
		OrderPhone: strings.TrimSpace(orderPhone.String), PaidAmount: paidAmount.Float64, PushAmount: pushAmount.Float64,
		IsToShop: strings.ToUpper(strings.TrimSpace(isToShop.String)), PushStatus: int(pushStatus.Int64),
	}, nil
}

func (adapter *Adapter) attachBojunRetailItems(ctx context.Context, orders []BojunRetailRow) error {
	if len(orders) == 0 {
		return nil
	}
	statement, arguments, err := buildBojunRetailItemsQuery(orders)
	if err != nil {
		return err
	}
	queryArguments := append(arguments,
		godror.PrefetchCount(adapter.prefetchRows),
		godror.FetchArraySize(adapter.fetchArraySize),
	)
	rows, err := adapter.db.QueryContext(ctx, statement, queryArguments...)
	if err != nil {
		return fmt.Errorf("query bojun Oracle retail items: %w", err)
	}
	defer rows.Close()

	itemsByRetailID := make(map[uint64][]bojunRetailItem, len(orders))
	for rows.Next() {
		item, scanErr := scanBojunRetailItem(rows)
		if scanErr != nil {
			return scanErr
		}
		itemsByRetailID[item.RetailID] = append(itemsByRetailID[item.RetailID], item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate bojun Oracle retail items: %w", err)
	}
	return setBojunRetailItemsJSON(orders, itemsByRetailID)
}

func setBojunRetailItemsJSON(orders []BojunRetailRow, itemsByRetailID map[uint64][]bojunRetailItem) error {
	for index := range orders {
		items := itemsByRetailID[orders[index].RetailID]
		if items == nil {
			items = []bojunRetailItem{}
		}
		encoded, marshalErr := json.Marshal(items)
		if marshalErr != nil {
			return fmt.Errorf("marshal bojun Oracle retail items %d: %w", orders[index].RetailID, marshalErr)
		}
		orders[index].ItemsJSON = string(encoded)
	}
	return nil
}

func buildBojunRetailItemsQuery(orders []BojunRetailRow) (string, []interface{}, error) {
	placeholders, arguments, err := buildBojunRetailIDBindings(orders, "items")
	if err != nil {
		return "", nil, err
	}
	return bojunRetailItemsSQLPrefix + placeholders + bojunRetailItemsSQLSuffix, arguments, nil
}

func (adapter *Adapter) attachBojunRetailPayItems(ctx context.Context, orders []BojunRetailRow) error {
	if len(orders) == 0 {
		return nil
	}
	statement, arguments, err := buildBojunRetailPayItemsQuery(orders)
	if err != nil {
		return err
	}
	queryArguments := append(arguments,
		godror.PrefetchCount(adapter.prefetchRows),
		godror.FetchArraySize(adapter.fetchArraySize),
	)
	rows, err := adapter.db.QueryContext(ctx, statement, queryArguments...)
	if err != nil {
		return fmt.Errorf("query bojun Oracle retail pay items: %w", err)
	}
	defer rows.Close()

	itemsByRetailID := make(map[uint64][]bojunRetailPayItem, len(orders))
	for rows.Next() {
		item, scanErr := scanBojunRetailPayItem(rows)
		if scanErr != nil {
			return scanErr
		}
		itemsByRetailID[item.RetailID] = append(itemsByRetailID[item.RetailID], item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate bojun Oracle retail pay items: %w", err)
	}
	return setBojunRetailPayItemsJSON(orders, itemsByRetailID)
}

func setBojunRetailPayItemsJSON(orders []BojunRetailRow, itemsByRetailID map[uint64][]bojunRetailPayItem) error {
	for index := range orders {
		items := itemsByRetailID[orders[index].RetailID]
		if items == nil {
			items = []bojunRetailPayItem{}
		}
		encoded, marshalErr := json.Marshal(items)
		if marshalErr != nil {
			return fmt.Errorf("marshal bojun Oracle retail pay items %d: %w", orders[index].RetailID, marshalErr)
		}
		orders[index].PayItemsJSON = string(encoded)
	}
	return nil
}

func buildBojunRetailPayItemsQuery(orders []BojunRetailRow) (string, []interface{}, error) {
	placeholders, arguments, err := buildBojunRetailIDBindings(orders, "pay items")
	if err != nil {
		return "", nil, err
	}
	return bojunRetailPayItemsSQLPrefix + placeholders + bojunRetailPayItemsSQLSuffix, arguments, nil
}

func buildBojunRetailIDBindings(orders []BojunRetailRow, detailName string) (string, []interface{}, error) {
	if len(orders) == 0 || len(orders) > maxBojunRetailBatchSize {
		return "", nil, fmt.Errorf("query bojun Oracle retail %s: order count must be between 1 and %d", detailName, maxBojunRetailBatchSize)
	}
	placeholders := make([]string, 0, len(orders))
	arguments := make([]interface{}, 0, len(orders))
	seen := make(map[uint64]struct{}, len(orders))
	for _, order := range orders {
		if order.RetailID == 0 {
			return "", nil, fmt.Errorf("query bojun Oracle retail %s: retail id is required", detailName)
		}
		if _, exists := seen[order.RetailID]; exists {
			continue
		}
		seen[order.RetailID] = struct{}{}
		placeholders = append(placeholders, ":"+strconv.Itoa(len(placeholders)+1))
		arguments = append(arguments, order.RetailID)
	}
	return strings.Join(placeholders, ", "), arguments, nil
}

func scanBojunRetailItem(scanner bojunRetailScanner) (bojunRetailItem, error) {
	var (
		retailID                                                           int64
		docNo, productName, productValue, productColor, no, value1, value2 sql.NullString
		itemType, markDis, qty, dmAmtRetail, totAmtActual, totAmtList      sql.NullFloat64
		amtAcc, totAmtAcc, discount, priceList, priceActual                sql.NullFloat64
	)
	if err := scanner.Scan(
		&retailID, &docNo, &itemType, &markDis,
		&productName, &productValue, &productColor, &no, &value1, &value2,
		&qty, &dmAmtRetail, &totAmtActual, &totAmtList, &amtAcc, &totAmtAcc,
		&discount, &priceList, &priceActual,
	); err != nil {
		return bojunRetailItem{}, fmt.Errorf("scan bojun Oracle retail item: %w", err)
	}
	if retailID <= 0 || strings.TrimSpace(docNo.String) == "" {
		return bojunRetailItem{}, fmt.Errorf("scan bojun Oracle retail item: required field is empty")
	}
	return bojunRetailItem{
		RetailID: uint64(retailID), DocNo: strings.TrimSpace(docNo.String),
		Type: nullableBojunFloat(itemType), MarkDis: nullableBojunFloat(markDis),
		ProductName: strings.TrimSpace(productName.String), ProductValue: strings.TrimSpace(productValue.String),
		ProductColor: strings.TrimSpace(productColor.String), No: strings.TrimSpace(no.String),
		Value1: strings.TrimSpace(value1.String), Value2: strings.TrimSpace(value2.String),
		Qty: nullableBojunFloat(qty), DMAmtRetail: nullableBojunFloat(dmAmtRetail),
		TotAmtActual: nullableBojunFloat(totAmtActual), TotAmtList: nullableBojunFloat(totAmtList),
		AmtAcc: nullableBojunFloat(amtAcc), TotAmtAcc: nullableBojunFloat(totAmtAcc),
		Discount: nullableBojunFloat(discount), PriceList: nullableBojunFloat(priceList),
		PriceActual: nullableBojunFloat(priceActual),
	}, nil
}

func scanBojunRetailPayItem(scanner bojunRetailScanner) (bojunRetailPayItem, error) {
	var (
		retailID int64
		paywayID sql.NullInt64
		name     sql.NullString
		amount   sql.NullFloat64
	)
	if err := scanner.Scan(&retailID, &paywayID, &name, &amount); err != nil {
		return bojunRetailPayItem{}, fmt.Errorf("scan bojun Oracle retail pay item: %w", err)
	}
	if retailID <= 0 || !paywayID.Valid {
		return bojunRetailPayItem{}, fmt.Errorf("scan bojun Oracle retail pay item: required field is empty")
	}
	return bojunRetailPayItem{
		RetailID: uint64(retailID), PaywayID: paywayID.Int64,
		PaywayName: strings.TrimSpace(name.String), PayAmount: nullableBojunFloat(amount),
	}, nil
}

func nullableBojunFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func validateBojunRetailBatchSize(limit int) error {
	if limit < 1 || limit > maxBojunRetailBatchSize {
		return fmt.Errorf("query bojun Oracle retail orders: batch size must be between 1 and %d", maxBojunRetailBatchSize)
	}
	return nil
}
