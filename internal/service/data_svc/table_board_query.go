package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"

	"gin-biz-web-api/internal/tableboard"
	"github.com/godror/godror"
)

var ErrBoardSourceInvalid = errors.New("看板来源配置无效")

// QueryBoardSource reads once per configured dataset, independent of the number
// of cells that reference it. It shares the report workers' cross-replica budget.
func QueryBoardSource(ctx context.Context, actor uint, source tableboard.Source) ([]map[string]*string, error) {
	if err := source.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBoardSourceInvalid, err)
	}
	var result []map[string]*string
	err := withBoardConnection(ctx, source.DatasourceID, func(ctx context.Context, conn *reportoracle.Adapter) error {
		var err error
		result, err = queryBoardDetail(ctx, conn, actor, source)
		return err
	})
	return result, err
}

func detailPlan(ctx context.Context, conn *reportoracle.Adapter, actor uint, source tableboard.Source) (string, []any, error) {
	m := tableboard.Metric{DatasourceID: source.DatasourceID, Owner: source.Owner, TableName: source.TableName}
	rel, err := NewReportBojunCatalogService().boardRelation(ctx, conn, actor, m, source.Fields, nil, true)
	if err != nil {
		return "", nil, err
	}
	query, err := boardSelectFrom(source, rel.schema, rel.from)
	return query, rel.args, err
}

func queryBoardDetail(ctx context.Context, conn *reportoracle.Adapter, actor uint, source tableboard.Source) ([]map[string]*string, error) {
	statement, args, err := detailPlan(ctx, conn, actor, source)
	if err != nil {
		return nil, err
	}
	args = append(args, sql.Named("board_limit", source.Limit), godror.NumberAsString())
	rows, err := conn.QuerySelect(ctx, statement, args...)
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
	return boardSelectFrom(source, schema, `"`+source.Owner+`"."`+source.TableName+`"`)
}
func boardSelectFrom(source tableboard.Source, schema []reportoracle.ResultColumn, from string) (string, error) {
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
	return `SELECT * FROM (SELECT ` + strings.Join(fields, ", ") + ` FROM ` + from + ` ORDER BY ` + strings.Join(ordering, ", ") + `) WHERE ROWNUM <= :board_limit`, nil
}
