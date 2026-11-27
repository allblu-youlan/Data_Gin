package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/tableboard"
	"gin-biz-web-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type boardPolicySchema struct{ ordinary, managed int }

func (s *boardPolicySchema) InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
	s.ordinary++
	return nil, errors.New("unexpected ordinary schema fallback")
}
func (s *boardPolicySchema) ValidateBojunMetadata(context.Context, reportdataset.Table) error {
	s.managed++
	return nil
}

func TestMySQLBoardManagedPhysicalIdentity(t *testing.T) {
	dsn := os.Getenv("TABLE_BOARD_TEST_DSN")
	if dsn == "" {
		t.Skip("set TABLE_BOARD_TEST_DSN for managed policy integration")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("dedicated test database required")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = db.AutoMigrate(&model.ReportBojunTable{}, &model.ReportBojunTableConfig{}); err != nil {
		t.Fatal(err)
	}
	ds := uint(time.Now().UnixNano())
	table := reportdataset.Table{Owner: "OPS", PhysicalTable: "ORDERS", DBLink: "BOJUN", Columns: []reportdataset.Column{
		{ID: "7", DBName: "STORE", OracleType: "VARCHAR2", LogicalType: "string", IsPhysical: true, IsSelectable: true},
		{ID: "8", DBName: "HIDDEN", OracleType: "NUMBER", LogicalType: "decimal", IsPhysical: true, IsSelectable: true},
	}}
	raw, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	row := model.ReportBojunTable{DatasourceID: ds, BojunTableID: "physical_scope", ResolvedOwner: table.Owner, ResolvedTable: table.PhysicalTable, LogicalName: "ORDERS_LOGICAL", ValidationStatus: "VALID", MetadataJSON: model.JSONText(raw), SourceActive: true}
	if err = db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&row)
	config := model.ReportBojunTableConfig{TableID: row.ID, ConfigJSON: model.JSONText(`{"enabled":true,"selectableColumns":["7"],"requiredFilterColumns":["7"],"dataScope":{"mode":"SCOPE_COLUMN","columnId":"7","userValues":{"5":["0007"]}}}`)}
	if err = db.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&config)
	schema := &boardPolicySchema{}
	catalog := ReportBojunCatalogService{repo: reportrepo.New(db)}
	mapping := tableboard.Metric{DatasourceID: ds, Owner: table.Owner, TableName: table.PhysicalTable, ColumnIDs: map[string]uint{"STORE": 7, "HIDDEN": 8}}
	call := func(actor uint, fields, restrictions []string, detail, ok bool) boardRelation {
		t.Helper()
		rel, err := catalog.boardRelation(t.Context(), schema, actor, mapping, fields, restrictions, detail)
		if ok && err != nil || !ok && !errors.Is(err, ErrBoardSourceInvalid) {
			t.Fatalf("ok=%v fields=%v restrictions=%v detail=%v error=%v", ok, fields, restrictions, detail, err)
		}
		return rel
	}
	fields := []string{"STORE"}
	call(5, fields, fields, false, false) // Missing catalog identity.
	call(5, fields, nil, true, false)     // Ordinary detail cannot satisfy the required filter.
	mapping.BojunTableID = row.ID
	call(5, []string{"HIDDEN"}, fields, false, false)
	call(6, fields, fields, false, false)
	rel := call(5, fields, fields, false, true)
	if !strings.Contains(rel.from, "OPS.ORDERS@BOJUN") || !strings.Contains(rel.from, "B0.STORE IN (:b1)") || len(rel.args) != 1 {
		t.Fatal(rel)
	}
	if err = db.Model(&row).Update("source_active", false).Error; err != nil {
		t.Fatal(err)
	}
	mapping.BojunTableID = 0
	call(5, fields, fields, true, false)
	if schema.ordinary != 0 || schema.managed != 1 {
		t.Fatal(schema)
	}
}
