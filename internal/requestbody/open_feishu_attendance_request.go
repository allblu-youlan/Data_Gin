package requestbody

// OpenFeishuAttendanceQueryRequest queries stored attendance by exact name and department.
type OpenFeishuAttendanceQueryRequest struct {
	StartDate string            `json:"startDate"` // YYYY-MM-DD，必填。
	EndDate   string            `json:"endDate"`   // YYYY-MM-DD，含结束日期，最多31天。
	Filters   map[string]string `json:"filters"`   // USER_NAME、DEPARTMENT_NAME 均必填。
	Page      *int              `json:"page"`      // 默认1，最大1000。
	PageSize  *int              `json:"pageSize"`  // 默认50，最大100。
}
