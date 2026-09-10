package reportrepo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportidentity"
	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestDefinitionScopeAlwaysFiltersOwnerAndDraftStatus(t *testing.T) {
	db := newDryRunDB(t)
	statement := definitionScope(db.WithContext(context.Background()), 17).
		Where("id = ?", 42).
		Find(&[]definitionRecord{})
	if statement.Error != nil {
		t.Fatalf("build scoped query: %v", statement.Error)
	}
	sqlText := statement.Statement.SQL.String()
	t.Logf("list SQL: %s", sqlText)
	for _, fragment := range []string{"owner_user_id = ?", "status IN", "id = ?"} {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("scope SQL %q does not contain %q", sqlText, fragment)
		}
	}
	if len(statement.Statement.Vars) < 2 || statement.Statement.Vars[0] != uint(17) {
		t.Fatalf("scope vars = %#v", statement.Statement.Vars)
	}
}

func TestVersionScopeAlwaysFiltersDraftStatus(t *testing.T) {
	db := newDryRunDB(t)
	statement := versionScope(db.WithContext(context.Background())).
		Where("definition_id = ?", 9).
		Find(&[]versionRecord{})
	if statement.Error != nil {
		t.Fatalf("build scoped query: %v", statement.Error)
	}
	sqlText := statement.Statement.SQL.String()
	if !strings.Contains(sqlText, "status = ?") {
		t.Fatalf("scope SQL = %q", sqlText)
	}
}

func TestRepositoryRejectsUnscopedOrStaleInputBeforeDatabase(t *testing.T) {
	repository := &Repository{}
	if _, err := repository.FindDraftByID(context.Background(), 0, 1); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("FindDraftByID() error = %v, want ErrInvalidDraft", err)
	}
	if _, err := repository.SaveDraftCollections(context.Background(), 1, 1, 1, nil, nil, nil); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("SaveDraftCollections() error = %v, want ErrInvalidDraft", err)
	}

	db := newDryRunDB(t)
	repository = New(db)
	draft := validDraft()
	draft.LockVersion = 4
	if err := repository.UpdateDraft(context.Background(), 1, 7, 3, draft); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("UpdateDraft() error = %v, want ErrInvalidDraft", err)
	}
}

func TestDeleteDraftRemovesUnpublishedConfigurationAtomically(t *testing.T) {
	db, transactionState := newTransactionDB(t)
	repository := New(db)
	repository.lockDefinition = func(context.Context, *gorm.DB, uint, uint) (*definitionRecord, error) {
		return &definitionRecord{ReportDefinition: model.ReportDefinition{
			BaseModel: model.BaseModel{ID: 7}, Code: "draft_report", OwnerUserID: 8,
			Status: model.ReportDefinitionStatusDraft, CurrentDraftVersionID: 11,
		}}, nil
	}
	repository.lockVersion = func(context.Context, *gorm.DB, uint, uint) (*versionRecord, error) {
		return &versionRecord{ReportVersion: model.ReportVersion{BaseModel: model.BaseModel{ID: 11}, DefinitionID: 7, VersionNumber: 4, Status: model.ReportVersionStatusDraft}}, nil
	}
	repository.writeAudit = func(_ context.Context, _ *gorm.DB, audit model.ReportAudit) error {
		if audit.Action != "REPORT_DRAFT_DELETE" || audit.ActorUserID != 8 || audit.TargetID != 7 || !strings.Contains(string(audit.DetailJSON), `"code":"draft_report"`) {
			t.Fatalf("delete audit = %#v", audit)
		}
		return nil
	}

	if err := repository.DeleteDraft(t.Context(), 8, 7, 4); err != nil {
		t.Fatalf("DeleteDraft() error = %v", err)
	}
	if transactionState.begins != 1 || transactionState.commits != 1 || transactionState.rollbacks != 0 {
		t.Fatalf("transaction state = %#v", transactionState)
	}
	statements := strings.Join(transactionState.execs, "\n")
	for _, fragment := range []string{
		"SELECT count(*) FROM `report_runs`", "DELETE FROM `report_parameters`", "DELETE FROM `report_columns`",
		"DELETE FROM `report_grants`", "DELETE FROM `report_result_table_bindings`", "DELETE FROM `report_versions`", "DELETE FROM `report_definitions`",
	} {
		if !strings.Contains(statements, fragment) {
			t.Fatalf("delete transaction is missing %q: %s", fragment, statements)
		}
	}
}

