package data_svc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/model"
)

func TestReportDatasourceServiceCreateEncryptsAndRedactsPassword(t *testing.T) {
	store := &fakeReportDatasourceStore{}
	cipher := &fakeReportDatasourceCipher{}
	service := NewReportDatasourceServiceWithDependencies(store, cipher, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) { return nil, nil })
	result, err := service.Create(t.Context(), 7, validReportDatasourceRequest("secret-password"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if cipher.encrypted != "secret-password" || store.created == nil || store.created.PasswordCiphertext != "ciphertext" || store.created.CredentialKeyVersion != "key-v1" {
		t.Fatalf("credential was not encrypted before persistence: cipher=%q datasource=%+v", cipher.encrypted, store.created)
	}
	if !result.HasPassword || strings.Contains(result.Host+result.Username+result.LastTestError, "secret-password") {
		t.Fatalf("Create() leaked or lost password state: %+v", result)
	}
}

func TestReportDatasourceServiceCreateClassifiesInvalidCredentialConfiguration(t *testing.T) {
	store := &fakeReportDatasourceStore{}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{err: reportsecret.ErrInvalidCredential}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) { return nil, nil })
	if _, err := service.Create(t.Context(), 7, validReportDatasourceRequest("secret-password")); !errors.Is(err, ErrReportDatasourceCredentialUnavailable) {
		t.Fatalf("Create() error = %v, want ErrReportDatasourceCredentialUnavailable", err)
	}
	if store.created != nil {
		t.Fatal("invalid credential configuration reached MySQL persistence")
	}
}

func TestReportDatasourceServiceUpdateKeepsCredentialWhenPasswordOmitted(t *testing.T) {
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{BaseModel: model.BaseModel{ID: 4}, Code: "report_oracle", Driver: model.ReportDatasourceDriverOracle, PasswordCiphertext: "existing", CredentialKeyVersion: "old"}}
	cipher := &fakeReportDatasourceCipher{}
	service := NewReportDatasourceServiceWithDependencies(store, cipher, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) { return nil, nil })
	request := validReportDatasourceRequest("")
	result, err := service.Update(t.Context(), 7, 4, request)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if cipher.encrypted != "" || store.updated == nil || store.updated.PasswordCiphertext != "" || !result.HasPassword {
		t.Fatalf("Update() unexpectedly rotated credential: cipher=%q updated=%+v result=%+v", cipher.encrypted, store.updated, result)
	}
}

func TestReportDatasourceServiceTestReturnsOnlySafeFailure(t *testing.T) {
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{BaseModel: model.BaseModel{ID: 4}, Driver: model.ReportDatasourceDriverOracle, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", ConnectTimeoutSeconds: 5}}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return nil, errors.New("ORA-01017 password=secret-password host=private.example")
	})
	start := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	service.now = func() time.Time { start = start.Add(25 * time.Millisecond); return start }
	result, err := service.Test(t.Context(), 7, 4)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if result.Status != reportDatasourceTestFailed || result.ErrorCode != "AUTHENTICATION_FAILED" || strings.Contains(result.Message+store.testError, "secret-password") || store.testStatus != reportDatasourceTestFailed {
		t.Fatalf("Test() result = %+v, stored=%q/%q", result, store.testStatus, store.testError)
	}
}

func TestReportDatasourceConnectionFailureClassifiesSafeOperationalErrors(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      error
		wantCode string
		wantText string
	}{
		{name: "missing instant client", err: errors.New("DPI-1047: cannot locate a 64-bit Oracle Client library: password=secret"), wantCode: "ORACLE_CLIENT_UNAVAILABLE", wantText: "服务端 Oracle 客户端不可用，请联系管理员"},
		{name: "locked account", err: errors.New("ORA-28000: the account is locked"), wantCode: "ACCOUNT_LOCKED", wantText: "Oracle 账号已锁定"},
		{name: "expired password", err: errors.New("ORA-28001: the password has expired"), wantCode: "PASSWORD_EXPIRED", wantText: "Oracle 密码已过期"},
		{name: "unknown sid", err: errors.New("ORA-12505: listener does not currently know of SID"), wantCode: "SERVICE_NOT_FOUND", wantText: "Oracle 服务名或 SID 不可用"},
		{name: "unreachable network", err: errors.New("dial tcp: connect: network is unreachable"), wantCode: "NETWORK_UNREACHABLE", wantText: "无法连接 Oracle 网络地址"},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, message := safeDatasourceConnectionFailure(test.err)
			if code != test.wantCode || message != test.wantText || strings.Contains(message, "secret") {
				t.Fatalf("safeDatasourceConnectionFailure() = %q, %q", code, message)
			}
		})
	}
}

