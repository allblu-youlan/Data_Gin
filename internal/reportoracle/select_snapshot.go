package reportoracle

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"github.com/godror/godror"
	"strconv"
)

var ErrSelectLimitExceeded = errors.New("REPORT_QUERY_LIMIT_EXCEEDED")

type selectSnapshotSchema struct {
	Version           int            `json:"version"`
	Columns           []CursorColumn `json:"columns"`
	DatasetSchemaHash string         `json:"datasetSchemaHash"`
}

func selectArguments(args []any) []any {
	out := append([]any{}, args...)
	for i, arg := range out {
		if named, ok := arg.(sql.NamedArg); ok {
			if number, ok := named.Value.(json.Number); ok {
				named.Value = godror.Number(number)
				out[i] = named
			}
		}
	}
	return append(out, godror.NumberAsString(), godror.ClobAsString())
}
func selectValue(value any, typ string) (any, error) {
	if value == nil {
		return nil, nil
	}
	if number, ok := value.(godror.Number); ok {
		return string(number), nil
	}
	if typ == "NUMBER" {
		switch v := value.(type) {
		case string:
			return v, nil
		case []byte:
			return string(v), nil
		case int64:
			return strconv.FormatInt(v, 10), nil
		case float64:
			return nil, fmt.Errorf("exact NUMBER was returned as a floating point value")
		}
	}
	raw, e := encodeCursorValue(value, typ)
	if e != nil {
		return nil, e
	}
	var decoded any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	e = d.Decode(&decoded)
	return decoded, e
}
func selectRow(rows *sql.Rows, columns []CursorColumn) (map[string]any, error) {
	values := make([]any, len(columns))
	dest := make([]any, len(values))
	for i := range dest {
		dest[i] = &values[i]
	}
	if e := rows.Scan(dest...); e != nil {
		return nil, e
	}
	out := make(map[string]any, len(values))
	for i, c := range columns {
		v, e := selectValue(values[i], c.OracleType)
		if e != nil {
			return nil, e
		}
		out[c.Name] = v
	}
	return out, nil
}
func (a *Adapter) QuerySelectSample(ctx context.Context, plan reportdataset.Plan, limit int) ([]CursorColumn, []map[string]any, error) {
	if limit < 0 || limit > 101 {
		return nil, nil, fmt.Errorf("invalid sample size")
	}
	statement := "SELECT * FROM (" + plan.SQL + ") WHERE ROWNUM <= " + strconv.Itoa(limit)
	rows, e := a.db.QueryContext(ctx, statement, selectArguments(plan.Args)...)
	if e != nil {
		return nil, nil, e
	}
	defer rows.Close()
	schema, e := cursorColumns(rows)
	if e != nil {
		return nil, nil, e
	}
	if e = validateExpectedCursorColumns(schema, plan.Columns); e != nil {
		return nil, nil, e
	}
	out := []map[string]any{}
	for rows.Next() {
		row, e := selectRow(rows, schema)
		if e != nil {
			return nil, nil, e
		}
		out = append(out, row)
	}
	return schema, out, rows.Err()
}
func (a *Adapter) ExecuteSelectSnapshot(ctx context.Context, runID string, c reportdataset.Compiled, plan reportdataset.Plan, maxRows int64, maxBytes int64) (snapshot CursorSnapshot, resultErr error) {
	if maxRows < 1 || maxBytes < 1 || runID == "" {
		return snapshot, fmt.Errorf("invalid SELECT snapshot limits")
	}
	tx, e := a.BeginTx(ctx, &sql.TxOptions{})
	if e != nil {
		return snapshot, e
	}
	defer tx.Rollback()
	if e = a.resetJSONSnapshot(ctx, tx, runID); e != nil {
		return snapshot, e
	}
	// Source SELECT and platform snapshot writes share the transaction; no source DML is generated.
	rows, e := tx.QueryContext(ctx, plan.SQL, selectArguments(plan.Args)...)
	if e != nil {
		return snapshot, e
	}
	defer rows.Close()
	schema, e := cursorColumns(rows)
	if e != nil {
		return snapshot, e
	}
	if e = validateExpectedCursorColumns(schema, plan.Columns); e != nil {
		return snapshot, e
	}
	snapshot.Columns = append([]CursorColumn{}, schema...)
	for _, o := range c.Outputs {
		snapshot.Columns = append(snapshot.Columns, CursorColumn{Name: o.Name, OracleType: o.OracleType, Nullable: true})
	}
	snapshot.SchemaHash = c.SchemaHash
	encodedSchema, e := json.Marshal(selectSnapshotSchema{Version: 2, Columns: snapshot.Columns, DatasetSchemaHash: c.SchemaHash})
	if e != nil {
		return snapshot, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO "+JSONSnapshotManifestTable+" (RUN_ID, SCHEMA_JSON, SCHEMA_HASH, ROW_COUNT, STATUS, CREATED_AT) VALUES (:1, :2, :3, 0, 'WRITING', SYSTIMESTAMP)", runID, godror.Lob{Reader: bytes.NewReader(encodedSchema), IsClob: true}, snapshot.SchemaHash); e != nil {
		return snapshot, e
	}
	batch := make([][]byte, 0, jsonSnapshotInsertBatch)
	var size int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if e := insertJSONSnapshotBatch(ctx, tx, runID, snapshot.RowCount-int64(len(batch))+1, batch); e != nil {
			return e
		}
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		if snapshot.RowCount >= maxRows {
			return snapshot, ErrSelectLimitExceeded
		}
		values, e := selectRow(rows, schema)
		if e != nil {
			return snapshot, e
		}
		values = c.Materialize(values)
		ordered := make([]any, len(snapshot.Columns))
		for i, col := range snapshot.Columns {
			ordered[i] = values[col.Name]
		}
		encoded, e := json.Marshal(ordered)
		if e != nil {
			return snapshot, e
		}
		size += int64(len(encoded))
		if size > maxBytes {
			return snapshot, ErrSelectLimitExceeded
		}
		snapshot.RowCount++
		batch = append(batch, encoded)
		if len(batch) == cap(batch) {
			if e = flush(); e != nil {
				return snapshot, e
			}
		}
	}
	if e = rows.Err(); e != nil {
		return snapshot, e
	}
	if e = rows.Close(); e != nil {
		return snapshot, e
	}
	if e = flush(); e != nil {
		return snapshot, e
	}
	result, e := tx.ExecContext(ctx, "UPDATE "+JSONSnapshotManifestTable+" SET ROW_COUNT = :1, STATUS = 'READY' WHERE RUN_ID = :2 AND STATUS = 'WRITING'", snapshot.RowCount, runID)
	if e != nil {
		return snapshot, e
	}
	if affected, e := result.RowsAffected(); e != nil || affected != 1 {
		return snapshot, fmt.Errorf("snapshot manifest changed")
	}
	if e = tx.Commit(); e != nil {
		return snapshot, fmt.Errorf("SELECT_SNAPSHOT_COMMIT_UNKNOWN: %w", e)
	}
	return snapshot, nil
}
func (a *Adapter) ValidateSelectSnapshot(ctx context.Context, runID, schemaHash string) (int64, error) {
	var rows int64
	var hash string
	if e := a.db.QueryRowContext(ctx, "SELECT ROW_COUNT, SCHEMA_HASH FROM "+JSONSnapshotManifestTable+" WHERE RUN_ID = :1 AND STATUS = 'READY'", runID).Scan(&rows, &hash); e != nil {
		return 0, e
	}
	if hash != schemaHash {
		return 0, fmt.Errorf("REPORT_METADATA_CHANGED: snapshot identity mismatch")
	}
	return rows, nil
}
