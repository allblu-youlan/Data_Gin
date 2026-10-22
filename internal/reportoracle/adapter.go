package reportoracle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/godror/godror"
	"github.com/godror/godror/dsn"

	"gin-biz-web-api/internal/reporting"
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]*$`)
	hostPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
	servicePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	overloadPattern   = regexp.MustCompile(`^[0-9]+$`)

	ErrInvalidConfiguration = errors.New("invalid oracle report configuration")
	ErrUnsupportedBinding   = errors.New("unsupported oracle report binding")
	ErrMetadataMismatch     = errors.New("oracle report metadata mismatch")
	ErrTemporaryResultTable = errors.New("oracle temporary result table is unsupported")
)

type Config struct {
	Host               string
	Port               int
	ServiceName        string
	SID                string
	Username           string
	Password           string
	Timezone           string
	ConnectTimeout     time.Duration
	MaxOpenConnections int
	MaxIdleConnections int
	ConnectionLifetime time.Duration
	ConnectionIdleTime time.Duration
	PrefetchRows       int
	FetchArraySize     int
}

type ProcedureRef struct {
	Owner    string
	Package  string
	Name     string
	Overload string
}

type ResultTableRef struct {
	Owner string
	Name  string
}

type ProcedureArgument struct {
	Name          string
	Position      int
	Sequence      int
	Direction     string
	DataType      string
	DataLength    *int64
	DataPrecision *int64
	DataScale     *int64
	TypeOwner     string
	TypeName      string
	Defaulted     bool
}

type ProcedureCatalogQuery struct {
	Owner    string
	Search   string
	AfterKey string
	Limit    int
}

type ProcedureSummary struct {
	ProcedureRef
	ArgumentCount int
	CursorKey     string
}

type ResultTableCatalogQuery struct {
	Owner    string
	Search   string
	AfterKey string
	Limit    int
}

type ResultTableSummary struct {
	ResultTableRef
	ColumnCount int
	CursorKey   string
}

type ResultColumn struct {
	Name          string
	Comment       string `json:"-"`
	Position      int
	DataType      string
	DataLength    int64
	DataPrecision *int64
	DataScale     *int64
	Nullable      bool
}

type InputOption struct {
	ID   interface{} `json:"id"`
	Name string      `json:"name"`
}

type Adapter struct {
	db             *sql.DB
	prefetchRows   int
	fetchArraySize int
}

// CallPlan can only be produced by BuildCallPlan, preventing callers from
// supplying arbitrary PL/SQL to Execute.
type CallPlan struct {
	compiled reporting.CompiledCall
	bindings map[string]oracleBindKind
}

type oracleBindKind uint8

const (
	oracleBindScalar oracleBindKind = iota
	oracleBindNumber
	oracleBindCLOB
)

func (plan CallPlan) Statement() string { return plan.compiled.Statement }

func (plan CallPlan) Slots() []reporting.BindSlot {
	return append([]reporting.BindSlot(nil), plan.compiled.Slots...)
}

func Open(ctx context.Context, config Config) (*Adapter, error) {
	params, err := connectionParams(config)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(godror.NewConnector(params))
	maxOpenConnections := positiveOrDefault(config.MaxOpenConnections, 20)
	maxIdleConnections := positiveOrDefault(config.MaxIdleConnections, 10)
	if maxIdleConnections > maxOpenConnections {
		maxIdleConnections = maxOpenConnections
	}
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxIdleConnections)
	db.SetConnMaxLifetime(durationOrDefault(config.ConnectionLifetime, 30*time.Minute))
	db.SetConnMaxIdleTime(durationOrDefault(config.ConnectionIdleTime, 5*time.Minute))

	pingCtx := ctx
	cancel := func() {}
	if config.ConnectTimeout > 0 {
		pingCtx, cancel = context.WithTimeout(ctx, config.ConnectTimeout)
	}
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping oracle report datasource: %w", err)
	}

	return &Adapter{
		db:             db,
		prefetchRows:   positiveOrDefault(config.PrefetchRows, 1000),
		fetchArraySize: positiveOrDefault(config.FetchArraySize, 1000),
	}, nil
}

// BeginTx starts the transaction used to execute a procedure and to keep its
// result snapshot atomic. Callers remain responsible for Commit or Rollback.
func (adapter *Adapter) BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("begin oracle report transaction: adapter is closed")
	}
	tx, err := adapter.db.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin oracle report transaction: %w", err)
	}
	return tx, nil
}

func (adapter *Adapter) Close() error {
	if adapter == nil || adapter.db == nil {
		return nil
	}
	return adapter.db.Close()
}

// QueryInputOptions runs a validated SELECT that returns id and name. Runtime
// option requests never accept SQL; management test requests validate SQL
// before reaching this adapter. Wrapping keeps the exact-name predicate valid
// when the base query already contains filtering or ordering.
func (adapter *Adapter) QueryInputOptions(ctx context.Context, configuredSelect, exactName string, limit int) ([]InputOption, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query oracle input options: adapter is closed")
	}
	statement, arguments, err := buildInputOptionQuery(configuredSelect, exactName, limit)
	if err != nil {
		return nil, err
	}
	arguments = append(arguments, godror.PrefetchCount(adapter.prefetchRows), godror.FetchArraySize(adapter.fetchArraySize))
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query oracle input options: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read oracle input option columns: %w", err)
	}
	if len(columns) != 2 || !strings.EqualFold(columns[0], "id") || !strings.EqualFold(columns[1], "name") {
		return nil, fmt.Errorf("%w: input option query must return id and name", ErrMetadataMismatch)
	}
	options := make([]InputOption, 0)
	for rows.Next() {
		var rawID interface{}
		var rawName interface{}
		if err := rows.Scan(&rawID, &rawName); err != nil {
			return nil, fmt.Errorf("scan oracle input option: %w", err)
		}
		id, err := normalizeInputOptionID(rawID)
		if err != nil {
			return nil, err
		}
		name, err := normalizeInputOptionName(rawName)
		if err != nil {
			return nil, err
		}
		options = append(options, InputOption{ID: id, Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oracle input options: %w", err)
	}
	return options, nil
}

func buildInputOptionQuery(configuredSelect, exactName string, limit int) (string, []interface{}, error) {
	statement := strings.TrimSpace(configuredSelect)
	lower := strings.ToLower(statement)
	if !strings.HasPrefix(lower, "select ") || strings.Contains(statement, ";") ||
		strings.Contains(statement, "--") || strings.Contains(statement, "/*") || strings.Contains(statement, "*/") {
		return "", nil, configurationError("input option query is invalid")
	}
	if limit < 1 || limit > 500 {
		return "", nil, configurationError("input option query limit is invalid")
	}
	query := "SELECT id, name FROM (" + statement + ") REPORT_INPUT_SOURCE"
	arguments := make([]interface{}, 0, 1)
	if exactName != "" {
		query += " WHERE name = :name"
		arguments = append(arguments, sql.Named("name", exactName))
	}
	query += fmt.Sprintf(" FETCH FIRST %d ROWS ONLY", limit)
	return query, arguments, nil
}

func normalizeInputOptionID(value interface{}) (interface{}, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	case godror.Number:
		return string(typed), nil
	default:
		return nil, fmt.Errorf("%w: input option id must be text or number", ErrMetadataMismatch)
	}
}

func normalizeInputOptionName(value interface{}) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("%w: input option name must be text", ErrMetadataMismatch)
	}
}

func (adapter *Adapter) InspectProcedure(ctx context.Context, ref ProcedureRef) ([]ProcedureArgument, error) {
	normalized, err := NormalizeProcedureRef(ref)
	if err != nil {
		return nil, err
	}
	rows, err := adapter.db.QueryContext(ctx, procedureArgumentsSQL,
		normalized.Owner, nullableQueryValue(normalized.Package), normalized.Name, nullableQueryValue(normalized.Overload),
		godror.PrefetchCount(adapter.prefetchRows), godror.FetchArraySize(adapter.fetchArraySize),
	)
	if err != nil {
		return nil, fmt.Errorf("inspect oracle procedure arguments: %w", err)
	}
	defer rows.Close()

	arguments := make([]ProcedureArgument, 0, 16)
	for rows.Next() {
		var argument ProcedureArgument
		var name, dataType, inOut, typeOwner, typeName, defaulted sql.NullString
		var dataLength, precision, scale sql.NullInt64
		if err := rows.Scan(
			&name, &argument.Position, &argument.Sequence, &inOut, &dataType,
			&dataLength, &precision, &scale, &typeOwner, &typeName, &defaulted,
		); err != nil {
			return nil, fmt.Errorf("scan oracle procedure argument: %w", err)
		}
		argument.Name = name.String
		argument.Direction = inOut.String
		argument.DataType = dataType.String
		argument.DataLength = nullableInt64(dataLength)
		argument.DataPrecision = nullableInt64(precision)
		argument.DataScale = nullableInt64(scale)
		argument.TypeOwner = typeOwner.String
		argument.TypeName = typeName.String
		argument.Defaulted = defaulted.String == "Y"
		arguments = append(arguments, argument)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oracle procedure arguments: %w", err)
	}
	if len(arguments) == 0 {
		var exists int
		if err := adapter.db.QueryRowContext(ctx, procedureExistsSQL,
			normalized.Owner, nullableQueryValue(normalized.Package), normalized.Name, nullableQueryValue(normalized.Overload),
		).Scan(&exists); err != nil {
			return nil, fmt.Errorf("inspect zero-argument oracle procedure: %w", err)
		}
		if exists == 0 {
			return nil, fmt.Errorf("%w: no matching procedure", ErrMetadataMismatch)
		}
	}
	return arguments, nil
}

// ListProcedures returns only procedures visible to the Oracle account used by
// this adapter. Oracle metadata views enforce object grants; caller input is
// used only as bound filter values.
func (adapter *Adapter) ListProcedures(ctx context.Context, query ProcedureCatalogQuery) ([]ProcedureSummary, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("list oracle procedures: adapter is closed")
	}
	owner := ""
	var err error
	if strings.TrimSpace(query.Owner) != "" {
		owner, err = normalizeIdentifier(query.Owner, "procedure owner")
		if err != nil {
			return nil, err
		}
	}
	search := strings.ToUpper(strings.TrimSpace(query.Search))
	if len(search) > 128 {
		return nil, configurationError("procedure search is too long")
	}
	if query.AfterKey != "" {
		if _, err := ParseProcedureCursorKey(query.AfterKey); err != nil {
			return nil, err
		}
	}
	if query.Limit < 1 || query.Limit > 101 {
		return nil, configurationError("procedure list limit is invalid")
	}

	rows, err := adapter.db.QueryContext(ctx, procedureCatalogSQL,
		nullableQueryValue(owner), nullableQueryValue(search), nullableQueryValue(query.AfterKey), query.Limit,
		godror.PrefetchCount(adapter.prefetchRows), godror.FetchArraySize(adapter.fetchArraySize),
	)
	if err != nil {
		return nil, fmt.Errorf("list oracle procedures: %w", err)
	}
	defer rows.Close()

	procedures := make([]ProcedureSummary, 0, query.Limit)
	for rows.Next() {
		var procedure ProcedureSummary
		var packageName, overload sql.NullString
		if err := rows.Scan(&procedure.Owner, &packageName, &procedure.Name, &overload, &procedure.ArgumentCount, &procedure.CursorKey); err != nil {
			return nil, fmt.Errorf("scan oracle procedure catalog: %w", err)
		}
		procedure.Package = packageName.String
		procedure.Overload = overload.String
		procedures = append(procedures, procedure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oracle procedure catalog: %w", err)
	}
	return procedures, nil
}

func ProcedureCursorKey(ref ProcedureRef) (string, error) {
	normalized, err := NormalizeProcedureRef(ref)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{normalized.Owner, normalized.Package, normalized.Name, normalized.Overload}, "\x1f"), nil
}

func ParseProcedureCursorKey(value string) (ProcedureRef, error) {
	parts := strings.Split(value, "\x1f")
	if len(parts) != 4 {
		return ProcedureRef{}, configurationError("procedure cursor is invalid")
	}
	normalized, err := NormalizeProcedureRef(ProcedureRef{Owner: parts[0], Package: parts[1], Name: parts[2], Overload: parts[3]})
	if err != nil {
		return ProcedureRef{}, configurationError("procedure cursor is invalid")
	}
	canonical, err := ProcedureCursorKey(normalized)
	if err != nil || canonical != value {
		return ProcedureRef{}, configurationError("procedure cursor is invalid")
	}
	return normalized, nil
}

// ListResultTables returns table objects visible to the Oracle account. Views
// are deliberately excluded because a successful export purges the bound
// result table.
func (adapter *Adapter) ListResultTables(ctx context.Context, query ResultTableCatalogQuery) ([]ResultTableSummary, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("list oracle result tables: adapter is closed")
	}
	owner := ""
	var err error
	if strings.TrimSpace(query.Owner) != "" {
		owner, err = normalizeIdentifier(query.Owner, "result table owner")
		if err != nil {
			return nil, err
		}
	}
	search := strings.ToUpper(strings.TrimSpace(query.Search))
	if len(search) > 128 {
		return nil, configurationError("result table search is too long")
	}
	if query.AfterKey != "" {
		if _, err := ParseResultTableCursorKey(query.AfterKey); err != nil {
			return nil, err
		}
	}
	if query.Limit < 1 || query.Limit > 101 {
		return nil, configurationError("result table list limit is invalid")
	}

	rows, err := adapter.db.QueryContext(ctx, resultTableCatalogSQL,
		nullableQueryValue(owner), nullableQueryValue(search), nullableQueryValue(query.AfterKey), query.Limit,
		godror.PrefetchCount(adapter.prefetchRows), godror.FetchArraySize(adapter.fetchArraySize),
	)
	if err != nil {
		return nil, fmt.Errorf("list oracle result tables: %w", err)
	}
	defer rows.Close()

	tables := make([]ResultTableSummary, 0, query.Limit)
	for rows.Next() {
		var table ResultTableSummary
		if err := rows.Scan(&table.Owner, &table.Name, &table.ColumnCount, &table.CursorKey); err != nil {
			return nil, fmt.Errorf("scan oracle result table catalog: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oracle result table catalog: %w", err)
	}
	return tables, nil
}

func ResultTableCursorKey(ref ResultTableRef) (string, error) {
	normalized, err := NormalizeResultTableRef(ref)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{normalized.Owner, normalized.Name}, "\x1f"), nil
}

func ParseResultTableCursorKey(value string) (ResultTableRef, error) {
	parts := strings.Split(value, "\x1f")
	if len(parts) != 2 {
		return ResultTableRef{}, configurationError("result table cursor is invalid")
	}
	normalized, err := NormalizeResultTableRef(ResultTableRef{Owner: parts[0], Name: parts[1]})
	if err != nil {
		return ResultTableRef{}, configurationError("result table cursor is invalid")
	}
	canonical, err := ResultTableCursorKey(normalized)
	if err != nil || canonical != value {
		return ResultTableRef{}, configurationError("result table cursor is invalid")
	}
	return normalized, nil
}

func (adapter *Adapter) InspectResultTable(ctx context.Context, ref ResultTableRef) ([]ResultColumn, error) {
	normalized, err := NormalizeResultTableRef(ref)
	if err != nil {
		return nil, err
	}
	rows, err := adapter.db.QueryContext(ctx, resultColumnsSQL,
		normalized.Owner, normalized.Name,
		godror.PrefetchCount(adapter.prefetchRows), godror.FetchArraySize(adapter.fetchArraySize),
	)
	if err != nil {
		return nil, fmt.Errorf("inspect oracle result table: %w", err)
	}
	defer rows.Close()

	columns := make([]ResultColumn, 0, 32)
	for rows.Next() {
		var column ResultColumn
		var precision, scale sql.NullInt64
		var comment sql.NullString
		var nullable string
		if err := rows.Scan(
			&column.Name, &column.Position, &column.DataType, &column.DataLength,
			&precision, &scale, &nullable, &comment,
		); err != nil {
			return nil, fmt.Errorf("scan oracle result column: %w", err)
		}
		column.DataPrecision = nullableInt64(precision)
		column.DataScale = nullableInt64(scale)
		column.Nullable = nullable == "Y"
		column.Comment = comment.String
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oracle result columns: %w", err)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("%w: no matching result table", ErrMetadataMismatch)
	}
	return columns, nil
}

// BuildCanonicalCall creates the only PL/SQL block accepted by the execution
// adapter. The configured {{code}} template is retained in MySQL for review,
// while the published runtime statement is generated from validated objects.
func BuildCallPlan(ref ProcedureRef, definitions []reporting.ParameterDefinition) (CallPlan, error) {
	normalized, err := NormalizeProcedureRef(ref)
	if err != nil {
		return CallPlan{}, err
	}
	ordered := append([]reporting.ParameterDefinition(nil), definitions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })
	bindings := make([]string, 0, len(ordered))
	bindKinds := make(map[string]oracleBindKind, len(ordered))
	for _, definition := range ordered {
		if strings.ToUpper(strings.TrimSpace(definition.Direction)) != "IN" {
			return CallPlan{}, fmt.Errorf("%w: parameter %q must be an IN parameter", ErrUnsupportedBinding, definition.Code)
		}
		argName, err := normalizeIdentifier(definition.ProcedureArgName, "procedure argument")
		if err != nil {
			return CallPlan{}, err
		}
		bindKind, err := compileBindKind(definition)
		if err != nil {
			return CallPlan{}, err
		}
		bindings = append(bindings, fmt.Sprintf("%s => {{%s}}", argName, definition.Code))
		bindKinds[definition.Code] = bindKind
	}
	target := normalized.Owner + "."
	if normalized.Package != "" {
		target += normalized.Package + "."
	}
	target += normalized.Name
	if len(ordered) == 0 {
		return CallPlan{
			compiled: reporting.CompiledCall{Statement: fmt.Sprintf("BEGIN %s(); END;", target)},
			bindings: bindKinds,
		}, nil
	}
	template := fmt.Sprintf("BEGIN %s(%s); END;", target, strings.Join(bindings, ", "))
	compiled, err := reporting.CompileCallTemplate(template, ordered)
	if err != nil {
		return CallPlan{}, err
	}
	return CallPlan{compiled: compiled, bindings: bindKinds}, nil
}

func (adapter *Adapter) Execute(
	ctx context.Context,
	tx *sql.Tx,
	plan CallPlan,
	values map[string]interface{},
) error {
	if tx == nil {
		return fmt.Errorf("execute oracle report procedure: transaction is required")
	}
	if strings.TrimSpace(plan.compiled.Statement) == "" || plan.bindings == nil {
		return fmt.Errorf("%w: call plan is empty", ErrUnsupportedBinding)
	}
	arguments := make([]interface{}, 0, len(plan.compiled.Slots))
	for _, slot := range plan.compiled.Slots {
		value, ok := values[slot.Code]
		if !ok {
			return fmt.Errorf("%w: parameter %q has no database value", ErrUnsupportedBinding, slot.Code)
		}
		bindKind, ok := plan.bindings[slot.Code]
		if !ok {
			return fmt.Errorf("%w: parameter %q has no compiled oracle type", ErrUnsupportedBinding, slot.Code)
		}
		boundValue, err := bindOracleValue(bindKind, value)
		if err != nil {
			return fmt.Errorf("%w: parameter %q: %v", ErrUnsupportedBinding, slot.Code, err)
		}
		arguments = append(arguments, sql.Named(slot.BindName, boundValue))
	}
	if _, err := tx.ExecContext(ctx, plan.compiled.Statement, arguments...); err != nil {
		return mapExecutionError(ctx, err)
	}
	return nil
}

func mapExecutionError(ctx context.Context, err error) error {
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var oracleError interface{ Code() int }
	if errors.As(err, &oracleError) && oracleError.Code() == 1013 {
		return context.Canceled
	}
	return fmt.Errorf("execute oracle report procedure: %w", err)
}

func compileBindKind(definition reporting.ParameterDefinition) (oracleBindKind, error) {
	oracleType := strings.ToUpper(strings.Join(strings.Fields(definition.OracleType), " "))
	if oracleType == "" {
		return 0, fmt.Errorf("%w: parameter %q has no oracle type", ErrUnsupportedBinding, definition.Code)
	}
	if definition.CollectionEncoding == reporting.CollectionEncodingJSONCLOB {
		if oracleType != "CLOB" {
			return 0, fmt.Errorf("%w: parameter %q with JSON_CLOB encoding must bind CLOB", ErrUnsupportedBinding, definition.Code)
		}
		return oracleBindCLOB, nil
	}
	if definition.LogicalType == reporting.LogicalTypeDecimal {
		if oracleType != "NUMBER" {
			return 0, fmt.Errorf("%w: decimal parameter %q must bind NUMBER", ErrUnsupportedBinding, definition.Code)
		}
		return oracleBindNumber, nil
	}
	if definition.LogicalType == reporting.LogicalTypeInteger && oracleType != "NUMBER" {
		return 0, fmt.Errorf("%w: integer parameter %q must bind NUMBER", ErrUnsupportedBinding, definition.Code)
	}
	if definition.LogicalType == reporting.LogicalTypeBoolean && oracleType != "BOOLEAN" {
		return 0, fmt.Errorf("%w: boolean parameter %q must bind BOOLEAN", ErrUnsupportedBinding, definition.Code)
	}
	if oracleType == "CLOB" || oracleType == "NCLOB" {
		return oracleBindCLOB, nil
	}
	return oracleBindScalar, nil
}

func bindOracleValue(kind oracleBindKind, value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	switch kind {
	case oracleBindScalar:
		return value, nil
	case oracleBindNumber:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("NUMBER value must be a normalized decimal string")
		}
		return godror.Number(text), nil
	case oracleBindCLOB:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("CLOB value must be a string")
		}
		return godror.Lob{Reader: strings.NewReader(text), IsClob: true}, nil
	default:
		return nil, fmt.Errorf("oracle bind kind is invalid")
	}
}

func NormalizeProcedureRef(ref ProcedureRef) (ProcedureRef, error) {
	owner, err := normalizeIdentifier(ref.Owner, "procedure owner")
	if err != nil {
		return ProcedureRef{}, err
	}
	name, err := normalizeIdentifier(ref.Name, "procedure name")
	if err != nil {
		return ProcedureRef{}, err
	}
	packageName := ""
	if strings.TrimSpace(ref.Package) != "" {
		packageName, err = normalizeIdentifier(ref.Package, "package name")
		if err != nil {
			return ProcedureRef{}, err
		}
	}
	overload := strings.TrimSpace(ref.Overload)
	if overload != "" && !overloadPattern.MatchString(overload) {
		return ProcedureRef{}, configurationError("procedure overload is invalid")
	}
	return ProcedureRef{Owner: owner, Package: packageName, Name: name, Overload: overload}, nil
}

func NormalizeResultTableRef(ref ResultTableRef) (ResultTableRef, error) {
	owner, err := normalizeIdentifier(ref.Owner, "result table owner")
	if err != nil {
		return ResultTableRef{}, err
	}
	name, err := normalizeIdentifier(ref.Name, "result table name")
	if err != nil {
		return ResultTableRef{}, err
	}
	return ResultTableRef{Owner: owner, Name: name}, nil
}

func connectionParams(config Config) (dsn.ConnectionParams, error) {
	connectString, err := BuildConnectString(config)
	if err != nil {
		return dsn.ConnectionParams{}, err
	}
	username := strings.TrimSpace(config.Username)
	if username == "" || strings.TrimSpace(config.Password) == "" {
		return dsn.ConnectionParams{}, configurationError("username and password are required")
	}
	timezoneName := strings.TrimSpace(config.Timezone)
	if timezoneName == "" {
		timezoneName = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return dsn.ConnectionParams{}, configurationError("timezone is invalid")
	}
	return dsn.ConnectionParams{
		CommonParams: dsn.CommonParams{CommonSimpleParams: dsn.CommonSimpleParams{
			Username:           username,
			Password:           dsn.NewPassword(config.Password),
			ConnectString:      connectString,
			Timezone:           location,
			PerSessionTimezone: true,
		}},
		StandaloneConnection: godror.Bool(false),
	}, nil
}

func BuildConnectString(config Config) (string, error) {
	host := strings.TrimSpace(config.Host)
	if !hostPattern.MatchString(host) {
		return "", configurationError("host is invalid")
	}
	if config.Port < 1 || config.Port > 65535 {
		return "", configurationError("port is invalid")
	}
	serviceName := strings.TrimSpace(config.ServiceName)
	sid := strings.TrimSpace(config.SID)
	if (serviceName == "") == (sid == "") {
		return "", configurationError("exactly one of service name or SID is required")
	}
	if serviceName != "" {
		if !servicePattern.MatchString(serviceName) {
			return "", configurationError("service name is invalid")
		}
		return fmt.Sprintf("%s:%d/%s", host, config.Port, serviceName), nil
	}
	if !identifierPattern.MatchString(sid) {
		return "", configurationError("SID is invalid")
	}
	return fmt.Sprintf(
		"(DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=%s)(PORT=%d))(CONNECT_DATA=(SID=%s)))",
		host, config.Port, strings.ToUpper(sid),
	), nil
}

func normalizeIdentifier(value, label string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) > 128 || !identifierPattern.MatchString(trimmed) {
		return "", configurationError(label + " is invalid")
	}
	return strings.ToUpper(trimmed), nil
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableQueryValue(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func positiveOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func durationOrDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func configurationError(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfiguration, message)
}

const procedureArgumentsSQL = `
WITH filters AS (
    SELECT :1 AS owner_name, :2 AS package_name, :3 AS object_name, :4 AS overload
    FROM dual
)
SELECT arguments.argument_name, arguments.position, arguments.sequence,
       arguments.in_out, arguments.data_type, arguments.data_length,
       arguments.data_precision, arguments.data_scale,
       arguments.type_owner, arguments.type_name, arguments.defaulted
