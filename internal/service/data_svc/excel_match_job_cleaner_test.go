package data_svc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

func TestExcelMatchJobCleanerDeletesOSSBeforeFinalizing(t *testing.T) {
	objectKey := "excel-match-results/2026/07/26/17/result.xlsx"
	store := &fakeExcelMatchCleanupStore{jobs: []model.ExcelMatchJob{{
		BaseModel:       model.BaseModel{ID: 17},
		Status:          excelMatchStatusExpired,
		ResultObjectKey: objectKey,
	}}}
	objects := &fakeExcelMatchCleanupObjectStore{}
	cleaner := newTestExcelMatchJobCleaner(store, objects)

	result, err := cleaner.Cleanup(t.Context())
	if err != nil {
		t.Fatalf("Cleanup() error=%v", err)
	}
	if result != (ExcelMatchCleanupResult{Scanned: 1, Claimed: 1, Expired: 1, Deleted: 1}) {
		t.Fatalf("Cleanup() result=%+v", result)
	}
	if !reflect.DeepEqual(objects.deleted, []string{objectKey}) {
		t.Fatalf("deleted keys=%v", objects.deleted)
	}
	if !reflect.DeepEqual(store.finished, []finishedExcelMatchCleanup{{jobID: 17, objectKey: objectKey}}) {
		t.Fatalf("finished=%+v", store.finished)
	}
}

func TestExcelMatchJobCleanerKeepsFailedObjectAndContinues(t *testing.T) {
	failedKey := "excel-match-results/2026/07/26/17/result.xlsx"
	successKey := "excel-match-results/2026/07/26/18/result.xlsx"
	store := &fakeExcelMatchCleanupStore{jobs: []model.ExcelMatchJob{
		{BaseModel: model.BaseModel{ID: 17}, Status: "success", ResultObjectKey: failedKey},
		{BaseModel: model.BaseModel{ID: 18}, Status: "success", ResultObjectKey: successKey},
	}}
	objects := &fakeExcelMatchCleanupObjectStore{errors: map[string]error{failedKey: errors.New("temporary OSS failure")}}
	cleaner := newTestExcelMatchJobCleaner(store, objects)
	cleaner.batchSize = 1

	result, err := cleaner.Cleanup(t.Context())
	if err == nil {
		t.Fatal("Cleanup() error=nil, want retryable delete error")
	}
	if result != (ExcelMatchCleanupResult{Scanned: 2, Claimed: 2, Expired: 1, Deleted: 1}) {
		t.Fatalf("Cleanup() result=%+v", result)
	}
	if !reflect.DeepEqual(store.finished, []finishedExcelMatchCleanup{{jobID: 18, objectKey: successKey}}) {
		t.Fatalf("failed object was finalized or later object was blocked: %+v", store.finished)
	}
	if !reflect.DeepEqual(store.released, []uint{17}) {
		t.Fatalf("failed object lease was not released: %v", store.released)
	}
}

func TestExcelMatchJobCleanerRejectsObjectOutsideExcelNamespace(t *testing.T) {
	store := &fakeExcelMatchCleanupStore{jobs: []model.ExcelMatchJob{{
		BaseModel:       model.BaseModel{ID: 17},
		Status:          "success",
		ResultObjectKey: "mall-weather-exports/2026/07/26/17/result.xlsx",
	}}}
	objects := &fakeExcelMatchCleanupObjectStore{}
	cleaner := newTestExcelMatchJobCleaner(store, objects)

	result, err := cleaner.Cleanup(t.Context())
	if err == nil {
		t.Fatal("Cleanup() error=nil, want invalid object key error")
	}
	if result.Scanned != 1 || len(objects.deleted) != 0 || len(store.finished) != 0 {
		t.Fatalf("invalid key cleanup result=%+v deleted=%v finished=%v", result, objects.deleted, store.finished)
	}
}

