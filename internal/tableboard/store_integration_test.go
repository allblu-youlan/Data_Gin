package tableboard

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TABLE_BOARD_TEST_DSN must select a dedicated test database. Independent
// connection pools exercise the same revision guard used by separate replicas.
func TestMySQLStoreOwnerAndRevision(t *testing.T) {
	dsn := os.Getenv("TABLE_BOARD_TEST_DSN")
	if dsn == "" {
		t.Skip("set TABLE_BOARD_TEST_DSN to run the MySQL integration test")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("TABLE_BOARD_TEST_DSN must select a database ending in _test")
	}
	connect := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Close() })
		return db
	}
	db, replica := connect(), connect()
	if err := db.AutoMigrate(&model.TableBoard{}); err != nil {
		t.Fatal(err)
	}
	ownerID := uint(time.Now().UnixNano())
	first := NewStore(db, ownerID)
	second := NewStore(replica, ownerID)
	other := NewStore(replica, ownerID+1)
	ctx := t.Context()
	t.Cleanup(func() { db.Where("owner_id = ?", ownerID).Delete(&model.TableBoard{}) })
	in := Input{Name: "演示看板", Layout: sampleLayout()}
	board, err := first.Save(ctx, 0, in)
	if err != nil {
		t.Fatal(err)
	}
	if board.Revision != 1 {
		t.Fatal(board.Revision)
	}
	if got, err := second.Get(ctx, board.ID); err != nil || got.Name != in.Name || got.Revision != 1 {
		t.Fatalf("replica read: %#v, %v", got, err)
	}
	if _, err := other.Get(ctx, board.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign read: %v", err)
	}
	if page, err := other.List(ctx, 0); err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	in.Revision = 1
	if _, err := other.Save(ctx, board.ID, in); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign update: %v", err)
	}
	if err := other.Delete(ctx, board.ID, 1); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}

	start := make(chan struct{})
	result := make(chan error, 2)
	for _, s := range []Store{first, second} {
		go func() {
			<-start
			_, err := s.Save(ctx, board.ID, in)
			result <- err
		}()
	}
	close(start)
	a, b := <-result, <-result
	if !((a == nil && errors.Is(b, ErrConflict)) || (b == nil && errors.Is(a, ErrConflict))) {
		t.Fatalf("concurrent writes: %v / %v", a, b)
	}
	if err := first.Delete(ctx, board.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if page, err := first.List(ctx, 0); err != nil || len(page.Items) != 1 || page.Items[0].Revision != 2 {
		t.Fatal(page, err)
	}
	if err := second.Delete(ctx, board.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Get(ctx, board.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted board: %v", err)
	}
}
