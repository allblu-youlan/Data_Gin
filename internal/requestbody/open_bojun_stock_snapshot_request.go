package requestbody

import "encoding/json"

// OpenBojunStockSnapshotQueryRequest accepts a YYYY-MM-DD date, one store ID,
// and 1-100 IDs per product list. IDs accept source strings and legacy numbers;
// the service validates positive integers before building procedure selectors.
type OpenBojunStockSnapshotQueryRequest struct {
	ChangeDate     string        `json:"changeDate"`
	StoreID        json.Number   `json:"storeId"`
	ProductID      []json.Number `json:"productId"`
	ProductColorID []json.Number `json:"productColorId"`
	Page           *int          `json:"page"`
	PageSize       *int          `json:"pageSize"`
}
