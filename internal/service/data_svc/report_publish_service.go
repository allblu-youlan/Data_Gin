package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gin-biz-web-api/internal/reportcontract"
	"gin-biz-web-api/internal/reportidentity"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/model"
)

var (
	ErrReportPublicationInvalid        = errors.New("report publication: invalid contract")
	ErrReportPublicationTemporaryTable = errors.New("report publication: temporary result table is unsupported")
)

const defaultReportPublicationInspectionTimeout = 5 * time.Minute

type reportPublicationStore interface {
	FindDraftByID(context.Context, uint, uint) (*reportrepo.Draft, error)
	FindPublishedVersion(context.Context, uint, uint, uint) (*reportrepo.Draft, error)
	FindDatasource(context.Context, uint) (*model.ReportDatasource, error)
	PublishDraft(context.Context, uint, uint, uint64, reportrepo.Publication) (*reportrepo.Draft, error)
	ActivatePublishedVersion(context.Context, uint, uint, uint, uint64, reportrepo.VersionActivation) (*reportrepo.Draft, error)
}

type reportCredentialDecryptor interface {
	Decrypt(version, ciphertext string) (string, error)
}

type reportOracleInspector interface {
	InspectDatabaseIdentity(context.Context) (reportoracle.DatabaseIdentity, error)
	InspectProcedure(context.Context, reportoracle.ProcedureRef) ([]reportoracle.ProcedureArgument, error)
	InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error)
	InspectResultSnapshotContract(context.Context, reportoracle.ResultSnapshotRef) (reportoracle.ResultSnapshotContract, error)
	ValidateResultSnapshotTable(context.Context, reportoracle.ResultTableRef) error
	ValidateJSONSnapshotStore(context.Context) error
	Close() error
}

type reportOracleOpener func(context.Context, reportoracle.Config) (reportOracleInspector, error)

func OpenReportOracle(ctx context.Context, config reportoracle.Config) (reportOracleInspector, error) {
	return reportoracle.Open(ctx, config)
}

type ReportPublishService struct {
	store     reportPublicationStore
	decryptor reportCredentialDecryptor
	open      reportOracleOpener
	now       func() time.Time
}

type ReportPublicationDTO struct {
	DefinitionID uint                            `json:"definitionId"`
	VersionID    uint                            `json:"versionId"`
	Version      uint64                          `json:"version"`
	Status       string                          `json:"status"`
	ContractHash string                          `json:"contractHash"`
	PublishedAt  time.Time                       `json:"publishedAt"`
	Validation   *ReportPublicationValidationDTO `json:"validation,omitempty"`
}

type ReportPublicationValidationDTO struct {
	ValidatedAt time.Time                     `json:"validatedAt"`
	Procedure   ReportPublicationProcedureDTO `json:"procedure"`
	Result      ReportPublicationResultDTO    `json:"result"`
	Snapshot    ReportPublicationSnapshotDTO  `json:"snapshot"`
	Export      ReportPublicationExportDTO    `json:"export"`
}

type ReportPublicationProcedureDTO struct {
	Owner         string `json:"owner"`
	Package       string `json:"package"`
	Name          string `json:"name"`
	Overload      string `json:"overload"`
	ArgumentCount int    `json:"argumentCount"`
	SignatureHash string `json:"signatureHash"`
}
type ReportPublicationResultDTO struct {
	TableOwner  string `json:"tableOwner"`
	TableName   string `json:"tableName"`
	ColumnCount int    `json:"columnCount"`
	SchemaHash  string `json:"schemaHash"`
}
type ReportPublicationSnapshotDTO struct {
	ResultTableValidated bool `json:"resultTableValidated"`
}
type ReportPublicationExportDTO struct {
	ExportableColumnCount int    `json:"exportableColumnCount"`
	SchemaHash            string `json:"schemaHash"`
}

type reportPublicationInspection struct {
	compiled               reportcontract.Compiled
	connectionFingerprint  string
	procedureArgumentCount int
	resultColumnCount      int
	resultTableValidated   bool
}

func NewReportPublishService(store reportPublicationStore, decryptor reportCredentialDecryptor, opener reportOracleOpener) *ReportPublishService {
	if store == nil || decryptor == nil || opener == nil {
		panic("report publication: dependencies are required")
	}
	return &ReportPublishService{store: store, decryptor: decryptor, open: opener, now: func() time.Time { return time.Now().UTC() }}
}

