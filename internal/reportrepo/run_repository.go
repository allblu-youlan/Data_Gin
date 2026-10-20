package reportrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormLogger "gorm.io/gorm/logger"

	"gin-biz-web-api/model"
)

var (
	ErrPublishedReportNotFound = errors.New("report run: published report not found")
	ErrReportActionDenied      = errors.New("report run: action denied")
	ErrInvalidRun              = errors.New("report run: invalid input")
	ErrReportRunBusy           = errors.New("report run: report snapshot is busy")
)

const (
	ReportActionQuery  = "QUERY"
	ReportActionExport = "EXPORT"
)

type PublishedReport struct {
	Definition               model.ReportDefinition
	Version                  model.ReportVersion
	Parameters               []model.ReportParameter
	Columns                  []model.ReportColumn
	Grants                   []model.ReportGrant
	CategoryAccessConfigured bool
	authority                runAuthority
}

type CreateRunCommand struct {
	Run    model.ReportRun
	Outbox model.AsyncJobOutbox
}

func (repository *Repository) FindPublishedReport(ctx context.Context, actor, definitionID uint, action string) (*PublishedReport, error) {
	if repository == nil || repository.db == nil || repository.loadPublished == nil || repository.validateRunSource == nil || ctx == nil || actor == 0 || definitionID == 0 || !validReportAction(action) {
		return nil, invalidRun("repository, actor, report and action are required")
	}
	published, err := repository.loadPublished(ctx, repository.db, actor, definitionID, action, false)
	if err != nil {
		return nil, err
	}
	if err := repository.validateRunSource(ctx, repository.db, published.Version.DatasourceID); err != nil {
		return nil, err
	}
	return published, nil
}

