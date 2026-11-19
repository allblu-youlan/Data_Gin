//go:build integration

package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestQimaiCharsetMigrationMySQLExistingV28PreservesValuesAndTypes(t *testing.T) {
	db := qimaiCharsetIntegrationDB(t)
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_general_ci").AutoMigrate(&model.QIMAI_ORDER_DATA{}); err != nil {
		t.Fatal(err)
	}
	legacy := model.QIMAI_ORDER_DATA{RawDataID: 240055, OrderNo: "legacy-order", ContactName: "历史昵称", BuyerRemarks: "保留原始备注"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	before := qimaiColumnDefinitions(t, db)
	emoji := model.QIMAI_ORDER_DATA{RawDataID: 240056, OrderNo: "emoji-order", ContactName: "昵称😊", BuyerRemarks: "备注🍰"}
	var mysqlErr *mysqlDriver.MySQLError
	if err := db.Create(&emoji).Error; !errors.As(err, &mysqlErr) || mysqlErr.Number != 1366 {
		t.Fatalf("legacy UTF8 table should reproduce MySQL 1366, got %v", err)
	}
	if err := ApplySchemaMigrations(); err != nil {
		t.Fatal(err)
	}
	emoji.ID = 0
	if err := db.Create(&emoji).Error; err != nil {
		t.Fatalf("emoji insert after current-v28 migration: %v", err)
	}
	var reloaded model.QIMAI_ORDER_DATA
	if err := db.First(&reloaded, emoji.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.ContactName != emoji.ContactName || reloaded.BuyerRemarks != emoji.BuyerRemarks {
		t.Fatal("migration did not preserve emoji exactly")
	}
	var unchanged model.QIMAI_ORDER_DATA
	if err := db.First(&unchanged, legacy.ID).Error; err != nil || unchanged.ContactName != legacy.ContactName || unchanged.BuyerRemarks != legacy.BuyerRemarks {
		t.Fatalf("legacy values changed: %v", err)
	}
	after := qimaiColumnDefinitions(t, db)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("column type/null/default/comment changed: before=%v after=%v", before, after)
	}
	assertQimaiCharsetAndIndex(t, db)
	if err := ApplySchemaMigrations(); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	assertQimaiCharsetAndIndex(t, db)
	var count int64
	if err := db.Model(&model.QIMAI_ORDER_DATA{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("repeat migration changed rows: count=%d error=%v", count, err)
	}
}

func TestQimaiCharsetMigrationMySQLCreatesNewTableWithUTF8MB4(t *testing.T) {
	db := qimaiCharsetIntegrationDB(t)
	if err := ApplySchemaMigrations(); err != nil {
		t.Fatal(err)
	}
	assertQimaiCharsetAndIndex(t, db)
	order := model.QIMAI_ORDER_DATA{RawDataID: 1, OrderNo: "new-order", ContactName: "新昵称😊"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("new table rejects emoji: %v", err)
	}
}

func TestQimaiCharsetMigrationMySQLSupportsPreparedStatements(t *testing.T) {
	db := qimaiCharsetIntegrationDBWithInterpolation(t, false)
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_general_ci").AutoMigrate(&model.QIMAI_ORDER_DATA{}); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchemaMigrations(); err != nil {
		t.Fatalf("migration with prepared parameters, including session timeout restoration: %v", err)
	}
	assertQimaiCharsetAndIndex(t, db)
	order := model.QIMAI_ORDER_DATA{RawDataID: 1, OrderNo: "prepared-order", ContactName: "昵称😊"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("prepared emoji insert after migration: %v", err)
	}
	var reloaded model.QIMAI_ORDER_DATA
	if err := db.First(&reloaded, order.ID).Error; err != nil || reloaded.ContactName != order.ContactName {
		t.Fatalf("prepared emoji readback changed: error=%v", err)
	}
}

func TestQimaiCharsetMigrationMySQLPreservesExistingUTF8MB4Bin(t *testing.T) {
	db := qimaiCharsetIntegrationDB(t)
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin").AutoMigrate(&model.QIMAI_ORDER_DATA{}); err != nil {
		t.Fatal(err)
	}
	order := model.QIMAI_ORDER_DATA{RawDataID: 1, OrderNo: "case-sensitive-order", ContactName: "Case"}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplySchemaMigrations(); err != nil {
		t.Fatal(err)
	}
	var tableCollation, columnCollation string
	if err := db.Raw(`SELECT TABLE_COLLATION FROM information_schema.TABLES
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='qimai_order_data'`).Scan(&tableCollation).Error; err != nil || tableCollation != "utf8mb4_bin" {
		t.Fatalf("existing table collation changed: %s error=%v", tableCollation, err)
	}
	if err := db.Raw(`SELECT COLLATION_NAME FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='qimai_order_data' AND COLUMN_NAME='contact_name'`).Scan(&columnCollation).Error; err != nil || columnCollation != "utf8mb4_bin" {
		t.Fatalf("existing column collation changed: %s error=%v", columnCollation, err)
	}
	var count int64
	if err := db.Model(&model.QIMAI_ORDER_DATA{}).Where("contact_name = ?", "case").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("case-sensitive comparison changed: count=%d error=%v", count, err)
	}
	assertQimaiCharsetAndIndex(t, db)
}