FROM all_arguments arguments
CROSS JOIN filters
WHERE arguments.owner = filters.owner_name
  AND ((filters.package_name IS NULL AND arguments.package_name IS NULL) OR arguments.package_name = filters.package_name)
  AND arguments.object_name = filters.object_name
  AND ((filters.overload IS NULL AND arguments.overload IS NULL) OR arguments.overload = filters.overload)
  AND arguments.data_level = 0
ORDER BY arguments.sequence`

const procedureExistsSQL = `
WITH filters AS (
    SELECT :1 AS owner_name, :2 AS package_name, :3 AS procedure_name, :4 AS overload
    FROM dual
)
SELECT CASE WHEN EXISTS (
    SELECT 1
    FROM all_procedures procedures
    CROSS JOIN filters
    WHERE procedures.owner = filters.owner_name
      AND ((filters.package_name IS NULL
            AND procedures.object_type = 'PROCEDURE'
            AND procedures.procedure_name IS NULL
            AND procedures.object_name = filters.procedure_name)
        OR (procedures.object_type = 'PACKAGE'
            AND procedures.object_name = filters.package_name
            AND procedures.procedure_name = filters.procedure_name))
      AND ((filters.overload IS NULL AND procedures.overload IS NULL) OR procedures.overload = filters.overload)
) THEN 1 ELSE 0 END
FROM dual`

const procedureCatalogSQL = `
WITH filters AS (
    SELECT :1 AS owner_name, :2 AS search_text, :3 AS after_key, :4 AS row_limit
    FROM dual
), local_synonyms AS (
    SELECT synonyms.owner, synonyms.synonym_name, synonyms.table_owner, synonyms.table_name
    FROM all_synonyms synonyms
    WHERE synonyms.owner IN (USER, 'PUBLIC')
      AND synonyms.db_link IS NULL
      AND (synonyms.owner = USER OR NOT EXISTS (
          SELECT 1
          FROM all_synonyms private_synonyms
          WHERE private_synonyms.owner = USER
            AND private_synonyms.synonym_name = synonyms.synonym_name
      ))
), candidate_procedures AS (
    SELECT procedures.owner,
           CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.object_name END AS package_name,
           CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.procedure_name ELSE procedures.object_name END AS procedure_name,
           procedures.overload,
           procedures.owner || CHR(31) ||
           NVL(CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.object_name END, '') || CHR(31) ||
           CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.procedure_name ELSE procedures.object_name END || CHR(31) ||
           NVL(procedures.overload, '') AS cursor_key
    FROM all_procedures procedures
    CROSS JOIN filters
    WHERE ((procedures.object_type = 'PROCEDURE' AND procedures.procedure_name IS NULL)
        OR (procedures.object_type = 'PACKAGE' AND procedures.procedure_name IS NOT NULL))
      AND (filters.owner_name IS NULL OR procedures.owner = filters.owner_name)
      AND (filters.search_text IS NULL OR INSTR(
          procedures.owner || '.' ||
          CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.object_name || '.' || procedures.procedure_name ELSE procedures.object_name END ||
          CASE WHEN procedures.overload IS NULL THEN '' ELSE '#' || procedures.overload END,
          filters.search_text
      ) > 0)
      AND (filters.after_key IS NULL OR
          procedures.owner || CHR(31) ||
          NVL(CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.object_name END, '') || CHR(31) ||
          CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.procedure_name ELSE procedures.object_name END || CHR(31) ||
          NVL(procedures.overload, '') > filters.after_key)
    UNION ALL
    SELECT procedures.owner,
           CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.object_name END AS package_name,
           CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.procedure_name ELSE procedures.object_name END AS procedure_name,
           procedures.overload,
           procedures.owner || CHR(31) ||
           NVL(CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.object_name END, '') || CHR(31) ||
           CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.procedure_name ELSE procedures.object_name END || CHR(31) ||
           NVL(procedures.overload, '') AS cursor_key
    FROM all_procedures procedures
    JOIN local_synonyms synonyms
      ON synonyms.table_owner = procedures.owner
     AND synonyms.table_name = procedures.object_name
    CROSS JOIN filters
    WHERE filters.search_text IS NOT NULL
      AND ((procedures.object_type = 'PROCEDURE' AND procedures.procedure_name IS NULL)
        OR (procedures.object_type = 'PACKAGE' AND procedures.procedure_name IS NOT NULL))
      AND (filters.owner_name IS NULL OR procedures.owner = filters.owner_name)
      AND INSTR(
          synonyms.owner || '.' || synonyms.synonym_name ||
          CASE WHEN procedures.object_type = 'PACKAGE' THEN '.' || procedures.procedure_name ELSE '' END ||
          CASE WHEN procedures.overload IS NULL THEN '' ELSE '#' || procedures.overload END,
          filters.search_text
      ) > 0
      AND (filters.after_key IS NULL OR
          procedures.owner || CHR(31) ||
          NVL(CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.object_name END, '') || CHR(31) ||
          CASE WHEN procedures.object_type = 'PACKAGE' THEN procedures.procedure_name ELSE procedures.object_name END || CHR(31) ||
          NVL(procedures.overload, '') > filters.after_key)
), catalog AS (
    SELECT DISTINCT candidates.owner, candidates.package_name, candidates.procedure_name, candidates.overload, candidates.cursor_key
    FROM candidate_procedures candidates
    WHERE NOT EXISTS (
          SELECT 1
          FROM all_arguments return_values
          WHERE return_values.owner = candidates.owner
            AND NVL(return_values.package_name, CHR(1)) = NVL(candidates.package_name, CHR(1))
            AND return_values.object_name = candidates.procedure_name
            AND NVL(return_values.overload, CHR(1)) = NVL(candidates.overload, CHR(1))
            AND return_values.data_level = 0
            AND return_values.position = 0
      )
), ordered_catalog AS (
    SELECT catalog.owner, catalog.package_name, catalog.procedure_name, catalog.overload,
           catalog.cursor_key, filters.row_limit
    FROM catalog
    CROSS JOIN filters
    ORDER BY catalog.cursor_key
), page_catalog AS (
    SELECT owner, package_name, procedure_name, overload, cursor_key
    FROM ordered_catalog
    WHERE ROWNUM <= row_limit
)
SELECT page_catalog.owner, page_catalog.package_name, page_catalog.procedure_name, page_catalog.overload,
       (
           SELECT COUNT(*)
           FROM all_arguments arguments
           WHERE arguments.owner = page_catalog.owner
             AND NVL(arguments.package_name, CHR(1)) = NVL(page_catalog.package_name, CHR(1))
             AND arguments.object_name = page_catalog.procedure_name
             AND NVL(arguments.overload, CHR(1)) = NVL(page_catalog.overload, CHR(1))
             AND arguments.data_level = 0
             AND arguments.position > 0
       ) AS argument_count,
       page_catalog.cursor_key
