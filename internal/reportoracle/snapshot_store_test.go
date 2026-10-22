package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

type snapshotStoreTestState struct {
	columns     map[string]int
	executed    []string
	queryError  error
	createError error
	raceCreate  bool
}

type snapshotStoreTestConnector struct{ state *snapshotStoreTestState }

func (c snapshotStoreTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &snapshotStoreTestConnection{state: c.state}, nil
}
func (c snapshotStoreTestConnector) Driver() driver.Driver { return snapshotStoreTestDriver{c} }

type snapshotStoreTestDriver struct{ connector snapshotStoreTestConnector }

func (d snapshotStoreTestDriver) Open(string) (driver.Conn, error) {
	return d.connector.Connect(context.Background())
}

type snapshotStoreTestConnection struct{ state *snapshotStoreTestState }

func (*snapshotStoreTestConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*snapshotStoreTestConnection) Close() error                        { return nil }
func (*snapshotStoreTestConnection) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c *snapshotStoreTestConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.state.queryError != nil {
		return nil, c.state.queryError
	}
	count := 0
	if strings.Contains(query, "user_tab_columns") {
		count = c.state.columns[JSONSnapshotManifestTable] + c.state.columns[JSONSnapshotRowsTable]
	} else if len(args) == 1 && c.state.columns[args[0].Value.(string)] > 0 {
		count = 1
	}
	return &bojunQueryTestRows{columns: []string{"COUNT"}, values: [][]driver.Value{{int64(count)}}}, nil
}
func (c *snapshotStoreTestConnection) ExecContext(ctx context.Context, statement string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.executed = append(c.state.executed, statement)
	if c.state.createError != nil {
		return nil, c.state.createError
	}
	if strings.HasPrefix(statement, "CREATE TABLE "+JSONSnapshotManifestTable+" (") {
		c.state.columns[JSONSnapshotManifestTable] = 6
	} else if strings.HasPrefix(statement, "CREATE TABLE "+JSONSnapshotRowsTable+" (") {
		c.state.columns[JSONSnapshotRowsTable] = 3
	} else {
		return nil, errors.New("unexpected schema operation")
	}
	if c.state.raceCreate {
		return nil, oracleTestError{code: 955}
	}
	return driver.RowsAffected(0), nil
}

func TestInitializeJSONSnapshotStorePreservesExistingObjectsAndIsRepeatable(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		columns map[string]int
		creates int
	}{
		{name: "missing store", columns: map[string]int{}, creates: 2},
		{name: "manifest already exists", columns: map[string]int{JSONSnapshotManifestTable: 6}, creates: 1},
		{name: "already initialized", columns: map[string]int{JSONSnapshotManifestTable: 6, JSONSnapshotRowsTable: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &snapshotStoreTestState{columns: test.columns}
			db := sql.OpenDB(snapshotStoreTestConnector{state})
			t.Cleanup(func() { _ = db.Close() })
			adapter := &Adapter{db: db}
			for range 2 {
				if err := adapter.InitializeJSONSnapshotStore(t.Context()); err != nil {
					t.Fatalf("InitializeJSONSnapshotStore() error = %v", err)
				}
			}
			if len(state.executed) != test.creates {
				t.Fatalf("created %d tables, want %d", len(state.executed), test.creates)
			}
			for _, statement := range state.executed {
				if !strings.HasPrefix(statement, "CREATE TABLE REPORT_RUN_SNAPSHOT") || strings.Contains(statement, "DROP ") || strings.Contains(statement, "TRUNCATE ") || strings.Contains(statement, "ALTER ") {
					t.Fatalf("unexpected schema mutation: %s", statement)
				}
			}
		})
	}
}

func TestInitializeJSONSnapshotStoreDoesNotRepairOrHideFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		state     snapshotStoreTestState
		wantError bool
		creates   int
	}{
		{name: "incompatible existing tables", state: snapshotStoreTestState{columns: map[string]int{JSONSnapshotManifestTable: 5, JSONSnapshotRowsTable: 3}}, wantError: true},
		{name: "metadata read failed", state: snapshotStoreTestState{columns: map[string]int{}, queryError: errors.New("metadata unavailable")}, wantError: true},
		{name: "DDL permission denied", state: snapshotStoreTestState{columns: map[string]int{}, createError: oracleTestError{code: 1031}}, wantError: true, creates: 1},
		{name: "another initializer created both tables", state: snapshotStoreTestState{columns: map[string]int{}, raceCreate: true}, creates: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := sql.OpenDB(snapshotStoreTestConnector{&test.state})
			t.Cleanup(func() { _ = db.Close() })
			err := (&Adapter{db: db}).InitializeJSONSnapshotStore(t.Context())
			if (err != nil) != test.wantError || len(test.state.executed) != test.creates {
				t.Fatalf("error = %v; executed %d statements, want error=%t statements=%d", err, len(test.state.executed), test.wantError, test.creates)
			}
			if test.state.queryError != nil && !errors.Is(err, test.state.queryError) {
				t.Fatalf("metadata failure was lost: %v", err)
			}
		})
	}
}

func TestInitializeJSONSnapshotStoreRespectsCancellation(t *testing.T) {
	t.Parallel()
	state := &snapshotStoreTestState{columns: map[string]int{}}
	db := sql.OpenDB(snapshotStoreTestConnector{state})
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (&Adapter{db: db}).InitializeJSONSnapshotStore(ctx); !errors.Is(err, context.Canceled) || len(state.executed) != 0 {
		t.Fatalf("cancelled initialization error = %v; writes=%d", err, len(state.executed))
	}
}
