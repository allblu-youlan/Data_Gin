package requestbody

// OpenTmallQueryRequest queries a bounded, inclusive range of business dates.
// Filters contains exact-match dimension values keyed by Oracle column name.
// Each endpoint only accepts its documented dimension columns.
type OpenTmallQueryRequest struct {
	StartDate string            `json:"startDate"` // YYYY-MM-DD，必填。
	EndDate   string            `json:"endDate"`   // YYYY-MM-DD，必填，最多31天。
	Filters   map[string]string `json:"filters"`
	Page      *int              `json:"page"`     // 默认1，最大1000。
	PageSize  *int              `json:"pageSize"` // 默认50，最大100。
}
