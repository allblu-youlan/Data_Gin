package requestbody

// OpenBojunOrderQueryRequest is the public filter contract for querying
// sanitized Bojun order details.
type OpenBojunOrderQueryRequest struct {
	StartTime        string   `json:"startTime"`
	EndTime          string   `json:"endTime"`
	UpdatedStartTime string   `json:"updatedStartTime"`
	UpdatedEndTime   string   `json:"updatedEndTime"`
	MallCodes        []string `json:"mallCodes"`
	OrderTypes       []string `json:"orderTypes"`
	Cursor           string   `json:"cursor"`
	PageSize         int      `json:"pageSize"`

	// StartDate, EndDate, and StoreCodes are deprecated compatibility aliases.
	// New clients must use StartTime, EndTime, and MallCodes.
	StartDate  string   `json:"startDate"`
	EndDate    string   `json:"endDate"`
	StoreCodes []string `json:"storeCodes"`
}

// OpenBojunOrderDetailQueryRequest is the public contract for paging through
// every item or payment row stored on one Bojun order.
type OpenBojunOrderDetailQueryRequest struct {
	OrderNo  string `json:"orderNo"`
	Cursor   string `json:"cursor"`
	PageSize int    `json:"pageSize"`
}
