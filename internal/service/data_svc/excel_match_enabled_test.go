package data_svc

import (
	"context"
	"encoding/json"
	"gin-biz-web-api/model"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExcelMatchStepEnabledCompatibility(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		json    string
		enabled bool
	}{
		{name: "legacy step", json: `{}`, enabled: true},
		{name: "null uses legacy default", json: `{"enabled":null}`, enabled: true},
		{name: "explicit enabled", json: `{"enabled":true}`, enabled: true},
		{name: "explicit disabled", json: `{"enabled":false}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var step ExcelMatchStep
			if err := json.Unmarshal([]byte(tt.json), &step); err != nil {
				t.Fatal(err)
			}
			if step.isEnabled() != tt.enabled {
				t.Fatalf("enabled = %v, want %v", step.isEnabled(), tt.enabled)
			}
		})
	}
}

func TestExcelDisabledRulesPersistWithoutValidation(t *testing.T) {
	t.Parallel()
	disabled := false
	steps := []ExcelMatchStep{
		{
			Name: " 暂存规则 ", Enabled: &disabled, MatchMode: "field", TableName: "missing_table",
			MatchExcelColumn: " 暂未配置 ", OutputColumnName: "重复输出",
			Filters: []ExcelMatchFilter{{Column: "不存在", Op: "未配置", Value: " 保留值 "}},
		},
		{Enabled: &disabled, MatchMode: excelMatchModeOrderItemSKU, OutputColumnName: "重复输出"},
		{Enabled: &disabled, MatchMode: excelMatchModeConditionalWrite, WriteValue: " 保留空格 "},
	}
	config, err := normalizeExcelMatchConfig(ExcelMatchConfig{Steps: steps})
	if err != nil {
		t.Fatalf("disabled rules should remain saveable: %v", err)
	}
	if err := validateExcelExportSteps(context.Background(), config, nil); err != nil {
		t.Fatalf("disabled rules must not validate database fields: %v", err)
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := excelMatchSchemeFromModel(model.ExcelMatchScheme{ConfigJSON: string(data)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scheme.Config.Steps, steps) {
		t.Fatalf("saved disabled rules changed: %#v, want %#v", scheme.Config.Steps, steps)
	}
	if _, err := normalizeExcelMatchExecutionConfig(scheme.Config); err == nil || !strings.Contains(err.Error(), "至少启用") {
		t.Fatalf("all-disabled execution error = %v, want enabled rule required", err)
	}
	service := &ExcelMatchJobService{}
	if _, err := service.createJobFromSource(context.Background(), excelJobSource{}, string(data)); err == nil || !strings.Contains(err.Error(), "至少启用") {
		t.Fatalf("all-disabled job creation error = %v, want rejection before database access", err)
	}
}

func TestExcelDisabledRulesPreviewAndExport(t *testing.T) {
	t.Parallel()
	input := excelize.NewFile()
	t.Cleanup(func() { _ = input.Close() })
	for i, values := range [][]interface{}{
		{"订单号", "保留列"},
		{"A001", "原始值"},
	} {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := input.SetSheetRow("Sheet1", cell, &values); err != nil {
			t.Fatal(err)
		}
	}
	disabled, enabled := false, true
	config, err := normalizeExcelMatchExecutionConfig(ExcelMatchConfig{Steps: []ExcelMatchStep{
		{
			Enabled: &disabled, MatchMode: excelMatchModeConditionalWrite, MatchExcelColumn: "订单号",
			ConditionOp: "contains", ContainsValue: "A", OutputColumnName: "保留列", WriteValue: "错误覆盖",
		},
		{
			TableName: "orders", MatchExcelColumn: "订单号", DBMatchField: "id", DBValueField: "store",
			OutputColumnName: "门店",
		},
		{
			Enabled: &disabled, MatchMode: excelMatchModeOrderItemSKU, TableName: "missing",
			MatchExcelColumn: "缺少订单号", OutputColumnName: "停用SKU",
			Filters: []ExcelMatchFilter{{Column: "缺少筛选列", Op: "invalid"}},
		},
		{
			Enabled: &enabled, MatchMode: excelMatchModeConditionalWrite, MatchExcelColumn: "门店",
			ContainsValue: "杭州", OutputColumnName: "地区", WriteValue: "华东",
		},
		{
			Enabled: &disabled, TableName: "missing", MatchExcelColumn: "缺少输入列", OutputColumnName: "门店",
		},
		{
			Enabled: &enabled, TableName: "regions", MatchExcelColumn: "地区", DBMatchField: "name", DBValueField: "code",
			OutputColumnName: "区域编码",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	validator := fakeExcelMatchSchemaValidator{tables: map[string]map[string]struct{}{
		"orders": {"id": {}, "store": {}}, "regions": {"name": {}, "code": {}},
	}}
	if err := validateExcelExportSteps(context.Background(), config, validator); err != nil {
		t.Fatalf("disabled database fields should not be checked: %v", err)
	}
	lookup := &fakeExcelMatchLookup{stepValues: map[string]map[string]string{
		"门店": {"A001": "杭州店"}, "区域编码": {"华东": "EAST"},
	}}
	preview, err := processExcelMatchPreview(context.Background(), input, config, lookup, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Samples) != 1 {
		t.Fatalf("preview sample count = %d, want 1", len(preview.Samples))
	}
	sample := preview.Samples[0]
	if len(sample.StepResults) != 3 {
		t.Fatalf("preview step results = %#v, want only enabled rules", sample.StepResults)
	}
	for index, want := range []int{2, 4, 6} {
		if sample.StepResults[index].StepIndex != want {
			t.Fatalf("step index = %d, want original rule index %d", sample.StepResults[index].StepIndex, want)
		}
	}
	wantValues := map[string]string{"订单号": "A001", "保留列": "原始值", "门店": "杭州店", "地区": "华东", "区域编码": "EAST"}
	if !reflect.DeepEqual(sample.Values, wantValues) {
		t.Fatalf("preview values = %#v, want %#v", sample.Values, wantValues)
	}
	if !reflect.DeepEqual(lookup.tables, []string{"orders", "regions"}) {
		t.Fatalf("preview queried tables = %v, want enabled rule tables", lookup.tables)
	}
	dir := t.TempDir()
	inputPath, outputPath := filepath.Join(dir, "source.xlsx"), filepath.Join(dir, "result.xlsx")
	if err := input.SaveAs(inputPath); err != nil {
		t.Fatal(err)
	}
	lookup.tables = nil
	stats, err := processExcelMatchFile(context.Background(), inputPath, outputPath, config, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if stats != preview.Stats || stats.MatchedRows != 1 {
		t.Fatalf("export and preview statistics = %+v / %+v, want one matched row", stats, preview.Stats)
	}
	if !reflect.DeepEqual(lookup.tables, []string{"orders", "regions"}) {
		t.Fatalf("export queried tables = %v, want enabled rule tables", lookup.tables)
	}
	output, err := excelize.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	rows, err := output.GetRows("Result_1")
	if err != nil {
		t.Fatal(err)
	}
	wantRows := [][]string{{"订单号", "保留列", "门店", "地区", "区域编码"}, {"A001", "原始值", "杭州店", "华东", "EAST"}}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Fatalf("export rows = %#v, want %#v", rows, wantRows)
	}
	for i := range config.Steps {
		config.Steps[i].Enabled = &disabled
	}
	if _, err := processExcelMatchPreview(context.Background(), input, config, nil, 10, 10); err == nil || !strings.Contains(err.Error(), "至少启用") {
		t.Fatalf("all-disabled preview error = %v, want enabled rule required", err)
	}
	if _, err := processExcelMatchFile(context.Background(), inputPath, outputPath, config, nil); err == nil || !strings.Contains(err.Error(), "至少启用") {
		t.Fatalf("all-disabled export error = %v, want enabled rule required", err)
	}
}

func TestExcelDisabledSKURulesSkipReservationScan(t *testing.T) {
	t.Parallel()
	disabled := false
	config := ExcelMatchConfig{Steps: []ExcelMatchStep{{Enabled: &disabled, MatchMode: excelMatchModeOrderItemSKU}}}
	reservations, err := collectExcelOrderItemReservations(context.Background(), nil, config, 0)
	if err != nil || len(reservations) != 0 {
		t.Fatalf("disabled SKU reservations = %v, %v; want no scan", reservations, err)
	}
}

func TestExcelEnabledRuleCannotReadDisabledOutput(t *testing.T) {
	t.Parallel()
	disabled := false
	config := ExcelMatchConfig{Steps: []ExcelMatchStep{
		{Enabled: &disabled, MatchExcelColumn: "订单号", OutputColumnName: "停用输出"},
		{MatchExcelColumn: "停用输出", OutputColumnName: "最终输出"},
	}}
	if _, err := prepareExcelMatchPipeline([]string{"订单号"}, config); err == nil || !strings.Contains(err.Error(), "第 2 个匹配步骤缺少输入列: 停用输出") {
		t.Fatalf("disabled output dependency error = %v, want missing input column", err)
	}
}
