package reportrepo

import (
	"context"
	"errors"
	"fmt"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

type ExportListQuery struct {
	AfterID uint
	Limit   int
	Status  string
}
type ExportListRecord struct {
	Export     model.ReportExport
	ReportName string
}
type ExportListPage struct {
	Items       []ExportListRecord
	HasMore     bool
	NextAfterID uint
}

func (repository *Repository) ListExportsForActor(ctx context.Context, actor uint, query ExportListQuery) (*ExportListPage, error) {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || query.Limit < 1 || query.Limit > 100 {
		return nil, fmt.Errorf("report export query: invalid list request")
	}
	type row struct {
		model.ReportExport
		ReportName string `gorm:"column:report_name"`
	}
	rows := make([]row, 0, query.Limit+1)
	db := buildActorExportListQuery(repository.db.WithContext(ctx), actor, query)
	if err := db.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("report export query: list exports: %w", err)
	}
	page := &ExportListPage{Items: make([]ExportListRecord, 0, query.Limit)}
	if len(rows) > query.Limit {
		page.HasMore = true
		rows = rows[:query.Limit]
	}
	for _, item := range rows {
		page.Items = append(page.Items, ExportListRecord{Export: item.ReportExport, ReportName: item.ReportName})
	}
	if page.HasMore && len(page.Items) > 0 {
		page.NextAfterID = page.Items[len(page.Items)-1].Export.ID
	}
	return page, nil
}

func buildActorExportListQuery(db *gorm.DB, actor uint, query ExportListQuery) *gorm.DB {
	db = db.Table("report_exports AS exports").Select("exports.*, definitions.name AS report_name").Joins("JOIN report_runs runs ON runs.id = exports.run_id AND runs.requested_by = ?", actor).Joins("JOIN report_definitions definitions ON definitions.id = runs.definition_id").Where("exports.created_by = ?", actor)
	if query.AfterID > 0 {
		db = db.Where("exports.id < ?", query.AfterID)
	}
	if query.Status != "" {
		db = db.Where("exports.status = ?", query.Status)
	}
	return db.Order("exports.id DESC").Limit(query.Limit + 1)
}

func (repository *Repository) FindExportForActor(ctx context.Context, actor, exportID uint) (*model.ReportExport, error) {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || exportID == 0 {
		return nil, fmt.Errorf("report export query: invalid request")
	}
	var export model.ReportExport
	err := repository.db.WithContext(ctx).Where("id = ? AND created_by = ?", exportID, actor).First(&export).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrReportExportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report export query: find export: %w", err)
	}
	var run model.ReportRun
	if err := repository.db.WithContext(ctx).First(&run, export.RunID).Error; err != nil {
		return nil, err
	}
	if run.RetainsQuerySnapshot() {
		if err := repository.authorizeRunExport(ctx, repository.db, actor, &run); err != nil {
			return nil, ErrReportExportNotFound
		}
	}
	return &export, nil
}
