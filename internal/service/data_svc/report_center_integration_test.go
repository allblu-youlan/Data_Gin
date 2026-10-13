//go:build integration

package data_svc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
	pkgconfig "gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/storage"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const reportIntegrationMaximumRows = int64(10000)

type reportIntegrationConfig struct {
	mysqlDSN string
	oracle   reportIntegrationOracleConfig
	ossBase  string
}

type reportIntegrationOracleConfig struct {
	host, serviceName, sid, username, password, timezone string
	owner, packageName, procedure, resultTable           string
	overload, resultColumn                               string
	port                                                 int
}

type reportIntegrationResources struct {
	datasourceID uint
	definitionID uint
	runIDs       []uint
	exportIDs    []uint
	taskKeys     []string
	objectKeys   []string
}

func TestReportCenterEndToEnd(t *testing.T) {
	cfg := requireReportIntegration(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()

	db := openReportIntegrationMySQL(t, ctx, cfg.mysqlDSN)
	migrateReportIntegrationModels(t, ctx, db)
	configureReportIntegrationOSS(t)

	credential := configureReportIntegrationSecrets(t)
	repository := reportrepo.New(db)
	resources := &reportIntegrationResources{}
	fixture := inspectReportIntegrationFixture(t, ctx, cfg)
	t.Cleanup(func() {
		cleanupReportIntegration(t, cfg, db, fixture, resources)
	})

	actor := reportIntegrationActorID()
	datasource := createReportIntegrationDatasource(t, ctx, repository, credential, actor, cfg)
	resources.datasourceID = datasource.ID
	draft := createReportIntegrationDraft(t, ctx, repository, actor, datasource.ID, cfg, fixture)
	resources.definitionID = draft.Definition.ID

	publisher := NewReportPublishService(repository, credential, OpenReportOracle)
	publication, err := publisher.Publish(ctx, actor, draft.Definition.ID, draft.LockVersion)
	if err != nil {
		t.Fatalf("publish report integration contract: %v", err)
	}
	if publication.Status != model.ReportVersionStatusPublished || len(publication.ContractHash) != 64 {
		t.Fatalf("published contract = %#v", publication)
	}

	t.Run("upload failure keeps Oracle snapshot", func(t *testing.T) {
		run := executeReportIntegrationRun(t, ctx, db, repository, credential, actor, draft.Definition.ID, resources)
		export := createReportIntegrationExport(t, ctx, repository, actor, run.ID, resources)
		realOSS, err := storage.NewOSSClientFromConfig()
		if err != nil {
			t.Fatalf("open OSS client: %v", err)
		}
		failingOSS := &reportIntegrationFailingUploadStore{delegate: realOSS}
		processor := newReportIntegrationExportProcessor(t, repository, credential, cfg.ossBase, func() (reportExportObjectStore, error) {
			return failingOSS, nil
		}, resources)
		err = processor.Process(ctx, export.ID, false)
		if !errors.Is(err, ErrReportExportProcessNonRetryable) {
			t.Fatalf("failed upload Process() error = %v, want non-retryable", err)
		}
		storedExport := loadReportIntegrationExport(t, ctx, db, export.ID)
		storedRun := loadReportIntegrationRun(t, ctx, db, run.ID)
		if storedExport.Status != model.ReportExportStatusFailed || storedExport.PurgedAt != nil {
			t.Fatalf("failed export state = %#v", storedExport)
		}
		if storedRun.Status != model.ReportRunStatusSucceeded || storedRun.ResultPurgedAt != nil {
			t.Fatalf("run was cleaned after failed upload: %#v", storedRun)
		}
		if count := countReportIntegrationOracleRows(t, ctx, cfg, fixture, run.DefinitionID); count != run.RowCount {
			t.Fatalf("Oracle rows after upload failure = %d, want %d", count, run.RowCount)
		}
		if len(failingOSS.attemptedKeys) != 1 || !strings.HasPrefix(failingOSS.attemptedKeys[0], cfg.ossBase+"/") {
			t.Fatalf("failed upload object keys = %#v, want one key under %q", failingOSS.attemptedKeys, cfg.ossBase)
		}
	})

	t.Run("query export download cleanup and reuse", func(t *testing.T) {
		run := executeReportIntegrationRun(t, ctx, db, repository, credential, actor, draft.Definition.ID, resources)
		expectedFirstValue := assertReportIntegrationQueries(t, ctx, repository, credential, actor, run, fixture.column.FieldID)

		export := createReportIntegrationExport(t, ctx, repository, actor, run.ID, resources)
		reused := createOrReuseReportIntegrationExport(t, ctx, repository, actor, run.ID)
		if reused.ID != export.ID || reused.ExportUUID != export.ExportUUID {
			t.Fatalf("reused export = %#v, want id %d uuid %s", reused, export.ID, export.ExportUUID)
		}

		realOSS, err := storage.NewOSSClientFromConfig()
		if err != nil {
			t.Fatalf("open OSS client: %v", err)
		}
		processor := newReportIntegrationExportProcessor(t, repository, credential, cfg.ossBase, func() (reportExportObjectStore, error) {
			return realOSS, nil
		}, resources)
		if err := processor.Process(ctx, export.ID, false); err != nil {
			t.Fatalf("process report export: %v", err)
		}

		storedExport := loadReportIntegrationExport(t, ctx, db, export.ID)
		storedRun := loadReportIntegrationRun(t, ctx, db, run.ID)
		if storedExport.Status != model.ReportExportStatusReady || storedExport.PurgedAt == nil || storedExport.PurgedRows != run.RowCount {
			t.Fatalf("ready export state = %#v", storedExport)
		}
		if storedRun.Status != model.ReportRunStatusResultPurged || storedRun.ResultPurgedAt == nil {
			t.Fatalf("purged run state = %#v", storedRun)
		}
		if count := countReportIntegrationOracleRows(t, ctx, cfg, fixture, run.DefinitionID); count != run.RowCount {
			t.Fatalf("Oracle rows after READY export = %d, want %d until the next procedure run clears them", count, run.RowCount)
		}
		assertReportIntegrationWorkbook(t, ctx, realOSS, storedExport, fixture.excelHeader, expectedFirstValue, run.RowCount)

		objectKey := storedExport.ResultObjectKey
		metadataBefore, err := realOSS.StatDownloadObject(ctx, objectKey)
		if err != nil {
			t.Fatalf("stat exported object before reuse: %v", err)
		}
		if err := processor.Process(ctx, export.ID, false); err != nil {
			t.Fatalf("reprocess READY export: %v", err)
		}
		metadataAfter, err := realOSS.StatDownloadObject(ctx, objectKey)
		if err != nil {
			t.Fatalf("stat exported object after reuse: %v", err)
		}
		if metadataBefore.Size != metadataAfter.Size || metadataAfter.Size != storedExport.FileSizeBytes {
			t.Fatalf("reused object sizes before=%d after=%d mysql=%d", metadataBefore.Size, metadataAfter.Size, storedExport.FileSizeBytes)
		}
		var exportCount int64
		if err := db.WithContext(ctx).Model(&model.ReportExport{}).Where("run_id = ?", run.ID).Count(&exportCount).Error; err != nil {
			t.Fatalf("count formal exports: %v", err)
		}
		if exportCount != 1 {
			t.Fatalf("formal export count = %d, want 1", exportCount)
		}
	})
}

type reportIntegrationFixture struct {
	parameter   model.ReportParameter
	column      model.ReportColumn
	excelHeader string
}

func requireReportIntegration(t *testing.T) reportIntegrationConfig {
	t.Helper()
	if strings.TrimSpace(os.Getenv("REPORT_INTEGRATION_ENABLED")) != "1" {
		t.Skip("report integration environment is not enabled")
	}
	required := func(name string) string {
		t.Helper()
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			t.Fatalf("%s is required when REPORT_INTEGRATION_ENABLED=1", name)
		}
		return value
	}
	port, err := strconv.Atoi(required("REPORT_INTEGRATION_ORACLE_PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("REPORT_INTEGRATION_ORACLE_PORT is invalid")
	}
	mysqlDSN := required("REPORT_INTEGRATION_MYSQL_DSN")
	parsedDSN, err := mysqldriver.ParseDSN(mysqlDSN)
	if err != nil {
		t.Fatalf("parse REPORT_INTEGRATION_MYSQL_DSN: %v", err)
	}
	databaseName := strings.ToLower(strings.TrimSpace(parsedDSN.DBName))
	if databaseName == "" || (!strings.Contains(databaseName, "_test") && !strings.Contains(databaseName, "integration")) {
		t.Fatalf("REPORT_INTEGRATION_MYSQL_DSN database %q must contain _test or integration", parsedDSN.DBName)
	}
	serviceName := strings.TrimSpace(os.Getenv("REPORT_INTEGRATION_ORACLE_SERVICE_NAME"))
	sid := strings.TrimSpace(os.Getenv("REPORT_INTEGRATION_ORACLE_SID"))
	if serviceName == "" && sid == "" {
		t.Fatalf("REPORT_INTEGRATION_ORACLE_SERVICE_NAME or REPORT_INTEGRATION_ORACLE_SID is required")
	}
	optional := func(name, fallback string) string {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			return fallback
		}
		return value
	}
	ossPrefix := strings.Trim(strings.TrimSpace(required("REPORT_INTEGRATION_OSS_PREFIX")), "/")
	if ossPrefix == "" || ossPrefix == "." || strings.Contains(ossPrefix, "..") {
		t.Fatalf("REPORT_INTEGRATION_OSS_PREFIX must be a dedicated safe prefix")
	}
	for _, name := range []string{"ALIYUN_OSS_REGION", "ALIYUN_OSS_ENDPOINT", "ALIYUN_OSS_BUCKET", "ALIYUN_OSS_ACCESS_KEY_ID", "ALIYUN_OSS_ACCESS_KEY_SECRET"} {
		required(name)
	}
	sessionID := uuid.NewString()
	return reportIntegrationConfig{
		mysqlDSN: mysqlDSN,
		ossBase:  path.Join(ossPrefix, "report-integration", sessionID),
		oracle: reportIntegrationOracleConfig{
			host: required("REPORT_INTEGRATION_ORACLE_HOST"), port: port,
			serviceName: serviceName, sid: sid,
			username: required("REPORT_INTEGRATION_ORACLE_USERNAME"), password: required("REPORT_INTEGRATION_ORACLE_PASSWORD"),
			timezone: optional("REPORT_INTEGRATION_ORACLE_TIMEZONE", "Asia/Shanghai"),
			owner:    required("REPORT_INTEGRATION_ORACLE_OWNER"), packageName: required("REPORT_INTEGRATION_ORACLE_PACKAGE"),
			procedure: required("REPORT_INTEGRATION_ORACLE_PROCEDURE"), overload: strings.TrimSpace(os.Getenv("REPORT_INTEGRATION_ORACLE_OVERLOAD")),
			resultTable:  required("REPORT_INTEGRATION_ORACLE_RESULT_TABLE"),
			resultColumn: optional("REPORT_INTEGRATION_ORACLE_RESULT_COLUMN", "ORDER_NO"),
		},
	}
}

