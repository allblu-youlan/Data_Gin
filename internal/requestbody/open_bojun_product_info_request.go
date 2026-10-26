package requestbody

// OpenBojunProductInfoQueryRequest 查询基础商品资料；不传商品筛选时分页查询全量资料。
// 商品编码、款色、款号至多传一种，按列表精确匹配。
type OpenBojunProductInfoQueryRequest struct {
	ProductCode  []string `json:"productCode"`  // SKU 条码列表，最多 100 项。
	ProductColor []string `json:"productColor"` // 款色列表，最多 20 项。
	ProductName  []string `json:"productName"`  // 款号列表，最多 20 项。
	Page         *int     `json:"page"`         // 页码，从 1 开始，默认 1。
	PageSize     *int     `json:"pageSize"`     // 每页条数，默认 50，最多 100。
}