func (repository *Repository) CreateRun(ctx context.Context, actor, definitionID uint, command *CreateRunCommand) error {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || definitionID == 0 || command == nil ||
		repository.transact == nil || repository.loadPublished == nil || repository.createReportRun == nil ||
		repository.createRunOutbox == nil || repository.writeAudit == nil || repository.validateRunSource == nil ||
		repository.prepareRunSlot == nil ||
		!validNewRun(command.Run, actor, definitionID) || !validRunOutbox(command.Outbox, command.Run.RunUUID) {
		return invalidRun("repository, actor, report, run and outbox are required")
	}
	run := command.Run
	outbox := command.Outbox
	err := repository.transact(ctx, repository.db, func(tx *gorm.DB) error {
		published, err := repository.loadPublished(ctx, tx, actor, definitionID, ReportActionQuery, true)
		if err != nil {
			return err
		}
		if err := repository.validateRunSource(ctx, tx, published.Version.DatasourceID); err != nil {
			return err
		}
		if published.Version.ID != command.Run.VersionID || published.Version.ContractHash != command.Run.ContractHash ||
			published.Version.ProcedureSignatureHash != command.Run.ProcedureSignatureHash || published.Version.ResultSchemaHash != command.Run.ResultSchemaHash {
			return ErrDraftVersionConflict
		}
		var slot reportRunSlotPreparation
		if published.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot {
			slot, err = prepareSelectRunSlot(ctx, tx, definitionID, actor, run.ExecutionFingerprint, outbox.AvailableAt)
		} else {
			slot, err = repository.prepareRunSlot(ctx, tx, definitionID, actor, run.ExecutionFingerprint, outbox.AvailableAt)
		}
		if err != nil {
			return err
		}
		if slot.ExistingRun != nil {
			run = *slot.ExistingRun
			return nil
		}
		exportAuthority, exportAllowed, err := actorCanRunReport(ctx, tx, actor, published.authorizationOwner(), ReportActionExport, published.Grants)
		if err != nil {
			return err
		}
		permissionSnapshot, err := encodeRunPermissionCapabilities(actor, published.authority, exportAuthority, exportAllowed)
		if err != nil {
			return err
		}
		presentationSnapshot, err := encodeRunPresentationSnapshot(published.Columns)
		if err != nil {
			return err
		}
		run.PermissionSnapshotJSON = permissionSnapshot
		run.PresentationSnapshotJSON = presentationSnapshot
		if err := repository.createReportRun(ctx, tx, &run); err != nil {
			return err
		}
		outbox.PayloadJSON = model.JSONText(fmt.Sprintf(`{"run_id":%d}`, run.ID))
		if err := repository.createRunOutbox(ctx, tx, &outbox); err != nil {
			return err
		}
		detail, err := json.Marshal(map[string]interface{}{
			"runId": run.ID, "runUuid": run.RunUUID, "versionId": run.VersionID,
		})
		if err != nil {
			return fmt.Errorf("report run: encode audit: %w", err)
		}
		audit := model.ReportAudit{
			ActorUserID: actor, Action: "REPORT_RUN_CREATE", TargetType: "REPORT_RUN", TargetID: run.ID,
			RequestID: uuid.NewString(), DetailJSON: model.JSONText(detail),
		}
		if err := repository.writeAudit(ctx, tx, audit); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	command.Run = run
	command.Outbox = outbox
	return nil
}

func prepareReportRunSlot(ctx context.Context, tx *gorm.DB, definitionID, actor uint, executionFingerprint string, now time.Time) (reportRunSlotPreparation, error) {
	var preparation reportRunSlotPreparation
	if ctx == nil || tx == nil || definitionID == 0 || actor == 0 || len(executionFingerprint) != 64 || now.IsZero() {
		return preparation, ErrInvalidRun
	}
	now = now.UTC().Truncate(time.Millisecond)
	activeStatuses := []string{
		model.ReportRunStatusQueued, model.ReportRunStatusRunning, model.ReportRunStatusCancelRequested,
		model.ReportRunStatusUnknown, model.ReportRunStatusReconciling, model.ReportRunStatusExporting,
		model.ReportRunStatusResultPurging,
	}
	var activeRuns []model.ReportRun
	if err := reportRunSlotScope(tx.WithContext(ctx), definitionID, actor).Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "run_uuid", "definition_id", "version_id", "requested_by", "status", "execution_fingerprint", "created_at").
		Where("status IN ?", activeStatuses).Order("id DESC").Find(&activeRuns).Error; err != nil {
		return preparation, fmt.Errorf("report run: lock active snapshot slot: %w", err)
	}
	if len(activeRuns) > 0 {
		if existing := reusableActiveReportRun(activeRuns, executionFingerprint); existing != nil {
			preparation.ExistingRun = existing
			return preparation, nil
		}
	}
	var snapshots []model.ReportRun
	if err := reportRunSlotScope(tx.WithContext(ctx), definitionID, actor).Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "run_uuid", "definition_id", "version_id", "requested_by", "status", "execution_fingerprint", "created_at").
		Where("status = ? AND result_purged_at IS NULL", model.ReportRunStatusSucceeded).
		Order("id DESC").Find(&snapshots).Error; err != nil {
		return preparation, fmt.Errorf("report run: lock current snapshot: %w", err)
	}
	if len(snapshots) == 0 {
		return preparation, nil
	}
	if existing := reusableReportRun(snapshots, executionFingerprint); existing != nil {
		preparation.ExistingRun = existing
		return preparation, nil
	}
	runIDs := make([]uint, 0, len(snapshots))
	for _, snapshot := range snapshots {
		runIDs = append(runIDs, snapshot.ID)
	}
	result := scheduleReplacedSnapshotsForCleanup(tx.WithContext(ctx), runIDs, now)
	if result.Error != nil {
		return preparation, fmt.Errorf("report run: schedule previous snapshot cleanup: %w", result.Error)
	}
	return preparation, nil
}