func openReportIntegrationMySQL(t *testing.T, ctx context.Context, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open report integration MySQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("read report integration MySQL handle: %v", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatalf("ping report integration MySQL: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close report integration MySQL: %v", err)
		}
	})
	return db
}

func migrateReportIntegrationModels(t *testing.T, ctx context.Context, db *gorm.DB) {
	t.Helper()
	models := []interface{}{
		&model.ReportDatasource{}, &model.ReportDefinition{}, &model.ReportVersion{}, &model.ReportParameter{},
		&model.ReportColumn{}, &model.ReportGrant{}, &model.ReportRun{}, &model.ReportExport{},
		&model.ReportResultReadLease{}, &model.ReportAudit{}, &model.AsyncJobOutbox{},
	}
	if err := db.WithContext(ctx).AutoMigrate(models...); err != nil {
		t.Fatalf("migrate report integration models: %v", err)
	}
}

func configureReportIntegrationOSS(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	contents := []byte("Storage:\n  Driver: oss\n  OSS:\n    Enabled: true\n    Region: ${ALIYUN_OSS_REGION}\n    Endpoint: ${ALIYUN_OSS_ENDPOINT}\n    Bucket: ${ALIYUN_OSS_BUCKET}\n    UseInternal: ${ALIYUN_OSS_USE_INTERNAL:-false}\n    UseCName: ${ALIYUN_OSS_USE_CNAME:-false}\n    DisableSSL: ${ALIYUN_OSS_DISABLE_SSL:-false}\n    ConnectTimeoutSeconds: ${ALIYUN_OSS_CONNECT_TIMEOUT_SECONDS:-10}\n    ReadWriteTimeoutSeconds: ${ALIYUN_OSS_READ_WRITE_TIMEOUT_SECONDS:-300}\n    EnableCheckpoint: false\n")
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), contents, 0o600); err != nil {
		t.Fatalf("write isolated OSS config: %v", err)
	}
	pkgconfig.NewConfig("", directory+string(os.PathSeparator))
}