func TestExcelMatchJobCleanerFinalizesJobWithoutOSSObject(t *testing.T) {
	store := &fakeExcelMatchCleanupStore{jobs: []model.ExcelMatchJob{{
		BaseModel: model.BaseModel{ID: 19},
		Status:    "failed",
	}}}
	factoryCalls := 0
	cleaner := newTestExcelMatchJobCleaner(store, &fakeExcelMatchCleanupObjectStore{})
	cleaner.newObjectStore = func() (excelMatchCleanupObjectStore, error) {
		factoryCalls++
		return nil, errors.New("must not be called")
	}

	result, err := cleaner.Cleanup(t.Context())
	if err != nil {
		t.Fatalf("Cleanup() error=%v", err)
	}
	if result != (ExcelMatchCleanupResult{Scanned: 1, Claimed: 1, Expired: 1}) || factoryCalls != 0 {
		t.Fatalf("Cleanup() result=%+v factoryCalls=%d", result, factoryCalls)
	}
}

func TestExcelMatchJobCleanerReclaimsExpiredLocalOnlyLease(t *testing.T) {
	workDir := excelMatchJobDir(20)
	store := &fakeExcelMatchCleanupStore{jobs: []model.ExcelMatchJob{{
		BaseModel: model.BaseModel{ID: 20},
		Status:    excelMatchStatusExpired,
		WorkDir:   workDir,
	}}}
	var removed []string
	cleaner := newTestExcelMatchJobCleaner(store, &fakeExcelMatchCleanupObjectStore{})
	cleaner.removeAll = func(value string) error {
		removed = append(removed, value)
		return nil
	}

	result, err := cleaner.Cleanup(t.Context())
	if err != nil {
		t.Fatalf("Cleanup() error=%v", err)
	}
	if result != (ExcelMatchCleanupResult{Scanned: 1, Claimed: 1, Expired: 1}) {
		t.Fatalf("Cleanup() result=%+v", result)
	}
	if !reflect.DeepEqual(removed, []string{workDir}) {
		t.Fatalf("removed work dirs=%v", removed)
	}
	if !reflect.DeepEqual(store.finished, []finishedExcelMatchCleanup{{jobID: 20}}) {
		t.Fatalf("finished=%+v", store.finished)
	}
}

func TestValidExcelMatchResultObjectKey(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "without prefix", value: "excel-match-results/2026/07/26/17/result.xlsx", want: true},
		{name: "Oracle split archive", value: "excel-match-results/2026/07/26/17/result.zip", want: true},
		{name: "other prefix", value: "warehouse/prod/excel-match-results/2026/07/26/17/result.xlsx"},
		{name: "wrong namespace", value: "weather/2026/07/26/17/result.xlsx"},
		{name: "path traversal", value: "excel-match-results/2026/07/26/../result.xlsx"},
		{name: "invalid date", value: "excel-match-results/2026/02/31/17/result.xlsx"},
		{name: "zero job", value: "excel-match-results/2026/07/26/0/result.xlsx"},
		{name: "wrong job", value: "excel-match-results/2026/07/26/18/result.xlsx"},
		{name: "wrong file", value: "excel-match-results/2026/07/26/17/source.xlsx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validExcelMatchResultObjectKey(tt.value, 17, "excel-match-results"); got != tt.want {
				t.Fatalf("validExcelMatchResultObjectKey(%q, 17)=%v want=%v", tt.value, got, tt.want)
			}
		})
	}
}

func TestExcelMatchJobCleanerDeletesOracleSplitArchive(t *testing.T) {
	objectKey := "excel-match-results/2026/10/21/17/result.zip"
	store := &fakeExcelMatchCleanupStore{jobs: []model.ExcelMatchJob{{
		BaseModel: model.BaseModel{ID: 17}, Status: "success", ResultObjectKey: objectKey,
	}}}
	objects := &fakeExcelMatchCleanupObjectStore{}
	result, err := newTestExcelMatchJobCleaner(store, objects).Cleanup(t.Context())
	if err != nil || result.Deleted != 1 || len(store.finished) != 1 || len(objects.deleted) != 1 || objects.deleted[0] != objectKey {
		t.Fatalf("Oracle archive cleanup result=%+v err=%v objects=%v finished=%v", result, err, objects.deleted, store.finished)
	}
}

func TestExcelMatchJobCleanerDoesNotDeleteWithoutLease(t *testing.T) {
	objectKey := "excel-match-results/2026/07/26/17/result.xlsx"
	store := &fakeExcelMatchCleanupStore{
		jobs:        []model.ExcelMatchJob{{BaseModel: model.BaseModel{ID: 17}, Status: "failed", ResultObjectKey: objectKey}},
		claimDenied: map[uint]bool{17: true},
	}
	objects := &fakeExcelMatchCleanupObjectStore{}
	cleaner := newTestExcelMatchJobCleaner(store, objects)

	result, err := cleaner.Cleanup(t.Context())
	if err != nil {
		t.Fatalf("Cleanup() error=%v", err)
	}
	if result != (ExcelMatchCleanupResult{Scanned: 1}) || len(objects.deleted) != 0 || len(store.finished) != 0 {
		t.Fatalf("cleanup without lease result=%+v deleted=%v finished=%v", result, objects.deleted, store.finished)
	}
}

