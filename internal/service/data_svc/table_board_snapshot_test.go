package data_svc

import (
	"bytes"
	"gin-biz-web-api/internal/tableboard"
	"github.com/xuri/excelize/v2"
	"testing"
)

func TestBoardSnapshotScreenCSVAndWorkbookAgree(t *testing.T) {
	layout := tableboard.Layout{Foreground: "#000000", Background: "#ffffff", Sections: []tableboard.Section{
		{ID: "s", Title: "本月", Cells: [][]tableboard.Cell{{{Kind: "text", Text: "0007"}, {Kind: "formula", Format: "money_2"}, {Kind: "formula"}, {Kind: "formula"}}}},
		{ID: "t", Title: "门店", Cells: [][]tableboard.Cell{{{Kind: "text", Text: "a,b"}}}}}}
	amount := "6120"
	results := []BoardCellResult{{SectionID: "s", Address: "B1", Status: "ok", Value: &amount}, {SectionID: "s", Address: "C1", Status: "null"}, {SectionID: "s", Address: "D1", Status: "error"}}
	snapshot, err := BuildBoardSnapshot(layout, results)
	if err != nil {
		t.Fatal(err)
	}
	amount = "12400"
	csv, err := RenderBoardCSV(snapshot)
	if err != nil || string(csv) != "\ufeff本月\n0007,\"6,120.00\",—,读取失败\n门店\n\"a,b\"\n" {
		t.Fatal(string(csv), err)
	}
	data, err := RenderBoardXLSX(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	sheets := book.GetSheetList()
	if len(sheets) != 2 {
		t.Fatal(sheets)
	}
	for i, cell := range []string{"A1", "B1", "C1", "D1"} {
		value, err := book.GetCellValue(sheets[0], cell)
		if err != nil || value != snapshot.Sections[0].Rows[0][i] {
			t.Fatalf("%s: %q, %v", cell, value, err)
		}
	}
	if results[0].Display != "6,120.00" {
		t.Fatal(results[0])
	}
}

func TestBoardSnapshotInheritsMetricFormatAndKeepsCellFormat(t *testing.T) {
	value := "6120.49"
	layout := tableboard.Layout{Foreground: "#000000", Background: "#ffffff", Metrics: []tableboard.Metric{{ID: "gmv", Aggregate: "SUM", Format: "integer"}}, Sections: []tableboard.Section{{ID: "s", Title: "继承", Cells: [][]tableboard.Cell{{{Kind: "formula", MetricID: "gmv"}, {Kind: "formula", MetricID: "gmv", Format: "money_2"}}}}}}
	results := []BoardCellResult{{SectionID: "s", Address: "A1", Status: "ok", Value: &value}, {SectionID: "s", Address: "B1", Status: "ok", Value: &value}}
	snapshot, err := BuildBoardSnapshot(layout, results)
	if err != nil || snapshot.Sections[0].Rows[0][0] != "6,120" || snapshot.Sections[0].Rows[0][1] != "6,120.49" {
		t.Fatal(snapshot, err)
	}
	layout.Metrics[0].Format = "number"
	snapshot, err = BuildBoardSnapshot(layout, results)
	if err != nil || snapshot.Sections[0].Rows[0][0] != "6120.49" || snapshot.Sections[0].Rows[0][1] != "6,120.49" {
		t.Fatal(snapshot, err)
	}
}
