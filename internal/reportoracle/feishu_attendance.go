package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/godror/godror"
)

type FeishuAttendanceDataset string

const (
	FeishuAttendanceDaily FeishuAttendanceDataset = "daily"
	FeishuAttendancePunch FeishuAttendanceDataset = "punch"
	FeishuAttendanceItem  FeishuAttendanceDataset = "item"
)

type FeishuAttendanceQuery struct {
	StartDate string
	EndDate   string
	Filters   map[string]string
	Offset    int
	PageSize  int
}

// FeishuAttendanceRow preserves Oracle column names, exact numbers and NULLs.
type FeishuAttendanceRow map[string]*string

type FeishuAttendanceColumn struct {
	Name       string `json:"name"`
	Comment    string `json:"comment"`
	DataType   string `json:"dataType"`
	Length     int    `json:"length"`
	Nullable   bool   `json:"nullable"`
	Filterable bool   `json:"filterable"`
}

type feishuAttendanceTable struct {
	name    string
	orderBy string
	columns []FeishuAttendanceColumn
}

// FeishuAttendanceColumns returns the fixed public projection for a dataset.
func FeishuAttendanceColumns(dataset FeishuAttendanceDataset) []FeishuAttendanceColumn {
	return append([]FeishuAttendanceColumn(nil), feishuAttendanceTables[dataset].columns...)
}

// NormalizeFeishuAttendanceQuery validates the employee scope and bounded date
// range before callers acquire a pool or execute a database query.
func NormalizeFeishuAttendanceQuery(dataset FeishuAttendanceDataset, query FeishuAttendanceQuery) (FeishuAttendanceQuery, error) {
	if _, ok := feishuAttendanceTables[dataset]; !ok {
		return FeishuAttendanceQuery{}, fmt.Errorf("unknown feishu attendance dataset")
	}
	query.StartDate, query.EndDate = strings.TrimSpace(query.StartDate), strings.TrimSpace(query.EndDate)
	start, startErr := time.Parse("2006-01-02", query.StartDate)
	end, endErr := time.Parse("2006-01-02", query.EndDate)
	if startErr != nil || endErr != nil || start.Year() < 1 || end.Year() < 1 || end.Before(start) || end.Sub(start) > 30*24*time.Hour {
		return FeishuAttendanceQuery{}, fmt.Errorf("startDate and endDate must define at most 31 days in YYYY-MM-DD format")
	}
	if query.PageSize < 1 || query.PageSize > 100 || query.Offset < 0 || query.Offset > 99900 {
		return FeishuAttendanceQuery{}, fmt.Errorf("invalid feishu attendance pagination")
	}
	if len(query.Filters) != 2 {
		return FeishuAttendanceQuery{}, fmt.Errorf("USER_NAME and DEPARTMENT_NAME are required feishu attendance filters")
	}
	filters := make(map[string]string, 2)
	for name, value := range query.Filters {
		var maxLength int
		switch name {
		case "USER_NAME":
			maxLength = 200
		case "DEPARTMENT_NAME":
			maxLength = 1000
		default:
			return FeishuAttendanceQuery{}, fmt.Errorf("unsupported feishu attendance filter %q", name)
		}
		value = strings.TrimSpace(value)
		if value == "" || utf8.RuneCountInString(value) > maxLength {
			return FeishuAttendanceQuery{}, fmt.Errorf("invalid feishu attendance filter %q", name)
		}
		filters[name] = value
	}
	query.Filters = filters
	return query, nil
}

// QueryFeishuAttendance returns at most PageSize+1 rows; the caller uses the
// additional row to determine whether another page exists.
func (adapter *Adapter) QueryFeishuAttendance(ctx context.Context, dataset FeishuAttendanceDataset, query FeishuAttendanceQuery) ([]FeishuAttendanceRow, error) {
	if adapter == nil || adapter.db == nil || ctx == nil {
		return nil, fmt.Errorf("query feishu attendance: missing connection or context")
	}
	query, err := NormalizeFeishuAttendanceQuery(dataset, query)
	if err != nil {
		return nil, err
	}
	table := feishuAttendanceTables[dataset]
	statement, arguments := buildFeishuAttendanceQuery(dataset, table, query)
	arguments = append(arguments, godror.NumberAsString())
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query feishu attendance %s: %w", dataset, mapExecutionError(ctx, err))
	}
	defer rows.Close()
	result := make([]FeishuAttendanceRow, 0, query.PageSize+1)
	for rows.Next() {
		values := make([]sql.NullString, len(table.columns))
		targets := make([]any, len(values))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("scan feishu attendance %s: %w", dataset, mapExecutionError(ctx, err))
		}
		row := make(FeishuAttendanceRow, len(values))
		for i, column := range table.columns {
			row[column.Name] = nil
			if values[i].Valid {
				value := values[i].String
				row[column.Name] = &value
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read feishu attendance %s: %w", dataset, mapExecutionError(ctx, err))
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close feishu attendance %s: %w", dataset, mapExecutionError(ctx, err))
	}
	return result, nil
}

func buildFeishuAttendanceQuery(dataset FeishuAttendanceDataset, table feishuAttendanceTable, query FeishuAttendanceQuery) (string, []any) {
	identityAlias := "a"
	if dataset != FeishuAttendanceDaily {
		identityAlias = "d"
	}
	projection := make([]string, 0, len(table.columns))
	for _, column := range table.columns {
		alias := "a"
		switch column.Name {
		case "USER_NAME", "EMPLOYEE_NO", "DEPARTMENT_NAME":
			alias = identityAlias
		}
		expression := alias + "." + column.Name
		if column.DataType == "DATE" {
			format := "YYYY-MM-DD"
			if column.Name == "MODIFIED_TIME" {
				format = "YYYY-MM-DD HH24:MI:SS"
			}
			expression = "TO_CHAR(" + expression + ", '" + format + "')"
		}
		projection = append(projection, expression)
	}
	statement := "SELECT " + strings.Join(projection, ", ") + "\nFROM " + table.name + " a"
	if dataset != FeishuAttendanceDaily {
		statement += "\nJOIN YL_DBS.ODS_FEISHU_ATTENDANCE_DAILY d ON d.USER_ID = a.USER_ID AND d.WORK_DATE = a.WORK_DATE"
	}
	statement += "\nWHERE a.WORK_DATE >= TO_DATE(:1, 'YYYY-MM-DD') AND a.WORK_DATE < TO_DATE(:2, 'YYYY-MM-DD') + 1"
	statement += "\n  AND " + identityAlias + ".DEPARTMENT_NAME = :3 AND " + identityAlias + ".USER_NAME = :4"
	statement += "\nORDER BY " + table.orderBy + "\nOFFSET :5 ROWS FETCH NEXT :6 ROWS ONLY"
	arguments := []any{query.StartDate, query.EndDate, query.Filters["DEPARTMENT_NAME"], query.Filters["USER_NAME"], query.Offset, query.PageSize + 1}
	return statement, arguments
}
