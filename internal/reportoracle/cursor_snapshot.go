package reportoracle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/godror/godror"
)

const (
	JSONSnapshotManifestTable = "REPORT_RUN_SNAPSHOTS"
	JSONSnapshotRowsTable     = "REPORT_RUN_SNAPSHOT_ROWS"
	jsonSnapshotReadyStatus   = "READY"
	jsonSnapshotInsertBatch   = 200
)

// ErrJSONSnapshotStoreNotReady indicates missing deployment storage, not an
// invalid report procedure, selected source table, or output configuration.
var ErrJSONSnapshotStoreNotReady = errors.New("oracle JSON snapshot store is not ready")

type JSONCursorCallPlan struct {
	statement     string
	payloadIsCLOB bool
}

type CursorColumn struct {
	Name       string `json:"name"`
	OracleType string `json:"oracleType"`
	Nullable   bool   `json:"nullable"`
}

type CursorSnapshot struct {
	Columns    []CursorColumn
	SchemaHash string
	RowCount   int64
}

func (adapter *Adapter) ValidateJSONSnapshotStore(ctx context.Context) error {
	if adapter == nil || adapter.db == nil || ctx == nil {
		return fmt.Errorf("validate JSON snapshot store: adapter is closed")
	}
	var columnCount int
	if err := adapter.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM user_tab_columns
WHERE (table_name = 'REPORT_RUN_SNAPSHOTS' AND column_name IN ('RUN_ID','SCHEMA_JSON','SCHEMA_HASH','ROW_COUNT','STATUS','CREATED_AT'))
   OR (table_name = 'REPORT_RUN_SNAPSHOT_ROWS' AND column_name IN ('RUN_ID','ROW_NO','VALUES_JSON'))`).Scan(&columnCount); err != nil {
		return fmt.Errorf("validate JSON snapshot store: %w", err)
	}
	if columnCount != 9 {
		return fmt.Errorf("%w: Oracle JSON snapshot tables are not installed", ErrJSONSnapshotStoreNotReady)
	}
	return nil
}

func (plan JSONCursorCallPlan) Statement() string { return plan.statement }

func BuildJSONCursorCallPlan(ref ProcedureRef, arguments []ProcedureArgument, inputArgName, outputArgName string) (JSONCursorCallPlan, error) {
	normalized, err := NormalizeProcedureRef(ref)
	if err != nil {
		return JSONCursorCallPlan{}, err
	}
	inputName, err := normalizeIdentifier(inputArgName, "JSON input argument")
	if err != nil {
		return JSONCursorCallPlan{}, err
	}
	outputName, err := normalizeIdentifier(outputArgName, "result cursor argument")
	if err != nil || inputName == outputName {
		return JSONCursorCallPlan{}, configurationError("JSON cursor argument names are invalid")
	}
	if len(arguments) != 2 {
		return JSONCursorCallPlan{}, fmt.Errorf("%w: procedure must have one JSON input and one REF CURSOR output", ErrUnsupportedBinding)
	}
	inputFound, outputFound, payloadIsCLOB := false, false, false
	for _, argument := range arguments {
		name := strings.ToUpper(strings.TrimSpace(argument.Name))
		direction := strings.ToUpper(strings.TrimSpace(argument.Direction))
		dataType := normalizeOracleMetadataType(argument.DataType)
		typeName := normalizeOracleMetadataType(argument.TypeName)
		switch {
		case name == inputName && direction == "IN" && jsonInputOracleType(dataType):
			inputFound = true
			payloadIsCLOB = dataType == "CLOB" || dataType == "NCLOB" || dataType == "JSON"
		case name == outputName && direction == "OUT" && (dataType == "REF CURSOR" || dataType == "SYS_REFCURSOR" || typeName == "SYS_REFCURSOR"):
			outputFound = true
		default:
			return JSONCursorCallPlan{}, fmt.Errorf("%w: procedure signature does not match the JSON cursor protocol", ErrUnsupportedBinding)
		}
	}
	if !inputFound || !outputFound {
		return JSONCursorCallPlan{}, fmt.Errorf("%w: procedure signature does not match configured JSON cursor arguments", ErrUnsupportedBinding)
	}
	target := normalized.Owner + "."
	if normalized.Package != "" {
		target += normalized.Package + "."
	}
	target += normalized.Name
	return JSONCursorCallPlan{
		statement:     fmt.Sprintf("BEGIN %s(%s => :payload, %s => :resultCursor); END;", target, inputName, outputName),
		payloadIsCLOB: payloadIsCLOB,
	}, nil
}

func (adapter *Adapter) ExecuteJSONCursorSnapshot(
	ctx context.Context,
	tx *sql.Tx,
	plan JSONCursorCallPlan,
	runID, payload string,
	expectedColumns []string,
) (snapshot CursorSnapshot, resultErr error) {
	if adapter == nil || tx == nil || ctx == nil || strings.TrimSpace(plan.statement) == "" || strings.TrimSpace(runID) == "" || !json.Valid([]byte(payload)) {
		return snapshot, fmt.Errorf("execute JSON cursor snapshot: invalid request")
	}
	if err := adapter.resetJSONSnapshot(ctx, tx, runID); err != nil {
		return snapshot, err
	}
	statement, err := tx.PrepareContext(ctx, plan.statement)
	if err != nil {
		return snapshot, fmt.Errorf("prepare JSON cursor procedure: %w", err)
	}
	defer func() {
		if closeErr := statement.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close JSON cursor procedure: %w", closeErr))
		}
	}()

	var cursor driver.Rows
	payloadValue := interface{}(payload)
	if plan.payloadIsCLOB {
		payloadValue = godror.Lob{Reader: strings.NewReader(payload), IsClob: true}
	}
	if _, err := statement.ExecContext(ctx,
		sql.Named("payload", payloadValue),
		sql.Named("resultCursor", sql.Out{Dest: &cursor}),
	); err != nil {
		return snapshot, mapExecutionError(ctx, err)
	}
	if cursor == nil {
		return snapshot, fmt.Errorf("execute JSON cursor snapshot: procedure returned no cursor")
	}
	defer cursor.Close()
	rows, err := godror.WrapRows(ctx, tx, cursor)
	if err != nil {
		return snapshot, fmt.Errorf("wrap JSON result cursor: %w", err)
	}
	defer rows.Close()

	snapshot.Columns, err = cursorColumns(rows)
	if err != nil {
		return snapshot, err
	}
	if err := validateExpectedCursorColumns(snapshot.Columns, expectedColumns); err != nil {
		return snapshot, err
	}
	schemaJSON, err := json.Marshal(snapshot.Columns)
	if err != nil {
		return snapshot, fmt.Errorf("encode JSON cursor schema: %w", err)
	}
	hash := sha256.Sum256(schemaJSON)
	snapshot.SchemaHash = hex.EncodeToString(hash[:])
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO "+JSONSnapshotManifestTable+" (RUN_ID, SCHEMA_JSON, SCHEMA_HASH, ROW_COUNT, STATUS, CREATED_AT) VALUES (:1, :2, :3, 0, 'WRITING', SYSTIMESTAMP)",
		runID, godror.Lob{Reader: bytes.NewReader(schemaJSON), IsClob: true}, snapshot.SchemaHash,
	); err != nil {
		return snapshot, fmt.Errorf("create JSON cursor snapshot manifest: %w", err)
	}

	values := make([]interface{}, len(snapshot.Columns))
	destinations := make([]interface{}, len(values))
	for index := range destinations {
		destinations[index] = &values[index]
	}
	batch := make([][]byte, 0, jsonSnapshotInsertBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		firstRow := snapshot.RowCount - int64(len(batch)) + 1
		if err := insertJSONSnapshotBatch(ctx, tx, runID, firstRow, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		for index := range values {
			values[index] = nil
		}
		if err := rows.Scan(destinations...); err != nil {
			return snapshot, fmt.Errorf("scan JSON result cursor row: %w", err)
		}
		encoded, err := encodeCursorValues(values, snapshot.Columns)
		if err != nil {
			return snapshot, err
		}
		snapshot.RowCount++
		batch = append(batch, encoded)
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				return snapshot, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate JSON result cursor: %w", err)
	}
	if err := flush(); err != nil {
		return snapshot, err
	}
	result, err := tx.ExecContext(ctx,
		"UPDATE "+JSONSnapshotManifestTable+" SET ROW_COUNT = :1, STATUS = '"+jsonSnapshotReadyStatus+"' WHERE RUN_ID = :2 AND STATUS = 'WRITING'",
		snapshot.RowCount, runID,
	)
	if err != nil {
		return snapshot, fmt.Errorf("finalize JSON cursor snapshot: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return snapshot, fmt.Errorf("finalize JSON cursor snapshot: manifest state changed")
	}
	return snapshot, nil
}

func insertJSONSnapshotBatch(ctx context.Context, tx *sql.Tx, runID string, firstRow int64, rows [][]byte) error {
	if tx == nil || len(rows) == 0 || len(rows) > jsonSnapshotInsertBatch || firstRow < 1 {
		return fmt.Errorf("insert JSON cursor snapshot batch: invalid request")
	}
	var statement strings.Builder
	statement.Grow(64 + len(rows)*96)
	statement.WriteString("INSERT ALL")
	arguments := make([]interface{}, 0, len(rows)*3)
	for index, encoded := range rows {
		base := index*3 + 1
		fmt.Fprintf(&statement, " INTO %s (RUN_ID, ROW_NO, VALUES_JSON) VALUES (:%d, :%d, :%d)", JSONSnapshotRowsTable, base, base+1, base+2)
		arguments = append(arguments, runID, firstRow+int64(index), godror.Lob{Reader: bytes.NewReader(encoded), IsClob: true})
	}
	statement.WriteString(" SELECT 1 FROM DUAL")
	if _, err := tx.ExecContext(ctx, statement.String(), arguments...); err != nil {
		return fmt.Errorf("insert JSON cursor snapshot rows %d-%d: %w", firstRow, firstRow+int64(len(rows))-1, err)
	}
	return nil
}

func (adapter *Adapter) ReadJSONSnapshotPage(ctx context.Context, runID string, columns []string, afterRowID int64, pageSize int) (ResultPage, error) {
	if adapter == nil || adapter.db == nil || ctx == nil || strings.TrimSpace(runID) == "" || afterRowID < 0 || len(columns) == 0 || len(columns) > maxResultColumns {
		return ResultPage{}, fmt.Errorf("read JSON snapshot page: invalid request")
	}
	if pageSize == 0 {
		pageSize = defaultResultPageSize
	}
	if pageSize < 1 || pageSize > maxResultPageSize {
		return ResultPage{}, fmt.Errorf("read JSON snapshot page: invalid page size")
	}
	schema, err := adapter.readJSONSnapshotSchema(ctx, runID)
	if err != nil {
		return ResultPage{}, err
	}
	indexes, normalizedColumns, err := snapshotColumnIndexes(schema, columns)
	if err != nil {
		return ResultPage{}, err
	}
	rows, err := adapter.db.QueryContext(ctx,
		"SELECT ROW_NO, VALUES_JSON FROM "+JSONSnapshotRowsTable+" WHERE RUN_ID = :1 AND ROW_NO > :2 ORDER BY ROW_NO FETCH NEXT :3 ROWS ONLY",
		runID, afterRowID, pageSize+1, godror.PrefetchCount(adapter.prefetchRows), godror.FetchArraySize(adapter.fetchArraySize), godror.ClobAsString(),
	)
	if err != nil {
		return ResultPage{}, fmt.Errorf("read JSON snapshot rows: %w", err)
	}
	defer rows.Close()
	page := ResultPage{Columns: normalizedColumns, Rows: make([]ResultRow, 0, pageSize)}
	for rows.Next() {
		var rowID int64
		var encoded string
		if err := rows.Scan(&rowID, &encoded); err != nil {
			return ResultPage{}, fmt.Errorf("scan JSON snapshot row: %w", err)
		}
		if len(page.Rows) == pageSize {
			page.HasNext = true
			break
		}
		allValues, err := decodeCursorValues([]byte(encoded), len(schema))
		if err != nil {
			return ResultPage{}, err
		}
		selected := make([]interface{}, len(indexes))
		for index, source := range indexes {
			selected[index] = allValues[source]
		}
		rowKey := strconv.FormatInt(rowID, 10)
		page.Rows = append(page.Rows, ResultRow{Key: rowKey, Values: selected})
		page.NextKey = rowKey
	}
	if err := rows.Err(); err != nil {
		return ResultPage{}, fmt.Errorf("iterate JSON snapshot rows: %w", err)
	}
	return page, nil
}

func (adapter *Adapter) CountJSONSnapshotRows(ctx context.Context, runID string) (int64, error) {
	if adapter == nil || adapter.db == nil || ctx == nil || strings.TrimSpace(runID) == "" {
		return 0, fmt.Errorf("count JSON snapshot rows: invalid request")
	}
	var count int64
	if err := adapter.db.QueryRowContext(ctx,
		"SELECT ROW_COUNT FROM "+JSONSnapshotManifestTable+" WHERE RUN_ID = :1 AND STATUS = '"+jsonSnapshotReadyStatus+"'", runID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count JSON snapshot rows: %w", err)
	}
	return count, nil
}

func (adapter *Adapter) PurgeJSONSnapshotBatch(ctx context.Context, tx *sql.Tx, runID string, batchSize int) (int64, error) {
	if adapter == nil || tx == nil || ctx == nil || strings.TrimSpace(runID) == "" {
		return 0, fmt.Errorf("purge JSON snapshot: invalid request")
	}
	if batchSize == 0 {
		batchSize = defaultPurgeBatchSize
	}
	if batchSize < 1 || batchSize > maxPurgeBatchSize {
		return 0, fmt.Errorf("purge JSON snapshot: invalid batch size")
	}
	result, err := tx.ExecContext(ctx,
		"DELETE FROM "+JSONSnapshotRowsTable+" WHERE ROWID IN (SELECT ROWID FROM "+JSONSnapshotRowsTable+" WHERE RUN_ID = :1 AND ROWNUM <= :2)",
		runID, batchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("purge JSON snapshot rows: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("purge JSON snapshot rows: %w", err)
	}
	if deleted == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+JSONSnapshotManifestTable+" WHERE RUN_ID = :1", runID); err != nil {
			return 0, fmt.Errorf("purge JSON snapshot manifest: %w", err)
		}
	}
	return deleted, nil
}

func (adapter *Adapter) resetJSONSnapshot(ctx context.Context, tx *sql.Tx, runID string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+JSONSnapshotRowsTable+" WHERE RUN_ID = :1", runID); err != nil {
		return fmt.Errorf("reset JSON snapshot rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+JSONSnapshotManifestTable+" WHERE RUN_ID = :1", runID); err != nil {
		return fmt.Errorf("reset JSON snapshot manifest: %w", err)
	}
	return nil
}

func (adapter *Adapter) readJSONSnapshotSchema(ctx context.Context, runID string) ([]CursorColumn, error) {
	var encoded string
	if err := adapter.db.QueryRowContext(ctx,
		"SELECT SCHEMA_JSON FROM "+JSONSnapshotManifestTable+" WHERE RUN_ID = :1 AND STATUS = '"+jsonSnapshotReadyStatus+"'", runID, godror.ClobAsString(),
	).Scan(&encoded); err != nil {
		return nil, fmt.Errorf("read JSON snapshot schema: %w", err)
	}

	return decodeSnapshotSchema([]byte(encoded))
}

func cursorColumns(rows *sql.Rows) ([]CursorColumn, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("read JSON result cursor columns: %w", err)
	}
	if len(types) == 0 || len(types) > maxResultColumns {
		return nil, fmt.Errorf("read JSON result cursor columns: column count is invalid")
	}
	columns := make([]CursorColumn, len(types))
	seen := make(map[string]struct{}, len(types))
	for index, column := range types {
		name := strings.ToUpper(strings.TrimSpace(column.Name()))
		if _, err := normalizeIdentifier(name, "result cursor column"); err != nil {
			return nil, fmt.Errorf("read JSON result cursor columns: %w", err)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("read JSON result cursor columns: duplicate column %s", name)
		}
		seen[name] = struct{}{}
		nullable, known := column.Nullable()
		columns[index] = CursorColumn{Name: name, OracleType: normalizeOracleMetadataType(column.DatabaseTypeName()), Nullable: !known || nullable}
	}
	return columns, nil
}

func validateExpectedCursorColumns(actual []CursorColumn, expected []string) error {
	seen := make(map[string]struct{}, len(actual))
	for _, column := range actual {
		seen[column.Name] = struct{}{}
	}
	for _, value := range expected {
		column, err := normalizeIdentifier(value, "configured cursor column")
		if err != nil {
			return err
		}
		if _, ok := seen[column]; !ok {
			return fmt.Errorf("%w: configured cursor column %s was not returned", ErrMetadataMismatch, column)
		}
	}
	return nil
}

func snapshotColumnIndexes(schema []CursorColumn, requested []string) ([]int, []string, error) {
	byName := make(map[string]int, len(schema))
	for index, column := range schema {
		byName[strings.ToUpper(column.Name)] = index
	}
	indexes := make([]int, len(requested))
	normalized := make([]string, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for index, value := range requested {
		name, err := normalizeIdentifier(value, "requested snapshot column")
		if err != nil {
			return nil, nil, err
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, nil, configurationError("requested snapshot columns are duplicated")
		}
		source, ok := byName[name]
		if !ok {
			return nil, nil, fmt.Errorf("%w: requested snapshot column %s does not exist", ErrMetadataMismatch, name)
		}
		seen[name] = struct{}{}
		indexes[index], normalized[index] = source, name
	}
	return indexes, normalized, nil
}

func encodeCursorValues(values []interface{}, columns []CursorColumn) ([]byte, error) {
	if len(values) != len(columns) {
		return nil, fmt.Errorf("encode JSON cursor row: column count changed")
	}
	encoded := make([]json.RawMessage, len(values))
	for index, value := range values {
		item, err := encodeCursorValue(value, columns[index].OracleType)
		if err != nil {
			return nil, fmt.Errorf("encode JSON cursor column %s: %w", columns[index].Name, err)
		}
		encoded[index] = item
	}
	result, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("encode JSON cursor row: %w", err)
	}
	return result, nil
}

func encodeCursorValue(value interface{}, oracleType string) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage("null"), nil
	}
	switch typed := value.(type) {
	case godror.Number:
		if !validJSONNumber(string(typed)) {
			return nil, fmt.Errorf("invalid Oracle NUMBER")
		}
		return json.RawMessage(typed), nil
	case time.Time:
		return json.Marshal(typed.UTC().Format(time.RFC3339Nano))
	case []byte:
		if strings.Contains(strings.ToUpper(oracleType), "RAW") || strings.Contains(strings.ToUpper(oracleType), "BLOB") {
			return json.Marshal(base64.StdEncoding.EncodeToString(typed))
		}
		return json.Marshal(string(typed))
	case godror.Lob:
		return encodeCursorLOB(typed.Reader)
	case *godror.Lob:
		if typed == nil {
			return json.RawMessage("null"), nil
		}
		return encodeCursorLOB(typed.Reader)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	}
}

func encodeCursorLOB(reader io.Reader) (json.RawMessage, error) {
	if reader == nil {
		return json.RawMessage("null"), nil
	}
	value, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(value))
}

func decodeCursorValues(raw []byte, expected int) ([]interface{}, error) {
	var values []interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil || len(values) != expected {
		return nil, fmt.Errorf("decode JSON snapshot row: invalid values")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode JSON snapshot row: trailing data")
	}
	return values, nil
}

func validJSONNumber(value string) bool {
	if strings.TrimSpace(value) != value || value == "" {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func jsonInputOracleType(value string) bool {
	switch normalizeOracleMetadataType(value) {
	case "CLOB", "NCLOB", "VARCHAR2", "NVARCHAR2", "CHAR", "NCHAR", "JSON":
		return true
	default:
		return false
	}
}

func normalizeOracleMetadataType(value string) string {
	return strings.ToUpper(strings.Join(strings.Fields(value), " "))
}

func snapshotRowKey(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case json.Number:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("invalid snapshot row key %T", value)
	}
}

func decodeSnapshotSchema(encoded []byte) ([]CursorColumn, error) {
	var schema []CursorColumn
	if len(bytes.TrimSpace(encoded)) == 0 {
		return nil, fmt.Errorf("empty snapshot schema")
	}
	if bytes.TrimSpace(encoded)[0] == '{' {
		var v2 selectSnapshotSchema
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&v2); err != nil || v2.Version != 2 || len(v2.DatasetSchemaHash) != 64 {
			return nil, fmt.Errorf("invalid v2 snapshot schema")
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("trailing snapshot schema")
		}
		schema = v2.Columns
	} else {
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&schema); err != nil {
			return nil, err
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("trailing snapshot schema")
		}
	}
	if len(schema) == 0 || len(schema) > maxResultColumns {
		return nil, fmt.Errorf("invalid snapshot column count")
	}
	return schema, nil
}
