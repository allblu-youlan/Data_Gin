package data_svc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

var (
	ErrOpenMonitoringInvalidQuery = errors.New("open monitoring: invalid query")
	ErrOpenMonitoringForbidden    = errors.New("open monitoring: forbidden")
	ErrOpenMonitoringUnavailable  = errors.New("open monitoring: unavailable")
	ErrOpenMonitoringNotFound     = errors.New("open monitoring: shop or mapping not found")
	monitoringLocation            = time.FixedZone("Asia/Shanghai", 8*60*60)
)

const openMonitoringQueryTimeout = 10 * time.Second

type monitoringMallReader interface {
	FindByCode(context.Context, string) (*model.Mall, error)
}
type monitoringScopeReader interface {
	CanAccess(context.Context, uint, uint) (bool, error)
}

type OpenMonitoringQueryService struct {
	hours       func(context.Context, string, uint, []time.Time) ([]reportoracle.MonitoringHourlyRow, error)
	areas       func(context.Context, string, uint, time.Time, string) ([]reportoracle.MonitoringAreaRow, error)
	permissions openBojunQueryPermissionReader
	malls       monitoringMallReader
	scopes      monitoringScopeReader
	now         func() time.Time
}

type OpenMonitoringShop struct {
	ShopCode string `json:"shopCode"`
	ShopName string `json:"shopName"`
	Timezone string `json:"timezone"`
}

type OpenMonitoringCoverage struct {
	Date          string `json:"date"`
	Hours         int    `json:"hours"`
	ExpectedHours int    `json:"expectedHours"`
	Complete      bool   `json:"complete"`
}

type OpenMonitoringHourlyResult struct {
	OpenMonitoringShop
	Dates        []string                           `json:"dates"`
	Items        []reportoracle.MonitoringHourlyRow `json:"items"`
	Coverage     []OpenMonitoringCoverage           `json:"coverage"`
	MissingDates []string                           `json:"missingDates"`
}

type OpenMonitoringDailyItem struct {
	OpenMonitoringCoverage
	Passby       int64 `json:"passby"`
	Traffic      int64 `json:"traffic"`
	TrafficCount int64 `json:"trafficCount"`
	TrafficBatch int64 `json:"trafficBatch"`
}

type OpenMonitoringDailyResult struct {
	OpenMonitoringShop
	Dates        []string                  `json:"dates"`
	Items        []OpenMonitoringDailyItem `json:"items"`
	MissingDates []string                  `json:"missingDates"`
}

type OpenMonitoringAreaResult struct {
	OpenMonitoringShop
	Date    string                           `json:"date"`
	Items   []reportoracle.MonitoringAreaRow `json:"items"`
	HasData bool                             `json:"hasData"`
}

func NewOpenMonitoringQueryService() *OpenMonitoringQueryService {
	return &OpenMonitoringQueryService{
		permissions: newAccountPermissionChecker(database.DB),
		malls:       data_dao.NewMallDAO(), scopes: auth_svc.NewMallScopeService(), now: time.Now,
		hours: func(ctx context.Context, shopID string, mallID uint, dates []time.Time) (rows []reportoracle.MonitoringHourlyRow, err error) {
			err = withOracleOperationConnection(ctx, openMonitoringQueryTimeout, func(queryCtx context.Context, adapter *reportoracle.Adapter) error {
				rows, err = adapter.QueryMonitoringHours(queryCtx, shopID, mallID, dates)
				return err
			})
			return rows, err
		},
		areas: func(ctx context.Context, shopID string, mallID uint, day time.Time, region string) (rows []reportoracle.MonitoringAreaRow, err error) {
			err = withOracleOperationConnection(ctx, openMonitoringQueryTimeout, func(queryCtx context.Context, adapter *reportoracle.Adapter) error {
				rows, err = adapter.QueryMonitoringAreas(queryCtx, shopID, mallID, day, region)
				return err
			})
			return rows, err
		},
	}
}

