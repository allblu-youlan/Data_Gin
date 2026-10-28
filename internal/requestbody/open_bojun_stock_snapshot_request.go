package requestbody

// OpenBojunStockSnapshotQueryRequest accepts a YYYY-MM-DD date, one store ID,
// and 1-100 IDs per product list. The server builds the procedure SQL selectors.
type OpenBojunStockSnapshotQueryRequest struct {
	ChangeDate     string  `json:"changeDate"`
	StoreID        int64   `json:"storeId"`
	ProductID      []int64 `json:"productId"`
	ProductColorID []int64 `json:"productColorId"`
	Page           *int    `json:"page"`
	PageSize       *int    `json:"pageSize"`
}