func TestDeleteDraftRejectsPublishedStaleOrExecutedTemplate(t *testing.T) {
	tests := []struct {
		name       string
		definition model.ReportDefinition
		version    uint64
		runCount   int64
		want       error
	}{
		{name: "published", definition: model.ReportDefinition{Status: model.ReportDefinitionStatusActive, CurrentDraftVersionID: 11, CurrentPublishedVersionID: 10}, version: 4, want: ErrDraftDeleteConflict},
		{name: "stale", definition: model.ReportDefinition{Status: model.ReportDefinitionStatusDraft, CurrentDraftVersionID: 11}, version: 5, want: ErrDraftVersionConflict},
		{name: "executed", definition: model.ReportDefinition{Status: model.ReportDefinitionStatusDraft, CurrentDraftVersionID: 11}, version: 4, runCount: 1, want: ErrDraftDeleteConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, transactionState := newTransactionDB(t)
			transactionState.queryCount = test.runCount
			repository := New(db)
			repository.lockDefinition = func(context.Context, *gorm.DB, uint, uint) (*definitionRecord, error) {
				definition := test.definition
				definition.ID = 7
				definition.OwnerUserID = 8
				return &definitionRecord{ReportDefinition: definition}, nil
			}
			repository.lockVersion = func(context.Context, *gorm.DB, uint, uint) (*versionRecord, error) {
				return &versionRecord{ReportVersion: model.ReportVersion{BaseModel: model.BaseModel{ID: 11}, DefinitionID: 7, VersionNumber: test.version, Status: model.ReportVersionStatusDraft}}, nil
			}
			if err := repository.DeleteDraft(t.Context(), 8, 7, 4); !errors.Is(err, test.want) {
				t.Fatalf("DeleteDraft() error = %v, want %v", err, test.want)
			}
			if transactionState.commits != 0 || transactionState.rollbacks != 1 || strings.Contains(strings.Join(transactionState.execs, "\n"), "DELETE FROM `report_definitions`") {
				t.Fatalf("transaction state = %#v", transactionState)
			}
		})
	}
}

func TestValidateCollectionsRejectsDuplicateStableKeys(t *testing.T) {
	tests := []struct {
		name       string
		parameters []model.ReportParameter
		columns    []model.ReportColumn
		grants     []model.ReportGrant
	}{
		{
			name: "parameter code",
			parameters: []model.ReportParameter{
				{ParameterCode: "runId", Position: 1},
				{ParameterCode: "runId", Position: 2},
			},
		},
		{
			name: "parameter position",
			parameters: []model.ReportParameter{
				{ParameterCode: "from", Position: 1},
				{ParameterCode: "to", Position: 1},
			},
		},
		{
			name: "column field id",
			columns: []model.ReportColumn{
				{LogicalCode: "a", FieldID: "field-1"},
				{LogicalCode: "b", FieldID: "field-1"},
			},
		},
		{
			name: "grant subject",
			grants: []model.ReportGrant{
				{SubjectType: "ROLE", SubjectID: 2},
				{SubjectType: "ROLE", SubjectID: 2},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateCollections(test.parameters, test.columns, test.grants); !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("validateCollections() error = %v, want ErrInvalidDraft", err)
			}
		})
	}
}

func TestValidateReferenceCountRejectsMissingOrDisabledReferences(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		actual    int64
		expected  int
		wantError bool
	}{
		{name: "enabled datasource", reference: "datasource", actual: 1, expected: 1},
		{name: "missing or disabled datasource", reference: "datasource", actual: 0, expected: 1, wantError: true},
		{name: "all active users", reference: "grant user", actual: 2, expected: 2},
		{name: "one missing or disabled user", reference: "grant user", actual: 1, expected: 2, wantError: true},
		{name: "all active roles", reference: "grant role", actual: 2, expected: 2},
		{name: "one missing or disabled role", reference: "grant role", actual: 1, expected: 2, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateReferenceCount(test.reference, test.actual, test.expected)
			if test.wantError && !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("validateReferenceCount() error = %v, want ErrInvalidDraft", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("validateReferenceCount() error = %v", err)
			}
		})
	}
}

func TestRepositoryReferenceValidationStopsDraftWrites(t *testing.T) {
	referenceError := invalidDraft("datasource or grant subject is unavailable")
	tests := []struct {
		name string
		run  func(*Repository) error
	}{
		{name: "create", run: func(repository *Repository) error {
			return repository.CreateDraft(context.Background(), 8, validDraft())
		}},
		{name: "update", run: func(repository *Repository) error {
			return repository.UpdateDraft(context.Background(), 8, 7, 1, validDraft())
		}},
		{name: "save collections", run: func(repository *Repository) error {
			_, err := repository.SaveDraftCollections(context.Background(), 8, 7, 1, nil, nil, nil)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			referenceCalls := 0
			repository := New(newDryRunDB(t))
			repository.transact = func(_ context.Context, db *gorm.DB, operation func(*gorm.DB) error) error { return operation(db) }
			repository.lockDefinition = func(context.Context, *gorm.DB, uint, uint) (*definitionRecord, error) {
				return &definitionRecord{ReportDefinition: model.ReportDefinition{BaseModel: model.BaseModel{ID: 7}, DatasourceID: 3, CurrentDraftVersionID: 11}}, nil
			}
			repository.lockVersion = func(context.Context, *gorm.DB, uint, uint) (*versionRecord, error) {
				return &versionRecord{ReportVersion: model.ReportVersion{BaseModel: model.BaseModel{ID: 11}, DefinitionID: 7, DatasourceID: 3, VersionNumber: 1, Status: model.ReportVersionStatusDraft}}, nil
			}
			repository.validateReferences = func(context.Context, *gorm.DB, uint, string, []model.ReportGrant) error {
				referenceCalls++
				return referenceError
			}

			if err := test.run(repository); !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("operation error = %v, want ErrInvalidDraft", err)
			}
			if referenceCalls != 1 {
				t.Fatalf("reference validation calls = %d, want 1", referenceCalls)
			}
		})
	}
}

