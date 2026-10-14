package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const BojunStoreTable = "BOSNDs3.C_STORE@Link_To_Bojun"

const bojunStoreQueryBaseSQL = `
SELECT s.ID,
       s.CODE,
       s.NAME,
       s.ISACTIVE,
       s.ISSTOP
FROM ` + BojunStoreTable + ` s
WHERE s.CODE IS NOT NULL`

type BojunStoreQuery struct {
	StoreCode  string
	NamePrefix string
	IsActive   string
	IsStop     string
	PageSize   int
	Offset     int
}

type BojunStoreRow struct {
	StoreID   int64
	StoreCode string
	StoreName string
	IsActive  string
	IsStop    *string
}

func (adapter *Adapter) QueryBojunStores(ctx context.Context, query BojunStoreQuery) ([]BojunStoreRow, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query bojun stores: adapter is closed")
	}
	if ctx == nil || query.PageSize < 1 || query.PageSize > 100 || query.Offset < 0 {
		return nil, fmt.Errorf("query bojun stores: invalid pagination or context")
	}
	query.StoreCode = strings.TrimSpace(query.StoreCode)
	query.NamePrefix = strings.TrimSpace(query.NamePrefix)
	query.IsActive = strings.TrimSpace(query.IsActive)
	query.IsStop = strings.TrimSpace(query.IsStop)
	if query.IsActive == "" {
		query.IsActive = "Y"
	}
	if len(query.StoreCode) > 80 || len(query.NamePrefix) > 255 ||
		(query.IsActive != "Y" && query.IsActive != "N" && query.IsActive != "ALL") ||
		(query.IsStop != "" && query.IsStop != "Y" && query.IsStop != "N") {
		return nil, fmt.Errorf("query bojun stores: invalid filter")
	}

	statement, arguments := buildBojunStoreQuery(query)
	rows, err := adapter.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query bojun stores: %w", err)
	}
	defer rows.Close()

	result := make([]BojunStoreRow, 0, query.PageSize+1)
	for rows.Next() {
		var row BojunStoreRow
		var storeCode, storeName, isActive, isStop sql.NullString
		if err := rows.Scan(&row.StoreID, &storeCode, &storeName, &isActive, &isStop); err != nil {
			return nil, fmt.Errorf("scan bojun store: %w", err)
		}
		row.StoreCode = strings.TrimSpace(storeCode.String)
		row.StoreName = strings.TrimSpace(storeName.String)
		row.IsActive = strings.TrimSpace(isActive.String)
		row.IsStop = nullableBojunString(isStop)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bojun stores: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close bojun stores: %w", err)
	}
	return result, nil
}

func buildBojunStoreQuery(query BojunStoreQuery) (string, []any) {
	var statement strings.Builder
	statement.WriteString(bojunStoreQueryBaseSQL)
	arguments := make([]any, 0, 6)
	appendFilter := func(fragment string, value string) {
		arguments = append(arguments, value)
		fmt.Fprintf(&statement, fragment, len(arguments))
	}
	if query.IsActive != "ALL" {
		appendFilter("\n  AND s.ISACTIVE = :%d", query.IsActive)
	}
	if query.StoreCode != "" {
		appendFilter("\n  AND s.CODE = :%d", query.StoreCode)
	}
	if query.NamePrefix != "" {
		prefix := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(query.NamePrefix)
		appendFilter("\n  AND s.NAME LIKE :%d ESCAPE '!'", prefix+"%")
	}
	if query.IsStop != "" {
		appendFilter("\n  AND s.ISSTOP = :%d", query.IsStop)
	}
	fmt.Fprintf(&statement, "\nORDER BY s.ID\nOFFSET :%d ROWS FETCH NEXT :%d ROWS ONLY", len(arguments)+1, len(arguments)+2)
	arguments = append(arguments, query.Offset, query.PageSize+1)
	return statement.String(), arguments
}

func nullableBojunString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := strings.TrimSpace(value.String)
	return &result
}