func TestReportDatasourceServiceTestsUnsavedConnectionDraft(t *testing.T) {
	store := &fakeReportDatasourceStore{}
	var opened reportoracle.Config
	connection := &fakeReportDatasourceConnection{}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(_ context.Context, config reportoracle.Config) (reportDatasourceConnection, error) {
		opened = config
		return connection, nil
	})
	request := validReportDatasourceConnectionTestRequest("draft-password")
	result, err := service.TestConnection(t.Context(), 7, request)
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Status != reportDatasourceTestSuccess || opened.Host != "draft.oracle.internal" || opened.ServiceName != "DRAFT" || opened.Username != "draft_user" || opened.Password != "draft-password" {
		t.Fatalf("TestConnection() result=%+v config=%+v", result, opened)
	}
	if store.created != nil || store.updated != nil || store.testStatus != "" {
		t.Fatalf("unsaved draft test mutated MySQL store: %+v", store)
	}
	if !connection.closed {
		t.Fatal("successful draft connection was not closed")
	}
}

func TestReportDatasourceServiceConnectionDraftClosesConnectionAndMapsTimeout(t *testing.T) {
	connection := &fakeReportDatasourceConnection{}
	service := NewReportDatasourceServiceWithDependencies(&fakeReportDatasourceStore{}, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return connection, context.DeadlineExceeded
	})
	result, err := service.TestConnection(t.Context(), 7, validReportDatasourceConnectionTestRequest("draft-password"))
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Status != reportDatasourceTestFailed || result.ErrorCode != "CONNECT_TIMEOUT" || !connection.closed {
		t.Fatalf("TestConnection() result=%+v closed=%t", result, connection.closed)
	}
}

func TestReportDatasourceServiceConnectionDraftReusesExistingPassword(t *testing.T) {
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{BaseModel: model.BaseModel{ID: 4}, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1"}}
	var opened reportoracle.Config
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(_ context.Context, config reportoracle.Config) (reportDatasourceConnection, error) {
		opened = config
		return &fakeReportDatasourceConnection{}, nil
	})
	request := validReportDatasourceConnectionTestRequest("")
	request.DatasourceID = 4
	result, err := service.TestConnection(t.Context(), 7, request)
	if err != nil || result.Status != reportDatasourceTestSuccess || opened.Password != "secret-password" || opened.Host != "draft.oracle.internal" {
		t.Fatalf("TestConnection() result=%+v error=%v config=%+v", result, err, opened)
	}
}

func TestReportDatasourceServiceConnectionDraftRequiresPasswordOrDatasource(t *testing.T) {
	service := NewReportDatasourceServiceWithDependencies(&fakeReportDatasourceStore{}, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) { return nil, nil })
	if _, err := service.TestConnection(t.Context(), 7, validReportDatasourceConnectionTestRequest("")); !errors.Is(err, ErrReportDatasourceInvalid) {
		t.Fatalf("TestConnection() error = %v, want ErrReportDatasourceInvalid", err)
	}
}

func TestReportDatasourceRequestRequiresExactlyOneOracleServiceSelector(t *testing.T) {
	for _, test := range []struct {
		name        string
		serviceName string
		sid         string
		valid       bool
	}{{name: "service name", serviceName: "REPORT", valid: true}, {name: "sid", sid: "REPORT", valid: true}, {name: "missing", valid: false}, {name: "both", serviceName: "REPORT", sid: "REPORT", valid: false}} {
		t.Run(test.name, func(t *testing.T) {
			request := validReportDatasourceRequest("password")
			request.ServiceName, request.SID = test.serviceName, test.sid
			_, err := reportDatasourceFromRequest(request, true)
			if (err == nil) != test.valid {
				t.Fatalf("reportDatasourceFromRequest() error = %v, valid=%t", err, test.valid)
			}
		})
	}
}

