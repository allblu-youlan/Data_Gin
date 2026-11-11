package kaiyu

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RegionStayMaxDays  = 30
	RegionStayMaxShops = 20
)

var regionLocation = time.FixedZone("Asia/Shanghai", 8*3600)

// RegionStay contains source daily metrics; AvgStayTime is seconds. The source
// does not provide a stable region ID or an averaging denominator.
type RegionStay struct {
	RegionName    string  `json:"regionName"`
	ExposureCount int64   `json:"exposureCount"`
	StayCount     int64   `json:"stayCount"`
	StayRate      float64 `json:"stayRate"`
	AvgStayTime   float64 `json:"avgStayTime"`
}

// UnmarshalJSON distinguishes missing metrics from explicit source zero values.
func (r *RegionStay) UnmarshalJSON(data []byte) error {
	var fields struct {
		RegionName    *string  `json:"regionName"`
		ExposureCount *int64   `json:"exposureCount"`
		StayCount     *int64   `json:"stayCount"`
		StayRate      *float64 `json:"stayRate"`
		AvgStayTime   *float64 `json:"avgStayTime"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.RegionName == nil || fields.ExposureCount == nil || fields.StayCount == nil || fields.StayRate == nil || fields.AvgStayTime == nil {
		return fmt.Errorf("kaiyu: region response is missing required metrics")
	}
	*r = RegionStay{*fields.RegionName, *fields.ExposureCount, *fields.StayCount, *fields.StayRate, *fields.AvgStayTime}
	return nil
}

type RegionStayDay struct {
	Dateline string       `json:"dateline"`
	Regions  []RegionStay `json:"regions"`
}

type RegionStayShop struct {
	ShopID   string       `json:"shopid"`
	ShopName string       `json:"shopname"`
	Regions  []RegionStay `json:"regions"`
}

// RegionStayByDay reads an inclusive range of at most 30 Shanghai calendar days.
// The provider omits shop ID in its response, so callers retain request context.
func (c *Client) RegionStayByDay(ctx context.Context, shopID string, start, end time.Time) ([]RegionStayDay, error) {
	if !validRegionShopID(shopID) || start.IsZero() || end.IsZero() {
		return nil, fmt.Errorf("kaiyu: region shop and date range are required")
	}
	start, end = regionDay(start), regionDay(end)
	if end.Before(start) || end.Sub(start) >= RegionStayMaxDays*24*time.Hour {
		return nil, fmt.Errorf("kaiyu: region date range must contain 1 to %d days", RegionStayMaxDays)
	}
	var result []RegionStayDay
	err := c.request(ctx, http.MethodPost, "/newretail/api/open/flow/getRegionStayByDay", map[string]string{
		"shopId": shopID, "startDate": start.Format("20060102"), "endDate": end.Format("20060102"),
	}, &result)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(result))
	for _, day := range result {
		date, err := time.ParseInLocation("20060102", day.Dateline, regionLocation)
		if err != nil || date.Before(start) || date.After(end) || seen[day.Dateline] {
			return nil, fmt.Errorf("kaiyu: region response contains an invalid or duplicate date")
		}
		seen[day.Dateline] = true
		if err := validateRegions(day.Regions); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// RegionStayByShops returns up to 20 requested shops for one Shanghai date.
// Missing shops and empty regions are not zero-valued observations.
func (c *Client) RegionStayByShops(ctx context.Context, day time.Time, shopIDs []string) ([]RegionStayShop, error) {
	if day.IsZero() || len(shopIDs) == 0 || len(shopIDs) > RegionStayMaxShops {
		return nil, fmt.Errorf("kaiyu: region query needs a date and 1 to %d shops", RegionStayMaxShops)
	}
	wanted := make(map[string]bool, len(shopIDs))
	for _, id := range shopIDs {
		if !validRegionShopID(id) || wanted[id] {
			return nil, fmt.Errorf("kaiyu: invalid or duplicate region shop id")
		}
		wanted[id] = true
	}
	var result []RegionStayShop
	err := c.request(ctx, http.MethodPost, "/newretail/api/open/flow/getRegionStayByShops", struct {
		Dateline string   `json:"dateline"`
		ShopIDs  []string `json:"shopIds"`
	}{regionDay(day).Format("20060102"), shopIDs}, &result)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(result))
	for _, shop := range result {
		if !wanted[shop.ShopID] || seen[shop.ShopID] {
			return nil, fmt.Errorf("kaiyu: region response contains an unrequested or duplicate shop")
		}
		seen[shop.ShopID] = true
		if err := validateRegions(shop.Regions); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func validRegionShopID(id string) bool {
	return strings.TrimSpace(id) != "" && utf8.RuneCountInString(id) <= 64
}

func regionDay(day time.Time) time.Time {
	y, m, d := day.In(regionLocation).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, regionLocation)
}

func validateRegions(regions []RegionStay) error {
	seen := make(map[string]bool, len(regions))
	for _, r := range regions {
		if strings.TrimSpace(r.RegionName) == "" || utf8.RuneCountInString(r.RegionName) > 255 || seen[r.RegionName] {
			return fmt.Errorf("kaiyu: region response contains an invalid or duplicate region name")
		}
		seen[r.RegionName] = true
		if r.ExposureCount < 0 || r.StayCount < 0 || r.StayRate < 0 || r.StayRate > 1 || r.AvgStayTime < 0 || math.IsNaN(r.StayRate) || math.IsInf(r.StayRate, 0) || math.IsNaN(r.AvgStayTime) || math.IsInf(r.AvgStayTime, 0) {
			return fmt.Errorf("kaiyu: region response contains invalid metrics")
		}
	}
	return nil
}
