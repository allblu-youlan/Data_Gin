package data_svc

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/model"
)

func dynamicSelectTestDataset() reportdataset.Compiled {
	return reportdataset.Compiled{
		Query: reportdataset.QuerySpec{
			Base:  reportdataset.Base{TableID: "1", AliasID: "base"},
			Where: &reportdataset.Condition{AliasID: "base", ColumnID: "2", Operator: "BETWEEN", Value: &reportdataset.Value{Kind: "TIME_RANGE", Code: "CURRENT_MONTH"}},
		},
		Tables: map[string]reportdataset.Table{"1": {
			ID: "1", Owner: "BOS", PhysicalTable: "ORDERS", PrimaryKeys: []string{"1"},
			Columns: []reportdataset.Column{
				{ID: "1", DBName: "ID", OracleType: "NUMBER", LogicalType: "integer", IsPhysical: true, IsSelectable: true},
				{ID: "2", DBName: "BILLDATE", OracleType: "NUMBER", LogicalType: "integer", IsPhysical: true, IsSelectable: true},
			},
			Config: reportdataset.TableConfig{Enabled: true, DataScope: reportdataset.DataScope{Mode: "GLOBAL_APPROVED"}},
		}},
		Projections: []reportdataset.Projection{{Name: "C001", AliasID: "base", ColumnID: "1"}},
	}
}

func TestSelectRunDynamicFiltersRebuildFromCreationSnapshot(t *testing.T) {
	compiled := dynamicSelectTestDataset()
	run := model.ReportRun{RequestedBy: 7, NormalizedParametersJSON: "{}", QueryInputsJSON: "{}"}
	run.CreatedAt = time.Date(2026, 10, 31, 15, 59, 59, 0, time.UTC)
	first, err := buildSelectRunPlan(compiled, run)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildSelectRunPlan(compiled, run)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("retry changed the frozen plan: %v %v %v", first, second, err)
	}
	if first.Args[0].(sql.NamedArg).Value != json.Number("20261001") || first.Args[1].(sql.NamedArg).Value != json.Number("20261031") {
		t.Fatalf("creation period was not preserved: %v", first.Args)
	}
	newPlan, err := compiled.BuildAt(nil, reportdataset.QueryInputs{}, "7", false, run.CreatedAt.Add(time.Second))
	if err != nil || newPlan.Args[0].(sql.NamedArg).Value != json.Number("20261101") {
		t.Fatalf("new run did not use its new Beijing month: %v %v", newPlan.Args, err)
	}
}

func TestSelectRunFingerprintTracksResolvedPeriodAndKeepsReuse(t *testing.T) {
	compiled := dynamicSelectTestDataset()
	inputs := reportdataset.QueryInputs{}
	fingerprint := func(at time.Time) string {
		t.Helper()
		plan, err := compiled.BuildAt(nil, inputs, "7", false, at)
		if err != nil {
			t.Fatal(err)
		}
		return selectRunFingerprint(json.RawMessage(`{}`), inputs, "scope", reportdataset.DefaultPolicy(), plan)
	}
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	current := fingerprint(now)
	if fingerprint(now.AddDate(0, 0, 1)) != current {
		t.Fatal("same effective month should reuse its snapshot")
	}
	if fingerprint(now.AddDate(0, 1, 0)) == current {
		t.Fatal("next month must not reuse the previous month's snapshot")
	}
	compiled.Query.Where = nil
	if fingerprint(now) != fingerprint(now.AddDate(0, 1, 0)) {
		t.Fatal("ordinary queries lost snapshot reuse")
	}
}
