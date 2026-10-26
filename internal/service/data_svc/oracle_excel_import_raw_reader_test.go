package data_svc

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"github.com/godror/godror"
	"github.com/xuri/excelize/v2"
)

func TestOracleExcelImportRawValuesPreserveUnderlyingNumbersAndCachedFormula(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	for cell, value := range map[string]interface{}{"A1": 12, "B1": "小数", "C1": "千分位", "D1": "百分比", "E1": "公式", "A2": "00123", "B2": 1.23456789, "C2": 1234.5, "D2": 0.125, "E2": 0.125} {
		if err := file.SetCellValue("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	for cell, format := range map[string]string{"A1": "0000", "A2": "000000", "B2": "0.00", "C2": "#,##0.00", "D2": "0.0%", "E2": "0.0%"} {
		setOracleExcelRawStyle(t, file, cell, format)
	}
	if err := file.SetCellFormula("Sheet1", "E2", "1/8"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "raw-numbers.xlsx")
	if err := file.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	config := OracleExcelImportConfig{HeaderRow: 1, SheetName: "Sheet1", RawCellValues: true, Mappings: []OracleExcelImportMapping{{ExcelColumn: "0012", Field: "CODE"}, {ExcelColumn: "小数", Field: "DECIMAL"}, {ExcelColumn: "千分位", Field: "AMOUNT"}, {ExcelColumn: "百分比", Field: "RATE"}, {ExcelColumn: "公式", Field: "FORMULA"}}}
	columns := []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2"}, {Name: "DECIMAL", DataType: "NUMBER"}, {Name: "AMOUNT", DataType: "NUMBER"}, {Name: "RATE", DataType: "NUMBER"}, {Name: "FORMULA", DataType: "NUMBER"}}
	var got []interface{}
	count, err := readOracleExcelImport(t.Context(), path, config, columns, func(values []interface{}) error { got = values; return nil })
	want := []interface{}{"00123", godror.Number("1.23456789"), godror.Number("1234.5"), godror.Number("0.125"), godror.Number("0.125")}
	if err != nil || count != 1 || !reflect.DeepEqual(got, want) {
		t.Fatalf("raw numbers changed: count=%d got=%v want=%v error=%v", count, got, want, err)
	}
	config.RawCellValues = false
	if _, err := readOracleExcelImport(t.Context(), path, config, columns, func([]interface{}) error { return nil }); err == nil || !strings.Contains(err.Error(), "AMOUNT") {
		t.Fatalf("legacy display-format semantics changed: %v", err)
	}
}

func TestOracleExcelImportRawValuesKeepTextCodesAndExposeNumericCodes(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	for cell, value := range map[string]interface{}{"A1": "编码", "A2": "000123", "A3": 12} {
		if err := file.SetCellValue("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	setOracleExcelRawStyle(t, file, "A2:A3", "000000")
	path := filepath.Join(t.TempDir(), "codes.xlsx")
	if err := file.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []bool{false, true} {
		config := OracleExcelImportConfig{HeaderRow: 1, SheetName: "Sheet1", RawCellValues: raw, Mappings: []OracleExcelImportMapping{{ExcelColumn: "编码", Field: "CODE"}}}
		var got []interface{}
		_, err := readOracleExcelImport(t.Context(), path, config, []reportoracle.ResultColumn{{Name: "CODE", DataType: "VARCHAR2"}}, func(values []interface{}) error { got = append(got, values[0]); return nil })
		want := []interface{}{"000123", "000012"}
		if raw {
			want[1] = "12"
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("raw=%t codes=%v want=%v error=%v", raw, got, want, err)
		}
	}
}

func TestOracleExcelImportRawValuesUseWorkbookDateSystemAndShanghaiWallTime(t *testing.T) {
	for _, date1904 := range []bool{false, true} {
		file := excelize.NewFile()
		if err := file.SetWorkbookProps(&excelize.WorkbookPropsOptions{Date1904: &date1904}); err != nil {
			t.Fatal(err)
		}
		serial := 46318.5
		if date1904 {
			serial -= 1462
		}
		for cell, value := range map[string]interface{}{"A1": "日期序号", "B1": "文本日期", "C1": "文本时间", "A2": serial, "B2": "2026-10-23", "C2": "2026-10-23 13:45:56.123"} {
			if err := file.SetCellValue("Sheet1", cell, value); err != nil {
				t.Fatal(err)
			}
		}
		setOracleExcelRawStyle(t, file, "A2", "yyyy-mm-dd hh:mm:ss")
		path := filepath.Join(t.TempDir(), "dates.xlsx")
		if err := file.SaveAs(path); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		config := OracleExcelImportConfig{HeaderRow: 1, SheetName: "Sheet1", RawCellValues: true, Mappings: []OracleExcelImportMapping{{ExcelColumn: "日期序号", Field: "SERIAL_DATE"}, {ExcelColumn: "文本日期", Field: "TEXT_DATE"}, {ExcelColumn: "文本时间", Field: "TEXT_TIME"}}}
		columns := []reportoracle.ResultColumn{{Name: "SERIAL_DATE", DataType: "DATE"}, {Name: "TEXT_DATE", DataType: "DATE"}, {Name: "TEXT_TIME", DataType: "TIMESTAMP(6)"}}
		var got []interface{}
		_, err := readOracleExcelImport(t.Context(), path, config, columns, func(values []interface{}) error { got = values; return nil })
		if err != nil {
			t.Fatal(err)
		}
		for index, want := range []string{"2026-10-23 12:00:00", "2026-10-23 00:00:00", "2026-10-23 13:45:56.123"} {
			value, ok := got[index].(time.Time)
			_, offset := value.Zone()
			if !ok || offset != 8*60*60 || value.Format("2006-01-02 15:04:05.999999999") != want {
				t.Fatalf("date1904=%t field=%s got=%v offset=%d want=%s", date1904, columns[index].Name, got[index], offset, want)
			}
		}
	}
}

func TestOracleExcelImportRawValuesConfigDefaultsAndNormalization(t *testing.T) {
	config := oracleImportTestConfig()
	config.RawCellValues = true
	normalized, _, _, err := buildOracleExcelImport(config, oracleImportTestColumns())
	if err != nil || !normalized.RawCellValues {
		t.Fatalf("normalization lost raw mode: config=%+v error=%v", normalized, err)
	}
	encoded, err := json.Marshal(normalized)
	if err != nil || !strings.Contains(string(encoded), `"rawCellValues":true`) {
		t.Fatalf("raw mode not persisted: %s error=%v", encoded, err)
	}
	var legacy OracleExcelImportConfig
	if err := json.Unmarshal([]byte(`{"operation":"oracle_import","owner":"DEMO","tableName":"ORDERS"}`), &legacy); err != nil || legacy.RawCellValues {
		t.Fatalf("legacy mode must remain formatted: %+v error=%v", legacy, err)
	}
}

func setOracleExcelRawStyle(t *testing.T, file *excelize.File, cell, format string) {
	t.Helper()
	style, err := file.NewStyle(&excelize.Style{CustomNumFmt: &format})
	if err != nil {
		t.Fatal(err)
	}
	start, end, found := strings.Cut(cell, ":")
	if !found {
		end = start
	}
	if err := file.SetCellStyle("Sheet1", start, end, style); err != nil {
		t.Fatal(err)
	}
}
