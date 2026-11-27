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
