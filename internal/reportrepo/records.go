package reportrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

type definitionRecord struct {
	model.ReportDefinition `gorm:"embedded"`
}

func (definitionRecord) TableName() string { return "report_definitions" }

type versionRecord struct {
	model.ReportVersion `gorm:"embedded"`
}

func (versionRecord) TableName() string { return "report_versions" }

type parameterRecord struct {
	model.ReportParameter `gorm:"embedded"`
}

func (parameterRecord) TableName() string { return "report_parameters" }

type columnRecord struct {
	model.ReportColumn `gorm:"embedded"`
}

func (columnRecord) TableName() string { return "report_columns" }

type grantRecord struct {
	model.ReportGrant `gorm:"embedded"`
}

func (grantRecord) TableName() string { return "report_grants" }

func newDefinitionRecord(definition model.ReportDefinition) definitionRecord {
	definition.ID = 0
	definition.CurrentDraftVersionID = 0
	definition.CurrentPublishedVersionID = 0
	definition.WeatherTimestamps = model.WeatherTimestamps{}
	return definitionRecord{ReportDefinition: definition}
}

func newVersionRecord(version model.ReportVersion) versionRecord {
	version.ID = 0
	version.WeatherTimestamps = model.WeatherTimestamps{}
	return versionRecord{ReportVersion: version}
}

func loadCollections(
	ctx context.Context,
	db *gorm.DB,
	_ uint, definitionID, versionID uint,
	draft *Draft,
) error {
	var parameters []parameterRecord
	if err := db.WithContext(ctx).Model(&parameterRecord{}).
		Where("version_id = ?", versionID).
		Order("position ASC, id ASC").Find(&parameters).Error; err != nil {
		return fmt.Errorf("report draft: list parameters: %w", err)
	}
	draft.Parameters = make([]model.ReportParameter, 0, len(parameters))
	for _, record := range parameters {
		draft.Parameters = append(draft.Parameters, record.ReportParameter)
	}

	var columns []columnRecord
	if err := db.WithContext(ctx).Model(&columnRecord{}).
		Where("version_id = ?", versionID).
		Order("display_order ASC, id ASC").Find(&columns).Error; err != nil {
		return fmt.Errorf("report draft: list columns: %w", err)
	}
	draft.Columns = make([]model.ReportColumn, 0, len(columns))
	for _, record := range columns {
		draft.Columns = append(draft.Columns, record.ReportColumn)
	}

	var grants []grantRecord
	if err := db.WithContext(ctx).Model(&grantRecord{}).
		Where("definition_id = ? AND version_id = ?", definitionID, versionID).
		Order("subject_type ASC, subject_id ASC, id ASC").Find(&grants).Error; err != nil {
		return fmt.Errorf("report draft: list grants: %w", err)
	}
	draft.Grants = make([]model.ReportGrant, 0, len(grants))
	for _, record := range grants {
		draft.Grants = append(draft.Grants, record.ReportGrant)
	}
	return nil
}

func replaceCollections(
	ctx context.Context,
	tx *gorm.DB,
	ownerUserID, definitionID, versionID uint,
	parameters []model.ReportParameter,
	columns []model.ReportColumn,
	grants []model.ReportGrant,
) error {
	if err := validateCollections(parameters, columns, grants); err != nil {
		return err
	}

	if ownerUserID == 0 {
		return invalidDraft("owner scope is required")
	}
	if err := tx.WithContext(ctx).
		Where("version_id = ?", versionID).
		Delete(&parameterRecord{}).Error; err != nil {
		return fmt.Errorf("report draft: replace parameters: delete old set: %w", err)
	}
	parameterRows := make([]parameterRecord, 0, len(parameters))
	for _, parameter := range parameters {
		parameter.ID = 0
		parameter.VersionID = versionID
		parameter.WeatherTimestamps = model.WeatherTimestamps{}
		parameterRows = append(parameterRows, parameterRecord{ReportParameter: parameter})
	}
	if len(parameterRows) > 0 {
		if err := tx.WithContext(ctx).Create(&parameterRows).Error; err != nil {
			return fmt.Errorf("report draft: replace parameters: create new set: %w", err)
		}
	}

	if err := tx.WithContext(ctx).
		Where("version_id = ?", versionID).
		Delete(&columnRecord{}).Error; err != nil {
		return fmt.Errorf("report draft: replace columns: delete old set: %w", err)
	}
	columnRows := make([]columnRecord, 0, len(columns))
	for _, column := range columns {
		column.ID = 0
		column.VersionID = versionID
		column.WeatherTimestamps = model.WeatherTimestamps{}
		columnRows = append(columnRows, columnRecord{ReportColumn: column})
	}
	if len(columnRows) > 0 {
		if err := createColumnRecords(ctx, tx, columnRows); err != nil {
			return fmt.Errorf("report draft: replace columns: create new set: %w", err)
		}
	}

	if err := tx.WithContext(ctx).
		Where("definition_id = ? AND version_id = ?", definitionID, versionID).
		Delete(&grantRecord{}).Error; err != nil {
		return fmt.Errorf("report draft: replace grants: delete old set: %w", err)
	}
	grantRows := newGrantRecords(definitionID, versionID, grants, time.Now().UTC())
	if len(grantRows) > 0 {
		if err := tx.WithContext(ctx).Create(&grantRows).Error; err != nil {
			return fmt.Errorf("report draft: replace grants: create new set: %w", err)
		}
	}
	return nil
}

