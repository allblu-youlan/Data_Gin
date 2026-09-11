package data_dao

import (
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

func TestBojunRetailOrderDAOSupplementOracleFieldsUsesNarrowCASUpdate(t *testing.T) {
	t.Parallel()
	db := dryRunWeatherDAOTestDB(t)
	var statement string
	if err := db.Callback().Update().After("gorm:update").Register("test:capture_bojun_oracle_supplement_sql", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}
	retailID := uint64(45077)
	updated, err := NewBojunRetailOrderDAO(db).SupplementOracleFieldsIfMissing(
		t.Context(),
		77,
		&model.BojunRetailOrder{
			OracleRetailID: &retailID,
			DocNo:          "SALE-45077",
			RetailBillType: "RET",
			StoreName:      "商场一店",
			OrderPhone:     "18616613488",
			PaidAmount:     88.8,
			PushAmount:     80,
			IsToShop:       "Y",
			TotalAmtList:   88.8,
			TotalAmtActual: 88.8,
			TotalAmtAcc:    88.8,
			TotalAmtAcc1:   88.8,
		},
	)
	if err != nil {
		t.Fatalf("SupplementOracleFieldsIfMissing() error=%v", err)
	}
	if updated {
		t.Fatal("dry-run supplement unexpectedly reported rows affected")
	}
	for _, fragment := range []string{
		"UPDATE `bojun_retail_orders`",
		"`oracle_retail_id`=?",
		"`retailbilltype`=?",
		"`c_store_name`=?",
		"`order_phone`=?",
		"`paid_amount`=?",
		"`push_amount`=?",
		"`is_to_shop`=?",
		"`tot_amt_list`=?",
		"`tot_amt_actual`=?",
		"`tot_amt_acc`=?",
		"`tot_amt_acc1`=?",
		"id = ? AND docno = ? AND oracle_retail_id IS NULL",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("statement missing %q: %s", fragment, statement)
		}
	}
	for _, forbidden := range []string{
		"`completed_at`=", "`c_store_code`=", "`retailsaletype`=", "`order_type_code`=", "`order_type_name`=",
		"`items_json`=", "`raw_content_json`=", "`raw_data_id`=", "`synced`=",
	} {
		if strings.Contains(statement, forbidden) {
			t.Fatalf("statement updates %q: %s", forbidden, statement)
		}
	}
	if strings.Contains(statement, "SALE-45077") || strings.Contains(statement, "18616613488") {
		t.Fatalf("statement interpolates Oracle supplement values: %s", statement)
	}
}

func TestBojunRetailOrderDAOUpdateCompletedAtIfEmptyUsesNarrowUpdate(t *testing.T) {
	t.Parallel()
	db := dryRunWeatherDAOTestDB(t)
	var statement string
	if err := db.Callback().Update().After("gorm:update").Register("test:capture_bojun_completed_at_sql", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}
	updated, err := NewBojunRetailOrderDAO(db).UpdateCompletedAtIfEmpty(
		t.Context(),
		"B001",
		time.Date(2026, 7, 11, 10, 31, 22, 0, time.FixedZone("CST", 8*60*60)),
	)
	if err != nil {
		t.Fatalf("UpdateCompletedAtIfEmpty() error=%v", err)
	}
	if updated {
		t.Fatal("dry-run update unexpectedly reported rows affected")
	}
	for _, fragment := range []string{
		"UPDATE `bojun_retail_orders`",
		"SET `completed_at`=?,`updated_at`=?",
		"docno = ? AND completed_at IS NULL",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("statement missing %q: %s", fragment, statement)
		}
	}
	for _, forbidden := range []string{"synced", "matched_docno", "raw_data_id"} {
		if strings.Contains(statement, forbidden) {
			t.Fatalf("statement updates %q: %s", forbidden, statement)
		}
	}
}

