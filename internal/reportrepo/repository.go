package reportrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"gin-biz-web-api/internal/reportidentity"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDraftNotFound         = errors.New("report draft: not found")
	ErrDatasourceUnavailable = errors.New("report draft: datasource unavailable")
	ErrDraftVersionConflict  = errors.New("report draft: version conflict")
	ErrDraftDeleteConflict   = errors.New("report draft: cannot delete published or executed report")
	ErrResultTableConflict   = errors.New("report draft: result table is already bound")
	ErrInvalidDraft          = errors.New("report draft: invalid input")
)

const maxDraftPageSize = 100

type Draft struct {
	Definition  model.ReportDefinition
	Version     model.ReportVersion
	Parameters  []model.ReportParameter
	Columns     []model.ReportColumn
	Grants      []model.ReportGrant
	LockVersion uint64
}

type DraftListQuery struct {
	AfterID          uint
	Limit            int
	Category         string
	Search           string
	PublishedOnly    bool
	Action           string
	AdditionalAction string
}

type DraftSummary struct {
	Definition  model.ReportDefinition
	LockVersion uint64
	IsOwner     bool
}

type draftSummaryRecord struct {
	model.ReportDefinition `gorm:"embedded"`
	LockVersion            uint64 `gorm:"column:lock_version"`
	IsOwner                bool   `gorm:"column:is_owner"`
}

type DraftPage struct {
	Items       []DraftSummary
	HasMore     bool
	NextAfterID uint
}

type Publication struct {
	CallTemplate                  string
	CompiledSpecJSON              model.JSONText
	ContractHash                  string
	ParameterSchemaHash           string
	ProcedureSignatureHash        string
	ResultSchemaHash              string
	PermissionHash                string
	ExportSchemaHash              string
	SchemaProbeToken              string
	SchemaValidatedAt             time.Time
	ConnectionFingerprint         string
	ConnectionIdentitySource      string
	DatasourceSnapshotFingerprint string
}

type VersionActivation struct {
	ContractHash                  string
	ConnectionFingerprint         string
	ConnectionIdentitySource      string
	DatasourceSnapshotFingerprint string
}

type transactionRunner func(context.Context, *gorm.DB, func(*gorm.DB) error) error
type draftReferenceValidator func(context.Context, *gorm.DB, uint, string, []model.ReportGrant) error
type draftDefinitionLocker func(context.Context, *gorm.DB, uint, uint) (*definitionRecord, error)
type draftVersionLocker func(context.Context, *gorm.DB, uint, uint) (*versionRecord, error)
type publishedVersionLocker func(context.Context, *gorm.DB, uint, uint) (*versionRecord, error)
type publicationDatasourceLocker func(context.Context, *gorm.DB, uint, string) error
type reportAuditWriter func(context.Context, *gorm.DB, model.ReportAudit) error
type systemReportAuditWriter func(context.Context, *gorm.DB, string, string, uint, map[string]interface{}) error
type draftCollectionsLoader func(context.Context, *gorm.DB, uint, uint, uint, *Draft) error
type publishedVersionWriter func(context.Context, *gorm.DB, uint, uint, uint64, map[string]interface{}) error
type draftVersionCreator func(context.Context, *gorm.DB, *versionRecord) error
type versionCollectionsCopier func(context.Context, *gorm.DB, uint, uint, []model.ReportParameter, []model.ReportColumn, []model.ReportGrant) error
type publishedDefinitionSwitcher func(context.Context, *gorm.DB, uint, uint, uint, uint, uint) error
type publishedReportLoader func(context.Context, *gorm.DB, uint, uint, string, bool) (*PublishedReport, error)
type reportRunWriter func(context.Context, *gorm.DB, *model.ReportRun) error
type reportRunOutboxWriter func(context.Context, *gorm.DB, *model.AsyncJobOutbox) error
type enabledDatasourceValidator func(context.Context, *gorm.DB, uint) error
type reportRunSlotPreparation struct {
	ExistingRun *model.ReportRun
}

type reportRunSlotPreparer func(context.Context, *gorm.DB, uint, uint, string, time.Time) (reportRunSlotPreparation, error)

type Repository struct {
	db                    *gorm.DB
	transact              transactionRunner
	validateReferences    draftReferenceValidator
	lockDefinition        draftDefinitionLocker
	lockVersion           draftVersionLocker
	lockPublishedVersion  publishedVersionLocker
	lockPublicationSource publicationDatasourceLocker
	writeAudit            reportAuditWriter
	writeSystemAudit      systemReportAuditWriter
	loadCollections       draftCollectionsLoader
	publishVersion        publishedVersionWriter
	createVersion         draftVersionCreator
	copyCollections       versionCollectionsCopier
	switchDefinition      publishedDefinitionSwitcher
	loadPublished         publishedReportLoader
	createReportRun       reportRunWriter
	createRunOutbox       reportRunOutboxWriter
	validateRunSource     enabledDatasourceValidator
	prepareRunSlot        reportRunSlotPreparer
}