func scheduleReplacedSnapshotsForCleanup(tx *gorm.DB, runIDs []uint, now time.Time) *gorm.DB {
	now = now.UTC().Truncate(time.Millisecond)
	activeExport := tx.Table("report_exports AS active_exports").Select("1").
		Where("active_exports.run_id = report_runs.id AND active_exports.purged_at IS NULL").
		Where("active_exports.status IN ?", []string{
			model.ReportExportStatusPending, model.ReportExportStatusRunning, model.ReportExportStatusReady,
		})
	return tx.Model(&model.ReportRun{}).
		Where("id IN ? AND status = ? AND result_purged_at IS NULL", runIDs, model.ReportRunStatusSucceeded).
		Where("data_scope_hash IS NULL OR data_scope_hash = ?", "").
		Where("NOT EXISTS (?)", activeExport).
		Updates(map[string]interface{}{"result_expires_at": now, "updated_at": now})
}

func reusableActiveReportRun(runs []model.ReportRun, executionFingerprint string) *model.ReportRun {
	for index := range runs {
		if (runs[index].Status == model.ReportRunStatusQueued || runs[index].Status == model.ReportRunStatusRunning) &&
			runs[index].ExecutionFingerprint == executionFingerprint {
			return &runs[index]
		}
	}
	return nil
}

func reusableReportRun(runs []model.ReportRun, executionFingerprint string) *model.ReportRun {
	for index := range runs {
		if runs[index].ExecutionFingerprint == executionFingerprint {
			return &runs[index]
		}
	}
	return nil
}

func reportRunSlotScope(db *gorm.DB, definitionID, actor uint) *gorm.DB {
	return db.Model(&model.ReportRun{}).Where("definition_id = ? AND requested_by = ?", definitionID, actor)
}

func writeReportRun(ctx context.Context, tx *gorm.DB, run *model.ReportRun) error {
	// Report parameters and their ciphertext must never pass through GORM's
	// interpolated SQL logger. Operational state changes remain logged elsewhere.
	if err := tx.Session(&gorm.Session{Logger: gormLogger.Discard}).WithContext(ctx).Create(run).Error; err != nil {
		return fmt.Errorf("report run: create control row: %w", err)
	}
	return nil
}

func writeReportRunOutbox(ctx context.Context, tx *gorm.DB, outbox *model.AsyncJobOutbox) error {
	if err := tx.WithContext(ctx).Create(outbox).Error; err != nil {
		return fmt.Errorf("report run: create outbox: %w", err)
	}
	return nil
}

type runGrantSnapshot struct {
	SubjectType string          `json:"subjectType"`
	SubjectID   uint            `json:"subjectId"`
	Actions     json.RawMessage `json:"actions"`
}

type runPermissionSnapshot struct {
	Actor     uint               `json:"actor"`
	Action    string             `json:"action"`
	Actions   []string           `json:"actions,omitempty"`
	GrantedBy string             `json:"grantedBy"`
	Grants    []runGrantSnapshot `json:"grants"`
}

type runAuthority struct {
	Source string
	Grants []model.ReportGrant
}

type runPresentationColumn struct {
	FieldID           string          `json:"fieldId"`
	LogicalCode       string          `json:"logicalCode"`
	DatabaseColumn    string          `json:"databaseColumn"`
	SourceOracleType  string          `json:"sourceOracleType"`
	Precision         *int            `json:"precision,omitempty"`
	Scale             *int            `json:"scale,omitempty"`
	Nullable          bool            `json:"nullable"`
	ValueType         string          `json:"valueType"`
	PreviewHeader     string          `json:"previewHeader"`
	ExcelHeader       string          `json:"excelHeader"`
	DisplayOrder      int             `json:"displayOrder"`
	ExportOrder       int             `json:"exportOrder"`
	PreviewVisible    bool            `json:"previewVisible"`
	ExportVisible     bool            `json:"exportVisible"`
	Filterable        bool            `json:"filterable"`
	Sortable          bool            `json:"sortable"`
	ExportAllowed     bool            `json:"exportAllowed"`
	AllowedOperators  json.RawMessage `json:"allowedOperators,omitempty"`
	Format            json.RawMessage `json:"format,omitempty"`
	DictionaryVersion json.RawMessage `json:"dictionaryVersion,omitempty"`
	MaskingPolicy     json.RawMessage `json:"maskingPolicy,omitempty"`
	ExcelWidth        float64         `json:"excelWidth"`
	NullDisplay       string          `json:"nullDisplay"`
}