func inspectReportIntegrationFixture(t *testing.T, ctx context.Context, cfg reportIntegrationConfig) reportIntegrationFixture {
	t.Helper()
	adapter, err := reportoracle.Open(ctx, reportIntegrationOracleAdapterConfig(cfg.oracle))
	if err != nil {
		t.Fatalf("open Oracle integration fixture: %v", err)
	}
	defer func() {
		if err := adapter.Close(); err != nil {
			t.Errorf("close Oracle fixture inspection: %v", err)
		}
	}()
	procedureRef := reportoracle.ProcedureRef{Owner: cfg.oracle.owner, Package: cfg.oracle.packageName, Name: cfg.oracle.procedure, Overload: cfg.oracle.overload}
	arguments, err := adapter.InspectProcedure(ctx, procedureRef)
	if err != nil {
		t.Fatalf("inspect Oracle integration procedure: %v", err)
	}
	if len(arguments) != 1 || !strings.EqualFold(arguments[0].Name, "P_RUN_ID") || !strings.EqualFold(arguments[0].Direction, "IN") {
		t.Fatalf("integration procedure must expose only IN P_RUN_ID, got %#v", arguments)
	}
	resultRef := reportoracle.ResultTableRef{Owner: cfg.oracle.owner, Name: cfg.oracle.resultTable}
	columns, err := adapter.InspectResultTable(ctx, resultRef)
	if err != nil {
		t.Fatalf("inspect Oracle integration result table: %v", err)
	}
	var resultColumn *reportoracle.ResultColumn
	for index := range columns {
		if strings.EqualFold(columns[index].Name, cfg.oracle.resultColumn) {
			copy := columns[index]
			resultColumn = &copy
			break
		}
	}
	if resultColumn == nil {
		t.Fatalf("Oracle result column %q does not exist", cfg.oracle.resultColumn)
	}
	if _, err := adapter.InspectResultSnapshotContract(ctx, reportoracle.ResultTableSnapshotRef(resultRef, []string{resultColumn.Name})); err != nil {
		t.Fatalf("inspect Oracle integration snapshot contract: %v", err)
	}
	logicalType, ok := reportIntegrationLogicalType(resultColumn.DataType)
	if !ok || logicalType != "string" || strings.EqualFold(resultColumn.DataType, "CLOB") || strings.EqualFold(resultColumn.DataType, "NCLOB") {
		t.Fatalf("Oracle integration result column %q must be a sortable character column, got %q", resultColumn.Name, resultColumn.DataType)
	}
	excelHeader := strings.TrimSpace(os.Getenv("REPORT_INTEGRATION_EXCEL_HEADER"))
	if excelHeader == "" {
		excelHeader = "集成测试字段"
	}
	return reportIntegrationFixture{
		parameter: model.ReportParameter{
			ParameterCode: "runId", Label: "运行编号", DisplayOrder: 1, ControlType: "TEXT", LogicalType: "string",
			Cardinality: "SINGLE", ProcedureArgName: arguments[0].Name, Position: arguments[0].Position,
			Direction: "IN", OracleType: arguments[0].DataType, Required: true, Nullable: false,
			SystemInjected: true, NullPolicy: "TYPED_NULL",
		},
		column: model.ReportColumn{
			FieldID: uuid.NewString(), LogicalCode: "fixtureValue", DatabaseColumn: resultColumn.Name,
			SourceOracleType: resultColumn.DataType, Nullable: resultColumn.Nullable, ValueType: logicalType,
			PreviewHeader: excelHeader, ExcelHeader: excelHeader, DisplayOrder: 1, ExportOrder: 1,
			PreviewVisible: true, ExportVisible: true, Filterable: true, Sortable: true, ExportAllowed: true,
			AllowedOperatorsJSON: model.JSONText(`["EQ"]`), ExcelWidth: 20,
		},
		excelHeader: excelHeader,
	}
}

