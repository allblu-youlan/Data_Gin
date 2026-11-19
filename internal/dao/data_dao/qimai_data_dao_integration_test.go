//go:build integration

package data_dao

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gin-biz-web-api/model"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestQimaiCreateMySQLConcurrentReplayPreservesSyncedAndUnicode(t *testing.T) {
	dsn := os.Getenv("QIMAI_ORDER_MYSQL_DSN")
	if dsn == "" {
		t.Skip("QIMAI_ORDER_MYSQL_DSN is required for the MySQL integration test")
	}
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid MySQL integration DSN")
	}
	if !strings.Contains(parsed.DBName, "_test") && !strings.Contains(parsed.DBName, "integration") {
		t.Fatal("integration database name must contain _test or integration")
	}
	parsed.ParseTime = true
	if parsed.Params == nil {
		parsed.Params = make(map[string]string)
	}
	parsed.Params["charset"] = "utf8mb4"
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
	for _, table := range []string{"raw_data", "qimai_order_data"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("integration test requires an empty database; table %s already exists", table)
		}
	}
	if err := db.Exec("CREATE TABLE raw_data (id BIGINT UNSIGNED PRIMARY KEY) ENGINE=InnoDB").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := db.WithContext(cleanupCtx).Migrator().DropTable("qimai_order_data", "raw_data"); err != nil {
			t.Errorf("clean up test tables: %v", err)
		}
	})
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci").AutoMigrate(&model.QIMAI_ORDER_DATA{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO raw_data (id) VALUES (?), (?)", 240056, 240057).Error; err != nil {
		t.Fatal(err)
	}
	dao := &QimaiDataDAO{db: db}

	createConcurrently := func(name string) uint {
		t.Helper()
		const workers = 16
		ids := make(chan uint, workers)
		failures := make(chan error, workers)
		start := make(chan struct{})
		var wait sync.WaitGroup
		for index := 0; index < workers; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				row := &model.QIMAI_ORDER_DATA{RawDataID: 240056, OrderNo: "same-business-order", ContactName: name}
				id, err := dao.Create(ctx, row)
				if err != nil {
					failures <- err
					return
				}
				if id == 0 || row.ID != id {
					failures <- errors.New("successful replay did not return and assign its persisted ID")
					return
				}
				ids <- id
			}()
		}
		close(start)
		wait.Wait()
		close(ids)
		close(failures)
		for err := range failures {
			t.Errorf("concurrent create: %v", err)
		}
		var wantID uint
		for id := range ids {
			if wantID == 0 {
				wantID = id
			} else if id != wantID {
				t.Errorf("same raw record created different order IDs: %d and %d", wantID, id)
			}
		}
		return wantID
	}

	id := createConcurrently("客人🍭")
	if err := db.Model(&model.QIMAI_ORDER_DATA{}).Where("id = ?", id).Update("synced", 1).Error; err != nil {
		t.Fatal(err)
	}
	if replayedID := createConcurrently("不应覆盖已保存的姓名"); replayedID != id {
		t.Errorf("replayed id=%d, want %d", replayedID, id)
	}
	var orders []model.QIMAI_ORDER_DATA
	if err := db.Where("raw_data_id = ?", 240056).Find(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].Synced != 1 || orders[0].ContactName != "客人🍭" {
		t.Fatalf("concurrent replays changed the saved order: %+v", orders)
	}
	// Different source events for the same business order retain their existing
	// semantics; this change adds no order-number uniqueness rule.
	secondID, err := dao.Create(ctx, &model.QIMAI_ORDER_DATA{RawDataID: 240057, OrderNo: "same-business-order"})
	if err != nil || secondID == id {
		t.Fatalf("separate raw record id=%d error=%v", secondID, err)
	}
	if _, err := dao.Create(ctx, &model.QIMAI_ORDER_DATA{RawDataID: 999999, OrderNo: "missing-raw"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing raw record error=%v, want record not found", err)
	}
}
