package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/storage"
)

func TestReportExportProcessorRendersUploadsVerifiesAndPurges(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	runtime := testReportExportRuntime(now)
	store := &fakeReportExportExecutionStore{runtime: runtime}
	session := &fakeReportExportOracleSession{pages: []reportoracle.ResultPage{{
		Columns: []string{"ORDER_NO"}, Rows: []reportoracle.ResultRow{{Key: "ROW-A", Values: []interface{}{"SO-1"}}}, NextKey: "ROW-A",
	}}, purgeCounts: []int64{1}}
	objects := &fakeReportExportObjectStore{}
	processor := NewReportExportProcessorWithDependencies(store, staticReportCredentialDecryptor{}, fakeReportExportOracleFactory{session: session})
	processor.now = func() time.Time { return now }
	processor.newToken = tokenSequence(
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	)
	processor.workerID = "worker"
	processor.workRoot = shortReportExportTestRoot(t)
	processor.newObjectStore = func() (reportExportObjectStore, error) { return objects, nil }
	processor.buildObjectKey = func(parts ...string) string { return parts[0] }
	processor.heartbeatInterval = time.Hour
	if err := processor.Process(t.Context(), 41, false); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	if !store.ready || !store.purged || store.readyRows != 1 || store.purgedRows != 1 || !objects.uploaded || objects.deleted {
		t.Fatalf("store=%#v objects=%#v", store, objects)
	}
	if session.readCalls != 1 || session.purgeCalls != 1 {
		t.Fatalf("session read=%d purge=%d", session.readCalls, session.purgeCalls)
	}
}

func TestReportExportWorkbookRenderRequestUsesTwoDecimalPlaces(t *testing.T) {
	columns := []frozenResultColumn{{ValueType: "decimal"}}
	request := reportExportWorkbookRenderRequest(columns, "report.xlsx")
	if request.decimalNumberFormat != reportExcelTwoDecimalNumberFormat || request.OutputPath != "report.xlsx" || len(request.Columns) != 1 {
		t.Fatalf("reportExportWorkbookRenderRequest()=%#v", request)
	}
}

func TestReportExportProcessorTableSnapshotMarksResultPurgedWithoutOracleDelete(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	runtime := testReportExportRuntime(now)
	runtime.Version.ExecutionMode = model.ReportExecutionModeTableSnapshot
	runtime.Export.Status = model.ReportExportStatusReady
	store := &fakeReportExportExecutionStore{runtime: runtime}
	session := &fakeReportExportOracleSession{purgeCounts: []int64{1}}
	processor := NewReportExportProcessorWithDependencies(store, failingReportCredentialDecryptor{}, fakeReportExportOracleFactory{session: session})
	processor.now = func() time.Time { return now }
	processor.newToken = tokenSequence("22222222-2222-4222-8222-222222222222")

	if err := processor.CleanupReadyResult(t.Context(), 41); err != nil {
		t.Fatalf("CleanupReadyResult() error=%v", err)
	}
	if !store.purged || store.purgedRows != runtime.Run.RowCount || session.purgeCalls != 0 {
		t.Fatalf("store=%#v purgeCalls=%d", store, session.purgeCalls)
	}
}

func TestReportExportProcessorDoesNotMarkPurgedWhenReadyResultClaimConflicts(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	runtime := testReportExportRuntime(now)
	runtime.Version.ExecutionMode = model.ReportExecutionModeTableSnapshot
	runtime.Export.Status = model.ReportExportStatusReady
	store := &fakeReportExportExecutionStore{runtime: runtime, claimErr: reportrepo.ErrReportResultPurgeConflict}
	processor := NewReportExportProcessorWithDependencies(store, failingReportCredentialDecryptor{}, fakeReportExportOracleFactory{})
	processor.now = func() time.Time { return now }
	processor.newToken = tokenSequence("22222222-2222-4222-8222-222222222222")

	if err := processor.CleanupReadyResult(t.Context(), 41); err == nil {
		t.Fatal("CleanupReadyResult() accepted a conflicting result claim")
	}
	if store.markPurgedCalls != 0 || store.purged {
		t.Fatalf("markPurgedCalls=%d purged=%t", store.markPurgedCalls, store.purged)
	}
}