func TestDraftAuditContainsOnlySafeMetadata(t *testing.T) {
	draft := validDraft()
	draft.Parameters = []model.ReportParameter{{
		ParameterCode: "secret", Position: 1, Sensitive: true,
		DefaultValueJSON: model.JSONText(`"must-not-appear"`),
	}}
	draft.Columns = []model.ReportColumn{{LogicalCode: "orderNo", FieldID: "field-1"}}
	draft.Grants = []model.ReportGrant{{SubjectType: "ROLE", SubjectID: 2}}

	audit := newDraftAudit("REPORT_DRAFT_UPDATE", 8, 7, 4, draft)
	if audit.ActorUserID != 8 || audit.TargetID != 7 || audit.TargetType != "REPORT_DEFINITION" || audit.Action != "REPORT_DRAFT_UPDATE" {
		t.Fatalf("audit identity = %#v", audit)
	}
	if len(audit.RequestID) != 36 {
		t.Fatalf("audit request id = %q", audit.RequestID)
	}
	if strings.Contains(string(audit.DetailJSON), "must-not-appear") || strings.Contains(string(audit.DetailJSON), "default") {
		t.Fatalf("audit leaked configuration: %s", audit.DetailJSON)
	}
	var detail reportDraftAuditDetail
	if err := json.Unmarshal([]byte(audit.DetailJSON), &detail); err != nil {
		t.Fatalf("decode audit detail: %v", err)
	}
	if detail.VersionNumber != 4 || detail.ParameterCount != 1 || detail.ColumnCount != 1 || detail.GrantCount != 1 {
		t.Fatalf("audit detail = %#v", detail)
	}
}

func TestFinalizeReportMutationPropagatesAuditFailure(t *testing.T) {
	auditError := errors.New("audit unavailable")
	auditCalls := 0
	err := finalizeReportMutation(context.Background(), newDryRunDB(t), model.ReportAudit{}, func(context.Context, *gorm.DB, model.ReportAudit) error {
		auditCalls++
		return auditError
	})
	if !errors.Is(err, auditError) {
		t.Fatalf("finalizeReportMutation() error = %v, want audit error", err)
	}
	if auditCalls != 1 {
		t.Fatalf("audit calls = %d, want 1", auditCalls)
	}
}

func TestValidPublicationRequiresCompleteImmutableContract(t *testing.T) {
	publication := Publication{
		CallTemplate:     "BEGIN REPORT.PKG.RUN; END;",
		CompiledSpecJSON: model.JSONText(`{"version":{}}`),
		ContractHash:     strings.Repeat("a", 64), ParameterSchemaHash: strings.Repeat("b", 64),
		ProcedureSignatureHash: strings.Repeat("c", 64), ResultSchemaHash: strings.Repeat("d", 64),
		PermissionHash: strings.Repeat("e", 64), ExportSchemaHash: strings.Repeat("f", 64),
		SchemaProbeToken: "11111111-1111-4111-8111-111111111111", SchemaValidatedAt: time.Now().UTC(),
		ConnectionFingerprint:         strings.Repeat("1", 64),
		ConnectionIdentitySource:      reportidentity.BindingIdentitySourceOracle,
		DatasourceSnapshotFingerprint: strings.Repeat("2", 64),
	}
	if !validPublication(publication) {
		t.Fatal("complete publication rejected")
	}
	publication.ContractHash = "short"
	if validPublication(publication) {
		t.Fatal("incomplete publication accepted")
	}
}

func TestPublishDraftRejectsInvalidContractBeforeTransaction(t *testing.T) {
	repository := New(newDryRunDB(t))
	transactionCalls := 0
	repository.transact = func(context.Context, *gorm.DB, func(*gorm.DB) error) error {
		transactionCalls++
		return nil
	}
	if _, err := repository.PublishDraft(context.Background(), 8, 7, 1, Publication{}); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("PublishDraft() error = %v, want ErrInvalidDraft", err)
	}
	if transactionCalls != 0 {
		t.Fatalf("transaction calls = %d, want 0", transactionCalls)
	}
}

