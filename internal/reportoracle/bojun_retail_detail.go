package reportoracle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/godror/godror"
)

const bojunRetailItemsByDocNoSQLPrefix = bojunRetailItemsSelectSQL + `
WHERE b.ISACTIVE = 'Y'
  AND a.DOCNO IN (`

const bojunRetailItemsByDocNoSQLSuffix = `)
ORDER BY a.DOCNO, c.NO`

const bojunRetailPayItemsByDocNoSQLPrefix = `
SELECT b.DOCNO,
       a.C_PAYWAY_ID,
       k.NAME AS C_PAYWAY_NAME,
       SUM(a.PAYAMOUNT) AS TOT_AMT_CX` + bojunRetailPayItemsFromSQL + `
  AND b.DOCNO IN (`

const bojunRetailPayItemsByDocNoSQLSuffix = `)
GROUP BY b.DOCNO, a.C_PAYWAY_ID, k.NAME
ORDER BY b.DOCNO, a.C_PAYWAY_ID`

type BojunRetailDetailRow struct {
	DocNo        string
	ItemsJSON    string
	PayItemsJSON string
}

type bojunRetailPayItemByDocNo struct {
	DocNo      string   `json:"-"`
	PaywayID   int64    `json:"cPaywayId"`
	PaywayName string   `json:"cPaywayName"`
	PayAmount  *float64 `json:"payamount"`
}

func (adapter *Adapter) QueryBojunRetailDetailsByDocNos(
	ctx context.Context,
	docNos []string,
) ([]BojunRetailDetailRow, error) {
	if adapter == nil || adapter.db == nil {
		return nil, fmt.Errorf("query bojun Oracle retail details: adapter is closed")
	}
	normalizedDocNos, err := normalizeBojunRetailDocNos(docNos)
	if err != nil {
		return nil, err
	}

	itemsByDocNo, err := adapter.queryBojunRetailItemsByDocNos(ctx, normalizedDocNos)
	if err != nil {
		return nil, err
	}
	payItemsByDocNo, err := adapter.queryBojunRetailPayItemsByDocNos(ctx, normalizedDocNos)
	if err != nil {
		return nil, err
	}

	result := make([]BojunRetailDetailRow, 0, len(normalizedDocNos))
	for _, docNo := range normalizedDocNos {
		itemsJSON, marshalErr := marshalBojunRetailDetailItems(itemsByDocNo[docNo])
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal bojun Oracle retail items %s: %w", docNo, marshalErr)
		}
		payItemsJSON, marshalErr := marshalBojunRetailDetailPayItems(payItemsByDocNo[docNo])
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal bojun Oracle retail pay items %s: %w", docNo, marshalErr)
		}
		result = append(result, BojunRetailDetailRow{
			DocNo: docNo, ItemsJSON: itemsJSON, PayItemsJSON: payItemsJSON,
		})
	}
	return result, nil
}

func (adapter *Adapter) queryBojunRetailItemsByDocNos(
	ctx context.Context,
	docNos []string,
) (map[string][]bojunRetailItem, error) {
	statement, arguments := buildBojunRetailDocNoQuery(
		bojunRetailItemsByDocNoSQLPrefix,
		bojunRetailItemsByDocNoSQLSuffix,
		docNos,
	)
	rows, err := adapter.db.QueryContext(ctx, statement, appendBojunOracleFetchOptions(arguments, adapter)...)
	if err != nil {
		return nil, fmt.Errorf("query bojun Oracle retail items by docno: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]bojunRetailItem, len(docNos))
	for rows.Next() {
		item, scanErr := scanBojunRetailItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result[item.DocNo] = append(result[item.DocNo], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bojun Oracle retail items by docno: %w", err)
	}
	return result, nil
}

func (adapter *Adapter) queryBojunRetailPayItemsByDocNos(
	ctx context.Context,
	docNos []string,
) (map[string][]bojunRetailPayItemByDocNo, error) {
	statement, arguments := buildBojunRetailDocNoQuery(
		bojunRetailPayItemsByDocNoSQLPrefix,
		bojunRetailPayItemsByDocNoSQLSuffix,
		docNos,
	)
	rows, err := adapter.db.QueryContext(ctx, statement, appendBojunOracleFetchOptions(arguments, adapter)...)
	if err != nil {
		return nil, fmt.Errorf("query bojun Oracle retail pay items by docno: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]bojunRetailPayItemByDocNo, len(docNos))
	for rows.Next() {
		item, scanErr := scanBojunRetailPayItemByDocNo(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result[item.DocNo] = append(result[item.DocNo], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bojun Oracle retail pay items by docno: %w", err)
	}
	return result, nil
}

func normalizeBojunRetailDocNos(docNos []string) ([]string, error) {
	if len(docNos) == 0 || len(docNos) > maxBojunRetailBatchSize {
		return nil, fmt.Errorf("query bojun Oracle retail details: docno count must be between 1 and %d", maxBojunRetailBatchSize)
	}
	result := make([]string, 0, len(docNos))
	seen := make(map[string]struct{}, len(docNos))
	for _, value := range docNos {
		docNo := strings.TrimSpace(value)
		if docNo == "" {
			return nil, fmt.Errorf("query bojun Oracle retail details: docno is required")
		}
		if _, exists := seen[docNo]; exists {
			continue
		}
		seen[docNo] = struct{}{}
		result = append(result, docNo)
	}
	return result, nil
}

func buildBojunRetailDocNoQuery(prefix, suffix string, docNos []string) (string, []interface{}) {
	placeholders := make([]string, 0, len(docNos))
	arguments := make([]interface{}, 0, len(docNos))
	for index, docNo := range docNos {
		placeholders = append(placeholders, ":"+strconv.Itoa(index+1))
		arguments = append(arguments, docNo)
	}
	return prefix + strings.Join(placeholders, ", ") + suffix, arguments
}

func appendBojunOracleFetchOptions(arguments []interface{}, adapter *Adapter) []interface{} {
	result := append([]interface{}{}, arguments...)
	return append(result,
		godror.PrefetchCount(adapter.prefetchRows),
		godror.FetchArraySize(adapter.fetchArraySize),
	)
}

func scanBojunRetailPayItemByDocNo(scanner bojunRetailScanner) (bojunRetailPayItemByDocNo, error) {
	var (
		docNo    sql.NullString
		paywayID sql.NullInt64
		name     sql.NullString
		amount   sql.NullFloat64
	)
	if err := scanner.Scan(&docNo, &paywayID, &name, &amount); err != nil {
		return bojunRetailPayItemByDocNo{}, fmt.Errorf("scan bojun Oracle retail pay item by docno: %w", err)
	}
	normalizedDocNo := strings.TrimSpace(docNo.String)
	if normalizedDocNo == "" || !paywayID.Valid {
		return bojunRetailPayItemByDocNo{}, fmt.Errorf("scan bojun Oracle retail pay item by docno: required field is empty")
	}
	return bojunRetailPayItemByDocNo{
		DocNo: normalizedDocNo, PaywayID: paywayID.Int64,
		PaywayName: strings.TrimSpace(name.String), PayAmount: nullableBojunFloat(amount),
	}, nil
}

func marshalBojunRetailDetailItems(items []bojunRetailItem) (string, error) {
	if items == nil {
		items = []bojunRetailItem{}
	}
	value, err := json.Marshal(items)
	return string(value), err
}

func marshalBojunRetailDetailPayItems(items []bojunRetailPayItemByDocNo) (string, error) {
	if items == nil {
		items = []bojunRetailPayItemByDocNo{}
	}
	value, err := json.Marshal(items)
	return string(value), err
}
