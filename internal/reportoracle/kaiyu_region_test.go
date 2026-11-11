package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/model"
)

type regionStoreRecorder struct {
	statements              []string
	args                    [][]driver.NamedValue
	commit, rollback, begin bool
	failAt                  int
	failure                 error
	query                   bojunQueryTestFunc
}
type regionTestConnector struct{ r *regionStoreRecorder }

func (c regionTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &regionTestConn{r: c.r}, nil
}
func (c regionTestConnector) Driver() driver.Driver { return regionTestDriver{r: c.r} }

type regionTestDriver struct{ r *regionStoreRecorder }

func (d regionTestDriver) Open(string) (driver.Conn, error) { return &regionTestConn{r: d.r}, nil }

type regionTestConn struct{ r *regionStoreRecorder }

func (*regionTestConn) Prepare(string) (driver.Stmt, error)      { return nil, driver.ErrSkip }
func (*regionTestConn) Close() error                             { return nil }
func (*regionTestConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func (c *regionTestConn) Begin() (driver.Tx, error) {
	c.r.begin = true
	return regionTestTx{r: c.r}, nil
}
func (c *regionTestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.r.statements = append(c.r.statements, q)
	c.r.args = append(c.r.args, append([]driver.NamedValue(nil), args...))
	if c.r.failAt == len(c.r.statements) {
		return nil, c.r.failure
	}
	return driver.RowsAffected(1), nil
}
func (c *regionTestConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.r.query(ctx, q, args)
}

type regionTestTx struct{ r *regionStoreRecorder }

func (tx regionTestTx) Commit() error   { tx.r.commit = true; return nil }
func (tx regionTestTx) Rollback() error { tx.r.rollback = true; return nil }
func newRegionStoreAdapter(t *testing.T, r *regionStoreRecorder) *Adapter {
	t.Helper()
	db := sql.OpenDB(regionTestConnector{r: r})
	t.Cleanup(func() { _ = db.Close() })
	return &Adapter{db: db}
}

func regionSnapshot() (model.KaiyuRegionSyncDay, []model.KaiyuRegionStayDaily) {
	day := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC)
	state := model.KaiyuRegionSyncDay{MallID: 7, ShopID: "shop'1", ShopName: "测试门店", StatDate: day, State: model.KaiyuRegionSyncSuccess, FetchedAt: day.Add(2 * time.Hour)}
	rows := []model.KaiyuRegionStayDaily{{MallID: 7, ShopID: state.ShopID, ShopName: state.ShopName, StatDate: day, RegionName: "童装'区域", ExposureCount: 100, StayCount: 0, StayRate: 0, AvgStayTime: 3.38}}
	return state, rows
}

func TestKaiyuRegionSnapshotCommitsAtomicReplacementAndShanghaiBindings(t *testing.T) {
	r := &regionStoreRecorder{}
	adapter := newRegionStoreAdapter(t, r)
	state, rows := regionSnapshot()
	if err := adapter.SaveKaiyuRegionDay(t.Context(), state, rows); err != nil {
		t.Fatal(err)
	}
	if !r.begin || !r.commit || r.rollback || len(r.statements) != 3 {
		t.Fatalf("transaction=%+v", r)
	}
	if !strings.HasPrefix(r.statements[0], "MERGE INTO YLZ_USER.KAIYU_REGION_SYNC_DAY") || !strings.HasPrefix(r.statements[1], "DELETE FROM") || !strings.HasPrefix(r.statements[2], "MERGE INTO YLZ_USER.KAIYU_REGION_STAY_DAILY") {
		t.Fatalf("replacement sequence=%v", r.statements)
	}
	var actual []any
	for _, v := range r.args[2] {
		actual = append(actual, v.Value)
	}
	want := []any{"shop'1", "2026-11-11", "童装'区域", uint(7), "测试门店", int64(100), int64(0), float64(0), float64(3.38), "2026-11-11 02:00:00"}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("bindings=%#v", actual)
	}
	for _, q := range r.statements {
		if strings.Contains(q, state.ShopID) || strings.Contains(q, rows[0].RegionName) {
			t.Fatal("source string was interpolated")
		}
	}
	if !strings.Contains(r.statements[0], "ELSE t.LAST_SUCCESS_AT END") {
		t.Fatal("previous success must survive empty/failed attempts")
	}
}

