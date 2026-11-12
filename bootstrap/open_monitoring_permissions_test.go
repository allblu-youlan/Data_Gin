package bootstrap

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"testing"

	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOpenMonitoringPermissionMigrationIsAdditiveAndIdempotent(t *testing.T) {
	state := newMonitoringPermissionMigrationState(t)
	db := newMonitoringPermissionMigrationDB(t, state)
	if err := applyOpenMonitoringPermissionMigration(db); err != nil {
		t.Fatal(err)
	}
	if !state.marker || len(state.permissions) != 5 || len(state.grants) != 10 {
		t.Fatalf("migration did not add four permissions and eight grants: %#v", state)
	}
	for _, existing := range []string{"1:existing.read", "9:custom.read"} {
		if !state.grants[existing] {
			t.Fatalf("existing grant %s was removed", existing)
		}
	}
	for _, code := range []string{model.PermissionMonitoringAPIRead, model.PermissionMonitoringHourlyRead, model.PermissionMonitoringDailyRead, model.PermissionMonitoringAreaRead} {
		if !state.permissions[code] || !state.grants["1:"+code] || !state.grants["2:"+code] {
			t.Fatalf("missing monitoring permission or system role grant: %s", code)
		}
	}
	writes := state.writes
	if err := applyOpenMonitoringPermissionMigration(db); err != nil {
		t.Fatal(err)
	}
	if state.writes != writes {
		t.Fatal("applied migration wrote permissions or grants again")
	}
}

func TestOpenMonitoringPermissionMigrationRollsBackAndRetries(t *testing.T) {
	for _, failure := range []string{"role_permissions", "app_schema_versions"} {
		t.Run(failure, func(t *testing.T) {
			state := newMonitoringPermissionMigrationState(t)
			state.failTable = failure
			db := newMonitoringPermissionMigrationDB(t, state)
			if err := applyOpenMonitoringPermissionMigration(db); err == nil {
				t.Fatal("expected injected migration failure")
			}
			if state.marker || len(state.permissions) != 1 || len(state.grants) != 2 {
				t.Fatal("failed migration left a marker or partial permission changes")
			}
			state.failTable = ""
			if err := applyOpenMonitoringPermissionMigration(db); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if !state.marker || len(state.permissions) != 5 || len(state.grants) != 10 {
				t.Fatal("retry did not complete the migration")
			}
		})
	}
}

func TestOpenMonitoringPermissionMigrationWithoutSystemRoles(t *testing.T) {
	state := newMonitoringPermissionMigrationState(t)
	state.roles = nil
	if err := applyOpenMonitoringPermissionMigration(newMonitoringPermissionMigrationDB(t, state)); err != nil {
		t.Fatal(err)
	}
	if !state.marker || len(state.permissions) != 5 || len(state.grants) != 2 {
		t.Fatal("missing system roles should only add the permission catalog")
	}
}

type monitoringPermissionMigrationState struct {
	t           *testing.T
	permissions map[string]bool
	grants      map[string]bool
	roles       [][]driver.Value
	marker      bool
	failTable   string
	writes      int
}

func newMonitoringPermissionMigrationState(t *testing.T) *monitoringPermissionMigrationState {
	return &monitoringPermissionMigrationState{
		t: t, permissions: map[string]bool{"existing.read": true},
		grants: map[string]bool{"1:existing.read": true, "9:custom.read": true},
		roles:  [][]driver.Value{{int64(1), model.RoleCodeAdmin}, {int64(2), model.RoleCodeOperator}},
	}
}

