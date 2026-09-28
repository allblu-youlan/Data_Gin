package data_svc

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestNormalizeExcelConditionalWrite(t *testing.T) {
	step := ExcelMatchStep{
		MatchMode:        excelMatchModeConditionalWrite,
		MatchExcelColumn: " 门店 ",
		ContainsValue:    " 杭州 ",
		OutputColumnName: " 区域 ",
		WriteValue:       " 华东 ",
	}
	cfg, err := normalizeExcelMatchConfig(ExcelMatchConfig{Steps: []ExcelMatchStep{step, step}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Steps[0].MatchExcelColumn != "门店" || cfg.Steps[0].ContainsValue != "杭州" || cfg.Steps[0].OutputColumnName != "区域" {
		t.Fatalf("column names and condition were not normalized: %+v", cfg.Steps[0])
	}
	if cfg.Steps[0].WriteValue != " 华东 " {
		t.Fatalf("write value must be preserved, got %q", cfg.Steps[0].WriteValue)
	}
	if err := validateExcelExportSteps(context.Background(), cfg, nil); err != nil {
		t.Fatalf("conditional writes must not require database validation: %v", err)
	}
	tests := []struct {
		name   string
		change func(*ExcelMatchStep)
	}{
		{name: "missing source", change: func(s *ExcelMatchStep) { s.MatchExcelColumn = "" }},
		{name: "missing condition", change: func(s *ExcelMatchStep) { s.ContainsValue = " " }},
		{name: "missing target", change: func(s *ExcelMatchStep) { s.OutputColumnName = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalid := step
			tt.change(&invalid)
			if _, err := normalizeExcelMatchConfig(ExcelMatchConfig{Steps: []ExcelMatchStep{invalid}}); err == nil {
				t.Fatal("expected incomplete conditional write to be rejected")
			}
		})
	}
}

func TestExcelConditionalWritesReuseColumnsInPreviewAndExport(t *testing.T) {
	input := excelize.NewFile()
	t.Cleanup(func() { _ = input.Close() })
	source := [][]interface{}{
		{"类型", "门店", "标签", "保留列"},
		{"A", "杭州门店", "原值", "尾列一"},
		{"B", "杭州门店", "原值", "尾列二"},
		{"A", "上海门店", "原值", "尾列三"},
		{"A", "杭州二店", "原值", "尾列四"},
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
	cfg, err := normalizeExcelMatchConfig(ExcelMatchConfig{
		Steps: []ExcelMatchStep{
			{
				MatchMode:        excelMatchModeConditionalWrite,
				Filters:          []ExcelMatchFilter{{Column: "类型", Op: "eq", Value: "A"}},
				MatchExcelColumn: "门店", ContainsValue: "杭州", OutputColumnName: "标签", WriteValue: "第一次",
			},
			{
				MatchMode:        excelMatchModeConditionalWrite,
				MatchExcelColumn: "标签", ContainsValue: "第一次", OutputColumnName: "标签", WriteValue: "第二次",
			},
			{
				MatchMode:        excelMatchModeConditionalWrite,
				Filters:          []ExcelMatchFilter{{Column: "标签", Op: "eq", Value: "第二次"}},
				MatchExcelColumn: "门店", ContainsValue: "杭州", OutputColumnName: "新增列", WriteValue: " 初值 ",
			},
			{
				MatchMode:        excelMatchModeConditionalWrite,
				MatchExcelColumn: "新增列", ContainsValue: "初值", OutputColumnName: "新增列", WriteValue: " 终值 ",
			},
			{
				MatchMode:        excelMatchModeConditionalWrite,
				MatchExcelColumn: "标签", ContainsValue: "第二次", OutputColumnName: "标签", WriteValue: "",
			},
			{
				MatchMode:        excelMatchModeConditionalWrite,
				MatchExcelColumn: "门店", ContainsValue: "成都", OutputColumnName: "标签", WriteValue: "西南",
			},
			{
				MatchMode: excelMatchModeConditionalWrite,
				MatchExcelColumn: "门店", ContainsValue: "二店", OutputColumnName: "新增列", WriteValue: "   ",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the fixture small while exercising writes across batch boundaries.
	cfg.BatchSize = 2
	preview, err := processExcelMatchPreview(context.Background(), input, cfg, nil, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inputPath, outputPath := filepath.Join(dir, "source.xlsx"), filepath.Join(dir, "result.xlsx")
	if err := input.SaveAs(inputPath); err != nil {
		t.Fatal(err)
	}
	stats, err := processExcelMatchFile(context.Background(), inputPath, outputPath, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats != preview.Stats || stats.MatchedRows != 2 || stats.ProcessedRows != 4 {
		t.Fatalf("export stats %+v and preview stats %+v must agree", stats, preview.Stats)
	}
	out, err := excelize.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = out.Close() })
	got, err := out.GetRows("Result_1")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"类型", "门店", "标签", "保留列", "新增列"},
		{"A", "杭州门店", "", "尾列一", " 终值 "},
		{"B", "杭州门店", "原值", "尾列二"},
		{"A", "上海门店", "原值", "尾列三"},
		{"A", "杭州二店", "", "尾列四", "   "},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("export rows = %#v, want %#v", got, want)
	}
	for i, sample := range preview.Samples {
		for j, header := range want[0] {
			value := ""
			if j < len(want[i+1]) {
				value = want[i+1][j]
			}
			if sample.Values[header] != value {
				t.Errorf("preview row %d column %s = %q, want %q", i+1, header, sample.Values[header], value)
			}
		}
	}
	if preview.Samples[1].StepResults[0].Status != "skipped" || preview.Samples[2].StepResults[0].Status != "skipped" {
		t.Fatal("a failed filter or contains condition must skip the write")
	}
	if preview.Samples[0].Status != "matched" || preview.Samples[0].StepResults[5].Status != "skipped" {
		t.Fatal("a later skipped write must preserve the earlier matched status")
	}
}

func TestExcelConditionalWriteBetweenDatabaseSteps(t *testing.T) {
	cfg, err := normalizeExcelMatchConfig(ExcelMatchConfig{Steps: []ExcelMatchStep{
		{
			TableName: "orders", MatchExcelColumn: "订单号", DBMatchField: "id", DBValueField: "store",
			OutputColumnName: "门店",
		},
		{
			MatchMode:        excelMatchModeConditionalWrite,
			MatchExcelColumn: "门店", ContainsValue: "杭州", OutputColumnName: "门店", WriteValue: "华东",
		},
		{
			TableName: "regions", MatchExcelColumn: "门店", DBMatchField: "name", DBValueField: "code",
			OutputColumnName: "区域编码",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	layout, err := prepareExcelMatchPipeline([]string{"订单号"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rows := []*excelMatchPipelineRow{{values: []string{"001"}}}
	lookup := &fakeExcelMatchLookup{stepValues: map[string]map[string]string{
		"门店": {"001": "杭州门店"}, "区域编码": {"华东": "EAST"},
	}}
	if err := runExcelMatchSteps(context.Background(), cfg, lookup, layout, newExcelMatchPipelineState(nil), rows); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows[0].values, []string{"001", "华东", "EAST"}) {
		t.Fatalf("chained values = %#v", rows[0].values)
	}
	if !reflect.DeepEqual(lookup.tables, []string{"orders", "regions"}) {
		t.Fatalf("conditional writes must not query database, queries = %#v", lookup.tables)
	}
}

func TestExcelConditionalWriteColumnValidation(t *testing.T) {
	write := ExcelMatchStep{
		MatchMode:        excelMatchModeConditionalWrite,
		MatchExcelColumn: "门店", ContainsValue: "杭州", OutputColumnName: "区域", WriteValue: "华东",
	}
	tests := []struct {
		name      string
		steps     []ExcelMatchStep
		wantError string
	}{
		{name: "missing source", steps: []ExcelMatchStep{write}, wantError: "缺少输入列"},
		{
			name: "forward reference",
			steps: []ExcelMatchStep{
				{MatchMode: excelMatchModeConditionalWrite, MatchExcelColumn: "区域", ContainsValue: "华东", OutputColumnName: "标记"},
				write,
			},
			wantError: "缺少输入列",
		},
		{
			name: "sku original input overwritten",
			steps: []ExcelMatchStep{
				{MatchMode: excelMatchModeConditionalWrite, MatchExcelColumn: "订单号", ContainsValue: "1", OutputColumnName: "订单号"},
				{MatchMode: excelMatchModeOrderItemSKU, MatchExcelColumn: "订单号", OutputColumnName: "SKU"},
			},
			wantError: "未被前序条件写入修改",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := prepareExcelMatchPipeline([]string{"订单号"}, ExcelMatchConfig{Steps: tt.steps})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}
