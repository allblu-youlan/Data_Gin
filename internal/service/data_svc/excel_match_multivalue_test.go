package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"gin-biz-web-api/model"

	"github.com/xuri/excelize/v2"
)

func TestNormalizeExcelMultiValueFilters(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		filter  ExcelMatchFilter
		want    ExcelMatchFilter
		wantErr bool
	}{
		{
			name:   "zero or empty",
			filter: ExcelMatchFilter{Column: " 标记 ", Op: " EQ_ANY ", Values: []string{" 0 ", "", " "}, IncludeEmpty: true},
			want:   ExcelMatchFilter{Column: "标记", Op: "eq_any", Values: []string{"0"}, IncludeEmpty: true},
		},
		{
			name:   "empty only",
			filter: ExcelMatchFilter{Column: "标记", Op: "eq_any", IncludeEmpty: true},
			want:   ExcelMatchFilter{Column: "标记", Op: "eq_any", Values: []string{}, IncludeEmpty: true},
		},
		{
			name:   "contains normalizes candidates and ignores include empty",
			filter: ExcelMatchFilter{Column: "渠道", Op: "contains_any", Values: []string{" 天猫 ", "", "抖音", " "}, IncludeEmpty: true},
			want:   ExcelMatchFilter{Column: "渠道", Op: "contains_any", Values: []string{"天猫", "抖音"}},
		},
		{
			name:   "legacy equality keeps delimiter literal",
			filter: ExcelMatchFilter{Column: "渠道", Op: "eq", Value: " 天猫,抖音 "},
			want:   ExcelMatchFilter{Column: "渠道", Op: "eq", Value: "天猫,抖音"},
		},
		{name: "equality requires candidates or empty", filter: ExcelMatchFilter{Column: "标记", Op: "eq_any", Values: []string{"", " "}}, wantErr: true},
		{name: "legacy value does not supply multiple candidates", filter: ExcelMatchFilter{Column: "标记", Op: "eq_any", Value: "0"}, wantErr: true},
		{name: "contains requires nonempty candidate", filter: ExcelMatchFilter{Column: "渠道", Op: "contains_any", Values: []string{"", " "}}, wantErr: true},
		{name: "contains cannot match empty only", filter: ExcelMatchFilter{Column: "渠道", Op: "contains_any", IncludeEmpty: true}, wantErr: true},
		{name: "candidates without column are not dropped", filter: ExcelMatchFilter{Values: []string{"0"}}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filters, err := normalizeExcelMatchFilters([]ExcelMatchFilter{tt.filter}, "测试规则")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected invalid multi-value filter to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(filters, []ExcelMatchFilter{tt.want}) {
				t.Fatalf("normalized filters = %#v, want %#v", filters, tt.want)
			}
		})
	}
}

func TestExcelRowMatchesMultiValueFilters(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		value   string
		filter  ExcelMatchFilter
		matched bool
	}{
		{name: "matches zero", value: "0", filter: ExcelMatchFilter{Op: "eq_any", Values: []string{"0"}, IncludeEmpty: true}, matched: true},
		{name: "matches empty", filter: ExcelMatchFilter{Op: "eq_any", Values: []string{"0"}, IncludeEmpty: true}, matched: true},
		{name: "matches whitespace as empty", value: " \t ", filter: ExcelMatchFilter{Op: "eq_any", Values: []string{"0"}, IncludeEmpty: true}, matched: true},
		{name: "matches second candidate", value: "2", filter: ExcelMatchFilter{Op: "eq_any", Values: []string{"0", "2"}}, matched: true},
		{name: "uses text equality", value: "00", filter: ExcelMatchFilter{Op: "eq_any", Values: []string{"0"}, IncludeEmpty: true}},
		{name: "empty requires explicit option", filter: ExcelMatchFilter{Op: "eq_any", Values: []string{"0", ""}}},
		{name: "contains first candidate", value: "天猫旗舰店", filter: ExcelMatchFilter{Op: "contains_any", Values: []string{"天猫", "抖音"}}, matched: true},
		{name: "contains second candidate", value: "抖音直播间", filter: ExcelMatchFilter{Op: "contains_any", Values: []string{"天猫", "抖音"}}, matched: true},
		{name: "contains either candidate once", value: "天猫抖音", filter: ExcelMatchFilter{Op: "contains_any", Values: []string{"天猫", "抖音"}}, matched: true},
		{name: "contains ignores empty candidates", value: "线下门店", filter: ExcelMatchFilter{Op: "contains_any", Values: []string{"", "天猫"}}},
		{name: "contains does not match empty", filter: ExcelMatchFilter{Op: "contains_any", Values: []string{"天猫"}, IncludeEmpty: true}},
		{name: "legacy equality matches full comma text", value: "天猫,抖音", filter: ExcelMatchFilter{Op: "eq", Value: "天猫,抖音"}, matched: true},
		{name: "legacy equality does not split comma", value: "天猫", filter: ExcelMatchFilter{Op: "eq", Value: "天猫,抖音"}},
		{name: "legacy equality ignores multiple candidates", value: "抖音", filter: ExcelMatchFilter{Op: "eq", Value: "天猫", Values: []string{"抖音"}}},
		{name: "legacy contains does not split comma", value: "天猫旗舰店", filter: ExcelMatchFilter{Op: "contains", Value: "天猫,抖音"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.filter.Column = "列"
			matched := excelRowMatchesFilters([]string{tt.value}, map[string]int{"列": 0}, []ExcelMatchFilter{tt.filter})
			if matched != tt.matched {
				t.Fatalf("matched = %v, want %v", matched, tt.matched)
			}
		})
	}
}

