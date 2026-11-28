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

	appConfig "gin-biz-web-api/config"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestFeishuMentionInitializeExistingScheduleDoesNotWrite(t *testing.T) {
	state := &mentionInitializeSQLState{exists: true}
	s := newMentionInitializeTestService(t, state)
	for i := 0; i < 2; i++ {
		if err := s.Initialize(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	// No INSERT or UPDATE can reset the existing cursor, clock, or enabled flag.
	if state.inserts != 0 || state.cancels != 0 || state.scheduleReads != 2 || state.commits != 2 {
		t.Fatalf("existing schedule was written: %+v", state)
	}
}

func TestFeishuMentionInitializeCreatesMissingScheduleOnce(t *testing.T) {
	state := &mentionInitializeSQLState{}
	s := newMentionInitializeTestService(t, state)
	for i := 0; i < 2; i++ {
		if err := s.Initialize(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if state.inserts != 1 || state.commits != 2 || !state.exists {
		t.Fatalf("schedule initialization=%+v", state)
	}
	if !strings.Contains(state.insertSQL, "ON DUPLICATE KEY UPDATE `id`=`id`") {
		t.Fatalf("concurrent initialization lost its unique-key guard: %s", state.insertSQL)
	}
	start, cutoff := false, false
	for _, arg := range state.insertArgs {
		if value, ok := arg.Value.(time.Time); ok {
			start = start || value.Equal(s.now())
			cutoff = cutoff || value.Equal(time.Date(2026, 11, 28, 18, 0, 0, 0, mentionLocation()))
		}
	}
	if !start || !cutoff {
		t.Fatalf("missing first window start/cutoff: %+v", state.insertArgs)
	}
}

func TestFeishuMentionInitializeRetainsCancellationOnExistingSchedule(t *testing.T) {
	for _, test := range []struct {
		name, reason       string
		personal, followup bool
		initial            bool
	}{
		{name: "all personal reminders disabled", reason: "personal_reminder_disabled", initial: true},
		{name: "followup disabled", reason: "followup_disabled", personal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &mentionInitializeSQLState{exists: true}
			s := newMentionInitializeTestService(t, state)
			s.cfg.PersonalReminderEnabled, s.cfg.FollowupEnabled = test.personal, test.followup
			if err := s.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			values := map[any]bool{}
			for _, arg := range state.cancelArgs {
				values[arg.Value] = true
			}
			if state.inserts != 0 || state.cancels != 1 || !values["cancelled"] || !values[test.reason] || !values[s.appID] || !values["pending"] || !values["retry_wait"] || !values["failed"] || !values["followup_reminder"] || values["initial_reminder"] != test.initial || values["card_update"] {
				t.Fatalf("cancellation behavior changed: %+v", state)
			}
		})
	}
}

func TestFeishuMentionInitializePreservesErrorPhaseAndCause(t *testing.T) {
	for _, test := range []struct{ stage, want string }{
		{"settings", "read digest settings"},
		{"begin", "phase=begin"},
		{"schedule", "read digest schedule"},
		{"create", "create digest schedule"},
		{"cancel", "cancel disabled reminders"},
		{"commit", "phase=commit"},
	} {
		t.Run(test.stage, func(t *testing.T) {
			state := &mentionInitializeSQLState{failStage: test.stage, cause: context.DeadlineExceeded}
			s := newMentionInitializeTestService(t, state)
			s.cfg.PersonalReminderEnabled = false
			err := s.Initialize(t.Context())
			if !errors.Is(err, state.cause) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("phase or cause lost: %v; want %q", err, test.want)
			}
			if state.begins > 1 || state.inserts > 1 || state.cancels > 1 {
				t.Fatalf("timeout must not replay work: %+v", state)
			}
		})
	}
}

func TestFeishuMentionInitializeRetriesOnlyFailedBegin(t *testing.T) {
	state := &mentionInitializeSQLState{exists: true, failBeginOnce: true}
	s := newMentionInitializeTestService(t, state)
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if state.begins != 2 || state.scheduleReads != 1 || state.commits != 1 || state.inserts != 0 {
		t.Fatalf("BEGIN recovery replayed work: %+v", state)
	}
}

func TestFeishuMentionInitializeDoesNotRetainCancelledContext(t *testing.T) {
	state := &mentionInitializeSQLState{exists: true}
	s := newMentionInitializeTestService(t, state)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Initialize(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled initialization: %v", err)
	}
	if state.begins != 0 || state.settingsReads != 0 {
		t.Fatalf("cancelled initialization accessed database: %+v", state)
	}
	if err := s.Initialize(t.Context()); err != nil {
		t.Fatalf("fresh initialization reused cancelled context: %v", err)
	}
}

type mentionInitializeSQLState struct {
	exists, failBeginOnce                 bool
	failStage, insertSQL                  string
	cause                                 error
	settingsReads, scheduleReads, inserts int
	cancels, begins, commits, rollbacks   int
	insertArgs, cancelArgs                []driver.NamedValue
}

func newMentionInitializeTestService(t *testing.T, state *mentionInitializeSQLState) *FeishuMentionService {
	t.Helper()
	sqlDB := sql.OpenDB(mentionInitializeConnector{state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	s := NewFeishuMentionService(db, "mention-initialize-test", appConfig.FeishuMentionConfig{Enabled: true, PersonalReminderEnabled: true, FollowupEnabled: true, DigestTime: "18:00"})
	s.now = func() time.Time { return time.Date(2026, 11, 28, 10, 0, 0, 0, mentionLocation()) }
	return s
}

type mentionInitializeConnector struct{ state *mentionInitializeSQLState }

func (c mentionInitializeConnector) Connect(context.Context) (driver.Conn, error) {
	return &mentionInitializeConn{state: c.state, valid: true}, nil
}
func (mentionInitializeConnector) Driver() driver.Driver { return mentionInitializeDriver{} }

type mentionInitializeDriver struct{}

func (mentionInitializeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use test connector")
}

type mentionInitializeConn struct {
	state *mentionInitializeSQLState
	valid bool
}

func (*mentionInitializeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *mentionInitializeConn) Close() error                      { c.valid = false; return nil }
func (c *mentionInitializeConn) IsValid() bool                     { return c.valid }
func (c *mentionInitializeConn) Begin() (driver.Tx, error) {
	c.state.begins++
	if c.state.failBeginOnce && c.state.begins == 1 {
		c.valid = false
		return nil, mysqlDriver.ErrInvalidConn
	}
	if c.state.failStage == "begin" {
		return nil, c.state.cause
	}
	return mentionInitializeTx{c.state}, nil
}
func (c *mentionInitializeConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(query, "`runtime_configs`"):
		c.state.settingsReads++
		if c.state.failStage == "settings" {
			return nil, c.state.cause
		}
		return &mentionInitializeRows{}, nil
	case strings.Contains(query, "`feishu_digest_schedules`"):
		c.state.scheduleReads++
		if c.state.failStage == "schedule" {
			return nil, c.state.cause
		}
		return &mentionInitializeRows{hasRow: c.state.exists}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}
func (c *mentionInitializeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(query, "INSERT INTO `feishu_digest_schedules`"):
		c.state.inserts++
		c.state.insertSQL, c.state.insertArgs = query, append([]driver.NamedValue(nil), args...)
		if c.state.failStage == "create" {
			return nil, c.state.cause
		}
		c.state.exists = true
	case strings.HasPrefix(query, "UPDATE `feishu_delivery_jobs`"):
		c.state.cancels++
		c.state.cancelArgs = append([]driver.NamedValue(nil), args...)
		if c.state.failStage == "cancel" {
			return nil, c.state.cause
		}
	default:
		return nil, fmt.Errorf("unexpected write: %s", query)
	}
	return mentionInitializeResult{}, nil
}

type mentionInitializeResult struct{}

func (mentionInitializeResult) LastInsertId() (int64, error) { return 1, nil }
func (mentionInitializeResult) RowsAffected() (int64, error) { return 1, nil }

type mentionInitializeTx struct{ state *mentionInitializeSQLState }

func (tx mentionInitializeTx) Commit() error {
	tx.state.commits++
	if tx.state.failStage == "commit" {
		return tx.state.cause
	}
	return nil
}
func (tx mentionInitializeTx) Rollback() error { tx.state.rollbacks++; return nil }

type mentionInitializeRows struct{ hasRow bool }

func (*mentionInitializeRows) Columns() []string { return []string{"id"} }
func (*mentionInitializeRows) Close() error      { return nil }
func (r *mentionInitializeRows) Next(values []driver.Value) error {
	if !r.hasRow {
		return io.EOF
	}
	r.hasRow = false
	values[0] = int64(1)
	return nil
}