func replaceVersionCollections(
	ctx context.Context,
	tx *gorm.DB,
	definitionID, versionID uint,
	parameters []model.ReportParameter,
	columns []model.ReportColumn,
	grants []model.ReportGrant,
) error {
	parameterRows := make([]parameterRecord, 0, len(parameters))
	for _, parameter := range parameters {
		parameter.ID = 0
		parameter.VersionID = versionID
		parameter.WeatherTimestamps = model.WeatherTimestamps{}
		parameterRows = append(parameterRows, parameterRecord{ReportParameter: parameter})
	}
	if len(parameterRows) > 0 {
		if err := tx.WithContext(ctx).Create(&parameterRows).Error; err != nil {
			return fmt.Errorf("report draft: copy parameters: %w", err)
		}
	}
	columnRows := make([]columnRecord, 0, len(columns))
	for _, column := range columns {
		column.ID = 0
		column.VersionID = versionID
		column.WeatherTimestamps = model.WeatherTimestamps{}
		columnRows = append(columnRows, columnRecord{ReportColumn: column})
	}
	if len(columnRows) > 0 {
		if err := createColumnRecords(ctx, tx, columnRows); err != nil {
			return fmt.Errorf("report draft: copy columns: %w", err)
		}
	}
	grantRows := newGrantRecords(definitionID, versionID, grants, time.Now().UTC())
	if len(grantRows) > 0 {
		if err := tx.WithContext(ctx).Create(&grantRows).Error; err != nil {
			return fmt.Errorf("report draft: copy grants: %w", err)
		}
	}
	return nil
}

// createColumnRecords uses map inserts so false values are not replaced by
// GORM's model-level default:true tags. Oracle metadata and visibility flags
// are explicit report configuration and must survive a draft save unchanged.
func createColumnRecords(ctx context.Context, tx *gorm.DB, rows []columnRecord) error {
	values := make([]map[string]interface{}, 0, len(rows))
	now := time.Now().UTC()
	for index := range rows {
		column := rows[index].ReportColumn
		if column.CreatedAt.IsZero() {
			column.CreatedAt = now
		}
		if column.UpdatedAt.IsZero() {
			column.UpdatedAt = now
		}
		values = append(values, reportColumnInsertValues(column))
	}
	return tx.WithContext(ctx).Model(&columnRecord{}).Create(&values).Error
}

func reportColumnInsertValues(column model.ReportColumn) map[string]interface{} {
	return map[string]interface{}{
		"version_id": column.VersionID, "field_id": column.FieldID, "logical_code": column.LogicalCode,
		"source_spec_json": column.SourceSpecJSON, "value_mode": column.ValueMode, "relation_id": column.RelationID,
		"database_column": column.DatabaseColumn, "source_oracle_type": column.SourceOracleType,
		"precision_value": column.PrecisionValue, "scale_value": column.ScaleValue, "nullable": column.Nullable,
		"value_type": column.ValueType, "preview_header": column.PreviewHeader, "excel_header": column.ExcelHeader,
		"display_order": column.DisplayOrder, "export_order": column.ExportOrder,
		"preview_visible": column.PreviewVisible, "export_visible": column.ExportVisible,
		"filterable": column.Filterable, "sortable": column.Sortable, "export_allowed": column.ExportAllowed,
		"allowed_operators_json": column.AllowedOperatorsJSON, "format_json": column.FormatJSON,
		"dictionary_version_json": column.DictionaryVersionJSON, "masking_policy_json": column.MaskingPolicyJSON,
		"excel_width": column.ExcelWidth, "null_display": column.NullDisplay,
		"created_at": column.CreatedAt, "updated_at": column.UpdatedAt,
	}
}

func newGrantRecords(definitionID, versionID uint, grants []model.ReportGrant, now time.Time) []grantRecord {
	rows := make([]grantRecord, 0, len(grants))
	for _, grant := range grants {
		grant.ID = 0
		grant.DefinitionID = definitionID
		grant.VersionID = versionID
		grant.CreatedAt = now
		grant.UpdatedAt = now
		rows = append(rows, grantRecord{ReportGrant: grant})
	}
	return rows
}

