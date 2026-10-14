package requestbody

type OpenBojunStockQueryRequest struct {
	ProductCode string `json:"productCode"`
	StoreCode   string `json:"storeCode"`
	Page        *int   `json:"page"`
	PageSize    *int   `json:"pageSize"`
}