func New(databases ...*gorm.DB) *Repository {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &Repository{
		db: db, transact: runTransaction, validateReferences: validateDraftReferences,
		lockDefinition: lockDraftDefinition, lockVersion: lockDraftVersion, lockPublicationSource: lockPublicationDatasource,
		lockPublishedVersion: lockPublishedReportVersion,
		writeAudit:           createReportAudit, writeSystemAudit: writeSystemReportAudit,
		loadCollections: loadCollections, publishVersion: writePublishedVersion, createVersion: createDraftVersion,
		copyCollections: replaceVersionCollections, switchDefinition: switchPublishedDefinition,
		loadPublished: loadPublishedReport, createReportRun: writeReportRun, createRunOutbox: writeReportRunOutbox,
		validateRunSource: requireEnabledOracleDatasource, prepareRunSlot: prepareReportRunSlot,
	}
}

func runTransaction(ctx context.Context, db *gorm.DB, operation func(*gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(operation)
}

func (repository *Repository) CreateDraft(ctx context.Context, ownerUserID uint, draft *Draft) error {
	if err := repository.validate(ctx, ownerUserID); err != nil {
		return err
	}
	if err := validateNewDraft(draft); err != nil {
		return err
	}

	normalizeNewDraft(draft)
	err := repository.transact(ctx, repository.db, func(tx *gorm.DB) error {
		if draft.Definition.OwnerUserID != ownerUserID {
			return invalidDraft("definition owner does not match owner scope")
		}
		if err := repository.validateReferences(ctx, tx, draft.Definition.DatasourceID, draft.Definition.Category, draft.Grants); err != nil {
			return err
		}
		definitionRecord := newDefinitionRecord(draft.Definition)
		if err := tx.WithContext(ctx).Create(&definitionRecord).Error; err != nil {
			return fmt.Errorf("report draft: create definition: %w", err)
		}

		draft.Definition.ID = definitionRecord.ID
		draft.Version.DefinitionID = definitionRecord.ID
		versionRecord := newVersionRecord(draft.Version)
		if err := tx.WithContext(ctx).Create(&versionRecord).Error; err != nil {
			return fmt.Errorf("report draft: create version: %w", err)
		}

		draft.Version.ID = versionRecord.ID
		result := definitionScope(tx.WithContext(ctx), ownerUserID).
			Where("id = ?", definitionRecord.ID).
			Updates(map[string]interface{}{
				"current_draft_version_id": versionRecord.ID,
			})
		if result.Error != nil {
			return fmt.Errorf("report draft: link draft version: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		if err := replaceCollections(ctx, tx, ownerUserID, definitionRecord.ID, versionRecord.ID,
			draft.Parameters, draft.Columns, draft.Grants); err != nil {
			return err
		}
		if err := finalizeReportMutation(ctx, tx, newDraftAudit("REPORT_DRAFT_CREATE", ownerUserID, definitionRecord.ID, versionRecord.VersionNumber, draft), repository.writeAudit); err != nil {
			return err
		}
		draft.Definition.CurrentDraftVersionID = versionRecord.ID
		draft.LockVersion = versionRecord.VersionNumber
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (repository *Repository) FindDraftByID(ctx context.Context, ownerUserID, definitionID uint) (*Draft, error) {
	if err := repository.validate(ctx, ownerUserID); err != nil || definitionID == 0 {
		return nil, invalidDraft("owner and definition id are required")
	}

	var definition definitionRecord
	err := definitionScope(repository.db.WithContext(ctx), ownerUserID).
		Where("id = ?", definitionID).
		First(&definition).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report draft: find definition: %w", err)
	}
	if definition.CurrentDraftVersionID == 0 {
		return nil, ErrDraftNotFound
	}

	var version versionRecord
	err = versionScope(repository.db.WithContext(ctx)).
		Where("id = ? AND definition_id = ?", definition.CurrentDraftVersionID, definitionID).
		First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report draft: find version: %w", err)
	}

	draft := &Draft{
		Definition:  definition.ReportDefinition,
		Version:     version.ReportVersion,
		LockVersion: version.VersionNumber,
	}
	if err := loadCollections(ctx, repository.db, ownerUserID, definitionID, version.ID, draft); err != nil {
		return nil, err
	}
	return draft, nil
}

// DeleteDraft permanently removes an unpublished template and its versioned
// configuration. Published reports and run history remain immutable.
func (repository *Repository) DeleteDraft(ctx context.Context, ownerUserID, definitionID uint, expectedLockVersion uint64) error {
	if err := repository.validate(ctx, ownerUserID); err != nil {
		return err
	}
	if definitionID == 0 || expectedLockVersion == 0 {
		return invalidDraft("definition id and lock version are required")
	}

	return repository.transact(ctx, repository.db, func(tx *gorm.DB) error {
		definition, err := repository.lockDefinition(ctx, tx, ownerUserID, definitionID)
		if err != nil {
			return err
		}
		version, err := repository.lockVersion(ctx, tx, definitionID, definition.CurrentDraftVersionID)
		if err != nil {
			return err
		}
		if version.VersionNumber != expectedLockVersion {
			return ErrDraftVersionConflict
		}
		if definition.Status != model.ReportDefinitionStatusDraft || definition.CurrentPublishedVersionID != 0 {
			return ErrDraftDeleteConflict
		}

		var runCount int64
		if err := tx.WithContext(ctx).Model(&model.ReportRun{}).Where("definition_id = ?", definitionID).Count(&runCount).Error; err != nil {
			return fmt.Errorf("report draft: inspect run history before delete: %w", err)
		}
		if runCount != 0 {
			return ErrDraftDeleteConflict
		}

		auditDraft := &Draft{Definition: definition.ReportDefinition, Version: version.ReportVersion, LockVersion: version.VersionNumber}
		if err := repository.writeAudit(ctx, tx, newDraftAudit("REPORT_DRAFT_DELETE", ownerUserID, definitionID, version.VersionNumber, auditDraft)); err != nil {
			return err
		}

		versionIDs := tx.WithContext(ctx).Model(&versionRecord{}).Select("id").Where("definition_id = ?", definitionID)
		if err := tx.WithContext(ctx).Where("version_id IN (?)", versionIDs).Delete(&parameterRecord{}).Error; err != nil {
			return fmt.Errorf("report draft: delete parameters: %w", err)
		}
		if err := tx.WithContext(ctx).Where("version_id IN (?)", versionIDs).Delete(&columnRecord{}).Error; err != nil {
			return fmt.Errorf("report draft: delete columns: %w", err)
		}
		if err := tx.WithContext(ctx).Where("definition_id = ?", definitionID).Delete(&grantRecord{}).Error; err != nil {
			return fmt.Errorf("report draft: delete grants: %w", err)
		}
		if err := tx.WithContext(ctx).Where("definition_id = ?", definitionID).Delete(&model.ReportResultTableBinding{}).Error; err != nil {
			return fmt.Errorf("report draft: delete result table binding: %w", err)
		}
		if err := tx.WithContext(ctx).Where("definition_id = ?", definitionID).Delete(&versionRecord{}).Error; err != nil {
			return fmt.Errorf("report draft: delete versions: %w", err)
		}
		result := definitionScope(tx.WithContext(ctx), ownerUserID).
			Where("id = ? AND current_draft_version_id = ? AND current_published_version_id = 0", definitionID, version.ID).
			Delete(&definitionRecord{})
		if result.Error != nil {
			return fmt.Errorf("report draft: delete definition: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		return nil
	})
}

func (repository *Repository) FindDatasource(ctx context.Context, datasourceID uint) (*model.ReportDatasource, error) {
	if repository == nil || repository.db == nil || ctx == nil || datasourceID == 0 {
		return nil, invalidDraft("repository, context and datasource id are required")
	}
	var datasource model.ReportDatasource
	err := repository.db.WithContext(ctx).Where("id = ? AND enabled = ? AND driver = ?", datasourceID, true, model.ReportDatasourceDriverOracle).First(&datasource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDatasourceUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("report draft: find datasource: %w", err)
	}
	return &datasource, nil
}

func (repository *Repository) PublishDraft(ctx context.Context, ownerUserID, definitionID uint, expectedLockVersion uint64, publication Publication) (*Draft, error) {
	if err := repository.validate(ctx, ownerUserID); err != nil {
		return nil, err
	}
	if definitionID == 0 || expectedLockVersion == 0 || !validPublication(publication) {
		return nil, invalidDraft("definition, lock version and publication are required")
	}
	var published Draft
	var publishedAt time.Time
	var nextDraftVersionID uint
	err := repository.transact(ctx, repository.db, func(tx *gorm.DB) error {
		definition, err := repository.lockDefinition(ctx, tx, ownerUserID, definitionID)
		if err != nil {
			return err
		}
		current, err := repository.lockVersion(ctx, tx, definitionID, definition.CurrentDraftVersionID)
		if err != nil {
			return err
		}
		if current.VersionNumber != expectedLockVersion {
			return ErrDraftVersionConflict
		}
		current.CallTemplate = publication.CallTemplate
		published = Draft{Definition: definition.ReportDefinition, Version: current.ReportVersion, LockVersion: current.VersionNumber}
		if err := repository.loadCollections(ctx, tx, ownerUserID, definitionID, current.ID, &published); err != nil {
			return err
		}
		if current.DatasourceID == 0 || current.DatasourceID != definition.DatasourceID {
			return invalidDraft("draft datasource snapshot is inconsistent")
		}
		if err := repository.lockPublicationSource(ctx, tx, current.DatasourceID, publication.DatasourceSnapshotFingerprint); err != nil {
			return err
		}
		if err := repository.validateReferences(ctx, tx, current.DatasourceID, definition.Category, published.Grants); err != nil {
			return err
		}
		if err := replaceResultTableBinding(ctx, tx, definitionID, current.ID, current.ReportVersion, publication.ConnectionFingerprint, publication.ConnectionIdentitySource); err != nil {
			return err
		}
		publishedAt = time.Now().UTC()
		publishedUpdates := map[string]interface{}{
			"status": model.ReportVersionStatusPublished, "call_template": publication.CallTemplate, "compiled_spec_json": publication.CompiledSpecJSON,
			"contract_hash": publication.ContractHash, "parameter_schema_hash": publication.ParameterSchemaHash,
			"procedure_signature_hash": publication.ProcedureSignatureHash, "result_schema_hash": publication.ResultSchemaHash,
			"permission_hash": publication.PermissionHash, "export_schema_hash": publication.ExportSchemaHash,
			"schema_probe_token": publication.SchemaProbeToken, "schema_validated_at": publication.SchemaValidatedAt,
			"published_by": ownerUserID, "published_at": publishedAt,
		}
		if err := repository.publishVersion(ctx, tx, current.ID, definitionID, expectedLockVersion, publishedUpdates); err != nil {
			return err
		}

		next := nextDraftAfterPublication(current.ReportVersion, ownerUserID)
		nextRecord := newVersionRecord(next)
		if err := repository.createVersion(ctx, tx, &nextRecord); err != nil {
			return err
		}
		nextDraftVersionID = nextRecord.ID
		if err := repository.copyCollections(
			ctx,
			tx,
			definitionID,
			nextRecord.ID,
			published.Parameters,
			published.Columns,
			published.Grants,
		); err != nil {
			return err
		}
		if err := repository.switchDefinition(ctx, tx, ownerUserID, definitionID, current.ID, nextRecord.ID, ownerUserID); err != nil {
			return err
		}
		return repository.writeAudit(ctx, tx, buildReportAudit("REPORT_PUBLISH", ownerUserID, definitionID, reportDraftAuditDetail{VersionNumber: expectedLockVersion, Code: definition.Code, DatasourceID: current.DatasourceID, ParameterCount: len(published.Parameters), ColumnCount: len(published.Columns), GrantCount: len(published.Grants)}))
	})
	if err != nil {
		return nil, err
	}
	published.Version.Status = model.ReportVersionStatusPublished
	published.Version.CallTemplate = publication.CallTemplate
	published.Version.CompiledSpecJSON = publication.CompiledSpecJSON
	published.Version.ContractHash = publication.ContractHash
	published.Version.ParameterSchemaHash = publication.ParameterSchemaHash
	published.Version.ProcedureSignatureHash = publication.ProcedureSignatureHash
	published.Version.ResultSchemaHash = publication.ResultSchemaHash
	published.Version.PermissionHash = publication.PermissionHash
	published.Version.ExportSchemaHash = publication.ExportSchemaHash
	published.Version.SchemaProbeToken = publication.SchemaProbeToken
	published.Version.SchemaValidatedAt = &publication.SchemaValidatedAt
	published.Version.PublishedBy = ownerUserID
	published.Version.PublishedAt = &publishedAt
	published.Definition.Status = model.ReportDefinitionStatusActive
	published.Definition.CurrentPublishedVersionID = published.Version.ID
	published.Definition.CurrentDraftVersionID = nextDraftVersionID
	return &published, nil
}

func (repository *Repository) ActivatePublishedVersion(ctx context.Context, ownerUserID, definitionID, versionID uint, expectedLockVersion uint64, activation VersionActivation) (*Draft, error) {
	if err := repository.validate(ctx, ownerUserID); err != nil {
		return nil, err
	}
	if definitionID == 0 || versionID == 0 || expectedLockVersion == 0 || len(activation.ContractHash) != 64 ||
		len(activation.ConnectionFingerprint) != 64 || activation.ConnectionIdentitySource != reportidentity.BindingIdentitySourceOracle || len(activation.DatasourceSnapshotFingerprint) != 64 {
		return nil, invalidDraft("definition, version, lock version and activation are required")
	}
	var activated Draft
	err := repository.transact(ctx, repository.db, func(tx *gorm.DB) error {
		definition, err := repository.lockDefinition(ctx, tx, ownerUserID, definitionID)
		if err != nil {
			return err
		}
		currentDraft, err := repository.lockVersion(ctx, tx, definitionID, definition.CurrentDraftVersionID)
		if err != nil {
			return err
		}
		if currentDraft.VersionNumber != expectedLockVersion || definition.CurrentPublishedVersionID == versionID {
			return ErrDraftVersionConflict
		}
		target, err := repository.lockPublishedVersion(ctx, tx, definitionID, versionID)
		if err != nil {
			return err
		}
		if target.ContractHash != activation.ContractHash || target.DatasourceID == 0 {
			return ErrDraftVersionConflict
		}
		activated = Draft{Definition: definition.ReportDefinition, Version: target.ReportVersion, LockVersion: target.VersionNumber}
		if err := repository.loadCollections(ctx, tx, ownerUserID, definitionID, target.ID, &activated); err != nil {
			return err
		}
		if err := repository.lockPublicationSource(ctx, tx, target.DatasourceID, activation.DatasourceSnapshotFingerprint); err != nil {
			return err
		}
		if err := repository.validateReferences(ctx, tx, target.DatasourceID, definition.Category, activated.Grants); err != nil {
			return err
		}
		if err := replaceResultTableBinding(ctx, tx, definitionID, target.ID, target.ReportVersion, activation.ConnectionFingerprint, activation.ConnectionIdentitySource); err != nil {
			return err
		}
		result := definitionScope(tx.WithContext(ctx), ownerUserID).
			Where("id = ? AND current_draft_version_id = ?", definitionID, currentDraft.ID).
			Updates(map[string]interface{}{"status": model.ReportDefinitionStatusActive, "current_published_version_id": target.ID, "updated_by": ownerUserID})
		if result.Error != nil {
			return fmt.Errorf("report version: activate definition: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		return repository.writeAudit(ctx, tx, buildReportAudit("REPORT_VERSION_ACTIVATE", ownerUserID, definitionID, reportDraftAuditDetail{
			VersionNumber: target.VersionNumber, Code: definition.Code, DatasourceID: target.DatasourceID,
			ParameterCount: len(activated.Parameters), ColumnCount: len(activated.Columns), GrantCount: len(activated.Grants),
		}))
	})
	if err != nil {
		return nil, err
	}
	activated.Definition.Status = model.ReportDefinitionStatusActive
	activated.Definition.CurrentPublishedVersionID = versionID
	return &activated, nil
}

func nextDraftAfterPublication(current model.ReportVersion, actor uint) model.ReportVersion {
	current.ID = 0
	current.VersionNumber++
	current.Status = model.ReportVersionStatusDraft
	current.CompiledSpecJSON = ""
	current.ContractHash, current.ParameterSchemaHash, current.ProcedureSignatureHash = "", "", ""
	current.ResultSchemaHash, current.PermissionHash, current.ExportSchemaHash = "", "", ""
	current.SchemaProbeToken, current.PublishedBy, current.PublishedAt, current.SchemaValidatedAt = "", 0, nil, nil
	current.CreatedBy = actor
	current.WeatherTimestamps = model.WeatherTimestamps{}
	return current
}

func writePublishedVersion(ctx context.Context, tx *gorm.DB, versionID, definitionID uint, expectedVersion uint64, updates map[string]interface{}) error {
	result := tx.WithContext(ctx).Model(&versionRecord{}).
		Where("id = ? AND definition_id = ? AND status = ? AND version_number = ?", versionID, definitionID, model.ReportVersionStatusDraft, expectedVersion).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("report draft: publish version: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrDraftVersionConflict
	}
	return nil
}

func createDraftVersion(ctx context.Context, tx *gorm.DB, version *versionRecord) error {
	if err := tx.WithContext(ctx).Create(version).Error; err != nil {
		return fmt.Errorf("report draft: create post-publication draft: %w", err)
	}
	return nil
}

func switchPublishedDefinition(ctx context.Context, tx *gorm.DB, ownerUserID, definitionID, publishedVersionID, draftVersionID, updatedBy uint) error {
	result := definitionScope(tx.WithContext(ctx), ownerUserID).Where("id = ? AND current_draft_version_id = ?", definitionID, publishedVersionID).
		Updates(map[string]interface{}{"status": model.ReportDefinitionStatusActive, "current_published_version_id": publishedVersionID, "current_draft_version_id": draftVersionID, "updated_by": updatedBy})
	if result.Error != nil {
		return fmt.Errorf("report draft: switch published version: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrDraftVersionConflict
	}
	return nil
}

func validPublication(value Publication) bool {
	return strings.TrimSpace(value.CallTemplate) != "" && json.Valid([]byte(value.CompiledSpecJSON)) && len(value.ContractHash) == 64 && len(value.ParameterSchemaHash) == 64 &&
		len(value.ProcedureSignatureHash) == 64 && len(value.ResultSchemaHash) == 64 && len(value.PermissionHash) == 64 &&
		len(value.ExportSchemaHash) == 64 && len(value.SchemaProbeToken) == 36 && !value.SchemaValidatedAt.IsZero() &&
		len(value.ConnectionFingerprint) == 64 && value.ConnectionIdentitySource == reportidentity.BindingIdentitySourceOracle &&
		len(value.DatasourceSnapshotFingerprint) == 64
}

func lockPublicationDatasource(ctx context.Context, tx *gorm.DB, datasourceID uint, expectedFingerprint string) error {
	if ctx == nil || tx == nil || datasourceID == 0 || len(expectedFingerprint) != 64 {
		return invalidDraft("publication datasource snapshot is required")
	}
	var datasource model.ReportDatasource
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND driver = ? AND enabled = ?", datasourceID, model.ReportDatasourceDriverOracle, true).
		First(&datasource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrDatasourceUnavailable
	}
	if err != nil {
		return fmt.Errorf("report draft: lock publication datasource: %w", err)
	}
	if reportidentity.DatasourceFingerprint(datasource) != expectedFingerprint {
		return ErrDraftVersionConflict
	}
	return nil
}

func replaceResultTableBinding(
	ctx context.Context,
	tx *gorm.DB,
	definitionID, versionID uint,
	version model.ReportVersion,
	connectionFingerprint, identitySource string,
) error {
	if tx == nil || ctx == nil || definitionID == 0 || versionID == 0 {
		return invalidDraft("result table binding transaction is required")
	}
	if err := tx.WithContext(ctx).Where("definition_id = ?", definitionID).Delete(&model.ReportResultTableBinding{}).Error; err != nil {
		return fmt.Errorf("report draft: remove previous result table binding: %w", err)
	}
	if version.ExecutionMode != model.ReportExecutionModeTableSnapshot {
		return nil
	}
	binding := model.ReportResultTableBinding{
		ConnectionFingerprint: strings.TrimSpace(connectionFingerprint),
		IdentitySource:        strings.TrimSpace(identitySource),
		TableOwner:            strings.ToUpper(strings.TrimSpace(version.ResultTableOwner)),
		ResultTableName:       strings.ToUpper(strings.TrimSpace(version.ResultTableName)),
		DefinitionID:          definitionID,
		VersionID:             versionID,
	}
	if len(binding.ConnectionFingerprint) != 64 || binding.IdentitySource != reportidentity.BindingIdentitySourceOracle || binding.TableOwner == "" || binding.ResultTableName == "" {
		return invalidDraft("result table binding identity is invalid")
	}
	var legacyBindings int64
	if err := tx.WithContext(ctx).Model(&model.ReportResultTableBinding{}).
		Where("table_owner = ? AND table_name = ? AND definition_id <> ?", binding.TableOwner, binding.ResultTableName, definitionID).
		Where("identity_source IS NULL OR identity_source <> ?", reportidentity.BindingIdentitySourceOracle).
		Count(&legacyBindings).Error; err != nil {
		return fmt.Errorf("report draft: inspect legacy result table bindings: %w", err)
	}
	if legacyBindings > 0 {
		return ErrResultTableConflict
	}
	now := time.Now().UTC()
	result := tx.WithContext(ctx).Exec(`INSERT INTO report_result_table_bindings
		(connection_fingerprint, identity_source, table_owner, table_name, definition_id, version_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, binding.ConnectionFingerprint, binding.IdentitySource, binding.TableOwner, binding.ResultTableName,
		binding.DefinitionID, binding.VersionID, now, now)
	if err := result.Error; err != nil {
		var mysqlError *mysqlDriver.MySQLError
		if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
			return ErrResultTableConflict
		}
		return fmt.Errorf("report draft: register result table binding: %w", err)
	}
	return nil
}

func (repository *Repository) ListDrafts(ctx context.Context, actor uint, query DraftListQuery) (DraftPage, error) {
	if err := repository.validate(ctx, actor); err != nil {
		return DraftPage{}, err
	}
	if query.Limit < 1 || query.Limit > maxDraftPageSize {
		return DraftPage{}, invalidDraft("page limit must be between 1 and 100")
	}

	dbQuery := buildDraftListQuery(repository.db.WithContext(ctx), actor, query)

	var records []draftSummaryRecord
	if err := dbQuery.Scan(&records).Error; err != nil {
		return DraftPage{}, fmt.Errorf("report draft: list definitions: %w", err)
	}
	page := DraftPage{Items: make([]DraftSummary, 0, min(query.Limit, len(records)))}
	if len(records) > query.Limit {
		page.HasMore = true
		records = records[:query.Limit]
	}
	for _, record := range records {
		page.Items = append(page.Items, DraftSummary{
			Definition: record.ReportDefinition, LockVersion: record.LockVersion, IsOwner: record.IsOwner,
		})
	}
	if len(page.Items) > 0 {
		page.NextAfterID = page.Items[len(page.Items)-1].Definition.ID
	}
	return page, nil
}

func buildDraftListQuery(db *gorm.DB, actor uint, query DraftListQuery) *gorm.DB {
	dbQuery := db.Table("report_definitions AS definitions").
		Select(`definitions.*,
			CASE WHEN definitions.owner_user_id = ? THEN draft_versions.version_number ELSE 0 END AS lock_version,
			definitions.owner_user_id = ? AS is_owner`, actor, actor).
		Joins("LEFT JOIN report_versions AS draft_versions ON draft_versions.id = definitions.current_draft_version_id AND draft_versions.definition_id = definitions.id AND draft_versions.status = ?", model.ReportVersionStatusDraft).
		Joins("LEFT JOIN report_versions AS published_versions ON published_versions.id = definitions.current_published_version_id AND published_versions.definition_id = definitions.id AND published_versions.status = ?", model.ReportVersionStatusPublished)
	action := query.Action
	if !validReportAction(action) {
		action = ReportActionQuery
	}
	publishedArguments := []interface{}{
		model.ReportDefinitionStatusActive,
		action, "USER", actor, "ROLE", model.RoleStatusActive, actor,
		action, "USER", actor, "ROLE", model.RoleStatusActive, actor,
	}
	if query.PublishedOnly {
		dbQuery = dbQuery.Where(publishedReportAccessPredicate, publishedArguments...)
		if additionalAction := query.AdditionalAction; validReportAction(additionalAction) && additionalAction != action {
			additionalArguments := []interface{}{
				model.ReportDefinitionStatusActive,
				additionalAction, "USER", actor, "ROLE", model.RoleStatusActive, actor,
				additionalAction, "USER", actor, "ROLE", model.RoleStatusActive, actor,
			}
			dbQuery = dbQuery.Where(publishedReportAccessPredicate, additionalArguments...)
		}
	} else {
		arguments := []interface{}{actor, []string{model.ReportDefinitionStatusDraft, model.ReportDefinitionStatusActive}}
		arguments = append(arguments, publishedArguments...)
		dbQuery = dbQuery.Where(`(
			definitions.owner_user_id = ?
			AND definitions.status IN ?
			AND draft_versions.id IS NOT NULL
		) OR (`+publishedReportAccessPredicate+`)`, arguments...)
	}
	if query.AfterID > 0 {
		dbQuery = dbQuery.Where("definitions.id > ?", query.AfterID)
	}
	if category := strings.TrimSpace(query.Category); category != "" {
		dbQuery = dbQuery.Where("definitions.category = ?", category)
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		like := "%" + escapeLike(search) + "%"
		dbQuery = dbQuery.Where("(definitions.code LIKE ? ESCAPE '\\\\' OR definitions.name LIKE ? ESCAPE '\\\\')", like, like)
	}

	return dbQuery.Order("definitions.id ASC").Limit(query.Limit + 1)
}

const publishedReportAccessPredicate = `
	definitions.status = ?
	AND published_versions.id IS NOT NULL
	AND (
				EXISTS (
					SELECT 1
					FROM report_category_access AS category_access
					JOIN report_category_grants AS category_grants ON category_grants.category_access_id = category_access.id
					WHERE category_access.category = definitions.category
						AND JSON_CONTAINS(category_grants.actions_json, JSON_QUOTE(?))
						AND (
							(category_grants.subject_type = ? AND category_grants.subject_id = ?)
							OR (
								category_grants.subject_type = ?
								AND EXISTS (
									SELECT 1
									FROM user_roles AS category_memberships
									JOIN roles AS category_roles ON category_roles.id = category_memberships.role_id AND category_roles.status = ?
									WHERE category_memberships.user_id = ? AND category_memberships.role_id = category_grants.subject_id
								)
							)
						)
				)
				OR (
					NOT EXISTS (
						SELECT 1 FROM report_category_access AS configured_category
						WHERE configured_category.category = definitions.category
					)
					AND EXISTS (
						SELECT 1
						FROM report_grants AS grants
						WHERE grants.definition_id = definitions.id
							AND grants.version_id = published_versions.id
							AND JSON_CONTAINS(grants.actions_json, JSON_QUOTE(?))
							AND (
								(grants.subject_type = ? AND grants.subject_id = ?)
								OR (
									grants.subject_type = ?
									AND EXISTS (
										SELECT 1
										FROM user_roles AS memberships
										JOIN roles ON roles.id = memberships.role_id AND roles.status = ?
										WHERE memberships.user_id = ? AND memberships.role_id = grants.subject_id
									)
								)
							)
					)
				)
			)`

func (repository *Repository) UpdateDraft(
	ctx context.Context,
	ownerUserID, definitionID uint,
	expectedLockVersion uint64,
	draft *Draft,
) error {
	if err := repository.validate(ctx, ownerUserID); err != nil {
		return err
	}
	if err := validateExistingDraft(definitionID, expectedLockVersion, draft); err != nil {
		return err
	}

	err := repository.transact(ctx, repository.db, func(tx *gorm.DB) error {
		current, err := repository.lockDefinition(ctx, tx, ownerUserID, definitionID)
		if err != nil {
			return err
		}
		version, err := repository.lockVersion(ctx, tx, definitionID, current.CurrentDraftVersionID)
		if err != nil {
			return err
		}
		if version.VersionNumber != expectedLockVersion {
			return ErrDraftVersionConflict
		}
		if draft.Definition.OwnerUserID != 0 && draft.Definition.OwnerUserID != ownerUserID {
			return invalidDraft("definition owner cannot be changed")
		}
		if err := repository.validateReferences(ctx, tx, draft.Definition.DatasourceID, draft.Definition.Category, draft.Grants); err != nil {
			return err
		}

		nextVersion := draft.Version
		nextVersion.ID = 0
		nextVersion.DefinitionID = definitionID
		nextVersion.VersionNumber = expectedLockVersion + 1
		nextVersion.Status = model.ReportVersionStatusDraft
		nextVersion.PublishedBy = 0
		nextVersion.PublishedAt = nil
		nextVersion.WeatherTimestamps = model.WeatherTimestamps{}
		if nextVersion.CreatedBy == 0 {
			nextVersion.CreatedBy = draft.Definition.UpdatedBy
		}
		nextRecord := newVersionRecord(nextVersion)
		if err := tx.WithContext(ctx).Create(&nextRecord).Error; err != nil {
			return fmt.Errorf("report draft: create next version: %w", err)
		}

		result := definitionScope(tx.WithContext(ctx), ownerUserID).
			Where("id = ? AND current_draft_version_id = ?", definitionID, version.ID).
			Updates(definitionUpdates(draft.Definition, nextRecord.ID))
		if result.Error != nil {
			return fmt.Errorf("report draft: update definition: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}

		if err := replaceCollections(ctx, tx, ownerUserID, definitionID, nextRecord.ID,
			draft.Parameters, draft.Columns, draft.Grants); err != nil {
			return err
		}
		if err := finalizeReportMutation(ctx, tx, newDraftAudit("REPORT_DRAFT_UPDATE", ownerUserID, definitionID, nextRecord.VersionNumber, draft), repository.writeAudit); err != nil {
			return err
		}

		draft.Definition.ID = definitionID
		draft.Definition.OwnerUserID = ownerUserID
		draft.Definition.Status = current.Status
		draft.Definition.CreatedAt = current.CreatedAt
		draft.Definition.CurrentDraftVersionID = nextRecord.ID
		draft.Definition.CurrentPublishedVersionID = current.CurrentPublishedVersionID
		draft.Version.ID = nextRecord.ID
		draft.Version.DefinitionID = definitionID
		draft.Version.VersionNumber = expectedLockVersion + 1
		draft.Version.Status = model.ReportVersionStatusDraft
		draft.LockVersion = expectedLockVersion + 1
		return nil
	})
	return err
}

func (repository *Repository) SaveDraftCollections(
	ctx context.Context,
	ownerUserID, definitionID uint,
	expectedLockVersion uint64,
	parameters []model.ReportParameter,
	columns []model.ReportColumn,
	grants []model.ReportGrant,
) (uint64, error) {
	if err := repository.validate(ctx, ownerUserID); err != nil {
		return 0, err
	}
	if definitionID == 0 || expectedLockVersion == 0 {
		return 0, invalidDraft("definition id and lock version are required")
	}
	if err := validateCollections(parameters, columns, grants); err != nil {
		return 0, err
	}

	nextLockVersion := expectedLockVersion + 1
	err := repository.transact(ctx, repository.db, func(tx *gorm.DB) error {
		definition, err := repository.lockDefinition(ctx, tx, ownerUserID, definitionID)
		if err != nil {
			return err
		}
		version, err := repository.lockVersion(ctx, tx, definitionID, definition.CurrentDraftVersionID)
		if err != nil {
			return err
		}
		if version.VersionNumber != expectedLockVersion {
			return ErrDraftVersionConflict
		}
		if err := repository.validateReferences(ctx, tx, definition.DatasourceID, definition.Category, grants); err != nil {
			return err
		}
		nextVersion := version.ReportVersion
		nextVersion.ID = 0
		nextVersion.VersionNumber = nextLockVersion
		nextVersion.PublishedBy = 0
		nextVersion.PublishedAt = nil
		nextVersion.WeatherTimestamps = model.WeatherTimestamps{}
		nextRecord := newVersionRecord(nextVersion)
		if err := tx.WithContext(ctx).Create(&nextRecord).Error; err != nil {
			return fmt.Errorf("report draft: create next collection version: %w", err)
		}
		result := definitionScope(tx.WithContext(ctx), ownerUserID).
			Where("id = ? AND current_draft_version_id = ?", definitionID, version.ID).
			Updates(map[string]interface{}{"current_draft_version_id": nextRecord.ID})
		if result.Error != nil {
			return fmt.Errorf("report draft: switch collection version: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		if err := replaceCollections(ctx, tx, ownerUserID, definitionID, nextRecord.ID, parameters, columns, grants); err != nil {
			return err
		}
		return finalizeReportMutation(ctx, tx, newCollectionAudit(ownerUserID, definitionID, nextRecord.VersionNumber, parameters, columns, grants), repository.writeAudit)
	})
	if err != nil {
		return 0, err
	}
	return nextLockVersion, nil
}

func (repository *Repository) validate(ctx context.Context, ownerUserID uint) error {
	if repository == nil || repository.db == nil || repository.transact == nil || repository.validateReferences == nil ||
		repository.lockDefinition == nil || repository.lockVersion == nil || repository.lockPublishedVersion == nil || repository.lockPublicationSource == nil || repository.writeAudit == nil || repository.loadCollections == nil ||
		repository.publishVersion == nil || repository.createVersion == nil || repository.copyCollections == nil || repository.switchDefinition == nil ||
		ctx == nil || ownerUserID == 0 {
		return invalidDraft("repository, context and owner scope are required")
	}
	return nil
}

func lockDraftDefinition(ctx context.Context, tx *gorm.DB, ownerUserID, definitionID uint) (*definitionRecord, error) {
	var definition definitionRecord
	err := definitionScope(tx.WithContext(ctx), ownerUserID).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", definitionID).
		First(&definition).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDraftVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("report draft: lock definition: %w", err)
	}
	return &definition, nil
}

func lockDraftVersion(ctx context.Context, tx *gorm.DB, definitionID, versionID uint) (*versionRecord, error) {
	if versionID == 0 {
		return nil, ErrDraftVersionConflict
	}
	var version versionRecord
	err := versionScope(tx.WithContext(ctx)).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND definition_id = ?", versionID, definitionID).
		First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDraftVersionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("report draft: lock version: %w", err)
	}
	return &version, nil
}

func lockPublishedReportVersion(ctx context.Context, tx *gorm.DB, definitionID, versionID uint) (*versionRecord, error) {
	if versionID == 0 {
		return nil, ErrDraftNotFound
	}
	var version versionRecord
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND definition_id = ? AND status = ?", versionID, definitionID, model.ReportVersionStatusPublished).Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDraftNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report version: lock activation target: %w", err)
	}
	return &version, nil
}

func definitionUpdates(definition model.ReportDefinition, currentDraftVersionID uint) map[string]interface{} {
	return map[string]interface{}{
		"code": definition.Code, "name": definition.Name, "category": definition.Category,
		"description": definition.Description, "datasource_id": definition.DatasourceID,
		"updated_by": definition.UpdatedBy, "current_draft_version_id": currentDraftVersionID,
	}
}

func definitionScope(db *gorm.DB, ownerUserID uint) *gorm.DB {
	return db.Model(&definitionRecord{}).Where("owner_user_id = ? AND status IN ?", ownerUserID,
		[]string{model.ReportDefinitionStatusDraft, model.ReportDefinitionStatusActive})
}

func versionScope(db *gorm.DB) *gorm.DB {
	return db.Model(&versionRecord{}).Where("status = ?", model.ReportVersionStatusDraft)
}

func invalidDraft(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidDraft, message)
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}