func encodeRunPermissionSnapshot(actor uint, action string, authority runAuthority) (model.JSONText, error) {
	items := make([]runGrantSnapshot, 0, len(authority.Grants))
	for _, grant := range authority.Grants {
		items = append(items, runGrantSnapshot{SubjectType: grant.SubjectType, SubjectID: grant.SubjectID, Actions: json.RawMessage(grant.ActionsJSON)})
	}
	encoded, err := json.Marshal(runPermissionSnapshot{Actor: actor, Action: action, GrantedBy: authority.Source, Grants: items})
	if err != nil {
		return "", fmt.Errorf("report run: encode permission snapshot: %w", err)
	}
	return model.JSONText(encoded), nil
}

func encodeRunPermissionCapabilities(actor uint, queryAuthority, exportAuthority runAuthority, exportAllowed bool) (model.JSONText, error) {
	actions := []string{ReportActionQuery}
	grantedBy := queryAuthority.Source
	grants := append([]model.ReportGrant(nil), queryAuthority.Grants...)
	if exportAllowed {
		actions = append(actions, ReportActionExport)
		if exportAuthority.Source != "" && exportAuthority.Source != grantedBy {
			grantedBy += "+" + exportAuthority.Source
		}
		for _, candidate := range exportAuthority.Grants {
			duplicate := false
			for _, existing := range grants {
				if existing.SubjectType == candidate.SubjectType && existing.SubjectID == candidate.SubjectID {
					duplicate = true
					break
				}
			}
			if !duplicate {
				grants = append(grants, candidate)
			}
		}
	}
	items := make([]runGrantSnapshot, 0, len(grants))
	for _, grant := range grants {
		items = append(items, runGrantSnapshot{SubjectType: grant.SubjectType, SubjectID: grant.SubjectID, Actions: json.RawMessage(grant.ActionsJSON)})
	}
	encoded, err := json.Marshal(runPermissionSnapshot{Actor: actor, Action: ReportActionQuery, Actions: actions, GrantedBy: grantedBy, Grants: items})
	if err != nil {
		return "", fmt.Errorf("report run: encode permission capabilities: %w", err)
	}
	return model.JSONText(encoded), nil
}

func frozenRunAllowsAction(raw model.JSONText, actor uint, action string) bool {
	var snapshot runPermissionSnapshot
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || snapshot.Actor != actor || !validReportAction(action) {
		return false
	}
	var trailing interface{}
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return false
	}
	if len(snapshot.Actions) == 0 {
		return false
	}
	allowed := false
	for _, candidate := range snapshot.Actions {
		candidate = strings.ToUpper(strings.TrimSpace(candidate))
		if !validReportAction(candidate) {
			return false
		}
		if candidate == action {
			allowed = true
		}
	}
	return allowed
}

