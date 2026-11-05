package data_svc

import (
	"context"
	"testing"

	"gin-biz-web-api/internal/reportcontract"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"
)

func TestReportResultPageReaderReusesPoolAndReleasesFailedReads(t *testing.T) {
	previousEntries, previousOpen := selectOraclePools.entries, selectOraclePools.open
	selectOraclePools.entries = map[uint]*selectOraclePoolEntry{}
	opens := 0
	selectOraclePools.open = func(context.Context, model.ReportDatasource, string) (*reportoracle.Adapter, error) {
		opens++
		return &reportoracle.Adapter{}, nil
	}
	t.Cleanup(func() {
		selectOraclePools.entries, selectOraclePools.open = previousEntries, previousOpen
	})
	contract := reportrepo.RunResultContract{
		Datasource: model.ReportDatasource{BaseModel: model.BaseModel{ID: 4}},
		Version:    model.ReportVersion{ExecutionMode: model.ReportExecutionModeSelectSnapshot},
	}
	reader := oracleReportResultPageReader{}
	for range 2 {
		if _, err := reader.Read(t.Context(), contract, "password", []string{"ORDER_NO"}, reportquery.Query{}, nil, 20); err == nil {
			t.Fatal("Read() accepted an unavailable snapshot store")
		}
	}
	if opens != 1 {
		t.Fatalf("two reads opened %d adapters, want one pooled adapter", opens)
	}
	if readers := selectOraclePools.entries[4].readers; readers != 0 {
		t.Fatalf("failed reads left %d active pool readers", readers)
	}
}

func TestReportResultPagePlanReadsMetadataOncePerPage(t *testing.T) {
	contract := resultPagePlanContract(t)
	metadata := &resultPageMetadata{columns: publicationResultColumns()}
	for page := range 2 {
		plan, err := buildReportResultPagePlan(t.Context(), metadata, contract, []string{"ORDER_NO"}, reportquery.Query{})
		if err != nil {
			t.Fatalf("page %d plan: %v", page, err)
		}
		if columns := plan.Columns(); len(columns) != 1 || columns[0] != "ORDER_NO" {
			t.Fatalf("page columns = %v", columns)
		}
	}
	if metadata.reads != 2 {
		t.Fatalf("two page plans read metadata %d times, want 2", metadata.reads)
	}
}

func TestReportResultPagePlanStillRejectsChangedSchema(t *testing.T) {
	contract := resultPagePlanContract(t)
	metadata := &resultPageMetadata{columns: publicationResultColumns()}
	if _, err := buildReportResultPagePlan(t.Context(), metadata, contract, []string{"ORDER_NO"}, reportquery.Query{}); err != nil {
		t.Fatal(err)
	}
	metadata.columns[2].DataLength = 256
	if _, err := buildReportResultPagePlan(t.Context(), metadata, contract, []string{"ORDER_NO"}, reportquery.Query{}); err == nil {
		t.Fatal("next page accepted a changed Oracle result schema")
	}
}

func resultPagePlanContract(t *testing.T) reportrepo.RunResultContract {
	t.Helper()
	draft := publicationDraft()
	columns := publicationResultColumns()
	snapshot, err := reportoracle.CompileResultSnapshotContract(reportoracle.ResultTableSnapshotRef(
		reportoracle.ResultTableRef{Owner: "REPORT", Name: "SALES_RESULT"}, []string{"ORDER_NO"},
	), columns)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := reportcontract.Compile(draft.Version, draft.Parameters, draft.Columns, draft.Grants, publicationProcedure(), columns, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	draft.Version.CompiledSpecJSON = model.JSONText(compiled.SpecJSON)
	return reportrepo.RunResultContract{
		Version: draft.Version,
		Run:     model.ReportRun{ContractHash: compiled.Hashes.Contract, ResultSchemaHash: compiled.Hashes.ResultSchema},
	}
}

type resultPageMetadata struct {
	columns []reportoracle.ResultColumn
	reads   int
}

func (metadata *resultPageMetadata) InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
	metadata.reads++
	return metadata.columns, nil
}
