package model

import "time"

// KaiyuPassengerHourly is stored only in Oracle, under the YLZ_USER account.
// HourStart is the beginning of the source hour in Asia/Shanghai.
type KaiyuPassengerHourly struct {
	ShopID            string    `json:"shopId"`
	HourStart         time.Time `json:"hourStart"`
	MallID            uint      `json:"mallId"`
	ShopName          string    `json:"shopName"`
	Passby            int64     `json:"passby"`
	Traffic           int64     `json:"traffic"`
	TrafficCount      int64     `json:"trafficCount"`
	TrafficBatch      int64     `json:"trafficBatch"`
	CaptureRate       float64   `json:"captureRate"`
	AvgVisitDuration  float64   `json:"avgVisitDuration"`
	NewVisitorsRatio  float64   `json:"newVisitorsRatio"`
	DeepVisitRatio    float64   `json:"deepVisitRatio"`
	NetSales          float64   `json:"netSales"`
	SalesTransactions float64   `json:"salesTransactions"`
	ATV               float64   `json:"atv"`
	ShopperYield      float64   `json:"shopperYield"`
}

func (KaiyuPassengerHourly) TableName() string { return "YLZ_USER.KAIYU_PASSENGER_HOURLY" }