func (service *ReportPublishService) Publish(ctx context.Context, actor, definitionID uint, expectedLockVersion uint64) (*ReportPublicationDTO, error) {
	if service == nil || service.store == nil || service.decryptor == nil || service.open == nil || ctx == nil || actor == 0 || definitionID == 0 || expectedLockVersion == 0 {
		return nil, fmt.Errorf("%w: service, actor, report and lock version are required", ErrReportPublicationInvalid)
	}
	draft, err := service.store.FindDraftByID(ctx, actor, definitionID)
	if err != nil {
		return nil, classifyPublicationStoreError(err)
	}
	if draft.LockVersion != expectedLockVersion {
		return nil, ErrReportConflict
	}
	if draft.Version.DatasourceID == 0 || draft.Version.DatasourceID != draft.Definition.DatasourceID {
		return nil, fmt.Errorf("%w: datasource snapshot is inconsistent", ErrReportPublicationInvalid)
	}
	datasource, err := service.store.FindDatasource(ctx, draft.Version.DatasourceID)
	if err != nil {
		return nil, classifyPublicationStoreError(err)
	}
	password, err := service.decryptor.Decrypt(datasource.CredentialKeyVersion, datasource.PasswordCiphertext)
	if err != nil {
		return nil, fmt.Errorf("report publication: decrypt datasource credential: %w", err)
	}
	inspection, err := service.inspectContract(ctx, datasource, password, draft)
	if err != nil {
		return nil, err
	}
	validatedAt := service.now().UTC()
	published, err := service.store.PublishDraft(ctx, actor, definitionID, expectedLockVersion, reportrepo.Publication{
		CallTemplate: draft.Version.CallTemplate, CompiledSpecJSON: model.JSONText(inspection.compiled.SpecJSON), ContractHash: inspection.compiled.Hashes.Contract,
		ParameterSchemaHash: inspection.compiled.Hashes.ParameterSchema, ProcedureSignatureHash: inspection.compiled.Hashes.ProcedureSignature,
		ResultSchemaHash: inspection.compiled.Hashes.ResultSchema, PermissionHash: inspection.compiled.Hashes.Permission,
		ExportSchemaHash: inspection.compiled.Hashes.ExportSchema, SchemaProbeToken: uuid.NewString(), SchemaValidatedAt: validatedAt,
		ConnectionFingerprint: inspection.connectionFingerprint, ConnectionIdentitySource: reportidentity.BindingIdentitySourceOracle,
		DatasourceSnapshotFingerprint: reportidentity.DatasourceFingerprint(*datasource),
	})
	if err != nil {
		return nil, classifyPublicationStoreError(err)
	}
	publishedAt := validatedAt
	if published.Version.PublishedAt != nil {
		publishedAt = published.Version.PublishedAt.UTC()
	}
	return &ReportPublicationDTO{
		DefinitionID: published.Definition.ID, VersionID: published.Version.ID, Version: published.Version.VersionNumber,
		Status: model.ReportVersionStatusPublished, ContractHash: inspection.compiled.Hashes.Contract, PublishedAt: publishedAt,
		Validation: publicationValidationDTO(validatedAt, draft, inspection),
	}, nil
}

func (service *ReportPublishService) Activate(ctx context.Context, actor, definitionID, versionID uint, expectedLockVersion uint64) (*ReportPublicationDTO, error) {
	if service == nil || service.store == nil || service.decryptor == nil || service.open == nil || ctx == nil || actor == 0 || definitionID == 0 || versionID == 0 || expectedLockVersion == 0 {
		return nil, fmt.Errorf("%w: service, actor, report, version and lock version are required", ErrReportPublicationInvalid)
	}
	current, err := service.store.FindDraftByID(ctx, actor, definitionID)
	if err != nil {
		return nil, classifyPublicationStoreError(err)
	}
	if current.LockVersion != expectedLockVersion || current.Definition.CurrentPublishedVersionID == versionID {
		return nil, ErrReportConflict
	}
	target, err := service.store.FindPublishedVersion(ctx, actor, definitionID, versionID)
	if err != nil {
		return nil, classifyPublicationStoreError(err)
	}
	datasource, err := service.store.FindDatasource(ctx, target.Version.DatasourceID)
	if err != nil {
		return nil, classifyPublicationStoreError(err)
	}
	password, err := service.decryptor.Decrypt(datasource.CredentialKeyVersion, datasource.PasswordCiphertext)
	if err != nil {
		return nil, fmt.Errorf("report publication: decrypt datasource credential: %w", err)
	}
	inspection, err := service.inspectContract(ctx, datasource, password, target)
	if err != nil {
		return nil, err
	}
	if inspection.compiled.Hashes.Contract != target.Version.ContractHash {
		return nil, fmt.Errorf("%w: selected version no longer matches the Oracle contract", ErrReportPublicationInvalid)
	}
	validatedAt := service.now().UTC()
	activated, err := service.store.ActivatePublishedVersion(ctx, actor, definitionID, versionID, expectedLockVersion, reportrepo.VersionActivation{
		ContractHash: target.Version.ContractHash, ConnectionFingerprint: inspection.connectionFingerprint,
		ConnectionIdentitySource:      reportidentity.BindingIdentitySourceOracle,
		DatasourceSnapshotFingerprint: reportidentity.DatasourceFingerprint(*datasource),
	})
	if err != nil {
		return nil, classifyPublicationStoreError(err)
	}
	publishedAt := validatedAt
	if activated.Version.PublishedAt != nil {
		publishedAt = activated.Version.PublishedAt.UTC()
	}
	return &ReportPublicationDTO{
		DefinitionID: activated.Definition.ID, VersionID: activated.Version.ID, Version: activated.Version.VersionNumber,
		Status: model.ReportVersionStatusPublished, ContractHash: activated.Version.ContractHash, PublishedAt: publishedAt,
		Validation: publicationValidationDTO(validatedAt, target, inspection),
	}, nil
}

