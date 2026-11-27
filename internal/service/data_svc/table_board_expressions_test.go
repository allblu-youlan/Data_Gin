package data_svc

import (
	"context"
	"errors"
	"gin-biz-web-api/internal/tableboard"
	"testing"
	"time"
)

func TestBoardExpressionUsesSameFacts(t *testing.T) {
	m := boardTestMetric()
	raw := "112.50"
	layout := tableboard.Layout{Metrics: []tableboard.Metric{m}, Sections: []tableboard.Section{{ID: "gmv", Cells: [][]tableboard.Cell{{
		{Kind: "expression", Expression: `=IFERROR(B1/$C$1-1,"-")`, Format: "percent_2"},
		{Kind: "formula", MetricID: m.ID, Period: "YEAR_TO_DATE"},
		{Kind: "text", Text: "100"},
	}}}}}
	calls := 0
	run := func(ctx context.Context, _ uint, _ aggregateRequest, asOf time.Time) (map[string]BoardCellResult, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("offline")
		}
		return map[string]BoardCellResult{"": {Status: "ok", Value: &raw}}, nil
	}
	result := evaluateBoard(t.Context(), 12, layout, time.Now(), run, nil)
	if result[0].Status != "ok" || result[0].Value == nil || *result[0].Value != "0.125" || calls != 1 {
		t.Fatal(result, calls)
	}
	result = evaluateBoard(t.Context(), 12, layout, time.Now(), run, nil)
	if result[0].Status != "error" || result[0].Value != nil || calls != 2 {
		t.Fatal(result, calls)
	}
}

func TestGMVBoardReportYearGrouping(t *testing.T) {
	m := boardTestMetric()
	first := tableboard.Cell{Kind: "formula", MetricID: m.ID, Period: "YEAR_TO_WEEK"}
	prior := first
	prior.YearOffset = -1
	layout := tableboard.Layout{ReportDate: "2026-11-15", Metrics: []tableboard.Metric{m}, Sections: []tableboard.Section{{ID: "gmv", Cells: [][]tableboard.Cell{{first, prior, first, {Kind: "expression", Expression: `=IFERROR(A1/B1-1,"-")`, Format: "percent_2"}}}}}}
	now := time.Date(2026, 12, 14, 12, 0, 0, 0, time.UTC)
	calls := 0
	run := func(ctx context.Context, _ uint, r aggregateRequest, asOf time.Time) (map[string]BoardCellResult, error) {
		calls++
		if !asOf.Equal(now) {
			t.Errorf("changed snapshot clock %s", asOf)
		}
		start, end, e := m.ReportBounds(r.period, asOf, r.reportDate, r.yearOffset)
		if e != nil {
			return nil, e
		}
		amount := "120"
		cutoff := "2026-11-16"
		if r.yearOffset == -1 {
			amount = "100"
			cutoff = "2025-11-17"
		}
		if end.Format(time.DateOnly) != cutoff {
			t.Errorf("end=%s", end)
		}
		return map[string]BoardCellResult{"": {Status: "ok", Value: &amount, Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339)}}, nil
	}
	cells := evaluateBoard(t.Context(), 1, layout, now, run, nil)
	if calls != 2 || cells[3].Value == nil || *cells[3].Value != "0.2" {
		t.Fatal(calls, cells)
	}
}
