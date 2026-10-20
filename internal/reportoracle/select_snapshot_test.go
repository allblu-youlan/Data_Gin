package reportoracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"gin-biz-web-api/internal/reportdataset"
	"github.com/godror/godror"
	"io"
	"strings"
	"testing"
)

type selectSnapshotState struct {
	values             [][]driver.Value
	columns            []string
	statements         []string
	rows               []string
	commits, rollbacks int
	nextError          error
}
type selectSnapshotConnector struct{ state *selectSnapshotState }

func (c selectSnapshotConnector) Connect(context.Context) (driver.Conn, error) {
	return &selectSnapshotConnection{state: c.state}, nil
}
func (c selectSnapshotConnector) Driver() driver.Driver { return selectSnapshotDriver{state: c.state} }

type selectSnapshotDriver struct{ state *selectSnapshotState }

func (d selectSnapshotDriver) Open(string) (driver.Conn, error) {
	return &selectSnapshotConnection{state: d.state}, nil
}

type selectSnapshotConnection struct{ state *selectSnapshotState }

func (c *selectSnapshotConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *selectSnapshotConnection) Close() error                        { return nil }
func (c *selectSnapshotConnection) Begin() (driver.Tx, error) {
	return &selectSnapshotTx{state: c.state}, nil
}
func (c *selectSnapshotConnection) CheckNamedValue(*driver.NamedValue) error { return nil }
func (c *selectSnapshotConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &selectSnapshotRows{bojunQueryTestRows: bojunQueryTestRows{columns: c.state.columns, values: c.state.values, nextErr: c.state.nextError}}, nil
}
func (c *selectSnapshotConnection) ExecContext(_ context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
	c.state.statements = append(c.state.statements, statement)
	if strings.HasPrefix(statement, "INSERT ALL") {
		for i := 2; i < len(args); i += 3 {
			lob := args[i].Value.(godror.Lob)
			data, e := io.ReadAll(lob.Reader)
			if e != nil {
				return nil, e
			}
			c.state.rows = append(c.state.rows, string(data))
		}
	}
	return driver.RowsAffected(1), nil
}

type selectSnapshotTx struct{ state *selectSnapshotState }

func (tx *selectSnapshotTx) Commit() error   { tx.state.commits++; return nil }
func (tx *selectSnapshotTx) Rollback() error { tx.state.rollbacks++; return nil }

type selectSnapshotRows struct{ bojunQueryTestRows }

func (r *selectSnapshotRows) ColumnTypeDatabaseTypeName(int) string { return "NUMBER" }
func TestSelectSnapshotIsBoundedTransactionalAndNeverWritesSource(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rows, bytes int64
		readError   error
		wantError   bool
	}{{"complete", 3, 10000, nil, false}, {"row limit", 1, 10000, nil, true}, {"byte limit", 3, 1, nil, true}, {"cancelled rows", 3, 10000, context.Canceled, true}} {
		t.Run(tc.name, func(t *testing.T) {
			state := &selectSnapshotState{columns: []string{"C001"}, values: [][]driver.Value{{"9007199254740993"}, {"1000000000000000001"}}, nextError: tc.readError}
			db := sql.OpenDB(selectSnapshotConnector{state: state})
			defer db.Close()
			a := &Adapter{db: db}
			c := reportdataset.Compiled{SchemaHash: strings.Repeat("a", 64), Outputs: []reportdataset.Output{{Name: "O001", ValueName: "C001", OracleType: "NUMBER"}}}
			snapshot, e := a.ExecuteSelectSnapshot(t.Context(), "run-one", c, reportdataset.Plan{SQL: "SELECT ID AS C001 FROM BOSNDS3.M_RETAILITEM@LINK ORDER BY ID", Columns: []string{"C001"}}, tc.rows, tc.bytes)
			if (e != nil) != tc.wantError {
				t.Fatalf("error=%v", e)
			}
			if tc.wantError {
				if state.commits != 0 || state.rollbacks != 1 {
					t.Fatalf("partial snapshot committed: %#v", state)
				}
			} else {
				if snapshot.RowCount != 2 || state.commits != 1 || len(state.rows) != 2 || !strings.Contains(state.rows[0], `"9007199254740993"`) {
					t.Fatalf("snapshot=%#v state=%#v", snapshot, state)
				}
			}
			for _, statement := range state.statements {
				if strings.Contains(statement, "BOSNDS3") || strings.HasPrefix(statement, "DELETE") && !strings.Contains(statement, "WHERE RUN_ID = :1") {
					t.Fatalf("unsafe write: %s", statement)
				}
			}
		})
	}
}
func TestSnapshotSchemaV2AndLegacyAreCompatible(t *testing.T) {
	legacy := []byte(`[{"name":"C001","oracleType":"NUMBER","nullable":false}]`)
	v2, _ := json.Marshal(selectSnapshotSchema{Version: 2, Columns: []CursorColumn{{Name: "C001", OracleType: "NUMBER"}}, DatasetSchemaHash: strings.Repeat("b", 64)})
	for _, tc := range []struct {
		name string
		raw  []byte
	}{{"legacy", legacy}, {"v2", v2}} {
		t.Run(tc.name, func(t *testing.T) {
			columns, e := decodeSnapshotSchema(tc.raw)
			if e != nil || len(columns) != 1 || columns[0].Name != "C001" {
				t.Fatalf("%#v %v", columns, e)
			}
		})
	}
	if _, e := decodeSnapshotSchema([]byte(`{"version":3,"columns":[]}`)); e == nil {
		t.Fatal("unknown schema accepted")
	}
}
func TestSelectNumbersCannotSilentlyUseFloat(t *testing.T) {
	if _, e := selectValue(float64(9007199254740993), "NUMBER"); e == nil {
		t.Fatal("accepted precision loss")
	}
	if _, e := selectValue(godror.Number("12345678901234567890.1234567890"), "NUMBER"); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(ErrSelectLimitExceeded, ErrSelectLimitExceeded) {
		t.Fatal("missing stable limit error")
	}
}