func publicationValidationDTO(validatedAt time.Time, draft *reportrepo.Draft, inspection reportPublicationInspection) *ReportPublicationValidationDTO {
	return &ReportPublicationValidationDTO{
		ValidatedAt: validatedAt,
		Procedure:   ReportPublicationProcedureDTO{Owner: draft.Version.ProcedureOwner, Package: draft.Version.PackageName, Name: draft.Version.ProcedureName, Overload: draft.Version.ProcedureOverload, ArgumentCount: inspection.procedureArgumentCount, SignatureHash: inspection.compiled.Hashes.ProcedureSignature},
		Result:      ReportPublicationResultDTO{TableOwner: draft.Version.ResultTableOwner, TableName: draft.Version.ResultTableName, ColumnCount: inspection.resultColumnCount, SchemaHash: inspection.compiled.Hashes.ResultSchema},
		Snapshot:    ReportPublicationSnapshotDTO{ResultTableValidated: inspection.resultTableValidated},
		Export:      ReportPublicationExportDTO{ExportableColumnCount: exportableReportColumnCount(draft.Columns), SchemaHash: inspection.compiled.Hashes.ExportSchema},
	}
}

func (service *ReportPublishService) inspectContract(
	ctx context.Context,
	datasource *model.ReportDatasource,
	password string,
	draft *reportrepo.Draft,
) (inspection reportPublicationInspection, resultErr error) {
	timeout := defaultReportPublicationInspectionTimeout
	if datasource.QueryTimeoutSeconds > 0 {
		timeout = time.Duration(datasource.QueryTimeoutSeconds) * time.Second
	}
	inspectionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inspector, err := service.open(inspectionCtx, oracleConfigFromDatasource(*datasource, password))
	if err != nil {
		return inspection, fmt.Errorf("report publication: open oracle datasource: %w", err)
	}
	defer func() {
		if closeErr := inspector.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("report publication: close oracle datasource: %w", closeErr))
		}
	}()
	databaseIdentity, err := inspector.InspectDatabaseIdentity(inspectionCtx)
	if err != nil {
		return inspection, classifyOracleInspectionError("database identity", err)
	}
	inspection.connectionFingerprint, err = reportidentity.OracleDatabaseFingerprint(reportidentity.OracleDatabaseIdentity{
		DBID: databaseIdentity.DBID, DBUniqueName: databaseIdentity.DBUniqueName, DBName: databaseIdentity.DBName,
		ContainerID: databaseIdentity.ContainerID, ContainerUID: databaseIdentity.ContainerUID, ContainerName: databaseIdentity.ContainerName,
	})
	if err != nil {
		return inspection, fmt.Errorf("%w: Oracle database identity is incomplete", ErrReportPublicationInvalid)
	}

	procedureRef := reportoracle.ProcedureRef{Owner: draft.Version.ProcedureOwner, Package: draft.Version.PackageName, Name: draft.Version.ProcedureName, Overload: draft.Version.ProcedureOverload}
	procedure, err := inspector.InspectProcedure(inspectionCtx, procedureRef)
	if err != nil {
		return inspection, classifyOracleInspectionError("procedure", err)
	}
	if isJSONTableSnapshot(draft.Version) {
		plan, planErr := reportoracle.BuildJSONTableCallPlan(procedureRef, procedure, draft.Version.JSONInputArgName)
		if planErr != nil {
			return inspection, fmt.Errorf("%w: compile JSON result-table call: %v", ErrReportPublicationInvalid, planErr)
		}
		draft.Version.CallTemplate = plan.Statement()
	}
	if draft.Version.ExecutionMode == model.ReportExecutionModeRefCursor {
		if err := inspector.ValidateJSONSnapshotStore(inspectionCtx); err != nil {
			return inspection, classifyOracleInspectionError("JSON snapshot store", err)
		}
		compiled, err := reportcontract.Compile(draft.Version, nil, draft.Columns, draft.Grants, procedure, nil, reportoracle.ResultSnapshotContract{})
		if err != nil {
			return inspection, fmt.Errorf("%w: %v", ErrReportPublicationInvalid, err)
		}
		inspection.compiled = compiled
		inspection.procedureArgumentCount = len(procedure)
		inspection.resultColumnCount = len(draft.Columns)
		inspection.resultTableValidated = true
		return inspection, nil
	}
	resultRef := reportoracle.ResultTableRef{Owner: draft.Version.ResultTableOwner, Name: draft.Version.ResultTableName}
	resultColumns, err := inspector.InspectResultTable(inspectionCtx, resultRef)
	if err != nil {
		return inspection, classifyOracleInspectionError("result table", err)
	}
	if err := inspector.ValidateResultSnapshotTable(inspectionCtx, resultRef); err != nil {
		return inspection, classifyOracleInspectionError("result table ROWID", err)
	}
	configuredColumns := make([]string, 0, len(draft.Columns))
	for _, column := range draft.Columns {
		configuredColumns = append(configuredColumns, column.DatabaseColumn)
	}
	snapshot, err := inspector.InspectResultSnapshotContract(inspectionCtx, reportoracle.ResultTableSnapshotRef(resultRef, configuredColumns))
	if err != nil {
		return inspection, classifyOracleInspectionError("result snapshot", err)
	}
	compiled, err := reportcontract.Compile(draft.Version, draft.Parameters, draft.Columns, draft.Grants, procedure, resultColumns, snapshot)
	if err != nil {
		return inspection, fmt.Errorf("%w: %v", ErrReportPublicationInvalid, err)
	}
	inspection.compiled = compiled
	inspection.procedureArgumentCount = len(procedure)
	inspection.resultColumnCount = len(resultColumns)
	inspection.resultTableValidated = true
	return inspection, nil
}