func TestReportExportProcessorDoesNotPurgeWhenUploadFails(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := &fakeReportExportExecutionStore{runtime: testReportExportRuntime(now)}
	session := &fakeReportExportOracleSession{pages: []reportoracle.ResultPage{{Columns: []string{"ORDER_NO"}}}}
	processor := NewReportExportProcessorWithDependencies(store, staticReportCredentialDecryptor{}, fakeReportExportOracleFactory{session: session})
	processor.now = func() time.Time { return now }
	processor.newToken = tokenSequence("11111111-1111-4111-8111-111111111111")
	processor.workerID = "worker"
	processor.workRoot = shortReportExportTestRoot(t)
	processor.heartbeatInterval = time.Hour
	processor.buildObjectKey = func(parts ...string) string { return parts[0] }
	processor.newObjectStore = func() (reportExportObjectStore, error) {
		return &fakeReportExportObjectStore{uploadErr: context.DeadlineExceeded}, nil
	}
	objects := &fakeReportExportObjectStore{uploadErr: context.DeadlineExceeded}
	processor.newObjectStore = func() (reportExportObjectStore, error) { return objects, nil }
	err := processor.Process(t.Context(), 41, false)
	if err == nil || !store.failed || store.ready || store.purged || !objects.deleted {
		t.Fatalf("Process() error=%v store=%#v objects=%#v", err, store, objects)
	}
}

func TestReportExportProcessorDoesNotPurgeWhenUploadedChecksumDiffers(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store := &fakeReportExportExecutionStore{runtime: testReportExportRuntime(now)}
	session := &fakeReportExportOracleSession{pages: []reportoracle.ResultPage{{Columns: []string{"ORDER_NO"}}}}
	objects := &fakeReportExportObjectStore{checksumOverride: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}
	processor := NewReportExportProcessorWithDependencies(store, staticReportCredentialDecryptor{}, fakeReportExportOracleFactory{session: session})
	processor.now = func() time.Time { return now }
	processor.newToken = tokenSequence("11111111-1111-4111-8111-111111111111")
	processor.workerID = "worker"
	processor.workRoot = shortReportExportTestRoot(t)
	processor.heartbeatInterval = time.Hour
	processor.buildObjectKey = func(parts ...string) string { return parts[0] }
	processor.newObjectStore = func() (reportExportObjectStore, error) { return objects, nil }
	err := processor.Process(t.Context(), 41, false)
	if err == nil || !store.failed || store.ready || store.purged || !objects.deleted {
		t.Fatalf("Process() error=%v store=%#v objects=%#v", err, store, objects)
	}
}