func reportIntegrationOracleAdapterConfig(cfg reportIntegrationOracleConfig) reportoracle.Config {
	return reportoracle.Config{
		Host: cfg.host, Port: cfg.port, ServiceName: cfg.serviceName, SID: cfg.sid, Username: cfg.username, Password: cfg.password,
		Timezone: cfg.timezone, ConnectTimeout: 10 * time.Second, MaxOpenConnections: 4, MaxIdleConnections: 1,
		PrefetchRows: 100, FetchArraySize: 100,
	}
}

func reportIntegrationLogicalType(oracleType string) (string, bool) {
	typeName := strings.ToUpper(strings.TrimSpace(oracleType))
	switch {
	case typeName == "CHAR", typeName == "NCHAR", typeName == "VARCHAR2", typeName == "NVARCHAR2", typeName == "CLOB", typeName == "NCLOB":
		return "string", true
	case typeName == "NUMBER", typeName == "BINARY_FLOAT", typeName == "BINARY_DOUBLE":
		return "decimal", true
	case typeName == "DATE", strings.HasPrefix(typeName, "TIMESTAMP"):
		return "datetime", true
	default:
		return "", false
	}
}

func reportIntegrationActorID() uint {
	value := uuid.New()
	return uint(value[0])<<24 | uint(value[1])<<16 | uint(value[2])<<8 | uint(value[3]) | 1
}

func configureReportIntegrationSecrets(t *testing.T) reportsecret.EnvironmentKeyring {
	t.Helper()
	credentialKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	parameterKey := base64.StdEncoding.EncodeToString([]byte("abcdef0123456789abcdef0123456789"))
	t.Setenv("REPORT_CREDENTIAL_KEY_VERSION", "integration-v1")
	t.Setenv("REPORT_CREDENTIAL_KEYS_JSON", `{"integration-v1":"`+credentialKey+`"}`)
	t.Setenv("REPORT_PARAMETER_KEY_VERSION", "integration-v1")
	t.Setenv("REPORT_PARAMETER_KEYS_JSON", `{"integration-v1":"`+parameterKey+`"}`)
	if err := (reportsecret.EnvironmentKeyring{}).Validate(); err != nil {
		t.Fatalf("validate integration credential keyring: %v", err)
	}
	if err := (reportsecret.EnvironmentParameterCipher{}).Validate(); err != nil {
		t.Fatalf("validate integration parameter keyring: %v", err)
	}
	return reportsecret.EnvironmentKeyring{}
}