func TestQimaiCharsetMigrationMySQLResumesAfterIndexFailure(t *testing.T) {
	db := qimaiCharsetIntegrationDB(t)
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_general_ci").AutoMigrate(&model.QIMAI_ORDER_DATA{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE INDEX idx_qimai_raw_data_id ON qimai_order_data (order_no)").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplySchemaMigrations(); err == nil {
		t.Fatal("migration accepted a conflicting raw-data index definition")
	}
	var markerCount int64
	if err := db.Model(&schemaMigrationRecord{}).Where("version = ?", "2026-11-18-qimai-order-utf8mb4-v1").Count(&markerCount).Error; err != nil || markerCount != 0 {
		t.Fatalf("failed migration wrote completion marker: count=%d error=%v", markerCount, err)
	}
	if err := db.Exec("DROP INDEX idx_qimai_raw_data_id ON qimai_order_data").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplySchemaMigrations(); err != nil {
		t.Fatalf("resume after fixing index conflict: %v", err)
	}
	assertQimaiCharsetAndIndex(t, db)
}

func TestQimaiCharsetMigrationMySQLReusesExistingRawDataPrefixIndex(t *testing.T) {
	db := qimaiCharsetIntegrationDB(t)
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_general_ci").AutoMigrate(&model.QIMAI_ORDER_DATA{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE INDEX existing_raw_data_lookup ON qimai_order_data (raw_data_id, id)").Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplySchemaMigrations(); err != nil {
		t.Fatal(err)
	}
	assertQimaiCharsetAndIndex(t, db)
}

func qimaiCharsetIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	return qimaiCharsetIntegrationDBWithInterpolation(t, true)
}

func qimaiCharsetIntegrationDBWithInterpolation(t *testing.T, interpolate bool) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("QIMAI_MIGRATION_MYSQL_DSN")
	if dsn == "" {
		t.Skip("QIMAI_MIGRATION_MYSQL_DSN is required for isolated MySQL 8 integration tests")
	}
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MySQL integration DSN")
	}
	if !strings.Contains(parsed.DBName, "_test") && !strings.Contains(parsed.DBName, "integration") {
		t.Fatal("integration database name must contain _test or integration")
	}
	parsed.ParseTime = true
	parsed.InterpolateParams = interpolate
	parsed.Params = map[string]string{"charset": "utf8mb4"}
	admin, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	name := "qimai_charset_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` DEFAULT CHARACTER SET utf8 COLLATE utf8_general_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE `"+name+"`"); err != nil {
			t.Errorf("clean up generated integration database: %v", err)
		}
	})
	parsed.DBName = name
	db, err := gorm.Open(mysql.Open(parsed.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db = db.WithContext(ctx)
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil || !strings.HasPrefix(version, "8.") {
		t.Fatalf("MySQL 8 required: version=%s error=%v", version, err)
	}
	if err := db.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{schemaMigrationVersion, openMonitoringPermissionMigrationVersion} {
		if err := db.Create(&schemaMigrationRecord{Version: version, AppliedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	originalDB, originalSQLDB := database.DB, database.SQLDB
	database.DB, database.SQLDB = db, sqlDB
	t.Cleanup(func() { database.DB, database.SQLDB = originalDB, originalSQLDB })
	return db
}

type qimaiColumnDefinition struct {
	Name     string
	Type     string
	Nullable string
	Default  sql.NullString `gorm:"column:default_value"`
	Extra    string
	Comment  string
}

func qimaiColumnDefinitions(t *testing.T, db *gorm.DB) []qimaiColumnDefinition {
	t.Helper()
	var columns []qimaiColumnDefinition
	if err := db.Raw(`SELECT COLUMN_NAME AS name, COLUMN_TYPE AS type, IS_NULLABLE AS nullable,
COLUMN_DEFAULT AS default_value, EXTRA AS extra, COLUMN_COMMENT AS comment
FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='qimai_order_data'
ORDER BY ORDINAL_POSITION`).Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	return columns
}

func assertQimaiCharsetAndIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	var unsupported int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='qimai_order_data'
AND CHARACTER_SET_NAME IS NOT NULL AND CHARACTER_SET_NAME <> 'utf8mb4'`).Scan(&unsupported).Error; err != nil || unsupported != 0 {
		t.Fatalf("unsupported text columns=%d error=%v", unsupported, err)
	}
	var collation string
	if err := db.Raw(`SELECT TABLE_COLLATION FROM information_schema.TABLES
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='qimai_order_data'`).Scan(&collation).Error; err != nil || !strings.HasPrefix(collation, "utf8mb4_") {
		t.Fatalf("table collation=%s error=%v", collation, err)
	}
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='qimai_order_data' AND COLUMN_NAME='raw_data_id'
AND SEQ_IN_INDEX=1 AND NON_UNIQUE=1 AND IS_VISIBLE='YES' AND SUB_PART IS NULL`).Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("raw-data nonunique indexes=%d error=%v", count, err)
	}
	if err := db.Model(&schemaMigrationRecord{}).Where("version = ?", "2026-11-18-qimai-order-utf8mb4-v1").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("completed migration markers=%d error=%v", count, err)
	}
}