func validateDraftReferences(ctx context.Context, tx *gorm.DB, datasourceID uint, category string, grants []model.ReportGrant) error {
	var datasourceCount int64
	if err := tx.WithContext(ctx).Model(&model.ReportDatasource{}).
		Where("id = ? AND enabled = ? AND driver = ?", datasourceID, true, model.ReportDatasourceDriverOracle).Count(&datasourceCount).Error; err != nil {
		return fmt.Errorf("report draft: validate datasource: %w", err)
	}
	if err := validateReferenceCount("datasource", datasourceCount, 1); err != nil {
		return err
	}
	var categoryPolicyCount int64
	if err := tx.WithContext(ctx).Model(&model.ReportCategoryAccess{}).
		Where("category = ?", strings.TrimSpace(category)).Count(&categoryPolicyCount).Error; err != nil {
		return fmt.Errorf("report draft: validate category access: %w", err)
	}
	if categoryPolicyCount > 0 {
		return nil
	}
	return validateGrantReferences(ctx, tx, grants)
}

func validateGrantReferences(ctx context.Context, tx *gorm.DB, grants []model.ReportGrant) error {
	userIDs := make([]uint, 0, len(grants))
	roleIDs := make([]uint, 0, len(grants))
	for _, grant := range grants {
		switch grant.SubjectType {
		case "USER":
			userIDs = append(userIDs, grant.SubjectID)
		case "ROLE":
			roleIDs = append(roleIDs, grant.SubjectID)
		default:
			return invalidDraft("grant subject type is invalid")
		}
	}
	if len(userIDs) > 0 {
		var userCount int64
		if err := tx.WithContext(ctx).Model(&model.User{}).
			Where("id IN ? AND status = ?", userIDs, model.AccountStatusActive).Count(&userCount).Error; err != nil {
			return fmt.Errorf("report draft: validate grant users: %w", err)
		}
		if err := validateReferenceCount("grant user", userCount, len(userIDs)); err != nil {
			return err
		}
	}
	if len(roleIDs) > 0 {
		var roleCount int64
		if err := tx.WithContext(ctx).Model(&model.Role{}).
			Where("id IN ? AND status = ?", roleIDs, model.RoleStatusActive).Count(&roleCount).Error; err != nil {
			return fmt.Errorf("report draft: validate grant roles: %w", err)
		}
		if err := validateReferenceCount("grant role", roleCount, len(roleIDs)); err != nil {
			return err
		}
	}
	return nil
}

func validateReferenceCount(reference string, actual int64, expected int) error {
	if actual != int64(expected) {
		return invalidDraft(reference + " does not exist or is disabled")
	}
	return nil
}

type reportDraftAuditDetail struct {
	VersionNumber  uint64 `json:"versionNumber"`
	Code           string `json:"code,omitempty"`
	DatasourceID   uint   `json:"datasourceId,omitempty"`
	ParameterCount int    `json:"parameterCount"`
	ColumnCount    int    `json:"columnCount"`
	GrantCount     int    `json:"grantCount"`
}

func newDraftAudit(action string, actor, definitionID uint, versionNumber uint64, draft *Draft) model.ReportAudit {
	detail := reportDraftAuditDetail{VersionNumber: versionNumber}
	if draft != nil {
		detail.Code = draft.Definition.Code
		detail.DatasourceID = draft.Definition.DatasourceID
		detail.ParameterCount = len(draft.Parameters)
		detail.ColumnCount = len(draft.Columns)
		detail.GrantCount = len(draft.Grants)
	}
	return buildReportAudit(action, actor, definitionID, detail)
}

func newCollectionAudit(actor, definitionID uint, versionNumber uint64, parameters []model.ReportParameter, columns []model.ReportColumn, grants []model.ReportGrant) model.ReportAudit {
	return buildReportAudit("REPORT_DRAFT_COLLECTIONS_UPDATE", actor, definitionID, reportDraftAuditDetail{
		VersionNumber: versionNumber, ParameterCount: len(parameters), ColumnCount: len(columns), GrantCount: len(grants),
	})
}

func buildReportAudit(action string, actor, definitionID uint, detail reportDraftAuditDetail) model.ReportAudit {
	encoded, err := json.Marshal(detail)
	if err != nil {
		encoded = []byte(`{}`)
	}
	return model.ReportAudit{
		ActorUserID: actor, Action: action, TargetType: "REPORT_DEFINITION", TargetID: definitionID,
		RequestID: uuid.NewString(), DetailJSON: model.JSONText(encoded),
	}
}

func createReportAudit(ctx context.Context, tx *gorm.DB, audit model.ReportAudit) error {
	if audit.ActorType == "" {
		audit.ActorType = model.ReportAuditActorUser
	}
	if !validReportAuditActor(audit) {
		return fmt.Errorf("report audit: invalid actor")
	}
	if err := tx.WithContext(ctx).Create(&audit).Error; err != nil {
		return fmt.Errorf("report draft: create audit: %w", err)
	}
	return nil
}

func finalizeReportMutation(ctx context.Context, tx *gorm.DB, audit model.ReportAudit, writeAudit reportAuditWriter) error {
	return writeAudit(ctx, tx, audit)
}
