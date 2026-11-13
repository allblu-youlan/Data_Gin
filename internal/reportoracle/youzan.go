package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/godror/godror"
)

type YouzanDataset string

const (
	YouzanOrders     YouzanDataset = "orders"
	YouzanOrderItems YouzanDataset = "order_items"
)

type YouzanQuery struct {
	StartTime string
	EndTime   string
	TimeField string
	Filters   map[string]string
	Offset    int
	PageSize  int
}

// YouzanRow preserves Oracle column names, NULLs and exact decimal values.
type YouzanRow map[string]*string

// NormalizeYouzanQuery validates the bounded range or exact TID lookup and the
// dataset's published filters before a pool is acquired or SQL is executed.
func NormalizeYouzanQuery(dataset YouzanDataset, query YouzanQuery) (YouzanQuery, error) {
	table, ok := youzanTables[dataset]
	if !ok {
		return YouzanQuery{}, fmt.Errorf("unknown youzan dataset")
	}
	if query.PageSize < 1 || query.PageSize > 100 || query.Offset < 0 || query.Offset > 999*query.PageSize {
		return YouzanQuery{}, fmt.Errorf("invalid youzan pagination")
	}
	filters := make(map[string]string, len(query.Filters))
	for name, value := range query.Filters {
		value = strings.TrimSpace(value)
		maxLength, allowed := table.filterLimits[name]
		if !allowed || value == "" || len(value) > maxLength {
			return YouzanQuery{}, fmt.Errorf("invalid youzan dimension filter %q", name)
		}
		filters[name] = value
	}
	query.Filters = filters
	query.TimeField = strings.TrimSpace(query.TimeField)
	if query.TimeField == "" {
		query.TimeField = "modifiedDwd"
	}
	if query.TimeField != "modifiedDwd" && query.TimeField != "created" {
		return YouzanQuery{}, fmt.Errorf("timeField must be modifiedDwd or created")
	}
	query.StartTime, query.EndTime = strings.TrimSpace(query.StartTime), strings.TrimSpace(query.EndTime)
	if query.StartTime == "" && query.EndTime == "" && query.Filters["TID"] != "" {
		return query, nil
	}
	const layout = "2006-01-02 15:04:05"
	start, startErr := time.Parse(layout, query.StartTime)
	end, endErr := time.Parse(layout, query.EndTime)
	if startErr != nil || endErr != nil || start.Year() < 1 || end.Year() < 1 ||
		start.Format(layout) != query.StartTime || end.Format(layout) != query.EndTime ||
		!end.After(start) || end.Sub(start) > 31*24*time.Hour {
		return YouzanQuery{}, fmt.Errorf("startTime and endTime must define a positive range of at most 31 days in YYYY-MM-DD HH:mm:ss format, or both be omitted with an exact TID")
	}
	return query, nil
}

func (adapter *Adapter) QueryYouzan(ctx context.Context, dataset YouzanDataset, query YouzanQuery) ([]YouzanRow, error) {
	if adapter == nil || adapter.db == nil || ctx == nil {
		return nil, fmt.Errorf("query youzan: missing connection or context")
	}
	query, err := NormalizeYouzanQuery(dataset, query)
	if err != nil {
		return nil, err
	}
	table := youzanTables[dataset]
	statement, arguments := buildYouzanQuery(table, query)
	arguments = append(arguments, godror.NumberAsString())
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query youzan %s: %w", dataset, err)
	}
	defer rows.Close()
	result := make([]YouzanRow, 0, query.PageSize+1)
	for rows.Next() {
		values := make([]sql.NullString, len(table.columns))
		targets := make([]any, len(values))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("scan youzan %s: %w", dataset, err)
		}
		row := make(YouzanRow, len(values))
		for i, column := range table.columns {
			row[column.name] = nil
			if values[i].Valid {
				value := values[i].String
				row[column.name] = &value
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read youzan %s: %w", dataset, err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close youzan %s: %w", dataset, err)
	}
	return result, nil
}

func buildYouzanQuery(table youzanTable, query YouzanQuery) (string, []any) {
	projection := make([]string, 0, len(table.columns))
	for _, column := range table.columns {
		expression := "a." + column.name
		if column.isDate {
			expression = "TO_CHAR(" + expression + ", 'YYYY-MM-DD HH24:MI:SS')"
		}
		projection = append(projection, expression)
	}
	statement := "SELECT " + strings.Join(projection, ", ") + "\nFROM " + table.name + " a"
	var arguments []any
	var predicates []string
	orderBy := "a.ID"
	if query.StartTime != "" {
		timeColumn := "a.MODIFIED_DWD"
		if query.TimeField == "created" {
			timeColumn = "a.CREATED"
		}
		predicates = append(predicates, timeColumn+" >= TO_DATE(:1, 'YYYY-MM-DD HH24:MI:SS') AND "+timeColumn+" < TO_DATE(:2, 'YYYY-MM-DD HH24:MI:SS')")
		arguments = append(arguments, query.StartTime, query.EndTime)
		orderBy = timeColumn + ", a.ID"
	}
	names := make([]string, 0, len(query.Filters))
	for name := range query.Filters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		arguments = append(arguments, query.Filters[name])
		predicates = append(predicates, fmt.Sprintf("a.%s = :%d", name, len(arguments)))
	}
	statement += "\nWHERE " + strings.Join(predicates, "\n  AND ")
	statement += fmt.Sprintf("\nORDER BY %s\nOFFSET :%d ROWS FETCH NEXT :%d ROWS ONLY", orderBy, len(arguments)+1, len(arguments)+2)
	arguments = append(arguments, query.Offset, query.PageSize+1)
	return statement, arguments
}
