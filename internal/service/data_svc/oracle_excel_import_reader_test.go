package data_svc

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

func TestOracleExcelImportCountSkipsConversionAndCountsMappedRowsAcrossSheets(t *testing.T) {
	path := oracleImportWorkbook(t, "count.xlsx",
		oracleImportSheetFixture{name: "首表", rows: [][]interface{}{{"金额", "订单号", "其他"}, {"not a number", "A", ""}, {" ", "", "忽略"}, {"", "B", ""}}},
		oracleImportSheetFixture{name: "次表", rows: [][]interface{}{{"订单号", "金额"}, {"C", "also invalid"}, {"", ""}}})
	config := oracleImportFilesConfig()
	var locations []oracleExcelImportLocation
	count, err := countOracleExcelImportLocated(t.Context(), path, config, func(location oracleExcelImportLocation) error {
		locations = append(locations, location)
		return nil
	})
	want := []oracleExcelImportLocation{{file: "count.xlsx", sheet: "首表", row: 2}, {file: "count.xlsx", sheet: "首表", row: 4}, {file: "count.xlsx", sheet: "次表", row: 2}}
	if err != nil || count != 3 || !reflect.DeepEqual(locations, want) {
		t.Fatalf("count=%d locations=%v error=%v", count, locations, err)
	}
	if _, err := readOracleExcelImportLocated(t.Context(), path, config, oracleImportFilesColumns(), func([]interface{}, oracleExcelImportLocation) error { return nil }); err == nil {
		t.Fatal("invalid number must still fail in the actual import reader")
	}
}

func TestOracleExcelImportCountUsesSelectedSheetAndConfiguredHeader(t *testing.T) {
	path := oracleImportWorkbook(t, "selected.xlsx",
		oracleImportSheetFixture{name: "忽略", rows: [][]interface{}{{"错误表头"}, {"无关数据"}}},
		oracleImportSheetFixture{name: "目标", rows: [][]interface{}{{"说明"}, {" 金额 ", " 订单号 "}, {"10", "A"}, {"", " "}, {"20", "B"}}})
	config := oracleImportTestConfig()
	config.SheetName = "目标"
	count, err := countOracleExcelImportLocated(t.Context(), path, config, nil)
	if err != nil || count != 2 {
		t.Fatalf("count=%d error=%v", count, err)
	}
}

func TestOracleExcelImportCountValidatesHeaders(t *testing.T) {
	for _, test := range []struct {
		name, message string
		rows          [][]interface{}
	}{
		{name: "missing configured header", message: "缺少表头", rows: [][]interface{}{{"订单号", "其他"}, {"A", "10"}}},
		{name: "duplicate header", message: "表头重复", rows: [][]interface{}{{"订单号", "金额", "金额"}, {"A", "10", "20"}}},
		{name: "missing header row", message: "缺少配置的表头行", rows: [][]interface{}{{"说明"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := oracleImportWorkbook(t, "invalid.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: test.rows})
			config := oracleImportFilesConfig()
			if test.name == "missing header row" {
				config.HeaderRow = 2
			}
			count, err := countOracleExcelImportLocated(t.Context(), path, config, nil)
			if count != 0 || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("count=%d error=%v", count, err)
			}
		})
	}
}

func TestOracleExcelImportCountStopsOnCancellationAndVisitErrors(t *testing.T) {
	path := oracleImportWorkbook(t, "cancel.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "金额"}, {"A", "10"}, {"B", "20"}}})
	config := oracleImportFilesConfig()
	ctx, cancel := context.WithCancel(t.Context())
	visited := 0
	count, err := countOracleExcelImportLocated(ctx, path, config, func(oracleExcelImportLocation) error { visited++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || count != 0 || visited != 1 {
		t.Fatalf("count=%d visited=%d error=%v", count, visited, err)
	}
	count, err = countOracleExcelImportLocated(ctx, "should-not-open.xlsx", config, nil)
	if !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatalf("already cancelled count=%d error=%v", count, err)
	}
	want := errors.New("stop counting")
	count, err = countOracleExcelImportLocated(t.Context(), path, config, func(oracleExcelImportLocation) error { return want })
	if !errors.Is(err, want) || count != 0 || !strings.Contains(err.Error(), "第 2 行") {
		t.Fatalf("callback error lost its row context: count=%d error=%v", count, err)
	}
}

func TestOracleExcelImportCountMatchesValidImportReader(t *testing.T) {
	path := oracleImportWorkbook(t, "valid.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "金额"}, {"A", "10"}, {"", ""}, {"B", "20"}}})
	config := oracleImportFilesConfig()
	count, err := countOracleExcelImportLocated(t.Context(), path, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	read, err := readOracleExcelImportLocated(t.Context(), path, config, []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2"}, {Name: "AMOUNT", DataType: "NUMBER"}, {Name: "BATCH_TAG", DataType: "VARCHAR2"}}, func([]interface{}, oracleExcelImportLocation) error { return nil })
	if err != nil || count != read || read != 2 {
		t.Fatalf("count=%d read=%d error=%v", count, read, err)
	}
}
