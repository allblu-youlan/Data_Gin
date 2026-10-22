package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
)

type fakeSelectSourceInspector struct {
	storeErr, metadataErr     error
	storeCalls, metadataCalls int
}

type fakeSelectPublicationInspector struct {
	*fakeReportOracleInspector
	source *fakeSelectSourceInspector
}

func (f *fakeSelectPublicationInspector) ValidateJSONSnapshotStore(ctx context.Context) error {
	return f.source.ValidateJSONSnapshotStore(ctx)
}

func (f *fakeSelectPublicationInspector) ValidateBojunMetadata(ctx context.Context, table reportdataset.Table) error {
	return f.source.ValidateBojunMetadata(ctx, table)
}

func TestSelectPublicationUsesSourceInspectionErrorClassification(t *testing.T) {
	t.Setenv("REPORT_SELECT_SNAPSHOT_ENABLED", "true")
	for _, tc := range []struct {
		name                  string
		storeErr, metadataErr error
		wantSourceUnavailable bool
	}{
		{name: "unchanged"},
		{name: "snapshot missing", storeErr: ErrReportSnapshotStoreNotReady},
		{name: "metadata timeout", metadataErr: context.DeadlineExceeded, wantSourceUnavailable: true},
		{name: "metadata changed", metadataErr: errors.New("REPORT_METADATA_CHANGED: source schema changed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := publicationDraft()
			draft.Version.ExecutionMode = model.ReportExecutionModeSelectSnapshot
			draft.Version.Status = model.ReportVersionStatusPublished
			draft.Version.InputSchemaJSON = "{}"
			dataset := reportdataset.Compiled{
				CompilerVersion: 1,
				Query:           reportdataset.QuerySpec{Base: reportdataset.Base{TableID: "1", AliasID: "base"}},
				Tables:          map[string]reportdataset.Table{"1": {ID: "1", Owner: "BOS", PhysicalTable: "C_STORE"}},
			}
			contract := selectCompiledContract(draft, dataset)
			draft.Version.CompiledSpecJSON = model.JSONText(contract.SpecJSON)
			draft.Version.ContractHash = contract.Hashes.Contract
			inspector := &fakeSelectPublicationInspector{
				fakeReportOracleInspector: &fakeReportOracleInspector{},
				source:                    &fakeSelectSourceInspector{storeErr: tc.storeErr, metadataErr: tc.metadataErr},
			}
			service := NewReportPublishService(&fakePublicationStore{}, &fakePublicationDecryptor{}, func(context.Context, reportoracle.Config) (reportOracleInspector, error) {
				return inspector, nil
			})
			_, err := service.inspectContract(t.Context(), publicationDatasource(), "password", draft)
			cause := tc.storeErr
			if cause == nil {
				cause = tc.metadataErr
			}
			if !errors.Is(err, cause) || errors.Is(err, ErrReportDatasourceOracleUnavailable) != tc.wantSourceUnavailable {
				t.Fatalf("source inspection classification: %v, cause: %v", err, cause)
			}
			if !inspector.closed {
				t.Fatal("publication inspector was not closed")
			}
		})
	}
}

func (f *fakeSelectSourceInspector) ValidateJSONSnapshotStore(context.Context) error {
	f.storeCalls++
	return f.storeErr
}

func (f *fakeSelectSourceInspector) ValidateBojunMetadata(context.Context, reportdataset.Table) error {
	f.metadataCalls++
	return f.metadataErr
}

func TestInspectSelectSourceDistinguishesMetadataChangesFromSourceFailures(t *testing.T) {
	dataset := reportdataset.Compiled{
		Query:  reportdataset.QuerySpec{Base: reportdataset.Base{TableID: "1", AliasID: "base"}},
		Tables: map[string]reportdataset.Table{"1": {ID: "1", Owner: "BOS", PhysicalTable: "C_STORE"}},
	}
	for _, tc := range []struct {
		name, stage string
		cause       error
		wantSource  bool
	}{
		{"snapshot missing", "store", fmt.Errorf("%w: missing tables", ErrReportSnapshotStoreNotReady), false},
		{"snapshot probe connection", "store", errors.New("ORA-03113: connection lost"), true},
		{"metadata probe timeout", "metadata", context.DeadlineExceeded, true},
		{"column changed", "metadata", errors.New("REPORT_METADATA_CHANGED: source schema changed"), false},
		{"unique key changed", "metadata", errors.New("REPORT_METADATA_CHANGED: required unique constraint changed"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspector := &fakeSelectSourceInspector{}
			if tc.stage == "store" {
				inspector.storeErr = tc.cause
			} else {
				inspector.metadataErr = tc.cause
			}
			err := inspectSelectSource(t.Context(), inspector, dataset)
			if !errors.Is(err, tc.cause) {
				t.Fatalf("original inspection error lost: %v", err)
			}
			if errors.Is(err, ErrReportDatasourceOracleUnavailable) != tc.wantSource {
				t.Fatalf("incorrect source failure classification: %v", err)
			}
			if strings.Contains(err.Error(), "REPORT_METADATA_CHANGED") != strings.Contains(tc.cause.Error(), "REPORT_METADATA_CHANGED") {
				t.Fatalf("inspection failure incorrectly became a metadata change: %v", err)
			}
			if tc.stage == "store" && inspector.metadataCalls != 0 {
				t.Fatal("metadata checked after snapshot validation failed")
			}
			if tc.stage == "metadata" && !strings.Contains(err.Error(), "BOS.C_STORE") {
				t.Fatalf("source table missing from private diagnostic: %v", err)
			}
		})
	}
}

func TestSelectSampleMetadataCheckDoesNotDependOnSnapshotStorage(t *testing.T) {
	dataset := reportdataset.Compiled{
		Query:  reportdataset.QuerySpec{Base: reportdataset.Base{TableID: "1", AliasID: "base"}},
		Tables: map[string]reportdataset.Table{"1": {ID: "1", Owner: "BOS", PhysicalTable: "C_STORE"}},
	}
	for _, test := range []struct {
		name            string
		metadataErr     error
		wantUnavailable bool
	}{
		{name: "source ready while platform store missing"},
		{name: "source schema still checked", metadataErr: errors.New("REPORT_METADATA_CHANGED: source schema changed")},
		{name: "source failure still checked", metadataErr: context.DeadlineExceeded, wantUnavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspector := &fakeSelectSourceInspector{storeErr: ErrReportSnapshotStoreNotReady, metadataErr: test.metadataErr}
			err := inspectSelectMetadata(t.Context(), inspector, dataset)
			if !errors.Is(err, test.metadataErr) || errors.Is(err, ErrReportDatasourceOracleUnavailable) != test.wantUnavailable {
				t.Fatalf("sample source inspection error = %v, want cause=%v unavailable=%t", err, test.metadataErr, test.wantUnavailable)
			}
			if inspector.storeCalls != 0 || inspector.metadataCalls != 1 {
				t.Fatalf("sample validation calls: store=%d metadata=%d", inspector.storeCalls, inspector.metadataCalls)
			}
			// Publication and execution retain the stricter deployment requirement.
			err = inspectSelectSource(t.Context(), inspector, dataset)
			if !errors.Is(err, ErrReportSnapshotStoreNotReady) || inspector.storeCalls != 1 || inspector.metadataCalls != 1 {
				t.Fatalf("snapshot execution did not enforce store readiness: error=%v store=%d metadata=%d", err, inspector.storeCalls, inspector.metadataCalls)
			}
		})
	}
}