func frozenLegacyRunAllowsLiveAuthorization(raw model.JSONText, actor uint) bool {
	var snapshot runPermissionSnapshot
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || snapshot.Actor != actor || len(snapshot.Actions) != 0 ||
		strings.ToUpper(strings.TrimSpace(snapshot.Action)) != ReportActionQuery ||
		strings.TrimSpace(snapshot.GrantedBy) == "" {
		return false
	}
	var trailing interface{}
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

func encodeRunPresentationSnapshot(columns []model.ReportColumn) (model.JSONText, error) {
	items := make([]runPresentationColumn, 0, len(columns))
	for _, column := range columns {
		items = append(items, runPresentationColumn{
			FieldID: column.FieldID, LogicalCode: column.LogicalCode, DatabaseColumn: column.DatabaseColumn,
			SourceOracleType: column.SourceOracleType, Precision: column.PrecisionValue, Scale: column.ScaleValue,
			Nullable: column.Nullable, ValueType: column.ValueType, PreviewHeader: column.PreviewHeader,
			ExcelHeader: column.ExcelHeader, DisplayOrder: column.DisplayOrder, ExportOrder: column.ExportOrder,
			PreviewVisible: column.PreviewVisible, ExportVisible: column.ExportVisible, Filterable: column.Filterable,
			Sortable: column.Sortable, ExportAllowed: column.ExportAllowed,
			AllowedOperators: json.RawMessage(column.AllowedOperatorsJSON), Format: json.RawMessage(column.FormatJSON),
			DictionaryVersion: json.RawMessage(column.DictionaryVersionJSON), MaskingPolicy: json.RawMessage(column.MaskingPolicyJSON),
			ExcelWidth: column.ExcelWidth, NullDisplay: column.NullDisplay,
		})
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("report run: encode presentation snapshot: %w", err)
	}
	return model.JSONText(encoded), nil
}

func loadPublishedReport(ctx context.Context, db *gorm.DB, actor, definitionID uint, action string, lock bool) (*PublishedReport, error) {
	query := db.WithContext(ctx).Model(&definitionRecord{}).
		Where("id = ? AND status = ? AND current_published_version_id <> 0", definitionID, model.ReportDefinitionStatusActive)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var definition definitionRecord
	if err := query.First(&definition).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPublishedReportNotFound
	} else if err != nil {
		return nil, fmt.Errorf("report run: find published definition: %w", err)
	}
	var version versionRecord
	versionQuery := db.WithContext(ctx).Model(&versionRecord{}).
		Where("id = ? AND definition_id = ? AND status = ?", definition.CurrentPublishedVersionID, definitionID, model.ReportVersionStatusPublished)
	if lock {
		versionQuery = versionQuery.Clauses(clause.Locking{Strength: "SHARE"})
	}
	if err := versionQuery.First(&version).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPublishedReportNotFound
	} else if err != nil {
		return nil, fmt.Errorf("report run: find published version: %w", err)
	}
	published := &PublishedReport{Definition: definition.ReportDefinition, Version: version.ReportVersion}
	draft := &Draft{}
	if err := loadCollections(ctx, db, actor, definitionID, version.ID, draft); err != nil {
		return nil, err
	}
	published.Parameters, published.Columns, published.Grants = draft.Parameters, draft.Columns, draft.Grants
	categoryConfigured, categoryGrants, err := loadCategoryReportGrants(ctx, db, definition.Category)
	if err != nil {
		return nil, err
	}
	if categoryConfigured {
		published.Grants = categoryGrants
		published.CategoryAccessConfigured = true
	}
	authority, allowed, err := actorCanRunReport(ctx, db, actor, published.authorizationOwner(), action, published.Grants)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrReportActionDenied
	}
	published.authority = authority
	return published, nil
}

func (published *PublishedReport) authorizationOwner() uint {
	if published == nil || published.CategoryAccessConfigured {
		return 0
	}
	return published.Definition.OwnerUserID
}

func requireEnabledOracleDatasource(ctx context.Context, db *gorm.DB, datasourceID uint) error {
	var datasource model.ReportDatasource
	if err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND enabled = ? AND driver = ?", datasourceID, true, model.ReportDatasourceDriverOracle).
		Select("id").First(&datasource).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrPublishedReportNotFound
	} else if err != nil {
		return fmt.Errorf("report run: validate datasource: %w", err)
	}
	return nil
}