func testReportExportRuntime(now time.Time) *reportrepo.ExportRuntime {
	expires := now.Add(time.Hour)
	return &reportrepo.ExportRuntime{
		Export:     model.ReportExport{BaseModel: model.BaseModel{ID: 41}, ExportUUID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", RunID: 31, Status: model.ReportExportStatusRunning, FrozenColumnsJSON: model.JSONText(`[{"fieldId":"1","logicalCode":"orderNo","databaseColumn":"ORDER_NO","excelHeader":"订单号","valueType":"string","exportVisible":true,"exportAllowed":true}]`)},
		Run:        model.ReportRun{BaseModel: model.BaseModel{ID: 31}, RunUUID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", VersionID: 23, DefinitionID: 9, Status: model.ReportRunStatusSucceeded, RowCount: 1, ResultExpiresAt: &expires},
		Version:    model.ReportVersion{BaseModel: model.BaseModel{ID: 23}, DatasourceID: 7, ExecutionMode: model.ReportExecutionModeRefCursor, ResultTableOwner: "REPORT_OWNER", ResultTableName: "REPORT_RESULT", ResultRunIDColumn: "RUN_ID", ResultRowIDColumn: "ID"},
		Datasource: model.ReportDatasource{BaseModel: model.BaseModel{ID: 7}, CredentialKeyVersion: "v1", PasswordCiphertext: "cipher"},
	}
}

type staticReportCredentialDecryptor struct{}

func (staticReportCredentialDecryptor) Decrypt(_, _ string) (string, error) { return "password", nil }

type failingReportCredentialDecryptor struct{}

func (failingReportCredentialDecryptor) Decrypt(_, _ string) (string, error) {
	return "", errors.New("credential decrypt must not be called")
}

type fakeReportExportOracleFactory struct{ session reportExportOracleSession }

func (factory fakeReportExportOracleFactory) Open(context.Context, reportrepo.ExportRuntime, string) (reportExportOracleSession, error) {
	return factory.session, nil
}

type fakeReportExportOracleSession struct {
	pages       []reportoracle.ResultPage
	purgeCounts []int64
	readCalls   int
	purgeCalls  int
}

func (session *fakeReportExportOracleSession) Read(_ context.Context, _ []string, _ *reportoracle.ResultCursor, _ int) (reportoracle.ResultPage, error) {
	page := session.pages[session.readCalls]
	session.readCalls++
	return page, nil
}

func (session *fakeReportExportOracleSession) Purge(_ context.Context, _ int) (int64, error) {
	count := session.purgeCounts[session.purgeCalls]
	session.purgeCalls++
	return count, nil
}

func (*fakeReportExportOracleSession) Close() error { return nil }

type fakeReportExportObjectStore struct {
	uploaded         bool
	deleted          bool
	uploadErr        error
	size             int64
	checksum         string
	checksumOverride string
}

func (store *fakeReportExportObjectStore) UploadFile(_ context.Context, objectKey, localPath, _ string) (storage.UploadResult, error) {
	if store.uploadErr != nil {
		return storage.UploadResult{}, store.uploadErr
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return storage.UploadResult{}, err
	}
	contents, err := os.ReadFile(localPath)
	if err != nil {
		return storage.UploadResult{}, err
	}
	digest := sha256.Sum256(contents)
	store.uploaded, store.size, store.checksum = true, info.Size(), hex.EncodeToString(digest[:])
	return storage.UploadResult{ObjectKey: objectKey}, nil
}

func (store *fakeReportExportObjectStore) StatDownloadObject(context.Context, string) (storage.ObjectMetadata, error) {
	checksum := store.checksum
	if store.checksumOverride != "" {
		checksum = store.checksumOverride
	}
	return storage.ObjectMetadata{Size: store.size, ChecksumSHA256: checksum}, nil
}

func (store *fakeReportExportObjectStore) DeleteObject(context.Context, string) error {
	store.deleted = true
	return nil
}

type fakeReportExportExecutionStore struct {
	runtime         *reportrepo.ExportRuntime
	claimErr        error
	ready           bool
	failed          bool
	purged          bool
	readyRows       int64
	purgedRows      int64
	markPurgedCalls int
}

func (store *fakeReportExportExecutionStore) BeginExport(_ context.Context, _ uint, _, token string, _ time.Time, _ time.Duration) (*reportrepo.ExportLease, error) {
	export := store.runtime.Export
	export.LeaseToken = token
	return &reportrepo.ExportLease{Disposition: reportrepo.ExportDispositionAcquired, Export: export, Run: store.runtime.Run}, nil
}

func (*fakeReportExportExecutionStore) HeartbeatExport(context.Context, uint, string, time.Time, time.Duration) (reportrepo.ExportControl, error) {
	return reportrepo.ExportControlContinue, nil
}

func (*fakeReportExportExecutionStore) InspectExport(context.Context, uint, string) (reportrepo.ExportControl, error) {
	return reportrepo.ExportControlContinue, nil
}

func (*fakeReportExportExecutionStore) UpdateExportProgress(context.Context, uint, string, int64, string, model.JSONText, time.Time) error {
	return nil
}

func (store *fakeReportExportExecutionStore) LoadExportRuntime(context.Context, uint, string) (*reportrepo.ExportRuntime, error) {
	copy := *store.runtime
	return &copy, nil
}

func (store *fakeReportExportExecutionStore) MarkExportReady(_ context.Context, _ uint, _ string, _, _ string, _ int64, rows int64, _ int, _ int64, _, _ time.Time) error {
	store.ready, store.readyRows = true, rows
	return nil
}

func (*fakeReportExportExecutionStore) ConfirmExportReady(context.Context, uint, string, string, int64, int64) (bool, error) {
	return false, nil
}

func (*fakeReportExportExecutionStore) ReleaseExportForRetry(context.Context, uint, string, time.Time) error {
	return nil
}

func (store *fakeReportExportExecutionStore) MarkExportFailed(context.Context, uint, string, string, string, time.Time) error {
	store.failed = true
	return nil
}

func (*fakeReportExportExecutionStore) MarkExportCancelled(context.Context, uint, string, time.Time) error {
	return nil
}

func (store *fakeReportExportExecutionStore) ClaimResultPurge(context.Context, uint, string, time.Time) (*reportrepo.ExportRuntime, error) {
	if store.claimErr != nil {
		return nil, store.claimErr
	}
	copy := *store.runtime
	return &copy, nil
}

func (*fakeReportExportExecutionStore) UpdateResultPurgeProgress(context.Context, uint, string, int64, time.Time) error {
	return nil
}

func (store *fakeReportExportExecutionStore) MarkResultPurged(_ context.Context, _ uint, _ string, rows int64, _ time.Time) error {
	store.markPurgedCalls++
	store.purged, store.purgedRows = true, rows
	return nil
}

func (store *fakeReportExportExecutionStore) ConfirmResultPurged(context.Context, uint) (bool, error) {
	return store.purged, nil
}

func (*fakeReportExportExecutionStore) ReleaseResultPurge(context.Context, uint, string, time.Time) error {
	return nil
}

func tokenSequence(tokens ...string) func() string {
	index := 0
	return func() string {
		token := tokens[index]
		index++
		return token
	}
}

func shortReportExportTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/private/tmp", "rx-")
	if err != nil {
		t.Fatalf("MkdirTemp() error=%v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}