func TestReportDatasourceWhitespacePasswordPolicyIsExplicit(t *testing.T) {
	request := validReportDatasourceRequest("   ")
	if _, err := reportDatasourceFromRequest(request, true); err != nil {
		t.Fatalf("Oracle passwords containing only spaces are valid credentials and must not be trimmed: %v", err)
	}
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{BaseModel: model.BaseModel{ID: 4}, Driver: model.ReportDatasourceDriverOracle, PasswordCiphertext: "existing", CredentialKeyVersion: "old"}}
	cipher := &fakeReportDatasourceCipher{}
	service := NewReportDatasourceServiceWithDependencies(store, cipher, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) { return nil, nil })
	if _, err := service.Update(t.Context(), 7, 4, request); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if cipher.encrypted != "   " || store.updated == nil || store.updated.PasswordCiphertext == "" {
		t.Fatalf("space-only Oracle password was not intentionally rotated: cipher=%q updated=%+v", cipher.encrypted, store.updated)
	}
}

func TestReportDatasourceServiceListsVisibleProceduresWithOpaqueCursor(t *testing.T) {
	refs := []reportoracle.ProcedureRef{
		{Owner: "REPORT", Package: "PKG_SALES", Name: "BUILD_DAILY", Overload: "1"},
		{Owner: "REPORT", Package: "PKG_SALES", Name: "BUILD_MONTHLY"},
		{Owner: "REPORT", Name: "REFRESH_ALL"},
	}
	procedures := make([]reportoracle.ProcedureSummary, 0, len(refs))
	for index, ref := range refs {
		cursor, err := reportoracle.ProcedureCursorKey(ref)
		if err != nil {
			t.Fatal(err)
		}
		procedures = append(procedures, reportoracle.ProcedureSummary{ProcedureRef: ref, ArgumentCount: index + 1, CursorKey: cursor})
	}
	connection := &fakeReportDatasourceConnection{procedures: procedures}
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{
		BaseModel: model.BaseModel{ID: 4}, Enabled: true, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", QueryTimeoutSeconds: 30,
	}}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return connection, nil
	})
	page, err := service.ListProcedures(t.Context(), 7, 4, ReportProcedureCatalogQuery{Owner: "report", Search: "daily", Limit: 2})
	if err != nil {
		t.Fatalf("ListProcedures() error = %v", err)
	}
	if len(page.Items) != 2 || !page.HasMore || page.NextAfter == "" || page.Items[0].QualifiedName != "REPORT.PKG_SALES.BUILD_DAILY #1" {
		t.Fatalf("ListProcedures() page = %+v", page)
	}
	if connection.query.Owner != "report" || connection.query.Search != "daily" || connection.query.Limit != 3 || !connection.closed {
		t.Fatalf("metadata query = %+v closed=%t", connection.query, connection.closed)
	}
	if _, err := service.ListProcedures(t.Context(), 7, 4, ReportProcedureCatalogQuery{After: "not-base64***", Limit: 2}); !errors.Is(err, ErrReportDatasourceInvalid) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}

