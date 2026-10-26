package data_svc

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/godror/godror"
)

func TestOracleExcelImportArrayBindsPreserveTypesPrecisionAndLeadingNulls(t *testing.T) {
	columns := []reportoracle.ResultColumn{
		{Name: "CODE", DataType: "VARCHAR2"},
		{Name: "AMOUNT", DataType: "NUMBER"},
		{Name: "CREATED_AT", DataType: "DATE"},
		{Name: "UPDATED_AT", DataType: "TIMESTAMP(6)"},
		{Name: "NOTES", DataType: "CLOB"},
		{Name: "DETAIL", DataType: "NCLOB"},
	}
	date := time.Date(2026, 10, 23, 12, 30, 45, 123000000, time.FixedZone("CST", 8*60*60))
	rows := [][]interface{}{
		{nil, nil, nil, nil, nil, nil},
		{"000123", godror.Number("12345678901234567890.125"), date, date, godror.Lob{Reader: strings.NewReader("中文🙂"), IsClob: true}, godror.Lob{Reader: strings.NewReader("备注"), IsClob: true}},
		{"000124", godror.Number("0"), date, date, nil, nil},
	}
	arguments, err := oracleExcelImportArrayBinds(columns, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(arguments) != len(columns) || !reflect.DeepEqual(arguments[0], []string{"", "000123", "000124"}) || !reflect.DeepEqual(arguments[1], []godror.Number{"", "12345678901234567890.125", "0"}) {
		t.Fatalf("lost native binding or numeric precision: %v", arguments)
	}
	for _, index := range []int{2, 3} {
		values, ok := arguments[index].([]time.Time)
		if !ok || !values[0].IsZero() || !values[1].Equal(date) || values[1].Nanosecond() != date.Nanosecond() {
			t.Fatalf("dates lost NULL, timezone or fractional seconds: %v", arguments[index])
		}
	}
	if !reflect.DeepEqual(arguments[4], []string{"", "中文🙂", ""}) {
		t.Fatalf("short CLOB did not use string array binding: %v", arguments[4])
	}
	reader := rows[1][4].(godror.Lob).Reader.(*strings.Reader)
	if int64(reader.Len()) != reader.Size() {
		t.Fatal("short CLOB conversion consumed the original reader")
	}
	for index, want := range map[int]string{5: "备注"} {
		values, ok := arguments[index].([]godror.Lob)
		if !ok || len(values) != 3 {
			t.Fatalf("LOB binding has wrong type: %T", arguments[index])
		}
		for _, value := range values {
			if !value.IsClob {
				t.Fatal("a leading NULL CLOB would make godror bind the column as BLOB")
			}
		}
		if values[0].Reader != nil || values[2].Reader != nil {
			t.Fatal("NULL CLOB became a non-null value")
		}
		text, err := io.ReadAll(values[1].Reader)
		if err != nil || string(text) != want {
			t.Fatalf("LOB content changed: %q error=%v", text, err)
		}
	}
}

func TestOracleExcelImportShortClobBindsRespectByteBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		fast bool
	}{
		{"ASCII boundary", strings.Repeat("x", 4000), true},
		{"ASCII over boundary", strings.Repeat("x", 4001), false},
		{"Chinese and emoji boundary", strings.Repeat("中", 1332) + "🙂", true},
		{"Chinese and emoji over boundary", strings.Repeat("中", 1332) + "🙂x", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := strings.NewReader(test.text)
			rows := [][]interface{}{{nil}, {godror.Lob{Reader: reader, IsClob: true}}, {nil}}
			arguments, err := oracleExcelImportArrayBinds([]reportoracle.ResultColumn{{Name: "NOTES", DataType: "CLOB"}}, rows)
			if err != nil {
				t.Fatal(err)
			}
			texts, fast := arguments[0].([]string)
			if fast != test.fast || fast && !reflect.DeepEqual(texts, []string{"", test.text, ""}) {
				t.Fatalf("short CLOB binding=%T value=%v fast=%t", arguments[0], arguments[0], fast)
			}
			if int64(reader.Len()) != reader.Size() {
				t.Fatal("CLOB binding consumed its reader")
			}
			if !fast {
				values := arguments[0].([]godror.Lob)
				if values[0].Reader != nil || !values[0].IsClob || values[1].Reader != reader || !values[2].IsClob {
					t.Fatalf("fallback changed NULL or LOB binding: %+v", values)
				}
			}
		})
	}
}

func TestOracleExcelImportClobFallbackNeverConsumesReaders(t *testing.T) {
	consumed := strings.NewReader("already partly read")
	if _, err := consumed.ReadByte(); err != nil {
		t.Fatal(err)
	}
	unknown := bytes.NewBufferString("unknown reader type")
	for _, reader := range []io.Reader{strings.NewReader(strings.Repeat("L", 4001)), unknown, consumed, strings.NewReader("")} {
		short := strings.NewReader("keep me intact")
		rows := [][]interface{}{{nil}, {godror.Lob{Reader: short, IsClob: true}}, {godror.Lob{Reader: reader, IsClob: true}}}
		arguments, err := oracleExcelImportArrayBinds([]reportoracle.ResultColumn{{Name: "NOTES", DataType: "CLOB"}}, rows)
		if err != nil {
			t.Fatal(err)
		}
		values, ok := arguments[0].([]godror.Lob)
		if !ok || !values[0].IsClob || values[0].Reader != nil || values[1].Reader != short || values[2].Reader != reader || short.Len() != len("keep me intact") {
			t.Fatalf("mixed CLOB fallback changed its readers: %T %v", arguments[0], arguments[0])
		}
	}
	if unknown.Len() != len("unknown reader type") || consumed.Len() != len("already partly read")-1 {
		t.Fatal("fallback consumed an unknown or previously consumed reader")
	}
}

func TestOracleExcelImportArrayBindsRejectMismatchedValues(t *testing.T) {
	for _, values := range [][][]interface{}{{{"wrong numeric type"}}, {{}}} {
		_, err := oracleExcelImportArrayBinds([]reportoracle.ResultColumn{{Name: "AMOUNT", DataType: "NUMBER"}}, values)
		if err == nil || !strings.Contains(err.Error(), "AMOUNT") {
			t.Fatalf("invalid binding was accepted: %v", err)
		}
	}
}

func TestOracleExcelImportCancellationPreventsWritesAndCommit(t *testing.T) {
	path := oracleImportWorkbook(t, "cancel.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "金额"}, {"A", "10"}, {"B", "20"}}})
	for _, stage := range []string{"importing", "committing"} {
		t.Run(stage, func(t *testing.T) {
			state := &oracleOperationsTestState{}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var last OracleExcelImportProgress
			count, err := executeOracleExcelImportFilesWithProgressOnConnection(ctx, oracleOperationsFakeConnection{db: db, columns: oracleImportFilesColumns()}, []string{path}, oracleImportFilesConfig(), oracleImportTestIdentifier, func(progress OracleExcelImportProgress) {
				last = progress
				if progress.Stage == stage {
					cancel()
				}
			})
			if count != 0 || !errors.Is(err, context.Canceled) || state.commits != 0 || last.Stage != "failed" {
				t.Fatalf("canceled import committed: count=%d error=%v state=%+v last=%+v", count, err, state, last)
			}
			if stage == "importing" && len(state.rows) != 0 {
				t.Fatal("canceled import wrote another batch")
			}
			if stage == "committing" && last.ProcessedRows != 2 {
				t.Fatal("rollback progress lost written row count")
			}
		})
	}
}
