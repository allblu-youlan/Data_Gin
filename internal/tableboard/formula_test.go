package tableboard

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFormulaMappingAndBounds(t *testing.T) {
	m := Metric{ID: "gmv", Name: "GMV", DatasourceID: 1, Owner: "OPS", TableName: "ORDERS", Aggregate: "SUM", TimeField: "PAID_AT", ValueField: "AMOUNT"}
	c := Cell{Kind: "formula", MetricID: "gmv", Period: "MONTH_TO_DATE", Overrides: map[string]json.RawMessage{"timeField": json.RawMessage(`"CREATED_AT"`)}}
	v, e := m.ForCell(c)
	if e != nil || v.TimeField != "CREATED_AT" || m.TimeField != "PAID_AT" || v.ValueField != "AMOUNT" {
		t.Fatal(v, e)
	}
	now := time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)
	start, end, e := v.Bounds("YEAR_TO_DATE", now)
	if e != nil || start.Format(time.RFC3339) != "2027-01-01T00:00:00+08:00" || !end.Equal(now) {
		t.Fatal(start, end, e)
	}
	start, end, e = v.Bounds("LAST_MONTH", now)
	if e != nil || start.Day() != 1 || start.Month() != 12 || end.Month() != 1 {
		t.Fatal(start, end, e)
	}
	c.Filters = []Filter{{"OTHER", "EQ", "A"}}
	if _, e = m.ForCell(c); e == nil {
		t.Fatal("unapproved dimension accepted")
	}
}