func TestPublishDraftFreezesContractAndCreatesCleanNextDraft(t *testing.T) {
	db, transactionState := newTransactionDB(t)
	repository := New(db)
	publication := validPublicationFixture()
	repository.lockPublicationSource = func(_ context.Context, _ *gorm.DB, datasourceID uint, fingerprint string) error {
		if datasourceID != 3 || fingerprint != publication.DatasourceSnapshotFingerprint {
			t.Fatalf("publication datasource snapshot = %d/%q", datasourceID, fingerprint)
		}
		return nil
	}
	repository.lockDefinition = func(context.Context, *gorm.DB, uint, uint) (*definitionRecord, error) {
		return &definitionRecord{ReportDefinition: model.ReportDefinition{BaseModel: model.BaseModel{ID: 7}, Code: "orders", DatasourceID: 3, OwnerUserID: 8, CurrentDraftVersionID: 11}}, nil
	}
	repository.lockVersion = func(context.Context, *gorm.DB, uint, uint) (*versionRecord, error) {
		return &versionRecord{ReportVersion: model.ReportVersion{BaseModel: model.BaseModel{ID: 11}, DefinitionID: 7, DatasourceID: 3, VersionNumber: 4, Status: model.ReportVersionStatusDraft, ExecutionMode: model.ReportExecutionModeTableSnapshot, ResultTableOwner: "REPORT", ResultTableName: "ORDER_RESULTS", ContractHash: "old", CompiledSpecJSON: model.JSONText(`{"old":true}`), SchemaProbeToken: "old", PublishedBy: 99}}, nil
	}
	repository.loadCollections = func(_ context.Context, _ *gorm.DB, _, _, _ uint, draft *Draft) error {
		draft.Parameters = []model.ReportParameter{{BaseModel: model.BaseModel{ID: 21}, VersionID: 11, ParameterCode: "runId"}}
		draft.Columns = []model.ReportColumn{{BaseModel: model.BaseModel{ID: 31}, VersionID: 11, FieldID: "field-1"}}
		draft.Grants = []model.ReportGrant{{DefinitionID: 7, SubjectType: "ROLE", SubjectID: 2}}
		return nil
	}
	repository.validateReferences = func(_ context.Context, _ *gorm.DB, datasourceID uint, _ string, grants []model.ReportGrant) error {
		if datasourceID != 3 || len(grants) != 1 {
			t.Fatalf("reference validation datasource=%d grants=%#v", datasourceID, grants)
		}
		return nil
	}
	var publishedUpdates map[string]interface{}
	repository.publishVersion = func(_ context.Context, _ *gorm.DB, versionID, definitionID uint, versionNumber uint64, updates map[string]interface{}) error {
		if versionID != 11 || definitionID != 7 || versionNumber != 4 {
			t.Fatalf("publish identity = %d/%d/%d", versionID, definitionID, versionNumber)
		}
		publishedUpdates = updates
		return nil
	}
	var next model.ReportVersion
	repository.createVersion = func(_ context.Context, _ *gorm.DB, record *versionRecord) error {
		next = record.ReportVersion
		record.ID = 12
		return nil
	}
	repository.copyCollections = func(
		_ context.Context,
		_ *gorm.DB,
		definitionID, versionID uint,
		parameters []model.ReportParameter,
		columns []model.ReportColumn,
		grants []model.ReportGrant,
	) error {
		if definitionID != 7 || versionID != 12 || len(parameters) != 1 || len(columns) != 1 || len(grants) != 1 {
			t.Fatalf("copied definition=%d version=%d parameters=%#v columns=%#v grants=%#v", definitionID, versionID, parameters, columns, grants)
		}
		return nil
	}
	repository.switchDefinition = func(_ context.Context, _ *gorm.DB, ownerID, definitionID, publishedID, draftID, updatedBy uint) error {
		if ownerID != 8 || definitionID != 7 || publishedID != 11 || draftID != 12 || updatedBy != 8 {
			t.Fatalf("definition switch = %d/%d/%d/%d/%d", ownerID, definitionID, publishedID, draftID, updatedBy)
		}
		return nil
	}
	repository.writeAudit = func(_ context.Context, _ *gorm.DB, audit model.ReportAudit) error {
		if audit.Action != "REPORT_PUBLISH" || audit.TargetID != 7 {
			t.Fatalf("audit = %#v", audit)
		}
		return nil
	}

	published, err := repository.PublishDraft(t.Context(), 8, 7, 4, publication)
	if err != nil {
		t.Fatalf("PublishDraft() error = %v", err)
	}
	for key, want := range map[string]interface{}{
		"call_template": publication.CallTemplate, "compiled_spec_json": publication.CompiledSpecJSON, "contract_hash": publication.ContractHash,
		"parameter_schema_hash": publication.ParameterSchemaHash, "procedure_signature_hash": publication.ProcedureSignatureHash,
		"result_schema_hash": publication.ResultSchemaHash, "permission_hash": publication.PermissionHash,
		"export_schema_hash": publication.ExportSchemaHash, "schema_probe_token": publication.SchemaProbeToken,
	} {
		if got := publishedUpdates[key]; got != want {
			t.Fatalf("published update %s = %#v, want %#v", key, got, want)
		}
	}
	if next.ID != 0 || next.DefinitionID != 7 || next.DatasourceID != 3 || next.VersionNumber != 5 || next.Status != model.ReportVersionStatusDraft || next.CallTemplate != publication.CallTemplate ||
		next.CompiledSpecJSON != "" || next.ContractHash != "" || next.ParameterSchemaHash != "" || next.ProcedureSignatureHash != "" ||
		next.ResultSchemaHash != "" || next.PermissionHash != "" || next.ExportSchemaHash != "" || next.SchemaProbeToken != "" ||
		next.PublishedBy != 0 || next.PublishedAt != nil || next.SchemaValidatedAt != nil || next.CreatedBy != 8 {
		t.Fatalf("next draft = %#v", next)
	}
	if published.Version.Status != model.ReportVersionStatusPublished || published.Definition.CurrentPublishedVersionID != 11 || published.Definition.CurrentDraftVersionID != 12 || published.Version.PublishedAt == nil ||
		published.Version.ContractHash != publication.ContractHash || published.Version.ParameterSchemaHash != publication.ParameterSchemaHash ||
		published.Version.ProcedureSignatureHash != publication.ProcedureSignatureHash || published.Version.ResultSchemaHash != publication.ResultSchemaHash ||
		published.Version.PermissionHash != publication.PermissionHash || published.Version.ExportSchemaHash != publication.ExportSchemaHash ||
		published.Version.SchemaProbeToken != publication.SchemaProbeToken || published.Version.SchemaValidatedAt == nil {
		t.Fatalf("published = %#v", published)
	}
	if transactionState.begins != 1 || transactionState.commits != 1 || transactionState.rollbacks != 0 {
		t.Fatalf("transaction state = %#v", transactionState)
	}
	statements := strings.Join(transactionState.execs, "\n")
	if !strings.Contains(statements, "DELETE FROM `report_result_table_bindings`") || !strings.Contains(statements, "INSERT INTO report_result_table_bindings") {
		t.Fatalf("publication transaction did not replace result table binding: %s", statements)
	}
}