func exportableReportColumnCount(columns []model.ReportColumn) int {
	count := 0
	for _, column := range columns {
		if column.ExportVisible && column.ExportAllowed {
			count++
		}
	}
	return count
}

func classifyOracleInspectionError(target string, err error) error {
	if errors.Is(err, reportoracle.ErrTemporaryResultTable) {
		return fmt.Errorf("%w: %w", ErrReportPublicationInvalid, ErrReportPublicationTemporaryTable)
	}
	if errors.Is(err, reportoracle.ErrInvalidConfiguration) || errors.Is(err, reportoracle.ErrMetadataMismatch) || errors.Is(err, reportoracle.ErrUnsupportedBinding) {
		return fmt.Errorf("%w: inspect Oracle %s: %v", ErrReportPublicationInvalid, target, err)
	}
	return fmt.Errorf("report publication: inspect Oracle %s: %w", target, err)
}

func oracleConfigFromDatasource(datasource model.ReportDatasource, password string) reportoracle.Config {
	return reportoracle.Config{
		Host: datasource.Host, Port: datasource.Port, ServiceName: datasource.ServiceName, SID: datasource.SID,
		Username: datasource.Username, Password: password, Timezone: datasource.SessionTimezone,
		ConnectTimeout:     time.Duration(datasource.ConnectTimeoutSeconds) * time.Second,
		MaxOpenConnections: datasource.MaxOpenConnections, MaxIdleConnections: datasource.MaxIdleConnections,
		PrefetchRows: datasource.PrefetchRows, FetchArraySize: datasource.ArraySize,
	}
}

func reportOracleQueryContext(parent context.Context, datasource model.ReportDatasource) (context.Context, context.CancelFunc) {
	timeout := time.Duration(datasource.QueryTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultReportPublicationInspectionTimeout
	}
	return context.WithTimeout(parent, timeout)
}

func classifyPublicationStoreError(err error) error {
	switch {
	case errors.Is(err, reportrepo.ErrDraftNotFound):
		return ErrReportNotFound
	case errors.Is(err, reportrepo.ErrDraftVersionConflict):
		return ErrReportConflict
	case errors.Is(err, reportrepo.ErrDatasourceUnavailable):
		return fmt.Errorf("%w: datasource does not exist, is disabled or is not Oracle", ErrReportPublicationInvalid)
	case errors.Is(err, reportrepo.ErrInvalidDraft):
		return fmt.Errorf("%w: repository rejected publication", ErrReportPublicationInvalid)
	case errors.Is(err, reportrepo.ErrResultTableConflict):
		return fmt.Errorf("%w: Oracle结果表已被其他已发布报表绑定，结果表必须独占", ErrReportPublicationInvalid)
	default:
		return fmt.Errorf("report publication: store: %w", err)
	}
}