func createReportIntegrationDatasource(t *testing.T, ctx context.Context, repository *reportrepo.Repository, credential reportsecret.EnvironmentKeyring, actor uint, cfg reportIntegrationConfig) model.ReportDatasource {
	t.Helper()
	service := NewReportDatasourceServiceWithDependencies(repository, credential, func(ctx context.Context, config reportoracle.Config) (reportDatasourceConnection, error) {
		return reportoracle.Open(ctx, config)
	})
	connection := requestbody.ReportDatasourceConnectionTestRequest{
		Host: cfg.oracle.host, Port: cfg.oracle.port, ServiceName: cfg.oracle.serviceName, SID: cfg.oracle.sid,
		Username: cfg.oracle.username, Password: cfg.oracle.password, SessionTimezone: cfg.oracle.timezone,
		ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 120,
		MaxOpenConnections: 4, MaxIdleConnections: 1, PrefetchRows: 100, ArraySize: 100,
	}
	tested, err := service.TestConnection(ctx, actor, connection)
	if err != nil || tested.Status != reportDatasourceTestSuccess {
		t.Fatalf("test report integration datasource draft: result=%#v error=%v", tested, err)
	}
	created, err := service.Create(ctx, actor, requestbody.ReportDatasourceSaveRequest{
		Code: "integration_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20], Name: "报表中心集成测试",
		Host: connection.Host, Port: connection.Port, ServiceName: connection.ServiceName, SID: connection.SID,
		Username: connection.Username, Password: connection.Password, SessionTimezone: connection.SessionTimezone,
		ConnectTimeoutSeconds: connection.ConnectTimeoutSeconds, QueryTimeoutSeconds: connection.QueryTimeoutSeconds,
		MaxOpenConnections: connection.MaxOpenConnections, MaxIdleConnections: connection.MaxIdleConnections,
		PrefetchRows: connection.PrefetchRows, ArraySize: connection.ArraySize, Enabled: true,
	})
	if err != nil {
		t.Fatalf("save report integration datasource: %v", err)
	}
	datasource, err := repository.GetReportDatasource(ctx, created.ID)
	if err != nil {
		t.Fatalf("reload report integration datasource: %v", err)
	}
	if datasource.PasswordCiphertext == "" || datasource.PasswordCiphertext == cfg.oracle.password || datasource.CredentialKeyVersion != "integration-v1" {
		t.Fatal("integration datasource credential was not encrypted")
	}
	tested, err = service.Test(ctx, actor, datasource.ID)
	if err != nil || tested.Status != reportDatasourceTestSuccess {
		t.Fatalf("test saved report integration datasource: result=%#v error=%v", tested, err)
	}
	return *datasource
}