FROM page_catalog
ORDER BY page_catalog.cursor_key`

const resultColumnsSQL = `
SELECT c.column_name, c.column_id, c.data_type, c.data_length,
       c.data_precision, c.data_scale, c.nullable, cc.comments
FROM all_tab_columns c
LEFT JOIN all_col_comments cc
  ON cc.owner = c.owner AND cc.table_name = c.table_name AND cc.column_name = c.column_name
WHERE c.owner = :1 AND c.table_name = :2
ORDER BY c.column_id`

const resultTableCatalogSQL = `
WITH filters AS (
    SELECT :1 AS owner_name, :2 AS search_text, :3 AS after_key, :4 AS row_limit
    FROM dual
), local_synonyms AS (
    SELECT synonyms.owner, synonyms.synonym_name, synonyms.table_owner, synonyms.table_name
    FROM all_synonyms synonyms
    WHERE synonyms.owner IN (USER, 'PUBLIC')
      AND synonyms.db_link IS NULL
      AND (synonyms.owner = USER OR NOT EXISTS (
          SELECT 1
          FROM all_synonyms private_synonyms
          WHERE private_synonyms.owner = USER
            AND private_synonyms.synonym_name = synonyms.synonym_name
      ))
), candidate_tables AS (
    SELECT tables.owner, tables.table_name,
           tables.owner || CHR(31) || tables.table_name AS cursor_key
    FROM all_tables tables
    CROSS JOIN filters
    WHERE REGEXP_LIKE(tables.owner, '^[A-Z][A-Z0-9_$#]*$')
      AND REGEXP_LIKE(tables.table_name, '^[A-Z][A-Z0-9_$#]*$')
      AND (filters.owner_name IS NULL OR tables.owner = filters.owner_name)
      AND (filters.search_text IS NULL OR INSTR(tables.owner || '.' || tables.table_name, filters.search_text) > 0)
      AND (filters.after_key IS NULL OR tables.owner || CHR(31) || tables.table_name > filters.after_key)
    UNION ALL
    SELECT tables.owner, tables.table_name,
           tables.owner || CHR(31) || tables.table_name AS cursor_key
    FROM all_tables tables
    JOIN local_synonyms synonyms
      ON synonyms.table_owner = tables.owner
     AND synonyms.table_name = tables.table_name
    CROSS JOIN filters
    WHERE filters.search_text IS NOT NULL
      AND REGEXP_LIKE(tables.owner, '^[A-Z][A-Z0-9_$#]*$')
      AND REGEXP_LIKE(tables.table_name, '^[A-Z][A-Z0-9_$#]*$')
      AND (filters.owner_name IS NULL OR tables.owner = filters.owner_name)
      AND INSTR(synonyms.owner || '.' || synonyms.synonym_name, filters.search_text) > 0
      AND (filters.after_key IS NULL OR tables.owner || CHR(31) || tables.table_name > filters.after_key)
), catalog AS (
    SELECT DISTINCT candidates.owner, candidates.table_name, candidates.cursor_key
    FROM candidate_tables candidates
), ordered_catalog AS (
    SELECT catalog.owner, catalog.table_name, catalog.cursor_key, filters.row_limit
    FROM catalog
    CROSS JOIN filters
    ORDER BY catalog.cursor_key
), page_catalog AS (
    SELECT owner, table_name, cursor_key
    FROM ordered_catalog
    WHERE ROWNUM <= row_limit
)
SELECT page_catalog.owner, page_catalog.table_name,
       (
           SELECT COUNT(*)
           FROM all_tab_columns columns
           WHERE columns.owner = page_catalog.owner
             AND columns.table_name = page_catalog.table_name
       ) AS column_count,
       page_catalog.cursor_key
FROM page_catalog
ORDER BY page_catalog.cursor_key`
