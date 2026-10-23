package data_svc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/xuri/excelize/v2"
)

func TestOracleExcelMultiSheetBalancesTablesWithinOneWorkbook(t *testing.T) {
	tests := []struct {
		name   string
		counts []int
		want   [][]int
	}{
		{"small tables", []int{2, 1}, [][]int{{2}, {1}}},
		{"even split", []int{4, 2}, [][]int{{2, 2}, {2}}},
		{"large and small", []int{5, 2}, [][]int{{3, 2}, {2}}},
		{"exact boundary", []int{6, 3}, [][]int{{3, 3}, {3}}},
		{"empty tables keep headers", []int{0, 0}, [][]int{{0}, {0}}},
		{"different large tables", []int{8, 5, 1}, [][]int{{3, 3, 2}, {3, 2}, {1}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection := newOracleExcelSheetsTestConnection(t, test.counts)
			config := OracleExcelExportConfig{Sheets: make([]OracleExcelExportSheet, len(test.counts))}
			total := 0
			for index, count := range test.counts {
				config.Sheets[index] = OracleExcelExportSheet{SheetName: fmt.Sprintf("表%d", index+1), Owner: "DEMO", TableName: fmt.Sprintf("TABLE_%d", index+1)}
				total += count
			}
			dir := t.TempDir()
			output, processed, err := executeOracleExcelExport(t.Context(), connection, config, dir, 3, nil)
			if err != nil || processed != total || filepath.Ext(output) != ".xlsx" {
				t.Fatalf("output=%s processed=%d want=%d error=%v", output, processed, total, err)
			}
			for _, reads := range connection.reads {
				if reads != 1 {
					t.Fatalf("table was queried again: %v", connection.reads)
				}
			}
			if len(connection.reads) != len(test.counts) || connection.active != 0 {
				t.Fatalf("cursor lifecycle reads=%v active=%d", connection.reads, connection.active)
			}
			if _, err := os.Stat(filepath.Join(dir, "sheet-data")); !os.IsNotExist(err) {
				t.Fatalf("temporary snapshots were not removed: %v", err)
			}
			file := openOracleExcelResultWorkbooks(t, output)[0]
			var wantNames []string
			for source, sizes := range test.want {
				start := 0
				for part, count := range sizes {
					name := fmt.Sprintf("表%d", source+1)
					if len(sizes) > 1 {
						name += fmt.Sprintf("_%d", part+1)
					}
					wantNames = append(wantNames, name)
					rows, err := file.GetRows(name)
					if err != nil || len(rows) != count+1 || strings.Join(rows[0], ",") != "编号,金额" {
						t.Fatalf("sheet=%s rows=%v want=%d error=%v", name, rows, count, err)
					}
					for index, row := range rows[1:] {
						want := fmt.Sprintf("%d_%03d", source+1, start+index+1)
						if len(row) != 2 || row[0] != want || row[1] != "123456789012345678.12" {
							t.Fatalf("source data duplicated, lost or rounded: sheet=%s row=%v wantCode=%s", name, row, want)
						}
					}
					start += count
				}
			}
			if !reflect.DeepEqual(file.GetSheetList(), wantNames) {
				t.Fatalf("sheets=%v want=%v", file.GetSheetList(), wantNames)
			}
		})
	}
}

func TestOracleExcelBalancedSheetSizesAtMillionRowLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		rows int
		want []int
	}{
		{"empty", 0, []int{0}},
		{"one million", 1_000_000, []int{1_000_000}},
		{"one extra row", 1_000_001, []int{500_001, 500_000}},
		{"user example 1.2 million", 1_200_000, []int{600_000, 600_000}},
		{"1.5 million", 1_500_000, []int{750_000, 750_000}},
		{"two million", 2_000_000, []int{1_000_000, 1_000_000}},
		{"three balanced sheets", 2_000_002, []int{666_668, 666_667, 666_667}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := oracleExcelBalancedSheetRows(test.rows, oracleExcelRowsPerSheet)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("sizes=%v want=%v", got, test.want)
			}
		})
	}
}