func createReportIntegrationDraft(t *testing.T, ctx context.Context, repository *reportrepo.Repository, actor, datasourceID uint, cfg reportIntegrationConfig, fixture reportIntegrationFixture) *reportrepo.Draft {
	t.Helper()
	target := strings.ToUpper(cfg.oracle.owner) + "." + strings.ToUpper(cfg.oracle.packageName) + "." + strings.ToUpper(cfg.oracle.procedure)
	draft := &reportrepo.Draft{
		Definition: model.ReportDefinition{Code: "integration_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20], Name: "报表中心全链路集成测试", DatasourceID: datasourceID, OwnerUserID: actor, CreatedBy: actor, UpdatedBy: actor},
		Version: model.ReportVersion{
			DatasourceID: datasourceID, ProcedureOwner: cfg.oracle.owner, PackageName: cfg.oracle.packageName,
			ProcedureName: cfg.oracle.procedure, ProcedureOverload: cfg.oracle.overload,
			ResultTableOwner: cfg.oracle.owner, ResultTableName: cfg.oracle.resultTable,
			CallTemplate: fmt.Sprintf("BEGIN %s(P_RUN_ID => {{runId}}); END;", target), CreatedBy: actor,
		},
		Parameters: []model.ReportParameter{fixture.parameter}, Columns: []model.ReportColumn{fixture.column}, Grants: []model.ReportGrant{},
	}
	if err := repository.CreateDraft(ctx, actor, draft); err != nil {
		t.Fatalf("create report integration draft: %v", err)
	}
	return draft
}

type integrationParameterCipher struct{}

func (integrationParameterCipher) Encrypt([]byte) (string, string, error) {
	return "", "", fmt.Errorf("integration test does not configure sensitive parameters")
}

type integrationParameterDecryptor struct{}

func (integrationParameterDecryptor) Decrypt(_, _ string) ([]byte, error) {
	return nil, fmt.Errorf("integration test does not configure sensitive parameters")
}

func executeReportIntegrationRun(t *testing.T, ctx context.Context, db *gorm.DB, repository *reportrepo.Repository, credential reportDatasourceCredentialDecryptor, actor, definitionID uint, resources *reportIntegrationResources) model.ReportRun {
	t.Helper()
	service := NewReportRunServiceWithDependencies(repository, integrationParameterCipher{})
	created, err := service.Create(ctx, actor, definitionID, requestbody.ReportRunCreateRequest{Parameters: map[string]json.RawMessage{}})
	if err != nil {
		t.Fatalf("create report integration run: %v", err)
	}
	resources.runIDs = append(resources.runIDs, created.ID)
	resources.taskKeys = append(resources.taskKeys, "report:run:"+created.RunUUID)
	processor := NewReportRunProcessorWithDependencies(repository, credential, integrationParameterDecryptor{}, oracleReportProcedureExecutor{})
	processor.heartbeatInterval = time.Hour
	if err := processor.Process(ctx, created.ID, 0, false); err != nil {
		t.Fatalf("execute report integration run: %v", err)
	}
	run := loadReportIntegrationRun(t, ctx, db, created.ID)
	if run.Status != model.ReportRunStatusSucceeded || run.RowCount < 3 || run.RowCount > reportIntegrationMaximumRows {
		t.Fatalf("integration procedure must create 3..%d rows, got status=%s rows=%d", reportIntegrationMaximumRows, run.Status, run.RowCount)
	}
	return run
}

func assertReportIntegrationQueries(t *testing.T, ctx context.Context, repository *reportrepo.Repository, credential reportDatasourceCredentialDecryptor, actor uint, run model.ReportRun, fieldID string) string {
	t.Helper()
	service := NewReportRunQueryServiceWithDependencies(repository, credential, oracleReportResultPageReader{}, []byte("report-integration-cursor-key"))
	defaultValues := readReportIntegrationQueryPages(t, ctx, service, actor, run.ID, reportquery.Input{}, run.RowCount, run.RowCount)
	firstValue := defaultValues[0]
	encodedFirstValue, err := json.Marshal(firstValue)
	if err != nil {
		t.Fatalf("encode integration filter value: %v", err)
	}
	filteredValues := readReportIntegrationQueryPages(t, ctx, service, actor, run.ID, reportquery.Input{Filters: []reportquery.FilterInput{{
		Field: fieldID, Operator: "EQ", Value: encodedFirstValue,
	}}}, run.RowCount, 0)
	for _, value := range filteredValues {
		if value != firstValue {
			t.Fatalf("EQ filter returned %q, want %q", value, firstValue)
		}
	}
	descendingValues := readReportIntegrationQueryPages(t, ctx, service, actor, run.ID, reportquery.Input{Sort: []reportquery.SortInput{{
		Field: fieldID, Direction: "DESC",
	}}}, run.RowCount, run.RowCount)
	for index := 1; index < len(descendingValues); index++ {
		if descendingValues[index-1] < descendingValues[index] {
			t.Fatalf("DESC sort values are not monotonic at %d: %#v", index, descendingValues)
		}
	}
	return firstValue
}

func readReportIntegrationQueryPages(t *testing.T, ctx context.Context, service *ReportRunQueryService, actor, runID uint, input reportquery.Input, maximumRows, exactRows int64) []string {
	t.Helper()
	cursor := ""
	seen := make(map[string]struct{}, maximumRows)
	values := make([]string, 0, maximumRows)
	for {
		page, err := service.QueryResults(ctx, actor, runID, input, cursor, 2)
		if err != nil {
			t.Fatalf("read report integration page: %v", err)
		}
		for _, row := range page.Rows {
			if _, duplicate := seen[row.Key]; duplicate {
				t.Fatalf("keyset pagination returned duplicate row %s", row.Key)
			}
			seen[row.Key] = struct{}{}
			value, ok := row.Values["fixtureValue"].(string)
			if !ok || value == "" {
				t.Fatalf("business value = %#v, want non-empty string", row.Values["fixtureValue"])
			}
			values = append(values, value)
		}
		if !page.Pagination.HasMore {
			break
		}
		if page.Pagination.NextCursor == "" || page.Pagination.NextCursor == cursor {
			t.Fatalf("keyset pagination did not advance cursor")
		}
		cursor = page.Pagination.NextCursor
	}
	if len(values) == 0 || int64(len(values)) > maximumRows {
		t.Fatalf("query rows = %d, want 1..%d", len(values), maximumRows)
	}
	if exactRows > 0 && int64(len(values)) != exactRows {
		t.Fatalf("query rows = %d, want %d", len(values), exactRows)
	}
	return values
}

func createReportIntegrationExport(t *testing.T, ctx context.Context, repository *reportrepo.Repository, actor, runID uint, resources *reportIntegrationResources) model.ReportExport {
	t.Helper()
	export := createOrReuseReportIntegrationExport(t, ctx, repository, actor, runID)
	resources.exportIDs = append(resources.exportIDs, export.ID)
	resources.taskKeys = append(resources.taskKeys, "report:export:"+export.ExportUUID)
	return export
}

func createOrReuseReportIntegrationExport(t *testing.T, ctx context.Context, repository *reportrepo.Repository, actor, runID uint) model.ReportExport {
	t.Helper()
	service := NewReportExportServiceWithStore(repository)
	created, _, err := service.Create(ctx, actor, runID, reportquery.Input{})
	if err != nil {
		t.Fatalf("create or reuse report integration export: %v", err)
	}
	return model.ReportExport{BaseModel: model.BaseModel{ID: created.ID}, ExportUUID: created.ExportUUID, RunID: created.RunID, Status: created.Status}
}

func newReportIntegrationExportProcessor(t *testing.T, repository *reportrepo.Repository, credential reportDatasourceCredentialDecryptor, ossBase string, objectStore func() (reportExportObjectStore, error), resources *reportIntegrationResources) *ReportExportProcessor {
	t.Helper()
	processor := NewReportExportProcessorWithDependencies(repository, credential, oracleReportExportSessionFactory{})
	processor.workerID = "report-integration-" + uuid.NewString()
	processor.workRoot = shortReportIntegrationTempRoot(t)
	processor.heartbeatInterval = time.Hour
	processor.newObjectStore = objectStore
	processor.buildObjectKey = func(parts ...string) string {
		key := path.Join(append([]string{ossBase}, parts...)...)
		resources.objectKeys = append(resources.objectKeys, key)
		return key
	}
	return processor
}

func shortReportIntegrationTempRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/private/tmp", "rci-")
	if err != nil {
		root, err = os.MkdirTemp("", "rci-")
	}
	if err != nil {
		t.Fatalf("create report integration work root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove report integration work root: %v", err)
		}
	})
	return root
}