func TestKaiyuRegionSnapshotFailureRollsBackStatusAndFacts(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			want := errors.New("oracle write failed")
			r := &regionStoreRecorder{failAt: failAt, failure: want}
			adapter := newRegionStoreAdapter(t, r)
			state, rows := regionSnapshot()
			if err := adapter.SaveKaiyuRegionDay(t.Context(), state, rows); !errors.Is(err, want) {
				t.Fatalf("error=%v", err)
			}
			if r.commit || !r.rollback || len(r.statements) != failAt {
				t.Fatalf("partial snapshot committed: %+v", r)
			}
		})
	}
}

func TestKaiyuRegionEmptyAndFailedKeepFactsAndBoundError(t *testing.T) {
	for _, status := range []string{model.KaiyuRegionSyncEmpty, model.KaiyuRegionSyncFailed} {
		t.Run(status, func(t *testing.T) {
			r := &regionStoreRecorder{}
			adapter := newRegionStoreAdapter(t, r)
			state, _ := regionSnapshot()
			state.State = status
			state.ErrorMessage = strings.Repeat("错", 1002)
			if err := adapter.SaveKaiyuRegionDay(t.Context(), state, nil); err != nil {
				t.Fatal(err)
			}
			if !r.commit || len(r.statements) != 1 || strings.Contains(r.statements[0], "DELETE") {
				t.Fatal("empty attempt altered facts")
			}
			if r.args[0][5].Value != 0 || utf8.RuneCountInString(r.args[0][7].Value.(string)) != 1000 {
				t.Fatalf("status bindings=%v", r.args)
			}
		})
	}
}

func TestKaiyuRegionRejectsInvalidSnapshotBeforeTransaction(t *testing.T) {
	for _, name := range []string{"empty-success", "failed-with-facts", "other-shop", "duplicate-region", "negative-metric"} {
		t.Run(name, func(t *testing.T) {
			state, rows := regionSnapshot()
			switch name {
			case "empty-success":
				rows = nil
			case "failed-with-facts":
				state.State = model.KaiyuRegionSyncFailed
			case "other-shop":
				rows[0].ShopID = "other"
			case "duplicate-region":
				rows = append(rows, rows[0])
			case "negative-metric":
				rows[0].StayCount = -1
			}
			r := &regionStoreRecorder{}
			if err := newRegionStoreAdapter(t, r).SaveKaiyuRegionDay(t.Context(), state, rows); err == nil || r.begin {
				t.Fatalf("err=%v transaction=%v", err, r.begin)
			}
		})
	}
}

func TestKaiyuRegionInitializeCreatesAndReusesOwnedTables(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "reuse"}[exists], func(t *testing.T) {
			r := &regionStoreRecorder{}
			validations := 0
			r.query = func(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
				switch {
				case q == "SELECT USER FROM DUAL":
					return &bojunQueryTestRows{columns: []string{"USER"}, values: [][]driver.Value{{"YLZ_USER"}}}, nil
				case strings.Contains(q, "USER_TABLES") || strings.Contains(q, "USER_INDEXES"):
					if len(args) != 1 || !strings.Contains(args[0].Value.(string), "KAIYU_REGION_") {
						t.Fatal("table check not bound")
					}
					count := int64(0)
					if exists {
						count = 1
					}
					return &bojunQueryTestRows{columns: []string{"COUNT"}, values: [][]driver.Value{{count}}}, nil
				case strings.Contains(q, "WHERE 1=0"):
					validations++
					return &bojunQueryTestRows{}, nil
				default:
					t.Fatalf("unexpected query: %s", q)
					return nil, nil
				}
			}
			if err := newRegionStoreAdapter(t, r).InitializeKaiyuRegionStore(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := 4
			if exists {
				want = 0
			}
			if len(r.statements) != want || validations != 2 {
				t.Fatalf("ddl=%d validations=%d", len(r.statements), validations)
			}
		})
	}
	r := &regionStoreRecorder{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return &bojunQueryTestRows{columns: []string{"USER"}, values: [][]driver.Value{{"OTHER_USER"}}}, nil
	}}
	if err := newRegionStoreAdapter(t, r).InitializeKaiyuRegionStore(t.Context()); err == nil || len(r.statements) != 0 {
		t.Fatalf("wrong owner allowed, err=%v", err)
	}
}