func newMonitoringPermissionMigrationDB(t *testing.T, state *monitoringPermissionMigrationState) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(monitoringPermissionMigrationConnector{state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type monitoringPermissionMigrationConnector struct {
	state *monitoringPermissionMigrationState
}

func (connector monitoringPermissionMigrationConnector) Connect(context.Context) (driver.Conn, error) {
	return &monitoringPermissionMigrationConnection{state: connector.state}, nil
}
func (connector monitoringPermissionMigrationConnector) Driver() driver.Driver {
	return monitoringPermissionMigrationDriver{connector}
}

type monitoringPermissionMigrationDriver struct {
	monitoringPermissionMigrationConnector
}

func (testDriver monitoringPermissionMigrationDriver) Open(string) (driver.Conn, error) {
	return testDriver.Connect(context.Background())
}

type monitoringPermissionMigrationConnection struct {
	state   *monitoringPermissionMigrationState
	pending *monitoringPermissionMigrationState
}

func (*monitoringPermissionMigrationConnection) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}
func (*monitoringPermissionMigrationConnection) Close() error { return nil }
func (conn *monitoringPermissionMigrationConnection) Begin() (driver.Tx, error) {
	copyState := *conn.state
	copyState.permissions = maps.Clone(conn.state.permissions)
	copyState.grants = maps.Clone(conn.state.grants)
	conn.pending = &copyState
	return conn, nil
}
func (conn *monitoringPermissionMigrationConnection) Commit() error {
	*conn.state = *conn.pending
	conn.pending = nil
	return nil
}
func (conn *monitoringPermissionMigrationConnection) Rollback() error {
	conn.pending = nil
	return nil
}

func (conn *monitoringPermissionMigrationConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	state := conn.pending
	if state == nil {
		return nil, errors.New("query outside migration transaction")
	}
	switch {
	case strings.Contains(query, "FROM `app_schema_versions`"):
		if len(args) != 1 || args[0].Value != openMonitoringPermissionMigrationVersion {
			state.t.Fatalf("unexpected marker query: %s %#v", query, args)
		}
		count := int64(0)
		if state.marker {
			count = 1
		}
		return &monitoringPermissionMigrationRows{columns: []string{"count(*)"}, values: [][]driver.Value{{count}}}, nil
	case strings.Contains(query, "FROM `roles`"):
		if !strings.Contains(query, "code IN (?,?) AND is_system = ?") || len(args) != 3 || args[0].Value != model.RoleCodeAdmin || args[1].Value != model.RoleCodeOperator || args[2].Value != true {
			state.t.Fatalf("role lookup must select existing system admin/operator only: %s %#v", query, args)
		}
		return &monitoringPermissionMigrationRows{columns: []string{"id", "code"}, values: state.roles}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (conn *monitoringPermissionMigrationConnection) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	state := conn.pending
	if state == nil {
		return nil, errors.New("write outside migration transaction")
	}
	// No account, scope, assignment or existing grant may be updated or deleted.
	for _, table := range []string{"permissions", "role_permissions", "app_schema_versions"} {
		if !strings.HasPrefix(query, "INSERT INTO `"+table+"`") {
			continue
		}
		if state.failTable == table {
			return nil, errors.New("injected " + table + " failure")
		}
		state.writes++
		switch table {
		case "permissions":
			if !strings.Contains(query, "ON DUPLICATE KEY UPDATE") {
				return nil, errors.New("permission catalog must support existing rows")
			}
			for _, arg := range args {
				if code, ok := arg.Value.(string); ok && strings.HasPrefix(code, "monitoring.") {
					state.permissions[code] = true
				}
			}
		case "role_permissions":
			if !strings.Contains(query, "ON DUPLICATE KEY UPDATE `role_id`=`role_id`") {
				return nil, errors.New("existing role grants must be preserved")
			}
			for i := 0; i < len(args); i += 3 {
				state.grants[fmt.Sprint(args[i].Value)+":"+fmt.Sprint(args[i+1].Value)] = true
			}
		case "app_schema_versions":
			if len(args) != 2 || args[0].Value != openMonitoringPermissionMigrationVersion {
				return nil, errors.New("unexpected migration marker")
			}
			state.marker = true
		}
		return driver.RowsAffected(1), nil
	}
	return nil, fmt.Errorf("unexpected mutation, existing account scopes and grants must be preserved: %s", query)
}

type monitoringPermissionMigrationRows struct {
	columns []string
	values  [][]driver.Value
}

func (rows *monitoringPermissionMigrationRows) Columns() []string { return rows.columns }
func (*monitoringPermissionMigrationRows) Close() error           { return nil }
func (rows *monitoringPermissionMigrationRows) Next(dest []driver.Value) error {
	if len(rows.values) == 0 {
		return io.EOF
	}
	copy(dest, rows.values[0])
	rows.values = rows.values[1:]
	return nil
}