type reportIntegrationFailingUploadStore struct {
	delegate      *storage.OSSClient
	attemptedKeys []string
}

func (store *reportIntegrationFailingUploadStore) UploadFile(_ context.Context, objectKey, _, _ string) (storage.UploadResult, error) {
	store.attemptedKeys = append(store.attemptedKeys, objectKey)
	return storage.UploadResult{}, errors.New("injected integration upload failure")
}

func (store *reportIntegrationFailingUploadStore) StatDownloadObject(ctx context.Context, objectKey string) (storage.ObjectMetadata, error) {
	return store.delegate.StatDownloadObject(ctx, objectKey)
}

func (store *reportIntegrationFailingUploadStore) DeleteObject(ctx context.Context, objectKey string) error {
	return store.delegate.DeleteObject(ctx, objectKey)
}

func assertReportIntegrationWorkbook(t *testing.T, ctx context.Context, client *storage.OSSClient, export model.ReportExport, expectedHeader, expectedFirstValue string, expectedRows int64) {
	t.Helper()
	object, err := client.OpenDownloadObject(ctx, export.ResultObjectKey)
	if err != nil {
		t.Fatalf("download exported workbook: %v", err)
	}
	defer object.Body.Close()
	if object.Size != export.FileSizeBytes {
		t.Fatalf("download size = %d, want %d", object.Size, export.FileSizeBytes)
	}
	file, err := os.CreateTemp(t.TempDir(), "report-integration-*.xlsx")
	if err != nil {
		t.Fatalf("create downloaded workbook: %v", err)
	}
	filePath := file.Name()
	if _, err := io.Copy(file, object.Body); err != nil {
		_ = file.Close()
		t.Fatalf("save downloaded workbook: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close downloaded workbook: %v", err)
	}
	workbook, err := excelize.OpenFile(filePath)
	if err != nil {
		t.Fatalf("open downloaded workbook: %v", err)
	}
	defer workbook.Close()
	rows, err := workbook.GetRows("数据")
	if err != nil {
		t.Fatalf("read downloaded workbook: %v", err)
	}
	if len(rows) == 0 || len(rows[0]) == 0 || rows[0][0] != expectedHeader {
		t.Fatalf("workbook header rows = %#v, want %q", rows, expectedHeader)
	}
	if int64(len(rows)-1) != expectedRows {
		t.Fatalf("workbook data rows = %d, want %d", len(rows)-1, expectedRows)
	}
	if len(rows) < 2 || len(rows[1]) == 0 || rows[1][0] != expectedFirstValue {
		t.Fatalf("workbook first business value = %#v, want %q", rows, expectedFirstValue)
	}
}

func loadReportIntegrationRun(t *testing.T, ctx context.Context, db *gorm.DB, runID uint) model.ReportRun {
	t.Helper()
	var run model.ReportRun
	if err := db.WithContext(ctx).First(&run, runID).Error; err != nil {
		t.Fatalf("load report integration run: %v", err)
	}
	return run
}

func loadReportIntegrationExport(t *testing.T, ctx context.Context, db *gorm.DB, exportID uint) model.ReportExport {
	t.Helper()
	var export model.ReportExport
	if err := db.WithContext(ctx).First(&export, exportID).Error; err != nil {
		t.Fatalf("load report integration export: %v", err)
	}
	return export
}

func countReportIntegrationOracleRows(t *testing.T, ctx context.Context, cfg reportIntegrationConfig, fixture reportIntegrationFixture, reportID uint) int64 {
	t.Helper()
	adapter, err := reportoracle.Open(ctx, reportIntegrationOracleAdapterConfig(cfg.oracle))
	if err != nil {
		t.Fatalf("open Oracle for integration count: %v", err)
	}
	defer adapter.Close()
	contract, err := adapter.InspectResultSnapshotContract(ctx, reportoracle.ResultTableSnapshotRef(
		reportoracle.ResultTableRef{Owner: cfg.oracle.owner, Name: cfg.oracle.resultTable}, []string{fixture.column.DatabaseColumn},
	))
	if err != nil {
		t.Fatalf("inspect Oracle contract for count: %v", err)
	}
	plan, err := reportoracle.BuildResultCountPlan(contract)
	if err != nil {
		t.Fatalf("build Oracle integration count: %v", err)
	}
	count, err := adapter.CountResultRows(ctx, plan)
	if err != nil {
		t.Fatalf("count Oracle integration rows: %v", err)
	}
	return count
}

