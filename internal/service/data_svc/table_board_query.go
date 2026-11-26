package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/internal/tableboard"
	"github.com/godror/godror"
)

var ErrBoardSourceInvalid = errors.New("看板来源配置无效")

// QueryBoardSource reads once per configured dataset, independent of the number
// of cells that reference it. It shares the report workers' cross-replica budget.
func QueryBoardSource(ctx context.Context, source tableboard.Source) ([]map[string]*string, error) {
	if err := source.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBoardSourceInvalid, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	repo := reportrepo.New()
	ds, err := repo.GetReportDatasource(ctx, source.DatasourceID)
	if err != nil {
		return nil, classifyReportDatasourceStoreError(err)
	}
	if !ds.Enabled {
		return nil, fmt.Errorf("%w: 连接已停用", ErrBoardSourceInvalid)
	}
	if ds.QueryTimeoutSeconds > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, time.Duration(ds.QueryTimeoutSeconds)*time.Second)
		defer stop()
	}
	release, err := repo.AcquireSelectDatasourceSlot(ctx, ds.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	password, err := (reportsecret.EnvironmentKeyring{}).Decrypt(ds.CredentialKeyVersion, ds.PasswordCiphertext)
	if err != nil {
		return nil, ErrReportDatasourceCredentialUnavailable
	}
	conn, done, err := acquireSelectOracle(ctx, *ds, password)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReportDatasourceOracleUnavailable, err)
	}
	defer done()
	ref := reportoracle.ResultTableRef{Owner: source.Owner, Name: source.TableName}
	schema, err := conn.InspectResultTable(ctx, ref)
	if err != nil {
		return nil, err
	}
	statement, err := boardSelect(source, schema)
	if err != nil {
		return nil, err
	}
	rows, err := conn.QuerySelect(ctx, statement, sql.Named("board_limit", source.Limit), godror.NumberAsString())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]*string, 0, source.Limit)
	totalBytes := 0
	for rows.Next() {
		values := make([]any, len(source.Fields))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		record := make(map[string]*string, len(values))
		for i, field := range source.Fields {
			if values[i] == nil {
				record[field] = nil
				continue
			}
			var value string
			switch v := values[i].(type) {
			case time.Time:
				value = v.Format("2006-01-02 15:04:05.999999999")
			case []byte:
				value = string(v)
			default:
				value = fmt.Sprint(v)
			}
			totalBytes += len(value)
			if totalBytes > 2<<20 {
				return nil, fmt.Errorf("%w: 数据超过 2 MiB，请减少行数或字段", ErrBoardSourceInvalid)
			}
			record[field] = &value
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func boardSelect(source tableboard.Source, schema []reportoracle.ResultColumn) (string, error) {
	if err := source.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrBoardSourceInvalid, err)
	}
	ref := reportoracle.ResultTableRef{Owner: source.Owner, Name: source.TableName}
	if _, err := reportoracle.CompileResultSnapshotContract(reportoracle.ResultTableSnapshotRef(ref, source.Fields), schema); err != nil {
		return "", fmt.Errorf("%w: 请重新选择表和字段", ErrBoardSourceInvalid)
	}
	types := make(map[string]string, len(schema))
	for _, col := range schema {
		types[col.Name] = col.DataType
	}
	fields := make([]string, len(source.Fields))
	for i, field := range source.Fields {
		kind, err := reportoracle.NormalizeOperationDataType(types[field])
		if err != nil || kind == "CLOB" || kind == "NCLOB" || kind == "LONG" {
			return "", fmt.Errorf("%w: %s 不是可展示的文本、数值或日期字段", ErrBoardSourceInvalid, field)
		}
		fields[i] = `"` + field + `"`
	}
	// For rows tied on every projected value the display is identical. Secondary
	// ordering by the projection also works on tables without a physical ROWID.
	ordering := []string{`"` + source.OrderBy + `"`}
	for i, field := range source.Fields {
		if field != source.OrderBy {
			ordering = append(ordering, fields[i])
		}
	}
	return `SELECT * FROM (SELECT ` + strings.Join(fields, ", ") + ` FROM "` + ref.Owner + `"."` + ref.Name + `" ORDER BY ` + strings.Join(ordering, ", ") + `) WHERE ROWNUM <= :board_limit`, nil
}
