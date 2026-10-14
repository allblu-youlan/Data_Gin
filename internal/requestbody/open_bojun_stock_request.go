package requestbody

type OpenBojunStockQueryRequest struct {
	ProductCode  string `json:"productCode"`
	ProductColor string `json:"productColor"`
	ProductName  string `json:"productName"`
	StoreCode    string `json:"storeCode"`
	Page         *int   `json:"page"`
	PageSize     *int   `json:"pageSize"`
}
