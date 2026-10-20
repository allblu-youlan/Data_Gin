package data_svc

import (
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"
	"strings"
	"testing"
	"time"
)

func TestSelectRowKeysAreSignedAndBoundToRun(t *testing.T) {
	s := &ReportRunQueryService{cursorKey: []byte("select-row-signature-key")}
	run := model.ReportRun{RunUUID: "run-1", ContractHash: strings.Repeat("c", 64)}
	key, e := s.signSelectRow(run, "57")
	if e != nil {
		t.Fatal(e)
	}
	row, e := s.decodeSelectRow(run, key)
	if e != nil || row != 57 {
		t.Fatalf("row=%d %v", row, e)
	}
	for _, tc := range []struct {
		name string
		run  model.ReportRun
		key  string
	}{{"other run", model.ReportRun{RunUUID: "run-2", ContractHash: run.ContractHash}, key}, {"tamper", run, key + "X"}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := s.decodeSelectRow(tc.run, tc.key); e == nil {
				t.Fatal("forged row accepted")
			}
		})
	}
}
func TestSelectResultRetentionUsesFrozenPolicyAndPreservesLegacy(t *testing.T) {
	fallback := time.Hour
	legacy := model.ReportRun{}
	run := model.ReportRun{DataScopeHash: strings.Repeat("a", 64), ExecutionPolicyJSON: model.JSONText(`{"exportPolicy":"MANUAL","resultRetentionHours":48,"purgeAfterExport":false,"snapshotFormatVersion":2}`)}
	if reportResultRetention(run, fallback) != 48*time.Hour || reportResultRetention(legacy, fallback) != fallback || !run.RetainsQuerySnapshot() || legacy.RetainsQuerySnapshot() {
		t.Fatal("incorrect frozen retention policy")
	}
}
func TestManualExportReadyDoesNotWaitForOrPurgeRetainedResult(t *testing.T) {
	store := &fakeReportExportExecutionStore{claimErr: reportrepo.ErrReportResultRetained}
	p := NewReportExportProcessorWithDependencies(store, failingReportCredentialDecryptor{}, fakeReportExportOracleFactory{})
	if e := p.purgeReadyExport(t.Context(), 1, true); e != nil {
		t.Fatal(e)
	}
	if store.markPurgedCalls != 0 || store.purged {
		t.Fatal("manual export purged snapshot")
	}
}
