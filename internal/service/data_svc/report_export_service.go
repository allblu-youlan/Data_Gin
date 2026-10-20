package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"

	"github.com/google/uuid"
)

var (
	ErrReportExportInvalid  = errors.New("report export service: invalid input")
	ErrReportExportNotFound = errors.New("report export service: not found")
	ErrReportExportConflict = errors.New("report export service: conflict")
)

type reportExportStore interface {
	CreateOrGetExport(context.Context, uint, uint, reportquery.Input, *reportrepo.CreateExportCommand) (bool, error)
}

type ReportExportDTO struct {
	ID         uint      `json:"id"`
	ExportUUID string    `json:"exportUuid"`
	RunID      uint      `json:"runId"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ReportExportService struct {
	store reportExportStore
	now   func() time.Time
}

func NewReportExportService() *ReportExportService {
	return NewReportExportServiceWithStore(reportrepo.New())
}

func NewReportExportServiceWithStore(store reportExportStore) *ReportExportService {
	if store == nil {
		panic("report export service: store is required")
	}
	return &ReportExportService{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (service *ReportExportService) Create(ctx context.Context, actor, runID uint, query reportquery.Input) (*ReportExportDTO, bool, error) {
	if service == nil || ctx == nil || actor == 0 || runID == 0 {
		return nil, false, ErrReportExportInvalid
	}
	now := service.now().UTC()
	exportUUID := uuid.NewString()
	command := &reportrepo.CreateExportCommand{Export: model.ReportExport{
		ExportUUID: exportUUID, RunID: runID, Status: model.ReportExportStatusPending,
		FrozenFiltersJSON: model.JSONText(`{}`), FrozenSortJSON: model.JSONText(`[]`), FrozenColumnsJSON: model.JSONText(`[]`), CreatedBy: actor,
	}, Outbox: reportrepo.NewReportExportOutbox(exportUUID, now)}
	created, err := service.store.CreateOrGetExport(ctx, actor, runID, query, command)
	if err != nil {
		switch {
		case errors.Is(err, reportrepo.ErrReportSelectResultExpired):
			return nil, false, ErrReportSelectResultExpired
		case errors.Is(err, reportrepo.ErrReportActionDenied), errors.Is(err, reportrepo.ErrReportRunAccessNotFound):
			return nil, false, ErrReportExportNotFound
		case errors.Is(err, reportrepo.ErrReportExportRunNotReady):
			return nil, false, ErrReportExportConflict
		default:
			return nil, false, fmt.Errorf("report export service: create: %w", err)
		}
	}
	return &ReportExportDTO{ID: command.Export.ID, ExportUUID: command.Export.ExportUUID, RunID: command.Export.RunID, Status: command.Export.Status, CreatedAt: command.Export.CreatedAt}, !created, nil
}