func TestExcelMultiValueFilterConfigRoundTrip(t *testing.T) {
	t.Parallel()
	disabled := false
	draft := ExcelMatchStep{Enabled: &disabled, Filters: []ExcelMatchFilter{{Column: "", Op: "contains_any", Values: []string{" 未填完 ", ""}, IncludeEmpty: true}}}
	config, err := normalizeExcelMatchConfig(ExcelMatchConfig{Steps: []ExcelMatchStep{
		{
			Name: "筛选后写入", MatchMode: excelMatchModeConditionalWrite,
			MatchExcelColumn: "状态", ConditionOp: "eq", ContainsValue: "完成", OutputColumnName: "标记", WriteValue: "1",
			Filters: []ExcelMatchFilter{
				{Column: "标记", Op: "eq_any", Values: []string{" 0 "}, IncludeEmpty: true},
				{Column: "渠道", Op: "contains_any", Values: []string{" 天猫 ", "抖音"}},
				{Column: "备注", Op: "eq", Value: "0,空"},
			},
		},
		draft,
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := excelMatchSchemeFromModel(model.ExcelMatchScheme{ConfigJSON: string(data)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scheme.Config.Steps, config.Steps) {
		t.Fatalf("saved filter configuration changed: %#v, want %#v", scheme.Config.Steps, config.Steps)
	}
	if !reflect.DeepEqual(scheme.Config.Steps[1], draft) {
		t.Fatalf("disabled draft changed: %#v, want %#v", scheme.Config.Steps[1], draft)
	}
}

func TestExcelMultiValueRulesPreviewAndExport(t *testing.T) {
	t.Parallel()
	input := excelize.NewFile()
	t.Cleanup(func() { _ = input.Close() })
	source := [][]interface{}{
		{"订单号", "标记", "渠道", "规格编码", "销售金额", "销售数量"},
		{"A001", "0", "天猫旗舰", "ABC", "100", "1"},
		{"A002", "", "抖音直播", "ABC", "100", "1"},
		{"A003", "1", "天猫旗舰", "ABC", "100", "1"},
		{"A004", "0", "线下", "ABC", "100", "1"},
		{"A005", "0", "天猫抖音店", "ABC", "100", "1"},
		{"A006", "0", "", "ABC", "100", "1"},
		{"A007", "  ", "抖音直播", "ABC", "100", "1"},
		{"A008", "00", "天猫旗舰", "ABC", "100", "1"},
	}
	for i, values := range source {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := input.SetSheetRow("Sheet1", cell, &values); err != nil {
			t.Fatal(err)
		}
	}
	filters := []ExcelMatchFilter{
		{Column: "标记", Op: "eq_any", Values: []string{" 0 ", ""}, IncludeEmpty: true},
		{Column: "渠道", Op: "contains_any", Values: []string{"天猫", " 抖音 "}},
	}
	config, err := normalizeExcelMatchExecutionConfig(ExcelMatchConfig{Steps: []ExcelMatchStep{
		{Name: "字段匹配", Filters: filters, TableName: "orders", MatchExcelColumn: "订单号", DBMatchField: "id", DBValueField: "store", OutputColumnName: "门店"},
		{Name: "条件写入", Filters: filters, MatchMode: excelMatchModeConditionalWrite, MatchExcelColumn: "订单号", ContainsValue: "A", OutputColumnName: "写入结果", WriteValue: "命中"},
		{
			Name: "商品SKU", Filters: filters, MatchMode: excelMatchModeOrderItemSKU, TableName: "orders",
			MatchExcelColumn: "订单号", DBMatchField: "id", DBValueField: "items_json", OutputColumnName: "SKU",
			SpecExcelColumn: "规格编码", PriceExcelColumn: "销售金额", QtyExcelColumn: "销售数量",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	matchedKeys := []string{"A001", "A002", "A005", "A007"}
	matchedRows := map[int]bool{0: true, 1: true, 4: true, 6: true}
	lookup := &fakeExcelMatchLookup{stepValues: map[string]map[string]string{"门店": {}, "SKU": {}}}
	for _, key := range matchedKeys {
		lookup.stepValues["门店"][key] = "门店-" + key
		lookup.stepValues["SKU"][key] = fmt.Sprintf(`[{"no":"SKU-%s","qty":1,"totAmtActual":100,"mProductName":"ABC123"}]`, key)
	}
	reservations, err := collectExcelOrderItemReservations(context.Background(), input, config, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations[2]) != len(matchedKeys) {
		t.Fatalf("SKU reservation orders = %#v, want only filtered orders", reservations[2])
	}
	for _, key := range matchedKeys {
		if len(reservations[2][key]) != 1 {
			t.Fatalf("missing SKU reservation for matched order %s", key)
		}
	}
	preview, err := processExcelMatchPreview(context.Background(), input, config, lookup, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Samples) != 8 || preview.Stats.MatchedRows != 4 || preview.Stats.FilteredRows != 4 || preview.Stats.UnmatchedRows != 0 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	wantLookupKeys := append(append([]string{}, matchedKeys...), matchedKeys...)
	if !reflect.DeepEqual(lookup.keys, wantLookupKeys) {
		t.Fatalf("lookup keys = %v, want %v", lookup.keys, wantLookupKeys)
	}
	for index, sample := range preview.Samples {
		wantStatus := "skipped"
		if matchedRows[index] {
			wantStatus = "matched"
		}
		if len(sample.StepResults) != 3 {
			t.Fatalf("row %d step results = %#v, want all three modes", index, sample.StepResults)
		}
		for _, result := range sample.StepResults {
			if result.Status != wantStatus {
				t.Fatalf("row %d step %d status = %s, want %s", index, result.StepIndex, result.Status, wantStatus)
			}
		}
	}
	dir := t.TempDir()
	inputPath, outputPath := filepath.Join(dir, "source.xlsx"), filepath.Join(dir, "result.xlsx")
	if err := input.SaveAs(inputPath); err != nil {
		t.Fatal(err)
	}
	lookup.keys = nil
	stats, err := processExcelMatchFile(context.Background(), inputPath, outputPath, config, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if stats != preview.Stats || !reflect.DeepEqual(lookup.keys, wantLookupKeys) {
		t.Fatalf("export differs from preview: stats %+v, keys %v", stats, lookup.keys)
	}
	output, err := excelize.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	for index, sample := range preview.Samples {
		for col, name := range []string{"门店", "写入结果", "SKU"} {
			cell, err := excelize.CoordinatesToCellName(7+col, index+2)
			if err != nil {
				t.Fatal(err)
			}
			value, err := output.GetCellValue("Result_1", cell)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if matchedRows[index] {
				key := source[index+1][0].(string)
				want = []string{"门店-" + key, "命中", "SKU-" + key}[col]
			}
			if value != want || sample.Values[name] != want {
				t.Fatalf("row %d %s export/preview = %q/%q, want %q", index, name, value, sample.Values[name], want)
			}
		}
	}
}