func TestActivatePublishedVersionKeepsDraftAndSwitchesOnlyActiveVersion(t *testing.T) {
	repository, transactionState := publicationTestRepository(t)
	repository.lockDefinition = func(context.Context, *gorm.DB, uint, uint) (*definitionRecord, error) {
		return &definitionRecord{ReportDefinition: model.ReportDefinition{BaseModel: model.BaseModel{ID: 7}, Code: "orders", DatasourceID: 3, OwnerUserID: 8, CurrentDraftVersionID: 11, CurrentPublishedVersionID: 19}}, nil
	}
	contractHash := strings.Repeat("a", 64)
	repository.lockPublishedVersion = func(_ context.Context, _ *gorm.DB, definitionID, versionID uint) (*versionRecord, error) {
		if definitionID != 7 || versionID != 18 {
			t.Fatalf("target = %d/%d", definitionID, versionID)
		}
		return &versionRecord{ReportVersion: model.ReportVersion{BaseModel: model.BaseModel{ID: 18}, DefinitionID: 7, DatasourceID: 3, VersionNumber: 2, Status: model.ReportVersionStatusPublished, ExecutionMode: model.ReportExecutionModeTableSnapshot, ResultTableOwner: "REPORT", ResultTableName: "ORDER_RESULTS", ContractHash: contractHash}}, nil
	}
	repository.validateReferences = func(context.Context, *gorm.DB, uint, string, []model.ReportGrant) error { return nil }
	repository.writeAudit = func(_ context.Context, _ *gorm.DB, audit model.ReportAudit) error {
		if audit.Action != "REPORT_VERSION_ACTIVATE" || audit.TargetID != 7 {
			t.Fatalf("audit = %#v", audit)
		}
		return nil
	}
	activation := VersionActivation{
		ContractHash: contractHash, ConnectionFingerprint: strings.Repeat("b", 64),
		ConnectionIdentitySource:      reportidentity.BindingIdentitySourceOracle,
		DatasourceSnapshotFingerprint: strings.Repeat("c", 64),
	}
	activated, err := repository.ActivatePublishedVersion(t.Context(), 8, 7, 18, 4, activation)
	if err != nil {
		t.Fatalf("ActivatePublishedVersion() error = %v", err)
	}
	if activated.Version.ID != 18 || activated.Definition.CurrentPublishedVersionID != 18 || activated.Definition.CurrentDraftVersionID != 11 {
		t.Fatalf("activated = %#v", activated)
	}
	if transactionState.begins != 1 || transactionState.commits != 1 || transactionState.rollbacks != 0 {
		t.Fatalf("transaction state = %#v", transactionState)
	}
	statements := strings.Join(transactionState.execs, "\n")
	for _, expected := range []string{"DELETE FROM `report_result_table_bindings`", "INSERT INTO report_result_table_bindings", "current_published_version_id"} {
		if !strings.Contains(statements, expected) {
			t.Fatalf("activation statements missing %q: %s", expected, statements)
		}
	}
}