func actorCanRunReport(ctx context.Context, db *gorm.DB, actor, owner uint, action string, grants []model.ReportGrant) (runAuthority, bool, error) {
	if actor == owner {
		return runAuthority{Source: "OWNER", Grants: []model.ReportGrant{}}, true, nil
	}
	roleIDs := make([]uint, 0, len(grants))
	for _, grant := range grants {
		if !grantAllowsAction(grant, action) {
			continue
		}
		if grant.SubjectType == "USER" && grant.SubjectID == actor {
			return runAuthority{Source: "USER", Grants: []model.ReportGrant{grant}}, true, nil
		}
		if grant.SubjectType == "ROLE" {
			roleIDs = append(roleIDs, grant.SubjectID)
		}
	}
	if len(roleIDs) == 0 {
		return runAuthority{}, false, nil
	}
	var activeRoleIDs []uint
	err := db.WithContext(ctx).Table("user_roles").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id = ? AND user_roles.role_id IN ? AND roles.status = ?", actor, roleIDs, model.RoleStatusActive).
		Pluck("user_roles.role_id", &activeRoleIDs).Error
	if err != nil {
		return runAuthority{}, false, fmt.Errorf("report run: authorize roles: %w", err)
	}
	matched := matchRoleGrants(grants, activeRoleIDs, action)
	return runAuthority{Source: "ROLE", Grants: matched}, len(matched) > 0, nil
}

func matchRoleGrants(grants []model.ReportGrant, activeRoleIDs []uint, action string) []model.ReportGrant {
	active := make(map[uint]struct{}, len(activeRoleIDs))
	for _, roleID := range activeRoleIDs {
		active[roleID] = struct{}{}
	}
	matched := make([]model.ReportGrant, 0, len(activeRoleIDs))
	for _, grant := range grants {
		if grant.SubjectType != "ROLE" || !grantAllowsAction(grant, action) {
			continue
		}
		if _, exists := active[grant.SubjectID]; exists {
			matched = append(matched, grant)
		}
	}
	return matched
}

func grantAllowsAction(grant model.ReportGrant, action string) bool {
	var actions []string
	if json.Unmarshal([]byte(grant.ActionsJSON), &actions) != nil {
		return false
	}
	for _, candidate := range actions {
		if strings.EqualFold(strings.TrimSpace(candidate), action) {
			return true
		}
	}
	return false
}

func validNewRun(run model.ReportRun, actor, definitionID uint) bool {
	return run.ID == 0 && uuid.Validate(run.RunUUID) == nil && run.DefinitionID == definitionID && run.VersionID != 0 &&
		run.RequestedBy == actor && run.Status == model.ReportRunStatusQueued && len(run.ExecutionFingerprint) == 64 &&
		json.Valid([]byte(run.NormalizedParametersJSON)) && len(run.ContractHash) == 64 &&
		len(run.ProcedureSignatureHash) == 64 && len(run.ResultSchemaHash) == 64 &&
		((run.SensitiveParametersCipher == "" && run.SensitiveParametersKeyVersion == "") ||
			(run.SensitiveParametersCipher != "" && run.SensitiveParametersKeyVersion != ""))
}

func validRunOutbox(outbox model.AsyncJobOutbox, runUUID string) bool {
	var payload struct {
		RunID uint `json:"run_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(outbox.PayloadJSON)))
	decoder.DisallowUnknownFields()
	payloadValid := decoder.Decode(&payload) == nil
	if payloadValid {
		var trailing interface{}
		payloadValid = errors.Is(decoder.Decode(&trailing), io.EOF)
	}
	return outbox.ID == 0 && outbox.TaskKey == "report:run:"+runUUID && outbox.TaskType == "report:run" &&
		outbox.QueueName == "report" && outbox.AvailableAt.IsZero() == false && outbox.PublishedAt == nil &&
		payloadValid && payload.RunID == 0
}

func validReportAction(action string) bool {
	return action == ReportActionQuery || action == ReportActionExport
}

func invalidRun(message string) error { return fmt.Errorf("%w: %s", ErrInvalidRun, message) }

func NewReportRunOutbox(runUUID string, now time.Time) model.AsyncJobOutbox {
	return model.AsyncJobOutbox{
		TaskKey: "report:run:" + runUUID, TaskType: "report:run", PayloadJSON: model.JSONText(`{"run_id":0}`),
		QueueName: "report", AvailableAt: now.UTC(),
	}
}
