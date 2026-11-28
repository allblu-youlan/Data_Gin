package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/job"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMallWeatherLockWaitStoreReadsFixedAnchorWithoutMutation(t *testing.T) {
	created := time.Date(2026, 11, 28, 5, 34, 11, 0, time.UTC)
	for _, test := range []struct {
		name, status, taskType, window, wantKey string
		run, outbox                             bool
	}{
		{name: "running run", run: true, status: "running", taskType: job.TypeMallWeatherFast, window: "fast:22:202611281330"},
		{name: "terminal run", run: true, status: "success", taskType: job.TypeMallWeatherFast, window: "fast:22:202611281330"},
		{name: "failed run remains retryable", run: true, status: "failed", taskType: job.TypeMallWeatherFast, window: "fast:22:202611281330"},
		{name: "scheduled fallback", outbox: true, taskType: job.TypeMallWeatherFast, window: "fast:22:202611281330", wantKey: "mall:weather:fast:22:202611281330"},
		{name: "manual fallback", outbox: true, taskType: job.TypeMallWeatherManual, window: "manual:abc123", wantKey: "mall:weather:manual:abc123:v26_weather"},
		{name: "missing durable identity", taskType: job.TypeMallWeatherFast, window: "fast:22:202611281330", wantKey: "mall:weather:fast:22:202611281330"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &weatherLockStoreFixture{run: test.run, outbox: test.outbox, status: test.status, createdAt: created}
			store := &gormMallWeatherTaskStore{db: weatherLockStoreDB(t, fixture)}
			input := mallWeatherTaskStart{TaskType: test.taskType, TaskKind: "fast", Payload: job.MallTaskPayload{MallID: 22, TaskWindow: test.window, EndpointKind: caiyun.EndpointWeatherV26}}
			if test.taskType == job.TypeMallWeatherManual {
				input.TaskKind = "full"
			}
			state, err := store.LockWaitState(t.Context(), input)
			wantTime := created
			if !test.run && !test.outbox {
				wantTime = time.Time{}
			}
			if err != nil || !state.CreatedAt.Equal(wantTime) || state.Terminal != (test.status == "success") || fixture.runReads != 1 || fixture.outboxKey != test.wantKey {
				t.Fatalf("state=%+v error=%v fixture=%+v", state, err, fixture)
			}
			if len(fixture.runArgs) != 4 || fixture.runArgs[0].Value != int64(22) || fixture.runArgs[1].Value != caiyun.EndpointWeatherV26 || fixture.runArgs[2].Value != input.TaskKind || fixture.runArgs[3].Value != test.window {
				t.Fatalf("run identity changed: %v", fixture.runArgs)
			}
		})
	}
}

func TestMallWeatherLockWaitStorePreservesReadFailure(t *testing.T) {
	cause := errors.New("read unavailable")
	for _, stage := range []string{"run", "outbox"} {
		fixture := &weatherLockStoreFixture{failStage: stage, cause: cause}
		store := &gormMallWeatherTaskStore{db: weatherLockStoreDB(t, fixture)}
		_, err := store.LockWaitState(t.Context(), mallWeatherTaskStart{TaskType: job.TypeMallWeatherFast, TaskKind: "fast", Payload: job.MallTaskPayload{MallID: 22, TaskWindow: "fast:22:202611281330", EndpointKind: caiyun.EndpointWeatherV26}})
		if !errors.Is(err, cause) {
			t.Fatalf("%s read lost cause: %v", stage, err)
		}
	}
}

type weatherLockStoreFixture struct {
	run, outbox                  bool
	status, failStage, outboxKey string
	createdAt                    time.Time
	cause                        error
	runReads                     int
	runArgs                      []driver.NamedValue
}

func weatherLockStoreDB(t *testing.T, fixture *weatherLockStoreFixture) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(weatherLockStoreConnector{fixture})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type weatherLockStoreConnector struct{ fixture *weatherLockStoreFixture }

func (c weatherLockStoreConnector) Connect(context.Context) (driver.Conn, error) {
	return weatherLockStoreConn{c.fixture}, nil
}
func (weatherLockStoreConnector) Driver() driver.Driver { return weatherLockStoreDriver{} }

type weatherLockStoreDriver struct{}

func (weatherLockStoreDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type weatherLockStoreConn struct{ fixture *weatherLockStoreFixture }

func (weatherLockStoreConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (weatherLockStoreConn) Close() error                        { return nil }
func (weatherLockStoreConn) Begin() (driver.Tx, error) {
	return nil, errors.New("lock wait must not start a transaction")
}
func (c weatherLockStoreConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(query, "FOR UPDATE") || strings.Contains(query, "published_at") || strings.Contains(query, "available_at") || !strings.HasPrefix(query, "SELECT ") {
		return nil, errors.New("lock wait must only read an immutable anchor")
	}
	switch {
	case strings.Contains(query, "`mall_weather_fetch_runs`"):
		c.fixture.runReads++
		c.fixture.runArgs = args
		if c.fixture.failStage == "run" {
			return nil, c.fixture.cause
		}
		rows := &weatherLockStoreRows{columns: []string{"status", "created_at"}}
		if c.fixture.run {
			rows.values = []driver.Value{c.fixture.status, c.fixture.createdAt}
		}
		return rows, nil
	case strings.Contains(query, "`async_job_outbox`"):
		if len(args) != 1 {
			return nil, fmt.Errorf("unexpected outbox query: %s", query)
		}
		c.fixture.outboxKey, _ = args[0].Value.(string)
		if c.fixture.failStage == "outbox" {
			return nil, c.fixture.cause
		}
		rows := &weatherLockStoreRows{columns: []string{"created_at"}}
		if c.fixture.outbox {
			rows.values = []driver.Value{c.fixture.createdAt}
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

type weatherLockStoreRows struct {
	columns []string
	values  []driver.Value
}

func (r *weatherLockStoreRows) Columns() []string { return r.columns }
func (*weatherLockStoreRows) Close() error        { return nil }
func (r *weatherLockStoreRows) Next(values []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(values, r.values)
	r.values = nil
	return nil
}
