package requestbody

type OpenBojunStoreQueryRequest struct {
	StoreCode  string `json:"storeCode"`
	NamePrefix string `json:"namePrefix"`
	IsActive   string `json:"isActive"`
	IsStop     string `json:"isStop"`
	Page       *int   `json:"page"`
	PageSize   *int   `json:"pageSize"`
}