func (s *OpenMonitoringQueryService) QueryHourly(ctx context.Context, actor uint, request requestbody.OpenMonitoringHourlyQueryRequest) (*OpenMonitoringHourlyResult, error) {
	return s.queryHours(ctx, actor, model.PermissionMonitoringHourlyRead, request)
}

func (s *OpenMonitoringQueryService) QueryDaily(ctx context.Context, actor uint, request requestbody.OpenMonitoringHourlyQueryRequest) (*OpenMonitoringDailyResult, error) {
	hourly, err := s.queryHours(ctx, actor, model.PermissionMonitoringDailyRead, request)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]*OpenMonitoringDailyItem, len(hourly.Dates))
	for _, coverage := range hourly.Coverage {
		if coverage.Hours > 0 {
			byDate[coverage.Date] = &OpenMonitoringDailyItem{OpenMonitoringCoverage: coverage}
		}
	}
	for _, row := range hourly.Items {
		item := byDate[row.HourStart.In(monitoringLocation).Format(time.DateOnly)]
		if item == nil {
			continue
		}
		item.Passby += row.Passby
		item.Traffic += row.Traffic
		item.TrafficCount += row.TrafficCount
		item.TrafficBatch += row.TrafficBatch
	}
	items := make([]OpenMonitoringDailyItem, 0, len(byDate))
	for _, date := range hourly.Dates {
		if item := byDate[date]; item != nil {
			items = append(items, *item)
		}
	}
	return &OpenMonitoringDailyResult{OpenMonitoringShop: hourly.OpenMonitoringShop, Dates: hourly.Dates, Items: items, MissingDates: hourly.MissingDates}, nil
}

func (s *OpenMonitoringQueryService) queryHours(ctx context.Context, actor uint, permission string, request requestbody.OpenMonitoringHourlyQueryRequest) (*OpenMonitoringHourlyResult, error) {
	if ctx == nil {
		return nil, ErrOpenMonitoringInvalidQuery
	}
	queryCtx, cancel := context.WithTimeout(ctx, openMonitoringQueryTimeout)
	defer cancel()
	dates, err := normalizeMonitoringDates(request)
	if err != nil {
		return nil, err
	}
	mall, err := s.resolveShop(queryCtx, actor, permission, request.ShopCode)
	if err != nil {
		return nil, err
	}
	rows, err := s.hours(queryCtx, strings.TrimSpace(mall.KaiyuShopID), mall.ID, dates)
	if err != nil {
		return nil, monitoringQueryError(err)
	}
	if rows == nil {
		rows = []reportoracle.MonitoringHourlyRow{}
	}
	result := &OpenMonitoringHourlyResult{OpenMonitoringShop: monitoringShop(mall), Items: rows, Dates: make([]string, 0, len(dates)), Coverage: make([]OpenMonitoringCoverage, 0, len(dates)), MissingDates: []string{}}
	hoursByDay := make(map[string]map[int]bool, len(dates))
	for _, row := range rows {
		local := row.HourStart.In(monitoringLocation)
		date := local.Format(time.DateOnly)
		if hoursByDay[date] == nil {
			hoursByDay[date] = make(map[int]bool)
		}
		hoursByDay[date][local.Hour()] = true
	}
	for _, day := range dates {
		date := day.Format(time.DateOnly)
		hours := len(hoursByDay[date])
		result.Dates = append(result.Dates, date)
		result.Coverage = append(result.Coverage, OpenMonitoringCoverage{Date: date, Hours: hours, ExpectedHours: 24, Complete: hours == 24})
		if hours == 0 {
			result.MissingDates = append(result.MissingDates, date)
		}
	}
	return result, nil
}

