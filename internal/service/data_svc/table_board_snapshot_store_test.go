package data_svc

import (
	"encoding/json"
	"gin-biz-web-api/pkg/config"
	projectredis "gin-biz-web-api/pkg/redis"
	"github.com/google/uuid"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/tableboard"
	"gin-biz-web-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestBoardSnapshotRetainsNumericFormatsThroughCacheJSON(t *testing.T) {
	input := storedBoardCalculation{Export: BoardExport{Foreground: "#000000", Background: "#ffffff", Sections: []BoardExportSheet{{Title: "金额", Rows: [][]string{{"12,400.00"}}, Numbers: map[string]boardNumber{"A1": {Raw: "12400", Format: "money_2"}}}}}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var loaded storedBoardCalculation
	if err = json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Export.Sections[0].Numbers["A1"].Raw != "12400" {
		t.Fatal(string(raw))
	}
}

func TestMySQLBoardSnapshotStampDetectsPolicyAndConnectionChanges(t *testing.T) {
	dsn := os.Getenv("TABLE_BOARD_TEST_DSN")
	if dsn == "" {
		t.Skip("set TABLE_BOARD_TEST_DSN to run MySQL snapshot policy integration")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("test database must end in _test")
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
	if err = db.AutoMigrate(&model.ReportDatasource{}, &model.ReportBojunTable{}, &model.ReportBojunTableConfig{}); err != nil {
		t.Fatal(err)
	}
	ds := model.ReportDatasource{Code: "board_stamp_test", Name: "测试连接", Host: "localhost", Username: "tester", Enabled: true, PasswordCiphertext: "x", CredentialKeyVersion: "test"}
	if err = db.Create(&ds).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&ds)
	table := model.ReportBojunTable{DatasourceID: ds.ID, BojunTableID: "stamp", ResolvedOwner: "OPS", ResolvedTable: "ORDERS", SourceActive: true, ValidationStatus: "VALID"}
	if err = db.Create(&table).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&table)
	config := model.ReportBojunTableConfig{TableID: table.ID, ConfigJSON: model.JSONText(`{"enabled":true,"dataScope":{"mode":"SCOPE_COLUMN","columnId":"7","userValues":{"1":["0007"]}}}`)}
	if err = db.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&config)
	layout := tableboard.Layout{Metrics: []tableboard.Metric{{DatasourceID: ds.ID}}}
	stamp, err := BoardAccessStamp(t.Context(), db, layout)
	if err != nil {
		t.Fatal(err)
	}
	same, err := BoardAccessStamp(t.Context(), db, layout)
	if err != nil || stamp != same {
		t.Fatal("unstable stamp", err)
	}
	if err = db.Model(&config).Update("config_json", model.JSONText(`{"enabled":false}`)).Error; err != nil {
		t.Fatal(err)
	}
	revoked, err := BoardAccessStamp(t.Context(), db, layout)
	if err != nil || revoked == stamp {
		t.Fatal("missed revocation", err)
	}
	if err = db.Model(&ds).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	disabled, err := BoardAccessStamp(t.Context(), db, layout)
	if err != nil || disabled == revoked {
		t.Fatal("missed disabled connection", err)
	}
}

func TestRedisBoardSnapshotScopeAndExpiration(t *testing.T) {
	address := os.Getenv("TABLE_BOARD_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set TABLE_BOARD_TEST_REDIS_ADDR to run Redis snapshot integration")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("Redis integration requires a loopback address")
	}
	if n, _ := strconv.Atoi(port); n == 6379 {
		t.Fatal("use a dedicated test Redis port")
	}
	cfgdir := t.TempDir()
	if err = os.WriteFile(filepath.Join(cfgdir, "config.yaml"), []byte("cfg:\n  app:\n    name: table-board-integration-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", cfgdir+string(os.PathSeparator))
	projectredis.ConnectRedis(projectredis.RdsConfigs{"default": {Addr: address}})
	ctx := t.Context()
	id := uuid.NewString()
	snapshot := BoardCalculation{CalculationID: id, Revision: 3, AsOf: time.Now(), Cells: []BoardCellResult{}, Export: BoardExport{Foreground: "#000000", Background: "#ffffff", Sections: []BoardExportSheet{{Title: "GMV", Rows: [][]string{{"6,120.00"}}, Numbers: map[string]boardNumber{"A1": {Raw: "6120", Format: "money_2"}}}}}}
	if err = StoreBoardCalculation(ctx, 41, 7, "policy-v1", snapshot); err != nil {
		t.Fatal(err)
	}
	key := boardCalculationKey(41, 7, id)
	defer projectredis.Instance().Client.Del(ctx, key)
	got, stamp, err := LoadBoardCalculation(ctx, 41, 7, id)
	if err != nil || stamp != "policy-v1" || got.Revision != 3 || got.Export.Sections[0].Numbers["A1"].Raw != "6120" {
		t.Fatal(got, stamp, err)
	}
	if _, _, err = LoadBoardCalculation(ctx, 42, 7, id); err != ErrBoardSnapshotExpired {
		t.Fatal("foreign account read accepted", err)
	}
	if _, _, err = LoadBoardCalculation(ctx, 41, 8, id); err != ErrBoardSnapshotExpired {
		t.Fatal("foreign board read accepted", err)
	}
	if err = projectredis.Instance().Client.Expire(ctx, key, -time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = LoadBoardCalculation(ctx, 41, 7, id); err != ErrBoardSnapshotExpired {
		t.Fatal("expired snapshot accepted", err)
	}
}
