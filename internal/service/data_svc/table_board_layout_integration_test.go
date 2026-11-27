package data_svc

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/tableboard"
	"github.com/xuri/excelize/v2"
)

func TestBoardConfiguredLayoutEvaluatesAndExportsSameResults(t *testing.T) {
	// A user-supplied layout with two grouped facts, a local total, and a ratio.
	const fixture = `{
  "reportDate":"2026-04-10","foreground":"#000000","background":"#ffffff","sources":[],
  "metrics":[{"id":"quantity","name":"数量","datasourceId":7,"owner":"DEMO","tableName":"EVENTS",
   "aggregate":"SUM","valueField":"QUANTITY","timeField":"EVENT_AT","dimensions":["GROUP_CODE"]}],
  "sections":[{"id":"summary","title":"汇总","cells":[[
   {"kind":"formula","metricId":"quantity","period":"MONTH_TO_DATE","filters":[{"field":"GROUP_CODE","op":"EQ","value":"A"}]},
   {"kind":"formula","metricId":"quantity","period":"MONTH_TO_DATE","filters":[{"field":"GROUP_CODE","op":"EQ","value":"B"}]},
   {"kind":"expression","expression":"=SUM(A1:B1)","format":"integer"},
   {"kind":"expression","expression":"=IFERROR(A1/C1,\"-\")","format":"percent_2"}
  ]]}]
 }`
	var layout tableboard.Layout
	if err := json.Unmarshal([]byte(fixture), &layout); err != nil {
		t.Fatal(err)
	}
	if err := layout.Validate(); err != nil {
		t.Fatal(err)
	}
	asOf := time.Date(2026, 4, 15, 3, 0, 0, 0, time.UTC)
	amountA, amountB := "3", "9"
	calls := 0
	run := func(_ context.Context, _ uint, r aggregateRequest, clock time.Time) (map[string]BoardCellResult, error) {
		calls++
		if !asOf.Equal(clock) || r.reportDate != layout.ReportDate || r.groupField != "GROUP_CODE" || len(r.groupValues) != 2 {
			t.Errorf("grouped evaluation: time=%v request=%+v", clock, r)
		}
		result := map[string]BoardCellResult{"A": {Status: "ok", Value: &amountA}, "B": {Status: "ok", Value: &amountB}}
		if calls > 1 {
			result["A"] = BoardCellResult{Status: "error", Message: "字段权限已撤销"}
		}
		return result, nil
	}
	cells := evaluateBoard(t.Context(), 1, layout, asOf, run, nil)
	snapshot, err := BuildBoardSnapshot(layout, cells)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || cells[2].Display != "12" || cells[3].Display != "25.00%" {
		t.Fatalf("snapshot %+v queries %d", cells, calls)
	}
	// A subsequent database result changes, but both downloads still use this snapshot.
	amountA = "8"
	content, err := RenderBoardCSV(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(content), "\ufeff汇总\n"))).ReadAll()
	if err != nil || len(rows) != 1 || len(rows[0]) != 4 || rows[0][2] != "12" || rows[0][3] != "25.00%" {
		t.Fatal(rows, err)
	}
	content, err = RenderBoardXLSX(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	for pos, want := range map[string]string{"C1": "12", "D1": "25.00%"} {
		got, err := book.GetCellValue(book.GetSheetList()[0], pos)
		if err != nil || got != want {
			t.Fatal(pos, got, err)
		}
	}
	if calls != 1 {
		t.Fatal("export triggered another evaluation")
	}
	cells = evaluateBoard(t.Context(), 1, layout, asOf, run, nil)
	for _, index := range []int{0, 2, 3} {
		if cells[index].Status != "error" || cells[index].Value != nil {
			t.Fatal("source failure hidden", cells)
		}
	}
	if cells[1].Status != "ok" || *cells[1].Value != "9" {
		t.Fatal("unaffected group", cells)
	}
}
