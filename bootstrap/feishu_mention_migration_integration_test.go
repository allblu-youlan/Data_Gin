//go:build integration

package bootstrap

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This is the v23 table definition, before report_kind separated manual reports.
type feishuReportV23 struct {
	model.BaseModel
	AppID           string         `gorm:"size:64;not null;uniqueIndex:uk_feishu_report,priority:1;index:idx_feishu_report_status,priority:1"`
	ChatID          string         `gorm:"size:128;not null;uniqueIndex:uk_feishu_report,priority:2"`
	RecipientOpenID string         `gorm:"size:128;not null;uniqueIndex:uk_feishu_report,priority:3"`
	ReportDate      string         `gorm:"type:date;serializer:feishu_date;not null;uniqueIndex:uk_feishu_report,priority:4;index:idx_feishu_report_status,priority:3"`
	WindowStart     time.Time      `gorm:"type:datetime(3);not null"`
	WindowEnd       time.Time      `gorm:"type:datetime(3);not null"`
	GeneratedAt     time.Time      `gorm:"type:datetime(3);not null"`
	Status          string         `gorm:"size:16;not null;default:ready;index:idx_feishu_report_status,priority:2"`
	PartCount       uint           `gorm:"not null;default:1"`
	ContentSnapshot model.JSONText `gorm:"type:json;not null"`
	SentAt          *time.Time     `gorm:"type:datetime(3)"`
	model.WeatherTimestamps
}

func TestFeishuMentionMySQLMigrationFromV23PreservesDailyAndSeparatesManual(t *testing.T) {
	dsn := os.Getenv("FEISHU_MENTION_MYSQL_DSN")
	if dsn == "" {
		t.Skip("FEISHU_MENTION_MYSQL_DSN is required for MySQL 8 integration tests")
	}
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MySQL integration DSN")
	}
	if !strings.Contains(parsed.DBName, "_test") && !strings.Contains(parsed.DBName, "integration") {
		t.Fatal("integration database name must contain _test or integration")
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	parsed.ParseTime, parsed.Loc = true, loc
	db, err := gorm.Open(mysql.Open(parsed.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db = db.WithContext(ctx)
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "8.") {
		t.Fatalf("MySQL 8 is required, got %s", version)
	}
	tableName := "feishu_report_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	// Every DDL statement is scoped to this generated table, never the live table.
	scoped := func() *gorm.DB { return db.Table(tableName) }
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := db.WithContext(cleanupCtx).Migrator().DropTable(tableName); err != nil {
			t.Errorf("clean up migration test table: %v", err)
		}
	})
	if err := scoped().Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin").AutoMigrate(&feishuReportV23{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 18, 0, 0, 0, loc)
	legacy := feishuReportV23{
		AppID: "cli_migration", ChatID: "oc_group", RecipientOpenID: "ou_recipient", ReportDate: "2026-10-14",
		WindowStart: now.Add(-24 * time.Hour), WindowEnd: now, GeneratedAt: now, Status: "sent", PartCount: 2, SentAt: &now,
		ContentSnapshot: model.JSONText(`{"schemaVersion":1,"messageCount":1,"messages":[{"displayText":"旧日报完整正文"}]}`),
	}
	if err := scoped().Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := scoped().First(&legacy, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalContent := string(legacy.ContentSnapshot)
	if err := finalizeFeishuMentionReportMigration(scoped()); err == nil {
		t.Fatal("migration removed the old constraint before its replacement existed")
	}
	if !scoped().Migrator().HasIndex(&model.FeishuDailyReport{}, "uk_feishu_report") {
		t.Fatal("failed finalization lost the v23 uniqueness constraint")
	}
	if err := scoped().AutoMigrate(&model.FeishuDailyReport{}); err != nil {
		t.Fatal(err)
	}
	if !scoped().Migrator().HasIndex(&model.FeishuDailyReport{}, "uk_feishu_report") || !scoped().Migrator().HasIndex(&model.FeishuDailyReport{}, "uk_feishu_report_kind") {
		t.Fatal("replacement constraint must be created before the v23 constraint is removed")
	}
	var columns []struct {
		ColumnName string
		NonUnique  int
	}
	if err := db.Raw("SELECT COLUMN_NAME AS column_name, NON_UNIQUE AS non_unique FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? ORDER BY SEQ_IN_INDEX", tableName, "uk_feishu_report_kind").Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(columns))
	for i, column := range columns {
		if column.NonUnique != 0 {
			t.Fatal("report kind key must enforce uniqueness")
		}
		names[i] = column.ColumnName
	}
	if !reflect.DeepEqual(names, []string{"app_id", "chat_id", "recipient_open_id", "report_date", "report_kind"}) {
		t.Fatalf("replacement key columns=%v", names)
	}
	if err := finalizeFeishuMentionReportMigration(scoped()); err != nil {
		t.Fatal(err)
	}
	if scoped().Migrator().HasIndex(&model.FeishuDailyReport{}, "uk_feishu_report") {
		t.Fatal("obsolete four-column constraint still blocks manual reports")
	}
	var daily model.FeishuDailyReport
	if err := scoped().First(&daily, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if daily.ReportKind != "daily" || daily.ReportDate != legacy.ReportDate || daily.Status != legacy.Status || daily.PartCount != legacy.PartCount ||
		!daily.WindowStart.Equal(legacy.WindowStart) || !daily.WindowEnd.Equal(legacy.WindowEnd) || daily.SentAt == nil || !daily.SentAt.Equal(*legacy.SentAt) || string(daily.ContentSnapshot) != originalContent {
		t.Fatal("v23 daily report changed during migration")
	}
	manual := daily
	manual.ID, manual.ReportKind = 0, "manual"
	if err := scoped().Create(&manual).Error; err != nil {
		t.Fatalf("same-day manual report could not coexist with daily: %v", err)
	}
	for _, original := range []model.FeishuDailyReport{daily, manual} {
		duplicate := original
		duplicate.ID = 0
		var duplicateError *mysqlDriver.MySQLError
		if err := scoped().Create(&duplicate).Error; !errors.As(err, &duplicateError) || duplicateError.Number != 1062 {
			t.Fatalf("duplicate %s report was not rejected: %v", original.ReportKind, err)
		}
	}
	// Repeat the exact schema-plus-finalization sequence after both kinds exist.
	if err := scoped().AutoMigrate(&model.FeishuDailyReport{}); err != nil {
		t.Fatal(err)
	}
	if err := finalizeFeishuMentionReportMigration(scoped()); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
	var reports []model.FeishuDailyReport
	if err := scoped().Order("id").Find(&reports).Error; err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 || reports[0].ID != daily.ID || reports[0].ReportKind != "daily" || reports[1].ID != manual.ID || reports[1].ReportKind != "manual" || string(reports[0].ContentSnapshot) != originalContent {
		t.Fatal("repeat migration changed or duplicated existing reports")
	}
}