func TestReportDatasourceServiceListsVisibleResultTablesWithOpaqueCursor(t *testing.T) {
	refs := []reportoracle.ResultTableRef{
		{Owner: "REPORT", Name: "DAILY_RESULT_ROWS"},
		{Owner: "REPORT", Name: "MONTHLY_RESULT_ROWS"},
		{Owner: "REPORT", Name: "YEARLY_RESULT_ROWS"},
	}
	tables := make([]reportoracle.ResultTableSummary, 0, len(refs))
	for index, ref := range refs {
		cursor, err := reportoracle.ResultTableCursorKey(ref)
		if err != nil {
			t.Fatal(err)
		}
		tables = append(tables, reportoracle.ResultTableSummary{ResultTableRef: ref, ColumnCount: index + 3, CursorKey: cursor})
	}
	connection := &fakeReportDatasourceConnection{resultTables: tables}
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{
		BaseModel: model.BaseModel{ID: 4}, Enabled: true, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", QueryTimeoutSeconds: 30,
	}}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return connection, nil
	})
	page, err := service.ListResultTables(t.Context(), 7, 4, ReportResultTableCatalogQuery{Owner: "report", Search: "result", Limit: 2})
	if err != nil {
		t.Fatalf("ListResultTables() error = %v", err)
	}
	if len(page.Items) != 2 || !page.HasMore || page.NextAfter == "" || page.Items[0].QualifiedName != "REPORT.DAILY_RESULT_ROWS" || page.Items[0].ColumnCount != 3 {
		t.Fatalf("ListResultTables() page = %+v", page)
	}
	if connection.resultTableQuery.Owner != "report" || connection.resultTableQuery.Search != "result" || connection.resultTableQuery.Limit != 3 || !connection.closed {
		t.Fatalf("metadata query = %+v closed=%t", connection.resultTableQuery, connection.closed)
	}
	store.managed = map[reportoracle.ResultTableRef]bool{refs[0]: true, refs[1]: true}
	filtered, err := service.ListResultTables(t.Context(), 7, 4, ReportResultTableCatalogQuery{Limit: 2})
	if err != nil || len(filtered.Items) != 0 || filtered.NextAfter != page.NextAfter {
		t.Fatalf("filtered page: %#v, %v", filtered, err)
	}
	if _, err := service.GetResultTableSchema(t.Context(), 7, 4, refs[0]); !errors.Is(err, ErrReportDatasourceInvalid) {
		t.Fatalf("expected managed schema rejection, got %v", err)
	}
	if _, err := service.ListResultTables(t.Context(), 7, 4, ReportResultTableCatalogQuery{After: "not-base64***", Limit: 2}); !errors.Is(err, ErrReportDatasourceInvalid) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}

func TestReportDatasourceServiceReturnsPhysicalRefsForCrossSchemaSynonymSearch(t *testing.T) {
	procedureRef := reportoracle.ProcedureRef{Owner: "APP_OWNER", Package: "PKG_REPORT", Name: "BUILD_DAILY"}
	procedureCursor, err := reportoracle.ProcedureCursorKey(procedureRef)
	if err != nil {
		t.Fatal(err)
	}
	tableRef := reportoracle.ResultTableRef{Owner: "APP_OWNER", Name: "DAILY_RESULT_ROWS"}
	tableCursor, err := reportoracle.ResultTableCursorKey(tableRef)
	if err != nil {
		t.Fatal(err)
	}
	connection := &fakeReportDatasourceConnection{
		procedures:   []reportoracle.ProcedureSummary{{ProcedureRef: procedureRef, ArgumentCount: 1, CursorKey: procedureCursor}},
		resultTables: []reportoracle.ResultTableSummary{{ResultTableRef: tableRef, ColumnCount: 3, CursorKey: tableCursor}},
	}
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{
		BaseModel: model.BaseModel{ID: 4}, Enabled: true, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", QueryTimeoutSeconds: 30,
	}}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return connection, nil
	})

	procedures, err := service.ListProcedures(t.Context(), 7, 4, ReportProcedureCatalogQuery{Owner: "APP_OWNER", Search: "REPORT_ALIAS.BUILD_DAILY", Limit: 10})
	if err != nil {
		t.Fatalf("ListProcedures() error = %v", err)
	}
	if len(procedures.Items) != 1 || procedures.Items[0].Owner != "APP_OWNER" || procedures.Items[0].Package != "PKG_REPORT" ||
		procedures.Items[0].Name != "BUILD_DAILY" || procedures.Items[0].QualifiedName != "APP_OWNER.PKG_REPORT.BUILD_DAILY" {
		t.Fatalf("physical procedure result = %+v", procedures)
	}
	if connection.query.Owner != "APP_OWNER" || connection.query.Search != "REPORT_ALIAS.BUILD_DAILY" {
		t.Fatalf("procedure query = %+v", connection.query)
	}

	tables, err := service.ListResultTables(t.Context(), 7, 4, ReportResultTableCatalogQuery{Owner: "APP_OWNER", Search: "RESULT_ALIAS", Limit: 10})
	if err != nil {
		t.Fatalf("ListResultTables() error = %v", err)
	}
	if len(tables.Items) != 1 || tables.Items[0].Owner != "APP_OWNER" || tables.Items[0].Name != "DAILY_RESULT_ROWS" ||
		tables.Items[0].QualifiedName != "APP_OWNER.DAILY_RESULT_ROWS" {
		t.Fatalf("physical result table = %+v", tables)
	}
	if connection.resultTableQuery.Owner != "APP_OWNER" || connection.resultTableQuery.Search != "RESULT_ALIAS" {
		t.Fatalf("result table query = %+v", connection.resultTableQuery)
	}
}

