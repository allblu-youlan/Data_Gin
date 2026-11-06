package requestbody

// OpenTmallQueryRequest queries business dates or an hourly snapshot.
// Filters contains exact-match dimension values keyed by Oracle column name.
// Each endpoint only accepts its documented dimension columns.
type OpenTmallQueryRequest struct {
	BatchNo   string            `json:"-"`         // 由小时接口映射，日维度请求不接受此字段。
	StartDate string            `json:"startDate"` // YYYY-MM-DD，必填。
	EndDate   string            `json:"endDate"`   // YYYY-MM-DD，必填，最多31天。
	Filters   map[string]string `json:"filters"`
	Page      *int              `json:"page"`     // 默认1，最大1000。
	PageSize  *int              `json:"pageSize"` // 默认50，最大100。
}

// OpenTmallHourlyQueryRequest selects the data's business hour, not its import time.
type OpenTmallHourlyQueryRequest struct {
	BatchNo  string            `json:"batchNo"` // 必填，yyyyMMddHHmm，分钟必须为00，如202611041300。
	Filters  map[string]string `json:"filters"`
	Page     *int              `json:"page"`     // 默认1，最大1000。
	PageSize *int              `json:"pageSize"` // 默认50，最大100。
}