func TestPublishDraftRejectsAnotherLegacyBindingForTheSameOwnerAndTable(t *testing.T) {
	repository, transactionState := publicationTestRepository(t)
	transactionState.queryCount = 1
	_, err := repository.PublishDraft(t.Context(), 8, 7, 4, validPublicationFixture())
	if !errors.Is(err, ErrResultTableConflict) {
		t.Fatalf("PublishDraft() error = %v, want ErrResultTableConflict", err)
	}
	if transactionState.commits != 0 || transactionState.rollbacks != 1 {
		t.Fatalf("transaction state = %#v", transactionState)
	}
	statements := strings.Join(transactionState.execs, "\n")
	if !strings.Contains(statements, "identity_source IS NULL OR identity_source <>") || strings.Contains(statements, "INSERT INTO report_result_table_bindings") {
		t.Fatalf("legacy binding guard statements = %s", statements)
	}
}

func TestPublishDraftRollsBackWhenDatasourceChangesAfterOracleInspection(t *testing.T) {
	repository, transactionState := publicationTestRepository(t)
	repository.lockPublicationSource = func(context.Context, *gorm.DB, uint, string) error {
		return ErrDraftVersionConflict
	}
	_, err := repository.PublishDraft(t.Context(), 8, 7, 4, validPublicationFixture())
	if !errors.Is(err, ErrDraftVersionConflict) {
		t.Fatalf("PublishDraft() error = %v, want ErrDraftVersionConflict", err)
	}
	if transactionState.commits != 0 || transactionState.rollbacks != 1 {
		t.Fatalf("transaction state = %#v", transactionState)
	}
	if statements := strings.Join(transactionState.execs, "\n"); strings.Contains(statements, "INSERT INTO report_result_table_bindings") {
		t.Fatalf("stale datasource publication wrote a binding: %s", statements)
	}
}

func TestPublishDraftStopsAtEveryAtomicBoundary(t *testing.T) {
	steps := []string{"reference validation", "publish version", "create draft", "copy collections", "switch definition", "write audit"}
	for _, failedStep := range steps {
		t.Run(failedStep, func(t *testing.T) {
			injected := errors.New("injected failure")
			repository, transactionState := publicationTestRepository(t)
			calls := make([]string, 0, len(steps))
			repository.validateReferences = func(context.Context, *gorm.DB, uint, string, []model.ReportGrant) error {
				calls = append(calls, steps[0])
				if failedStep == steps[0] {
					return injected
				}
				return nil
			}
			repository.publishVersion = func(context.Context, *gorm.DB, uint, uint, uint64, map[string]interface{}) error {
				calls = append(calls, steps[1])
				if failedStep == steps[1] {
					return injected
				}
				return nil
			}
			repository.createVersion = func(_ context.Context, _ *gorm.DB, version *versionRecord) error {
				calls = append(calls, steps[2])
				if failedStep == steps[2] {
					return injected
				}
				version.ID = 12
				return nil
			}
			repository.copyCollections = func(
				context.Context,
				*gorm.DB,
				uint,
				uint,
				[]model.ReportParameter,
				[]model.ReportColumn,
				[]model.ReportGrant,
			) error {
				calls = append(calls, steps[3])
				if failedStep == steps[3] {
					return injected
				}
				return nil
			}
			repository.switchDefinition = func(context.Context, *gorm.DB, uint, uint, uint, uint, uint) error {
				calls = append(calls, steps[4])
				if failedStep == steps[4] {
					return injected
				}
				return nil
			}
			repository.writeAudit = func(context.Context, *gorm.DB, model.ReportAudit) error {
				calls = append(calls, steps[5])
				if failedStep == steps[5] {
					return injected
				}
				return nil
			}
			if _, err := repository.PublishDraft(t.Context(), 8, 7, 4, validPublicationFixture()); !errors.Is(err, injected) {
				t.Fatalf("PublishDraft() error = %v, want injected failure", err)
			}
			if calls[len(calls)-1] != failedStep {
				t.Fatalf("calls = %#v", calls)
			}
			if transactionState.begins != 1 || transactionState.rollbacks != 1 || transactionState.commits != 0 {
				t.Fatalf("transaction state = %#v", transactionState)
			}
		})
	}
}