func cleanupReportIntegration(t *testing.T, cfg reportIntegrationConfig, db *gorm.DB, fixture reportIntegrationFixture, resources *reportIntegrationResources) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if resources.definitionID != 0 {
		if err := purgeReportIntegrationOracleRows(ctx, cfg, fixture, resources.definitionID); err != nil {
			t.Errorf("clean Oracle rows for report %d: %v", resources.definitionID, err)
		}
	}
	if len(resources.objectKeys) > 0 {
		client, err := storage.NewOSSClientFromConfig()
		if err != nil {
			t.Errorf("open OSS for integration cleanup: %v", err)
		} else {
			for _, objectKey := range uniqueReportIntegrationStrings(resources.objectKeys) {
				if err := client.DeleteObject(ctx, objectKey); err != nil {
					t.Errorf("delete integration object %s: %v", objectKey, err)
				}
			}
		}
	}
	cleanupReportIntegrationMySQL(t, ctx, db, resources)
}

func purgeReportIntegrationOracleRows(ctx context.Context, cfg reportIntegrationConfig, fixture reportIntegrationFixture, reportID uint) error {
	adapter, err := reportoracle.Open(ctx, reportIntegrationOracleAdapterConfig(cfg.oracle))
	if err != nil {
		return err
	}
	defer adapter.Close()
	contract, err := adapter.InspectResultSnapshotContract(ctx, reportoracle.ResultTableSnapshotRef(
		reportoracle.ResultTableRef{Owner: cfg.oracle.owner, Name: cfg.oracle.resultTable}, []string{fixture.column.DatabaseColumn},
	))
	if err != nil {
		return err
	}
	plan, err := reportoracle.BuildPurgePlan(contract)
	if err != nil {
		return err
	}
	for {
		tx, err := adapter.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		deleted, err := adapter.PurgeResultBatch(ctx, tx, plan, 1000)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if deleted < 1000 {
			return nil
		}
	}
}

func cleanupReportIntegrationMySQL(t *testing.T, ctx context.Context, db *gorm.DB, resources *reportIntegrationResources) {
	t.Helper()
	deleteWhere := func(modelValue interface{}, query string, arguments ...interface{}) {
		if err := db.WithContext(ctx).Where(query, arguments...).Delete(modelValue).Error; err != nil {
			t.Errorf("clean MySQL %T: %v", modelValue, err)
		}
	}
	if len(resources.taskKeys) > 0 {
		deleteWhere(&model.AsyncJobOutbox{}, "task_key IN ?", uniqueReportIntegrationStrings(resources.taskKeys))
	}
	if len(resources.runIDs) > 0 {
		deleteWhere(&model.ReportResultReadLease{}, "run_id IN ?", resources.runIDs)
	}
	if len(resources.exportIDs) > 0 {
		deleteWhere(&model.ReportAudit{}, "target_type = ? AND target_id IN ?", "REPORT_EXPORT", resources.exportIDs)
		deleteWhere(&model.ReportExport{}, "id IN ?", resources.exportIDs)
	}
	if len(resources.runIDs) > 0 {
		deleteWhere(&model.ReportAudit{}, "target_type = ? AND target_id IN ?", "REPORT_RUN", resources.runIDs)
		deleteWhere(&model.ReportRun{}, "id IN ?", resources.runIDs)
	}
	if resources.definitionID != 0 {
		deleteWhere(&model.ReportAudit{}, "target_type = ? AND target_id = ?", "REPORT_DEFINITION", resources.definitionID)
		var versionIDs []uint
		if err := db.WithContext(ctx).Model(&model.ReportVersion{}).Where("definition_id = ?", resources.definitionID).Pluck("id", &versionIDs).Error; err != nil {
			t.Errorf("list integration report versions: %v", err)
		} else if len(versionIDs) > 0 {
			deleteWhere(&model.ReportGrant{}, "version_id IN ?", versionIDs)
			deleteWhere(&model.ReportColumn{}, "version_id IN ?", versionIDs)
			deleteWhere(&model.ReportParameter{}, "version_id IN ?", versionIDs)
			deleteWhere(&model.ReportVersion{}, "id IN ?", versionIDs)
		}
		deleteWhere(&model.ReportDefinition{}, "id = ?", resources.definitionID)
	}
	if resources.datasourceID != 0 {
		deleteWhere(&model.ReportAudit{}, "target_type = ? AND target_id = ?", "REPORT_DATASOURCE", resources.datasourceID)
		deleteWhere(&model.ReportDatasource{}, "id = ?", resources.datasourceID)
	}
}

func uniqueReportIntegrationStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