func TestOracleExcelLegacyRequestAlsoBalancesSheets(t *testing.T) {
	connection := newOracleExcelSheetsTestConnection(t, []int{4})
	dir := t.TempDir()
	output, total, err := executeOracleExcelExport(t.Context(), connection, OracleExcelExportConfig{Owner: "DEMO", TableName: "TABLE_1"}, dir, 3, nil)
	if err != nil || total != 4 || connection.active != 0 || connection.reads["TABLE_1"] != 1 {
		t.Fatalf("output=%s total=%d reads=%v active=%d error=%v", output, total, connection.reads, connection.active, err)
	}
	file := openOracleExcelResultWorkbooks(t, output)[0]
	if !reflect.DeepEqual(file.GetSheetList(), []string{"数据_1", "数据_2"}) {
		t.Fatalf("legacy sheets=%v", file.GetSheetList())
	}
	for _, name := range file.GetSheetList() {
		rows, err := file.GetRows(name)
		if err != nil || len(rows) != 3 {
			t.Fatalf("sheet=%s rows=%v error=%v", name, rows, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sheet-data")); !os.IsNotExist(err) {
		t.Fatalf("legacy snapshots were not removed: %v", err)
	}
}

func TestOracleExcelBalancedSheetNamesPreserveConfiguredNames(t *testing.T) {
	connection := newOracleExcelSheetsTestConnection(t, []int{4, 1, 4})
	longName := strings.Repeat("长", 31)
	config := OracleExcelExportConfig{Sheets: []OracleExcelExportSheet{
		{SheetName: "A", Owner: "DEMO", TableName: "TABLE_1"},
		{SheetName: "a_1", Owner: "DEMO", TableName: "TABLE_2"},
		{SheetName: longName, Owner: "DEMO", TableName: "TABLE_3"},
	}}
	output, _, err := executeOracleExcelExport(t.Context(), connection, config, t.TempDir(), 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := openOracleExcelResultWorkbooks(t, output)[0]
	want := []string{"A_1_2", "A_2", "a_1", strings.Repeat("长", 29) + "_1", strings.Repeat("长", 29) + "_2"}
	if !reflect.DeepEqual(file.GetSheetList(), want) {
		t.Fatalf("sheet names=%v want=%v", file.GetSheetList(), want)
	}
	for _, name := range file.GetSheetList() {
		if utf8.RuneCountInString(name) > 31 {
			t.Fatalf("sheet name too long: %s", name)
		}
	}
}

func TestOracleExcelMultiSheetSnapshotPreservesTextDatesAndNulls(t *testing.T) {
	dir := t.TempDir()
	rows := &oracleExcelSpoolFixtureRows{values: []interface{}{[]byte("=2+3"), time.Date(2026, 10, 22, 8, 30, 15, 0, time.FixedZone("Asia/Shanghai", 8*60*60)), nil}}
	sheet := OracleExcelExportSheet{SheetName: "类型", Columns: []OracleExcelExportColumn{{Field: "CODE", Title: "编号"}, {Field: "TIME", Title: "时间"}, {Field: "EMPTY", Title: "空值"}}}
	schema := []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2"}, {Name: "TIME", DataType: "DATE"}, {Name: "EMPTY", DataType: "VARCHAR2"}}
	snapshot, err := spoolOracleExcelSheet(t.Context(), rows, sheet, schema, filepath.Join(dir, "spool"), nil)
	if err != nil {
		t.Fatal(err)
	}
	output, total, err := renderOracleExcelSnapshots(t.Context(), []oracleExcelSheetSnapshot{snapshot}, dir, 3, nil)
	if err != nil || total != 1 {
		t.Fatalf("output=%s total=%d error=%v", output, total, err)
	}
	file := openOracleExcelResultWorkbooks(t, output)[0]
	code, _ := file.GetCellValue("类型", "A2")
	date, _ := file.GetCellValue("类型", "B2")
	empty, _ := file.GetCellValue("类型", "C2")
	formula, _ := file.GetCellFormula("类型", "A2")
	if code != "=2+3" || formula != "" || date != "2026-10-22 08:30:15" || empty != "" {
		t.Fatalf("snapshot changed cell values: code=%s formula=%s date=%s empty=%s", code, formula, date, empty)
	}
}

type oracleExcelSpoolFixtureRows struct {
	values []interface{}
	done   bool
}

func (rows *oracleExcelSpoolFixtureRows) Next() bool {
	if rows.done {
		return false
	}
	rows.done = true
	return true
}
func (rows *oracleExcelSpoolFixtureRows) Scan(targets ...interface{}) error {
	for index := range targets {
		*targets[index].(*interface{}) = rows.values[index]
	}
	return nil
}
func (*oracleExcelSpoolFixtureRows) Err() error { return nil }

func TestOracleExcelMultiSheetMetadataAndNames(t *testing.T) {
	input := OracleExcelExportConfig{Sheets: []OracleExcelExportSheet{{Owner: "demo", TableName: "orders"}, {Owner: "demo", TableName: "orders"}}}
	inspected := 0
	normalized, err := normalizeOracleExcelExportWithMetadata(t.Context(), input, func(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
		inspected++
		return []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2", Comment: "订单编号"}}, nil
	})
	if err != nil || inspected != 2 || normalized.Sheets[0].SheetName != "orders" || normalized.Sheets[1].SheetName != "orders_2" || normalized.Sheets[0].Columns[0].Title != "订单编号" || normalized.Sheets[1].Owner != "DEMO" || normalized.Sheets[1].Filters == nil {
		t.Fatalf("normalized=%+v inspected=%d error=%v", normalized, inspected, err)
	}
	for _, names := range [][]string{{"订单", "订单"}, {"Orders", "orders"}, {"bad/name"}, {strings.Repeat("长", 32)}, {"'bad"}} {
		t.Run(strings.Join(names, ","), func(t *testing.T) {
			config := OracleExcelExportConfig{Sheets: make([]OracleExcelExportSheet, len(names))}
			for index, name := range names {
				config.Sheets[index] = OracleExcelExportSheet{SheetName: name, Owner: "DEMO", TableName: "ORDERS"}
			}
			if _, _, err := oracleExcelExportSheets(config); err == nil {
				t.Fatal("accepted invalid or duplicate sheet names")
			}
		})
	}
	if _, _, err := oracleExcelExportSheets(OracleExcelExportConfig{Sheets: []OracleExcelExportSheet{}}); err == nil {
		t.Fatal("accepted an empty workbook")
	}
}

func TestOracleExcelMultiSheetReadFailureDoesNotProducePartialDownload(t *testing.T) {
	connection := newOracleExcelSheetsTestConnection(t, []int{2, 2})
	connection.failTable = "TABLE_2"
	dir := t.TempDir()
	_, _, err := executeOracleExcelExport(t.Context(), connection, OracleExcelExportConfig{Sheets: []OracleExcelExportSheet{{SheetName: "A", Owner: "DEMO", TableName: "TABLE_1"}, {SheetName: "B", Owner: "DEMO", TableName: "TABLE_2"}}}, dir, 3, nil)
	if err == nil || connection.active != 0 {
		t.Fatalf("read failure error=%v active=%d", err, connection.active)
	}
	if _, err := os.Stat(filepath.Join(dir, "result.xlsx")); !os.IsNotExist(err) {
		t.Fatal("a failed multi-sheet export produced a partial result")
	}
}

func TestOracleExcelMultiSheetConfigurationRoundTrips(t *testing.T) {
	store := &fakeOracleExcelExportSchemeStore{}
	service := &OracleExcelExportSchemeService{store: store, normalize: func(ctx context.Context, config OracleExcelExportConfig) (OracleExcelExportConfig, error) {
		return normalizeOracleExcelExportWithMetadata(ctx, config, func(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
			return oracleExcelTestSchema(), nil
		})
	}}
	input := OracleExcelExportConfig{Sheets: []OracleExcelExportSheet{
		{SheetName: "订单", Owner: "DEMO", TableName: "ORDERS", Columns: []OracleExcelExportColumn{{Field: "CODE", Title: "自定义订单号"}}, Filters: []OracleExcelExportFilter{{Field: "CREATED_AT", Op: "gte", Value: "2026-10-22T08:00"}}},
		{SheetName: "店铺", Owner: "DEMO", TableName: "STORES"},
	}}
	saved, err := service.Save(t.Context(), "订单及店铺", input)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := service.List(t.Context())
	if err != nil || len(loaded) != 1 || !reflect.DeepEqual(loaded[0], *saved) || loaded[0].Config.Sheets[0].Filters[0].Value != "2026-10-22 08:00:00" {
		t.Fatalf("multi-sheet configuration was not retained: loaded=%+v error=%v", loaded, err)
	}
}

func openOracleExcelResultWorkbooks(t *testing.T, path string) []*excelize.File {
	t.Helper()
	if filepath.Ext(path) != ".xlsx" {
		t.Fatalf("expected a single XLSX: %s", path)
	}
	file, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return []*excelize.File{file}
}

type oracleExcelSheetsTestConnection struct {
	db        *sql.DB
	counts    map[string]int
	reads     map[string]int
	active    int
	failTable string
}

func newOracleExcelSheetsTestConnection(t *testing.T, counts []int) *oracleExcelSheetsTestConnection {
	t.Helper()
	connection := &oracleExcelSheetsTestConnection{counts: map[string]int{}, reads: map[string]int{}}
	for index, count := range counts {
		connection.counts[fmt.Sprintf("TABLE_%d", index+1)] = count
	}
	connection.db = sql.OpenDB(oracleExcelSheetsConnector{connection})
	connection.db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.db.Close() })
	return connection
}

func (connection *oracleExcelSheetsTestConnection) InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
	return []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2", Comment: "编号"}, {Name: "AMOUNT", DataType: "NUMBER", Comment: "金额"}}, nil
}
func (connection *oracleExcelSheetsTestConnection) QuerySelect(ctx context.Context, statement string, args ...interface{}) (*sql.Rows, error) {
	return connection.db.QueryContext(ctx, statement, args...)
}

