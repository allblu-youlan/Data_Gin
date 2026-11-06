package requestbody

// OpenTmallQueryRequest carries daily, hourly or realtime query input.
// Filters contains exact-match dimension values keyed by Oracle column name.
// Each endpoint only accepts its documented dimension columns.
type OpenTmallQueryRequest struct {
	StartTime string            `json:"-"`
	EndTime   string            `json:"-"`
	TimeField string            `json:"-"`
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

// OpenTmallOrdersQueryRequest queries the current order details in a half-open
// database-local time range; timeField defaults to created.
type OpenTmallOrdersQueryRequest struct {
	StartTime string            `json:"startTime"` // YYYY-MM-DD HH:mm:ss，必填，包含起点。
	EndTime   string            `json:"endTime"`   // YYYY-MM-DD HH:mm:ss，必填，不含终点，范围最多31天。
	TimeField string            `json:"timeField"` // created（默认）或 modified。
	Filters   map[string]string `json:"filters"`
	Page      *int              `json:"page"`     // 默认1，最大1000。
	PageSize  *int              `json:"pageSize"` // 默认50，最大100。
}