func publicationTestRepository(t *testing.T) (*Repository, *transactionDriverState) {
	t.Helper()
	db, transactionState := newTransactionDB(t)
	repository := New(db)
	repository.lockPublicationSource = func(context.Context, *gorm.DB, uint, string) error { return nil }
	repository.lockDefinition = func(context.Context, *gorm.DB, uint, uint) (*definitionRecord, error) {
		return &definitionRecord{ReportDefinition: model.ReportDefinition{BaseModel: model.BaseModel{ID: 7}, Code: "orders", DatasourceID: 3, OwnerUserID: 8, CurrentDraftVersionID: 11}}, nil
	}
	repository.lockVersion = func(context.Context, *gorm.DB, uint, uint) (*versionRecord, error) {
		return &versionRecord{ReportVersion: model.ReportVersion{BaseModel: model.BaseModel{ID: 11}, DefinitionID: 7, DatasourceID: 3, VersionNumber: 4, Status: model.ReportVersionStatusDraft, ExecutionMode: model.ReportExecutionModeTableSnapshot, ResultTableOwner: "REPORT", ResultTableName: "ORDER_RESULTS"}}, nil
	}
	repository.loadCollections = func(_ context.Context, _ *gorm.DB, _, _, _ uint, draft *Draft) error {
		draft.Parameters = []model.ReportParameter{{ParameterCode: "runId"}}
		draft.Columns = []model.ReportColumn{{FieldID: "field-1"}}
		draft.Grants = []model.ReportGrant{{SubjectType: "ROLE", SubjectID: 2}}
		return nil
	}
	return repository, transactionState
}

func validPublicationFixture() Publication {
	return Publication{
		CallTemplate:     "BEGIN REPORT.PKG.RUN; END;",
		CompiledSpecJSON: model.JSONText(`{"version":{}}`), ContractHash: strings.Repeat("a", 64),
		ParameterSchemaHash: strings.Repeat("b", 64), ProcedureSignatureHash: strings.Repeat("c", 64),
		ResultSchemaHash: strings.Repeat("d", 64), PermissionHash: strings.Repeat("e", 64), ExportSchemaHash: strings.Repeat("f", 64),
		SchemaProbeToken: "11111111-1111-4111-8111-111111111111", SchemaValidatedAt: time.Now().UTC(),
		ConnectionFingerprint:         strings.Repeat("1", 64),
		ConnectionIdentitySource:      reportidentity.BindingIdentitySourceOracle,
		DatasourceSnapshotFingerprint: strings.Repeat("2", 64),
	}
}

var (
	reportTransactionDriverOnce sync.Once
	reportTransactionStates     sync.Map
	reportTransactionSequence   atomic.Uint64
)

type transactionDriver struct{}

type transactionConnection struct {
	state *transactionDriverState
}

type transactionDriverState struct {
	begins     int
	commits    int
	rollbacks  int
	execs      []string
	queryCount int64
}

type transactionHandle struct {
	state *transactionDriverState
}

type transactionRows struct {
	read  bool
	count int64
}

func (*transactionRows) Columns() []string { return []string{"count"} }
func (*transactionRows) Close() error      { return nil }
func (rows *transactionRows) Next(values []driver.Value) error {
	if rows.read {
		return io.EOF
	}
	rows.read = true
	values[0] = rows.count
	return nil
}

func (transactionDriver) Open(name string) (driver.Conn, error) {
	value, ok := reportTransactionStates.Load(name)
	if !ok {
		return nil, errors.New("report transaction test: state not found")
	}
	return &transactionConnection{state: value.(*transactionDriverState)}, nil
}

func (connection *transactionConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("report transaction test: prepare is unsupported")
}
func (connection *transactionConnection) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	connection.state.execs = append(connection.state.execs, query)
	return driver.RowsAffected(1), nil
}
func (connection *transactionConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	connection.state.execs = append(connection.state.execs, query)
	return &transactionRows{count: connection.state.queryCount}, nil
}
func (connection *transactionConnection) Close() error { return nil }
func (connection *transactionConnection) Begin() (driver.Tx, error) {
	connection.state.begins++
	return &transactionHandle{state: connection.state}, nil
}
func (transaction *transactionHandle) Commit() error {
	transaction.state.commits++
	return nil
}
func (transaction *transactionHandle) Rollback() error {
	transaction.state.rollbacks++
	return nil
}

func newTransactionDB(t *testing.T) (*gorm.DB, *transactionDriverState) {
	t.Helper()
	reportTransactionDriverOnce.Do(func() { sql.Register("report_transaction_test", transactionDriver{}) })
	name := fmt.Sprintf("transaction-%d", reportTransactionSequence.Add(1))
	state := &transactionDriverState{}
	reportTransactionStates.Store(name, state)
	t.Cleanup(func() { reportTransactionStates.Delete(name) })
	sqlDB, err := sql.Open("report_transaction_test", name)
	if err != nil {
		t.Fatalf("open transaction test database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open transaction test GORM database: %v", err)
	}
	return db, state
}