type oracleExcelSheetsConnector struct {
	connection *oracleExcelSheetsTestConnection
}

func (connector oracleExcelSheetsConnector) Connect(context.Context) (driver.Conn, error) {
	return &oracleExcelSheetsSQLConnection{connector.connection}, nil
}
func (oracleExcelSheetsConnector) Driver() driver.Driver { return oracleExcelSheetsDriver{} }

type oracleExcelSheetsDriver struct{}

func (oracleExcelSheetsDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type oracleExcelSheetsSQLConnection struct {
	connection *oracleExcelSheetsTestConnection
}

func (*oracleExcelSheetsSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (*oracleExcelSheetsSQLConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unsupported")
}
func (*oracleExcelSheetsSQLConnection) Close() error                             { return nil }
func (*oracleExcelSheetsSQLConnection) CheckNamedValue(*driver.NamedValue) error { return nil }
func (sqlConnection *oracleExcelSheetsSQLConnection) QueryContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
	connection := sqlConnection.connection
	for table, count := range connection.counts {
		if strings.Contains(statement, `FROM "DEMO"."`+table+`"`) {
			connection.reads[table]++
			connection.active++
			return &oracleExcelSheetsSQLRows{connection: connection, table: table, count: count}, nil
		}
	}
	return nil, fmt.Errorf("unexpected query: %s", statement)
}

type oracleExcelSheetsSQLRows struct {
	connection   *oracleExcelSheetsTestConnection
	table        string
	count, index int
	closed       bool
}

func (*oracleExcelSheetsSQLRows) Columns() []string { return []string{"CODE", "AMOUNT"} }
func (rows *oracleExcelSheetsSQLRows) Close() error {
	if !rows.closed {
		rows.connection.active--
		rows.closed = true
	}
	return nil
}
func (rows *oracleExcelSheetsSQLRows) Next(values []driver.Value) error {
	if rows.table == rows.connection.failTable {
		return errors.New("Oracle cursor interrupted")
	}
	if rows.index == rows.count {
		return io.EOF
	}
	rows.index++
	values[0], values[1] = strings.TrimPrefix(rows.table, "TABLE_")+fmt.Sprintf("_%03d", rows.index), "123456789012345678.12"
	return nil
}