func TestReportDatasourceServiceReturnsResultTableSchema(t *testing.T) {
	precision, scale := int64(18), int64(0)
	connection := &fakeReportDatasourceConnection{resultColumns: []reportoracle.ResultColumn{
		{Name: "RUN_ID", Position: 1, DataType: "VARCHAR2", DataLength: 36, Nullable: false},
		{Name: "ID", Position: 2, DataType: "NUMBER", DataLength: 22, DataPrecision: &precision, DataScale: &scale, Nullable: false},
		{Name: "STORE_NAME", Position: 3, DataType: "NVARCHAR2", DataLength: 400, Nullable: true},
	}}
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{
		BaseModel: model.BaseModel{ID: 4}, Enabled: true, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", QueryTimeoutSeconds: 30,
	}}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return connection, nil
	})
	schema, err := service.GetResultTableSchema(t.Context(), 7, 4, reportoracle.ResultTableRef{Owner: "report", Name: "daily_result_rows"})
	if err != nil {
		t.Fatalf("GetResultTableSchema() error = %v", err)
	}
	if schema.Table.QualifiedName != "REPORT.DAILY_RESULT_ROWS" || schema.Table.ColumnCount != 3 || len(schema.Columns) != 3 || schema.Columns[1].OracleType != "NUMBER" || schema.Columns[1].Precision == nil || *schema.Columns[1].Precision != 18 {
		t.Fatalf("GetResultTableSchema() = %+v", schema)
	}
	if connection.resultTableRef.Owner != "REPORT" || connection.resultTableRef.Name != "DAILY_RESULT_ROWS" || !connection.closed {
		t.Fatalf("schema ref=%+v closed=%t", connection.resultTableRef, connection.closed)
	}
}

func TestReportDatasourceServiceClassifiesResultTableMetadataErrors(t *testing.T) {
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{
		BaseModel: model.BaseModel{ID: 4}, Enabled: true, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", QueryTimeoutSeconds: 30,
	}}

	missing := &fakeReportDatasourceConnection{metadataErr: reportoracle.ErrMetadataMismatch}
	missingService := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return missing, nil
	})
	if _, err := missingService.GetResultTableSchema(t.Context(), 7, 4, reportoracle.ResultTableRef{Owner: "REPORT", Name: "MISSING_ROWS"}); !errors.Is(err, ErrReportDatasourceNotFound) {
		t.Fatalf("missing schema error = %v", err)
	}
	if !missing.closed {
		t.Fatal("missing schema connection was not closed")
	}

	unavailable := &fakeReportDatasourceConnection{metadataErr: errors.New("ORA-03113: end-of-file on communication channel")}
	unavailableService := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return unavailable, nil
	})
	if _, err := unavailableService.ListResultTables(t.Context(), 7, 4, ReportResultTableCatalogQuery{Limit: 20}); !errors.Is(err, ErrReportDatasourceOracleUnavailable) {
		t.Fatalf("unavailable catalog error = %v", err)
	}
	if !unavailable.closed {
		t.Fatal("unavailable catalog connection was not closed")
	}
}