func TestBojunRetailOrderDAOListOpenOrdersUsesBoundedSanitizedQuery(t *testing.T) {
	t.Parallel()
	db := dryRunWeatherDAOTestDB(t)
	var statement string
	if err := db.Callback().Query().After("gorm:query").Register("test:capture_open_bojun_orders_sql", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}
	dao := NewBojunRetailOrderDAO(db)
	before := time.Date(2026, 7, 20, 12, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	orders, err := dao.ListOpenOrders(t.Context(), OpenBojunOrderQuery{
		StartCompletedAt:  time.Date(2026, 7, 1, 0, 0, 0, 0, before.Location()),
		EndCompletedAt:    time.Date(2026, 8, 1, 0, 0, 0, 0, before.Location()),
		BeforeCompletedAt: &before,
		StoreCodes:        []string{"ABCN001P012"},
		OrderTypes:        []string{"CMR"},
		BeforeID:          99,
		Limit:             51,
	})
	if err != nil {
		t.Fatalf("ListOpenOrders() error=%v", err)
	}
	if orders == nil {
		t.Fatal("ListOpenOrders() returned nil slice")
	}
	for _, fragment := range []string{
		"SELECT `id`,`otherdocno`,`docno`,`order_phone`,`billdate`,`completed_at`,`c_store_code`,`c_store_name`",
		"completed_at >= ? AND completed_at < ?",
		"c_store_code IN (?)",
		"order_type_code IN (?)",
		"completed_at < ? OR (completed_at = ? AND id < ?)",
		"ORDER BY completed_at DESC,id DESC",
		"LIMIT 51",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("statement missing %q: %s", fragment, statement)
		}
	}
	for _, sensitive := range []string{"raw_content_json", "pay_items_json", "vipno", "raw_data_id"} {
		if strings.Contains(statement, sensitive) {
			t.Fatalf("statement selects sensitive column %q: %s", sensitive, statement)
		}
	}
	if strings.Contains(statement, "ABCN001P012") || strings.Contains(statement, "CMR") {
		t.Fatalf("statement interpolates filters: %s", statement)
	}
}

func TestBojunRetailOrderDAOCountOpenOrdersAllowsOmittedMallCodes(t *testing.T) {
	t.Parallel()
	db := dryRunWeatherDAOTestDB(t)
	var statement string
	if err := db.Callback().Query().After("gorm:query").Register("test:capture_open_bojun_all_malls_count_sql", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	_, err := NewBojunRetailOrderDAO(db).CountOpenOrders(t.Context(), OpenBojunOrderQuery{
		StartCompletedAt: time.Date(2026, 7, 11, 0, 0, 0, 0, location),
		EndCompletedAt:   time.Date(2026, 7, 12, 0, 0, 0, 0, location),
	})
	if err != nil {
		t.Fatalf("CountOpenOrders() error=%v", err)
	}
	if !strings.Contains(statement, "completed_at >= ? AND completed_at < ?") {
		t.Fatalf("statement missing completed-at range: %s", statement)
	}
	if strings.Contains(statement, "c_store_code IN") {
		t.Fatalf("statement unexpectedly filters malls: %s", statement)
	}
}

func TestBojunRetailOrderDAOListOpenOrdersRejectsUnboundedQuery(t *testing.T) {
	dao := &BojunRetailOrderDAO{}
	if _, err := dao.ListOpenOrders(t.Context(), OpenBojunOrderQuery{}); err == nil {
		t.Fatal("ListOpenOrders() accepted unbounded query")
	}
}

func TestBojunRetailOrderDAOCountOpenOrdersIgnoresCursorBoundary(t *testing.T) {
	t.Parallel()
	db := dryRunWeatherDAOTestDB(t)
	var statement string
	if err := db.Callback().Query().After("gorm:query").Register("test:capture_open_bojun_count_sql", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}
	_, err := NewBojunRetailOrderDAO(db).CountOpenOrders(t.Context(), OpenBojunOrderQuery{
		StartBillDate: 20260701, EndBillDate: 20260731, StoreCodes: []string{"ABCN001P012"},
		OrderTypes: []string{"CMR"}, BeforeBillDate: 20260720, BeforeID: 99,
	})
	if err != nil {
		t.Fatalf("CountOpenOrders() error=%v", err)
	}
	for _, fragment := range []string{"SELECT count(*)", "billdate BETWEEN ? AND ?", "c_store_code IN (?)", "order_type_code IN (?)"} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("statement missing %q: %s", fragment, statement)
		}
	}
	if strings.Contains(statement, "billdate < ?") || strings.Contains(statement, "LIMIT") || strings.Contains(statement, "ORDER BY") {
		t.Fatalf("count statement contains page boundary: %s", statement)
	}
}
