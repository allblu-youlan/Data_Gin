//go:build integration

package data_svc

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
	"github.com/xuri/excelize/v2"
)

type excelScaleLookup struct{}

func (excelScaleLookup) Lookup(_ context.Context, _ ExcelMatchStep, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = `[{"no":"SKU-001","qty":1,"totAmtActual":10,"mProductName":"SPEC1234A"}]`
	}
	return result, nil
}

// This opt-in local file test deliberately exceeds a single Excel sheet's
// capacity; no Oracle service or external database is contacted.
func TestExcelWorkflowMillionRowsAcrossFiles(t *testing.T) {
	const rowsPerFile = 525000
	dir := t.TempDir()
	sources := make([]string, 2)
	for part := range sources {
		file := excelize.NewFile()
		stream, err := file.NewStreamWriter("Sheet1")
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.SetRow("A1", []interface{}{"Order", "Spec", "Price", "Qty"}); err != nil {
			t.Fatal(err)
		}
		for row := 0; row < rowsPerFile; row++ {
			if err := stream.SetRow("A"+strconv.Itoa(row+2), []interface{}{fmt.Sprintf("ORDER-%d", part*rowsPerFile+row), "SPEC1234", 10, 1}); err != nil {
				t.Fatal(err)
			}
		}
		if err := stream.Flush(); err != nil {
			t.Fatal(err)
		}
		sources[part] = filepath.Join(dir, fmt.Sprintf("source_%d.xlsx", part))
		if err := file.SaveAs(sources[part]); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("generated file %d with %d rows", part+1, rowsPerFile)
	}
	matching, err := normalizeExcelMatchConfig(ExcelMatchConfig{SheetName: "Sheet1", Steps: []ExcelMatchStep{{MatchMode: excelMatchModeOrderItemSKU, TableName: bojunRetailOrdersTable, MatchExcelColumn: "Order", SpecExcelColumn: "Spec", PriceExcelColumn: "Price", QtyExcelColumn: "Qty", DBMatchField: "docno", DBValueField: "items_json", OutputColumnName: "SKU"}}})
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := processExcelMatchFiles(t.Context(), sources, filepath.Join(dir, "matched"), matching, excelScaleLookup{}, func(done, total int, stats ExcelMatchJobStats) {
		if stats.ProcessedRows == rowsPerFile {
			t.Logf("matched %d/%d files, current rows=%d", done, total, stats.ProcessedRows)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	input, _, columns, err := buildOracleExcelImport(OracleExcelImportConfig{Owner: "DEMO", TableName: "ORDERS", SheetName: defaultExcelResultSheet, AllSheets: true, HeaderRow: 1, Mappings: []OracleExcelImportMapping{{ExcelColumn: "Order", Field: "ORDER_CODE"}, {ExcelColumn: "SKU", Field: "SKU"}}, FixedIdentifier: &OracleExcelImportFixedIdentifier{Enabled: true, Field: "BATCH_TAG", Value: "001"}}, []reportoracle.ResultColumn{{Name: "ORDER_CODE", DataType: "VARCHAR2"}, {Name: "SKU", DataType: "VARCHAR2"}, {Name: "BATCH_TAG", DataType: "VARCHAR2"}})
	if err != nil {
		t.Fatal(err)
	}
	// The reader receives a number already allocated once for this execution.
	input.FixedIdentifier.Value = "1"
	imported := 0
	for _, path := range outputs {
		count, err := readOracleExcelImport(t.Context(), path, input, columns, func(values []interface{}) error {
			if values[1] != "SKU-001" || values[2] != "1" {
				return fmt.Errorf("matched value or import number lost: %v", values)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		imported += count
	}
	if len(outputs) != 2 || imported != 2*rowsPerFile || imported <= excelMaxRowsPerSheet {
		t.Fatalf("files=%d imported=%d", len(outputs), imported)
	}
	t.Logf("verified %d matched/imported rows with one shared import number", imported)
}