func TestRefreshExpiredJobOnlyProjectsTerminalStatus(t *testing.T) {
	expiresAt := model.TimeNormal{Time: time.Now().Add(-time.Hour)}
	job := &model.ExcelMatchJob{
		BaseModel:       model.BaseModel{ID: 17},
		Status:          excelMatchStatusSuccess,
		ResultObjectKey: "excel-match-results/2026/07/26/17/result.xlsx",
		ResultURL:       "https://example.invalid/result.xlsx",
		WorkDir:         "/tmp/excel-job-17",
		ExpiresAt:       &expiresAt,
	}
	refreshExpiredJob(job)
	if job.Status != excelMatchStatusExpired || job.ResultObjectKey == "" || job.ResultURL == "" || job.WorkDir == "" {
		t.Fatalf("refreshExpiredJob() mutated persisted cleanup fields: %+v", job)
	}

	running := &model.ExcelMatchJob{Status: "running", ExpiresAt: &expiresAt}
	refreshExpiredJob(running)
	if running.Status != "running" {
		t.Fatalf("refreshExpiredJob() expired active job: %+v", running)
	}
}

func newTestExcelMatchJobCleaner(
	store excelMatchCleanupStore,
	objects excelMatchCleanupObjectStore,
) *excelMatchJobCleaner {
	return &excelMatchJobCleaner{
		jobs: store,
		newObjectStore: func() (excelMatchCleanupObjectStore, error) {
			return objects, nil
		},
		removeAll:       func(string) error { return nil },
		now:             func() time.Time { return time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC) },
		objectKeyPrefix: "excel-match-results",
		batchSize:       100,
		maxJobs:         1000,
		staleAfter:      time.Minute,
		deleteTimeout:   time.Second,
	}
}

type finishedExcelMatchCleanup struct {
	jobID     uint
	objectKey string
}

type fakeExcelMatchCleanupStore struct {
	jobs        []model.ExcelMatchJob
	findErr     error
	finishErr   error
	finished    []finishedExcelMatchCleanup
	claimDenied map[uint]bool
	released    []uint
}

func (store *fakeExcelMatchCleanupStore) ListExpiredCleanupCandidates(
	_ context.Context,
	_ time.Time,
	_ time.Time,
	afterID uint,
	limit int,
) ([]model.ExcelMatchJob, error) {
	if store.findErr != nil {
		return nil, store.findErr
	}
	jobs := make([]model.ExcelMatchJob, 0, limit)
	for _, job := range store.jobs {
		if job.ID <= afterID {
			continue
		}
		jobs = append(jobs, job)
		if len(jobs) == limit {
			break
		}
	}
	return jobs, nil
}

func (store *fakeExcelMatchCleanupStore) ClaimExpiredCleanup(
	_ context.Context,
	candidate model.ExcelMatchJob,
	_ time.Time,
	_ time.Time,
) (bool, error) {
	return !store.claimDenied[candidate.ID], nil
}

func (store *fakeExcelMatchCleanupStore) FinishExpiredCleanup(
	_ context.Context,
	jobID uint,
	objectKey string,
	_ int64,
) error {
	if store.finishErr != nil {
		return store.finishErr
	}
	store.finished = append(store.finished, finishedExcelMatchCleanup{jobID: jobID, objectKey: objectKey})
	return nil
}

func (store *fakeExcelMatchCleanupStore) ReleaseExpiredCleanup(
	_ context.Context,
	candidate model.ExcelMatchJob,
	_ int64,
) error {
	store.released = append(store.released, candidate.ID)
	return nil
}

type fakeExcelMatchCleanupObjectStore struct {
	deleted []string
	errors  map[string]error
}

func (store *fakeExcelMatchCleanupObjectStore) DeleteObject(_ context.Context, objectKey string) error {
	store.deleted = append(store.deleted, objectKey)
	return store.errors[objectKey]
}
