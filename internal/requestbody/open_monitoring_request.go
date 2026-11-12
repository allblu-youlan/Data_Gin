package requestbody

type OpenMonitoringHourlyQueryRequest struct {
	ShopCode  string   `json:"shopCode"`
	Dates     []string `json:"dates"`
	StartDate string   `json:"startDate"`
	EndDate   string   `json:"endDate"`
}

type OpenMonitoringAreaQueryRequest struct {
	ShopCode   string `json:"shopCode"`
	Date       string `json:"date"`
	RegionName string `json:"regionName"`
}
