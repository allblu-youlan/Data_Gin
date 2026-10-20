package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"
)

func TestReportExportServiceCreatesOneFrozenExportPerRun(t *testing.T) {
	store := &fakeReportExportStore{}
	service := NewReportExportServiceWithStore(store)
	service.now = func() time.Time { return time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC) }
	result, replayed, err := service.Create(t.Context(), 17, 31, reportquery.Input{})
	if err != nil || replayed || result.RunID != 31 || result.Status != model.ReportExportStatusPending ||
		store.actor != 17 || store.command.Outbox.TaskType != "report:export" || store.command.Outbox.TaskKey == "" {
		t.Fatalf("Create() = %#v replayed=%t error=%v store=%#v", result, replayed, err, store)
	}
	store.replayed = true
	result, replayed, err = service.Create(t.Context(), 17, 31, reportquery.Input{})
	if err != nil || !replayed || result.ID != 41 {
		t.Fatalf("replayed Create() = %#v replayed=%t error=%v", result, replayed, err)
	}
}

func TestReportExportServicePreservesExpiredSnapshotError(t *testing.T) {
	store := &fakeReportExportStore{err: reportrepo.ErrReportSelectResultExpired}
	result, replayed, err := NewReportExportServiceWithStore(store).Create(t.Context(), 17, 31, reportquery.Input{})
	if !errors.Is(err, ErrReportSelectResultExpired) || result != nil || replayed {
		t.Fatalf("Create() result=%#v replayed=%t error=%v", result, replayed, err)
	}
}

type fakeReportExportStore struct {
	actor    uint
	replayed bool
	command  *reportrepo.CreateExportCommand
	err      error
}

func (store *fakeReportExportStore) CreateOrGetExport(_ context.Context, actor, _ uint, _ reportquery.Input, command *reportrepo.CreateExportCommand) (bool, error) {
	store.actor, store.command = actor, command
	command.Export.ID = 41
	command.Export.CreatedAt = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	return !store.replayed, store.err
}