func (s *OpenMonitoringQueryService) QueryAreas(ctx context.Context, actor uint, request requestbody.OpenMonitoringAreaQueryRequest) (*OpenMonitoringAreaResult, error) {
	if ctx == nil {
		return nil, ErrOpenMonitoringInvalidQuery
	}
	queryCtx, cancel := context.WithTimeout(ctx, openMonitoringQueryTimeout)
	defer cancel()
	day, err := monitoringDate(request.Date)
	region := strings.TrimSpace(request.RegionName)
	if err != nil || utf8.RuneCountInString(region) > 255 {
		return nil, ErrOpenMonitoringInvalidQuery
	}
	mall, err := s.resolveShop(queryCtx, actor, model.PermissionMonitoringAreaRead, request.ShopCode)
	if err != nil {
		return nil, err
	}
	rows, err := s.areas(queryCtx, strings.TrimSpace(mall.KaiyuShopID), mall.ID, day, region)
	if err != nil {
		return nil, monitoringQueryError(err)
	}
	if rows == nil {
		rows = []reportoracle.MonitoringAreaRow{}
	}
	return &OpenMonitoringAreaResult{OpenMonitoringShop: monitoringShop(mall), Date: day.Format(time.DateOnly), Items: rows, HasData: len(rows) > 0}, nil
}

func (s *OpenMonitoringQueryService) resolveShop(ctx context.Context, actor uint, permission, code string) (*model.Mall, error) {
	if actor == 0 {
		return nil, ErrOpenMonitoringForbidden
	}
	allowed, err := s.permissions.HasPermission(ctx, actor, permission, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%w: permission: %w", ErrOpenMonitoringUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenMonitoringForbidden
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if !mallCodePattern.MatchString(code) {
		return nil, ErrOpenMonitoringInvalidQuery
	}
	mall, err := s.malls.FindByCode(ctx, code)
	if errors.Is(err, data_dao.ErrMallNotFound) {
		return nil, ErrOpenMonitoringNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: shop: %w", ErrOpenMonitoringUnavailable, err)
	}
	allowed, err = s.scopes.CanAccess(ctx, actor, mall.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: scope: %w", ErrOpenMonitoringUnavailable, err)
	}
	if !allowed {
		return nil, ErrOpenMonitoringForbidden
	}
	if strings.TrimSpace(mall.KaiyuShopID) == "" {
		return nil, ErrOpenMonitoringNotFound
	}
	return mall, nil
}

func monitoringShop(mall *model.Mall) OpenMonitoringShop {
	return OpenMonitoringShop{ShopCode: mall.MallCode, ShopName: mall.NameCN, Timezone: "Asia/Shanghai"}
}

func monitoringQueryError(err error) error {
	if errors.Is(err, reportoracle.ErrMonitoringTooManyRows) {
		return fmt.Errorf("%w: %w", ErrOpenMonitoringInvalidQuery, err)
	}
	return fmt.Errorf("%w: query: %w", ErrOpenMonitoringUnavailable, err)
}

func monitoringDate(value string) (time.Time, error) {
	day, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(value), monitoringLocation)
	if err != nil || day.Year() < 1 || day.Year() > 9998 {
		return time.Time{}, ErrOpenMonitoringInvalidQuery
	}
	return day, nil
}

func normalizeMonitoringDates(request requestbody.OpenMonitoringHourlyQueryRequest) ([]time.Time, error) {
	start, end := strings.TrimSpace(request.StartDate), strings.TrimSpace(request.EndDate)
	if len(request.Dates) > 0 {
		if start != "" || end != "" || len(request.Dates) > 31 {
			return nil, ErrOpenMonitoringInvalidQuery
		}
		dates := make([]time.Time, 0, len(request.Dates))
		seen := make(map[time.Time]bool, len(request.Dates))
		for _, value := range request.Dates {
			day, err := monitoringDate(value)
			if err != nil {
				return nil, err
			}
			if !seen[day] {
				seen[day] = true
				dates = append(dates, day)
			}
		}
		sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
		return dates, nil
	}
	first, err := monitoringDate(start)
	if err != nil {
		return nil, err
	}
	last, err := monitoringDate(end)
	if err != nil || last.Before(first) || last.Sub(first) > 30*24*time.Hour {
		return nil, ErrOpenMonitoringInvalidQuery
	}
	dates := make([]time.Time, 0, 31)
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		dates = append(dates, day)
	}
	return dates, nil
}