func TestReportDatasourceServiceBuildsProcedureSignatureRecommendations(t *testing.T) {
	length := int64(4000)
	connection := &fakeReportDatasourceConnection{arguments: []reportoracle.ProcedureArgument{
		{Name: "P_QUERY_JSON", Position: 1, Sequence: 1, Direction: "IN", DataType: "CLOB", DataLength: &length},
	}}
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{
		BaseModel: model.BaseModel{ID: 4}, Enabled: true, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", QueryTimeoutSeconds: 30,
	}}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return connection, nil
	})
	signature, err := service.GetProcedureSignature(t.Context(), 7, 4, reportoracle.ProcedureRef{Owner: "report", Package: "pkg_sales", Name: "build_daily", Overload: "1"})
	if err != nil {
		t.Fatalf("GetProcedureSignature() error = %v", err)
	}
	if !signature.AllSupported || !signature.ProtocolReady || signature.InputArgName != "P_QUERY_JSON" || signature.OutputArgName != "" || len(signature.Arguments) != 1 || signature.Arguments[0].Role != "JSON_INPUT" {
		t.Fatalf("signature recommendations = %+v", signature)
	}
	want := "BEGIN REPORT.PKG_SALES.BUILD_DAILY(P_QUERY_JSON => :payload); END;"
	if signature.CallTemplate != want || connection.ref.Owner != "REPORT" || !connection.closed {
		t.Fatalf("signature template=%q ref=%+v closed=%t", signature.CallTemplate, connection.ref, connection.closed)
	}
}

func TestReportDatasourceServiceAcceptsJSONInputWithErrorOutput(t *testing.T) {
	connection := &fakeReportDatasourceConnection{arguments: []reportoracle.ProcedureArgument{
		{Name: "P_QUERY_JSON", Position: 1, Sequence: 1, Direction: "IN", DataType: "CLOB"},
		{Name: "R_ERROR", Position: 2, Sequence: 2, Direction: "OUT", DataType: "VARCHAR2"},
	}}
	store := &fakeReportDatasourceStore{item: model.ReportDatasource{
		BaseModel: model.BaseModel{ID: 4}, Enabled: true, PasswordCiphertext: "ciphertext", CredentialKeyVersion: "key-v1", QueryTimeoutSeconds: 30,
	}}
	service := NewReportDatasourceServiceWithDependencies(store, &fakeReportDatasourceCipher{}, func(context.Context, reportoracle.Config) (reportDatasourceConnection, error) {
		return connection, nil
	})
	signature, err := service.GetProcedureSignature(t.Context(), 7, 4, reportoracle.ProcedureRef{Owner: "REPORT", Name: "EXPORT_CURSOR"})
	if err != nil {
		t.Fatalf("GetProcedureSignature() error = %v", err)
	}
	want := "DECLARE error_output_2 VARCHAR2(32767); BEGIN REPORT.EXPORT_CURSOR(P_QUERY_JSON => :payload, R_ERROR => error_output_2); IF error_output_2 IS NOT NULL AND TRIM(SUBSTR(error_output_2, 1, 500)) <> '执行成功' THEN RAISE_APPLICATION_ERROR(-20001, SUBSTR(error_output_2, 1, 500)); END IF; END;"
	if !signature.AllSupported || !signature.ProtocolReady || signature.CallTemplate != want || !signature.Arguments[0].Supported || !signature.Arguments[1].Supported || signature.Arguments[1].Role != "ERROR_OUTPUT" || len(signature.BlockingReasons) != 0 {
		t.Fatalf("signature = %+v", signature)
	}
}

func (store *fakeReportDatasourceStore) ManagedResultTableRefs(context.Context, uint, []reportoracle.ResultTableRef) (map[reportoracle.ResultTableRef]bool, error) {
	return store.managed, nil
}

type fakeReportDatasourceStore struct {
	managed    map[reportoracle.ResultTableRef]bool
	item       model.ReportDatasource
	created    *model.ReportDatasource
	updated    *model.ReportDatasource
	testStatus string
	testError  string
}

