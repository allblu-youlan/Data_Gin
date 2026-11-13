package requestbody

// OpenYouzanQueryRequest selects a database-local half-open time range.
// Both times may be omitted only when filters includes an exact TID.
type OpenYouzanQueryRequest struct {
	StartTime string            `json:"startTime"` // YYYY-MM-DD HH:mm:ss，包含起点。
	EndTime   string            `json:"endTime"`   // 不含终点，范围最多31天。
	TimeField string            `json:"timeField"` // modifiedDwd（默认）或 created。
	Filters   map[string]string `json:"filters"`   // 仅接受接口文档中列出的精确匹配字段。
	Page      *int              `json:"page"`      // 默认1，最大1000。
	PageSize  *int              `json:"pageSize"`  // 默认50，最大100。
}
