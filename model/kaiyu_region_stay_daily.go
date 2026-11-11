package model

import "time"

const (
	KaiyuRegionSyncSuccess = "success"
	KaiyuRegionSyncEmpty   = "empty"
	KaiyuRegionSyncFailed  = "failed"
)

// KaiyuRegionStayDaily preserves daily source region metrics in YLZ_USER.
// StatDate and all stored wall-clock timestamps use Asia/Shanghai.
type KaiyuRegionStayDaily struct {
	MallID        uint      `json:"mallId"`
	ShopID        string    `json:"shopId"`
	ShopName      string    `json:"shopName"`
	StatDate      time.Time `json:"statDate"`
	RegionName    string    `json:"regionName"`
	ExposureCount int64     `json:"exposureCount"`
	StayCount     int64     `json:"stayCount"`
	StayRate      float64   `json:"stayRate"`
	AvgStayTime   float64   `json:"avgStayTime"`
	FetchedAt     time.Time `json:"fetchedAt"`
}

func (KaiyuRegionStayDaily) TableName() string { return "YLZ_USER.KAIYU_REGION_STAY_DAILY" }

// KaiyuRegionSyncDay records the latest attempt separately from retained facts.
// Empty/failed attempts retain the preceding successful snapshot and timestamp.
type KaiyuRegionSyncDay struct {
	MallID        uint       `json:"mallId"`
	ShopID        string     `json:"shopId"`
	ShopName      string     `json:"shopName"`
	StatDate      time.Time  `json:"statDate"`
	State         string     `json:"state"`
	RecordCount   int        `json:"recordCount"`
	FetchedAt     time.Time  `json:"fetchedAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	ErrorMessage  string     `json:"errorMessage"`
}

func (KaiyuRegionSyncDay) TableName() string { return "YLZ_USER.KAIYU_REGION_SYNC_DAY" }