func TestListDraftsUsesBoundedKeysetAndSharedQueryAccess(t *testing.T) {
	db := newDryRunDB(t)
	statement := buildDraftListQuery(db.Session(&gorm.Session{DryRun: true}), 5,
		DraftListQuery{AfterID: 10, Limit: 20, Search: `a%b_`}).Scan(&[]draftSummaryRecord{})
	sqlText := statement.Statement.SQL.String()
	for _, fragment := range []string{
		"draft_versions.version_number ELSE 0 END AS lock_version", "definitions.owner_user_id = ?", "definitions.status IN", "definitions.id > ?",
		"FROM report_category_access AS category_access", "JOIN report_category_grants AS category_grants", "configured_category.category = definitions.category",
		"published_versions.id IS NOT NULL", "FROM report_grants AS grants", "grants.version_id = published_versions.id",
		"JSON_CONTAINS(grants.actions_json, JSON_QUOTE(?))",
		"grants.subject_type = ? AND grants.subject_id = ?", "FROM user_roles AS memberships", "JOIN roles", "roles.status = ?",
		")) AND definitions.id > ? AND ((definitions.code LIKE ?",
		"ORDER BY definitions.id ASC", "LIMIT 21",
	} {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("list SQL %q does not contain %q", sqlText, fragment)
		}
	}
	for _, value := range []interface{}{ReportActionQuery, "USER", uint(5), "ROLE", model.RoleStatusActive} {
		if !containsSQLVariable(statement.Statement.Vars, value) {
			t.Fatalf("list vars %#v do not contain %#v", statement.Statement.Vars, value)
		}
	}
	if got := escapeLike(`a%b_`); got != `a\%b\_` {
		t.Fatalf("escapeLike() = %q", got)
	}
}

func TestDownloadCatalogQueryExcludesOwnerDraftsAndRequiresExport(t *testing.T) {
	db := newDryRunDB(t)
	statement := buildDraftListQuery(db.Session(&gorm.Session{DryRun: true}), 5,
		DraftListQuery{Limit: 20, PublishedOnly: true, Action: ReportActionExport, AdditionalAction: ReportActionQuery}).Scan(&[]draftSummaryRecord{})
	sqlText := statement.Statement.SQL.String()
	if strings.Contains(sqlText, "definitions.owner_user_id = ? AND definitions.status IN") {
		t.Fatalf("download SQL includes owner draft branch: %s", sqlText)
	}
	if countSQLVariable(statement.Statement.Vars, ReportActionExport) != 2 || countSQLVariable(statement.Statement.Vars, ReportActionQuery) != 2 {
		t.Fatalf("download SQL vars = %#v", statement.Statement.Vars)
	}
}

func countSQLVariable(values []interface{}, expected interface{}) int {
	count := 0
	for _, value := range values {
		if value == expected {
			count++
		}
	}
	return count
}

func containsSQLVariable(values []interface{}, expected interface{}) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestNewVersionRecordPreservesResultIdentityContract(t *testing.T) {
	record := newVersionRecord(model.ReportVersion{
		BaseModel:         model.BaseModel{ID: 99},
		DefinitionID:      7,
		DatasourceID:      3,
		VersionNumber:     4,
		ResultRunIDColumn: "EXECUTION_ID",
		ResultRowIDColumn: "RESULT_NO",
	})
	if record.ID != 0 || record.DefinitionID != 7 || record.DatasourceID != 3 || record.VersionNumber != 4 ||
		record.ResultRunIDColumn != "EXECUTION_ID" || record.ResultRowIDColumn != "RESULT_NO" {
		t.Fatalf("newVersionRecord() = %#v", record.ReportVersion)
	}
}

func TestNormalizeNewDraftStartsAtVersionOne(t *testing.T) {
	draft := validDraft()
	draft.Version.VersionNumber = 99
	normalizeNewDraft(draft)
	if draft.Version.VersionNumber != 1 {
		t.Fatalf("VersionNumber = %d, want 1", draft.Version.VersionNumber)
	}
}

func TestRecordTableNamesUseExistingReportTables(t *testing.T) {
	tests := []struct{ got, want string }{
		{(definitionRecord{}).TableName(), "report_definitions"},
		{(versionRecord{}).TableName(), "report_versions"},
		{(parameterRecord{}).TableName(), "report_parameters"},
		{(columnRecord{}).TableName(), "report_columns"},
		{(grantRecord{}).TableName(), "report_grants"},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Fatalf("TableName() = %q, want %q", test.got, test.want)
		}
	}
}

func TestNewGrantRecordsBindEveryGrantToOneVersion(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	rows := newGrantRecords(7, 12, []model.ReportGrant{{
		BaseModel:    model.BaseModel{ID: 99},
		DefinitionID: 2,
		VersionID:    3,
		SubjectType:  "ROLE",
		SubjectID:    5,
	}}, now)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	grant := rows[0].ReportGrant
	if grant.ID != 0 || grant.DefinitionID != 7 || grant.VersionID != 12 || grant.CreatedAt != now || grant.UpdatedAt != now {
		t.Fatalf("versioned grant = %#v", grant)
	}
}

func validDraft() *Draft {
	return &Draft{
		Definition: model.ReportDefinition{
			Code: "orders", Name: "订单报表", DatasourceID: 3, OwnerUserID: 8,
			Status: model.ReportDefinitionStatusDraft, CreatedBy: 8, UpdatedBy: 8,
		},
		Version: model.ReportVersion{Status: model.ReportVersionStatusDraft, DatasourceID: 3, VersionNumber: 1, CreatedBy: 8},
	}
}

func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn: &sql.DB{}, SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	return db
}
