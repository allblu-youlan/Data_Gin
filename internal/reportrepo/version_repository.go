package reportrepo

import (
	"context"
	"errors"
	"fmt"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

const MaxVersionPageSize = 100

type VersionListQuery struct {
	AfterID uint
	Limit   int
}
type VersionSummary struct {
	Version                                 model.ReportVersion
	ParameterCount, ColumnCount, GrantCount int
}
type VersionPage struct {
	Items       []VersionSummary
	HasMore     bool
	NextAfterID uint
}

type versionSummaryRecord struct {
	model.ReportVersion `gorm:"embedded"`
	ParameterCount      int `gorm:"column:parameter_count"`
	ColumnCount         int `gorm:"column:column_count"`
	GrantCount          int `gorm:"column:grant_count"`
}

func (repository *Repository) ListPublishedVersions(ctx context.Context, ownerUserID, definitionID uint, query VersionListQuery) (VersionPage, error) {
	if err := validateVersionRead(repository, ctx, ownerUserID, definitionID); err != nil {
		return VersionPage{}, err
	}
	if query.Limit < 1 || query.Limit > MaxVersionPageSize {
		return VersionPage{}, invalidDraft("version page limit must be between 1 and 100")
	}
	dbQuery := buildPublishedVersionListQuery(repository.db.WithContext(ctx), ownerUserID, definitionID, query)
	var records []versionSummaryRecord
	if err := dbQuery.Scan(&records).Error; err != nil {
		return VersionPage{}, fmt.Errorf("report version: list: %w", err)
	}
	page := VersionPage{Items: make([]VersionSummary, 0, min(query.Limit, len(records)))}
	if len(records) > query.Limit {
		page.HasMore = true
		records = records[:query.Limit]
	}
	for _, record := range records {
		page.Items = append(page.Items, VersionSummary{Version: record.ReportVersion, ParameterCount: record.ParameterCount, ColumnCount: record.ColumnCount, GrantCount: record.GrantCount})
	}
	if len(page.Items) > 0 {
		page.NextAfterID = page.Items[len(page.Items)-1].Version.ID
	}
	return page, nil
}

func buildPublishedVersionListQuery(db *gorm.DB, ownerUserID, definitionID uint, query VersionListQuery) *gorm.DB {
	dbQuery := publishedVersionSummaryQuery(db, ownerUserID, definitionID).
		Order("versions.id DESC").Limit(query.Limit + 1)
	if query.AfterID > 0 {
		dbQuery = dbQuery.Where("versions.id < ?", query.AfterID)
	}
	return dbQuery
}

func publishedVersionSummaryQuery(db *gorm.DB, ownerUserID, definitionID uint) *gorm.DB {
	return db.Table("report_versions AS versions").
		Select(`versions.*,
			(SELECT COUNT(*) FROM report_parameters AS parameters WHERE parameters.version_id = versions.id) AS parameter_count,
			(SELECT COUNT(*) FROM report_columns AS columns WHERE columns.version_id = versions.id) AS column_count,
			(SELECT COUNT(*) FROM report_grants AS grants WHERE grants.definition_id = versions.definition_id AND grants.version_id = versions.id) AS grant_count`).
		Joins("JOIN report_definitions AS definitions ON definitions.id = versions.definition_id AND definitions.owner_user_id = ?", ownerUserID).
		Where("versions.definition_id = ? AND versions.status = ?", definitionID, model.ReportVersionStatusPublished)
}

func (repository *Repository) FindPublishedVersionSummary(ctx context.Context, ownerUserID, definitionID, versionID uint) (VersionSummary, error) {
	if err := validateVersionRead(repository, ctx, ownerUserID, definitionID); err != nil || versionID == 0 {
		return VersionSummary{}, invalidDraft("repository, context, owner, definition and version id are required")
	}
	var record versionSummaryRecord
	err := publishedVersionSummaryQuery(repository.db.WithContext(ctx), ownerUserID, definitionID).
		Where("versions.id = ?", versionID).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return VersionSummary{}, ErrDraftNotFound
	}
	if err != nil {
		return VersionSummary{}, fmt.Errorf("report version: find: %w", err)
	}
	return VersionSummary{Version: record.ReportVersion, ParameterCount: record.ParameterCount, ColumnCount: record.ColumnCount, GrantCount: record.GrantCount}, nil
}

func (repository *Repository) FindPublishedVersion(ctx context.Context, ownerUserID, definitionID, versionID uint) (*Draft, error) {
	if err := validateVersionRead(repository, ctx, ownerUserID, definitionID); err != nil || versionID == 0 {
		return nil, invalidDraft("repository, context, owner, definition and version id are required")
	}
	var definition definitionRecord
	if err := definitionScope(repository.db.WithContext(ctx), ownerUserID).Where("id = ?", definitionID).Take(&definition).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDraftNotFound
		}
		return nil, fmt.Errorf("report version: find definition: %w", err)
	}
	var version versionRecord
	err := repository.db.WithContext(ctx).Where("id = ? AND definition_id = ? AND status = ?", versionID, definitionID, model.ReportVersionStatusPublished).Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report version: find published version: %w", err)
	}
	draft := &Draft{Definition: definition.ReportDefinition, Version: version.ReportVersion, LockVersion: version.VersionNumber}
	if err := repository.loadCollections(ctx, repository.db, ownerUserID, definitionID, versionID, draft); err != nil {
		return nil, err
	}
	return draft, nil
}

func validateVersionRead(repository *Repository, ctx context.Context, ownerUserID, definitionID uint) error {
	if repository == nil || repository.db == nil || ctx == nil || ownerUserID == 0 || definitionID == 0 {
		return invalidDraft("repository, context, owner and definition id are required")
	}
	return nil
}