func (store *fakeReportDatasourceStore) ListReportDatasources(context.Context) ([]model.ReportDatasource, error) {
	return []model.ReportDatasource{store.item}, nil
}
func (store *fakeReportDatasourceStore) GetReportDatasource(_ context.Context, id uint) (*model.ReportDatasource, error) {
	if id == 0 {
		return nil, reportrepo.ErrDatasourceNotFound
	}
	copy := store.item
	copy.ID = id
	return &copy, nil
}
func (store *fakeReportDatasourceStore) CreateReportDatasource(_ context.Context, _ uint, datasource *model.ReportDatasource) error {
	copy := *datasource
	copy.ID = 4
	store.created = &copy
	datasource.ID = 4
	store.item = copy
	return nil
}
func (store *fakeReportDatasourceStore) UpdateReportDatasource(_ context.Context, _ uint, datasource *model.ReportDatasource) error {
	copy := *datasource
	store.updated = &copy
	if datasource.PasswordCiphertext != "" {
		store.item.PasswordCiphertext = datasource.PasswordCiphertext
		store.item.CredentialKeyVersion = datasource.CredentialKeyVersion
	}
	store.item.BaseModel = datasource.BaseModel
	store.item.Code = datasource.Code
	store.item.Name = datasource.Name
	store.item.Driver = datasource.Driver
	return nil
}
func (store *fakeReportDatasourceStore) RecordReportDatasourceTest(_ context.Context, _, _ uint, status, safeError string, _ time.Time) error {
	store.testStatus, store.testError = status, safeError
	return nil
}

type fakeReportDatasourceCipher struct {
	encrypted string
	err       error
}

func (cipher *fakeReportDatasourceCipher) Encrypt(plaintext string) (string, string, error) {
	cipher.encrypted = plaintext
	return "key-v1", "ciphertext", cipher.err
}
func (*fakeReportDatasourceCipher) Decrypt(string, string) (string, error) {
	return "secret-password", nil
}

func validReportDatasourceRequest(password string) requestbody.ReportDatasourceSaveRequest {
	return requestbody.ReportDatasourceSaveRequest{Code: "report_oracle", Name: "报表 Oracle", Host: "oracle.internal", Port: 1521, ServiceName: "REPORT", Username: "report_user", Password: password, SessionTimezone: "Asia/Shanghai", ConnectTimeoutSeconds: 5, QueryTimeoutSeconds: 300, MaxOpenConnections: 10, MaxIdleConnections: 2, PrefetchRows: 1000, ArraySize: 1000, Enabled: true}
}

type fakeReportDatasourceConnection struct {
	closed           bool
	procedures       []reportoracle.ProcedureSummary
	arguments        []reportoracle.ProcedureArgument
	resultTables     []reportoracle.ResultTableSummary
	resultColumns    []reportoracle.ResultColumn
	metadataErr      error
	query            reportoracle.ProcedureCatalogQuery
	ref              reportoracle.ProcedureRef
	resultTableQuery reportoracle.ResultTableCatalogQuery
	resultTableRef   reportoracle.ResultTableRef
}

func (connection *fakeReportDatasourceConnection) Close() error {
	connection.closed = true
	return nil
}

func (connection *fakeReportDatasourceConnection) ListProcedures(_ context.Context, query reportoracle.ProcedureCatalogQuery) ([]reportoracle.ProcedureSummary, error) {
	connection.query = query
	return connection.procedures, connection.metadataErr
}

func (connection *fakeReportDatasourceConnection) InspectProcedure(_ context.Context, ref reportoracle.ProcedureRef) ([]reportoracle.ProcedureArgument, error) {
	connection.ref = ref
	return connection.arguments, connection.metadataErr
}

func (connection *fakeReportDatasourceConnection) ListResultTables(_ context.Context, query reportoracle.ResultTableCatalogQuery) ([]reportoracle.ResultTableSummary, error) {
	connection.resultTableQuery = query
	return connection.resultTables, connection.metadataErr
}

func (connection *fakeReportDatasourceConnection) InspectResultTable(_ context.Context, ref reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error) {
	connection.resultTableRef = ref
	return connection.resultColumns, connection.metadataErr
}

func validReportDatasourceConnectionTestRequest(password string) requestbody.ReportDatasourceConnectionTestRequest {
	return requestbody.ReportDatasourceConnectionTestRequest{Host: "draft.oracle.internal", Port: 1521, ServiceName: "DRAFT", Username: "draft_user", Password: password, SessionTimezone: "Asia/Shanghai", ConnectTimeoutSeconds: 5, QueryTimeoutSeconds: 300, MaxOpenConnections: 10, MaxIdleConnections: 2, PrefetchRows: 1000, ArraySize: 1000}
}
