package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/godror/godror"
)

type TmallDataset string

const (
	TmallOrders     TmallDataset = "orders"
	TmallSycm       TmallDataset = "sycm"
	TmallQztg       TmallDataset = "qztg"
	TmallTot        TmallDataset = "tot"
	TmallQztgHourly TmallDataset = "qztg_hourly"
	TmallSycmHourly TmallDataset = "sycm_hourly"
)

type TmallQuery struct {
	StartTime string
	EndTime   string
	TimeField string
	BatchNo   string
	StartDate string
	EndDate   string
	Filters   map[string]string
	Offset    int
	PageSize  int
}

// TmallRow preserves Oracle column names, NULLs and exact decimal values.
// Numbers are strings so IDs and financial metrics do not lose precision.
type TmallRow map[string]*string

type TmallColumn struct {
	Name       string `json:"name"`
	Comment    string `json:"comment"`
	DataType   string `json:"dataType"`
	Length     int    `json:"length"`
	Nullable   bool   `json:"nullable"`
	Filterable bool   `json:"filterable"`
}

type tmallTable struct {
	name          string
	payDateLayout string
	columns       []TmallColumn
}

func TmallColumns(dataset TmallDataset) []TmallColumn {
	return append([]TmallColumn(nil), tmallTables[dataset].columns...)
}

// NormalizeTmallQuery validates the fixed dataset, date range or hourly batch and
// documented dimensions before a pool is acquired or a query is executed.
func NormalizeTmallQuery(dataset TmallDataset, query TmallQuery) (TmallQuery, error) {
	table, ok := tmallTables[dataset]
	if !ok {
		return TmallQuery{}, fmt.Errorf("unknown tmall dataset")
	}
	query.StartDate, query.EndDate = strings.TrimSpace(query.StartDate), strings.TrimSpace(query.EndDate)
	query.BatchNo = strings.TrimSpace(query.BatchNo)
	if dataset == TmallOrders {
		var err error
		query, err = normalizeTmallOrdersTime(query)
		if err != nil {
			return TmallQuery{}, err
		}
	} else if query.StartTime != "" || query.EndTime != "" || query.TimeField != "" {
		return TmallQuery{}, fmt.Errorf("time range is only supported for realtime orders")
	} else if dataset == TmallQztgHourly || dataset == TmallSycmHourly {
		if query.StartDate != "" || query.EndDate != "" {
			return TmallQuery{}, fmt.Errorf("startDate and endDate are not supported for hourly tmall datasets")
		}
		batch, err := time.Parse("200601021504", query.BatchNo)
		if err != nil || batch.Year() < 1 || batch.Minute() != 0 || batch.Format("200601021504") != query.BatchNo {
			return TmallQuery{}, fmt.Errorf("batchNo must identify the data hour in yyyyMMddHHmm format with minutes 00")
		}
	} else {
		if query.BatchNo != "" {
			return TmallQuery{}, fmt.Errorf("batchNo is only supported for hourly tmall datasets")
		}
		start, startErr := time.Parse("2006-01-02", query.StartDate)
		end, endErr := time.Parse("2006-01-02", query.EndDate)
		if startErr != nil || endErr != nil || start.Year() < 1 || end.Year() < 1 || end.Before(start) || end.Sub(start) > 30*24*time.Hour {
			return TmallQuery{}, fmt.Errorf("startDate and endDate must define at most 31 days in YYYY-MM-DD format")
		}
	}
	if query.PageSize < 1 || query.PageSize > 100 || query.Offset < 0 || query.Offset > 999*query.PageSize {
		return TmallQuery{}, fmt.Errorf("invalid tmall pagination")
	}
	filters := make(map[string]string, len(query.Filters))
	for name, value := range query.Filters {
		maxLength := 0
		for _, column := range table.columns {
			if column.Name == name && column.Filterable {
				maxLength = column.Length
				break
			}
		}
		value = strings.TrimSpace(value)
		if maxLength == 0 || value == "" || len(value) > maxLength {
			return TmallQuery{}, fmt.Errorf("invalid tmall dimension filter %q", name)
		}
		if dataset == TmallOrders && name == "NUM_IID" {
			if _, err := strconv.ParseUint(value, 10, 64); err != nil || len(value) > 18 {
				return TmallQuery{}, fmt.Errorf("invalid numeric product ID")
			}
		}
		filters[name] = value
	}
	query.Filters = filters
	return query, nil
}

func (adapter *Adapter) QueryTmall(ctx context.Context, dataset TmallDataset, query TmallQuery) ([]TmallRow, error) {
	if adapter == nil || adapter.db == nil || ctx == nil {
		return nil, fmt.Errorf("query tmall: missing connection or context")
	}
	query, err := NormalizeTmallQuery(dataset, query)
	if err != nil {
		return nil, err
	}
	table := tmallTables[dataset]
	statement, arguments := buildTmallQuery(table, query)
	arguments = append(arguments, godror.NumberAsString())
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query tmall %s: %w", dataset, err)
	}
	defer rows.Close()
	result := make([]TmallRow, 0, query.PageSize+1)
	for rows.Next() {
		values := make([]sql.NullString, len(table.columns))
		targets := make([]any, len(values))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("scan tmall %s: %w", dataset, err)
		}
		row := make(TmallRow, len(values))
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
		return nil, fmt.Errorf("read tmall %s: %w", dataset, err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close tmall %s: %w", dataset, err)
	}
	return result, nil
}

func buildTmallQuery(table tmallTable, query TmallQuery) (string, []any) {
	projection := make([]string, 0, len(table.columns))
	for _, column := range table.columns {
		expression := "a." + column.Name
		if column.DataType == "DATE" {
			expression = "TO_CHAR(" + expression + ", 'YYYY-MM-DD HH24:MI:SS')"
		}
		projection = append(projection, expression)
	}
	statement := "SELECT " + strings.Join(projection, ", ") + "\nFROM " + table.name + " a"
	var arguments []any
	orderBy := "a.ID"
	if query.TimeField != "" {
		timeColumn := "a." + strings.ToUpper(query.TimeField)
		statement += "\nWHERE " + timeColumn + " >= TO_DATE(:1, 'YYYY-MM-DD HH24:MI:SS') AND " + timeColumn + " < TO_DATE(:2, 'YYYY-MM-DD HH24:MI:SS')"
		arguments = []any{query.StartTime, query.EndTime}
		orderBy = timeColumn + ", a.ID"
	} else if query.BatchNo != "" {
		statement += "\nWHERE a.BATCH_NO = :1"
		arguments = []any{query.BatchNo}
	} else {
		start, _ := time.Parse("2006-01-02", query.StartDate)
		end, _ := time.Parse("2006-01-02", query.EndDate)
		statement += "\nWHERE a.PAY_DATE >= :1 AND a.PAY_DATE < :2"
		arguments = []any{start.Format(table.payDateLayout), end.AddDate(0, 0, 1).Format(table.payDateLayout)}
		orderBy = "a.PAY_DATE, a.ID"
	}
	names := make([]string, 0, len(query.Filters))
	for name := range query.Filters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		arguments = append(arguments, query.Filters[name])
		statement += fmt.Sprintf("\n  AND a.%s = :%d", name, len(arguments))
	}
	statement += fmt.Sprintf("\nORDER BY %s\nOFFSET :%d ROWS FETCH NEXT :%d ROWS ONLY", orderBy, len(arguments)+1, len(arguments)+2)
	arguments = append(arguments, query.Offset, query.PageSize+1)
	return statement, arguments
}
