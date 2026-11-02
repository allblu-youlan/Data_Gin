package data_svc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
)

var (
	ErrKaiyuPassengerInvalid     = errors.New("kaiyu passenger: invalid query")
	ErrKaiyuPassengerUnavailable = errors.New("kaiyu passenger: unavailable")
)

type KaiyuPassengerQuery struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	MallID    uint   `json:"mallId,omitempty"`
}

type KaiyuPassengerMetrics struct {
	Passby       int64   `json:"passby"`
	Traffic      int64   `json:"traffic"`
	TrafficCount int64   `json:"trafficCount"`
	TrafficBatch int64   `json:"trafficBatch"`
	CaptureRate  float64 `json:"captureRate"`
	RecordCount  int     `json:"recordCount"`
}

type KaiyuPassengerSummary struct {
	KaiyuPassengerMetrics
	MallCount  int        `json:"mallCount"`
	LatestHour *time.Time `json:"latestHour"`
}

type KaiyuPassengerTrend struct {
	HourStart time.Time `json:"hourStart"`
	KaiyuPassengerMetrics
}

type KaiyuPassengerMallSummary struct {
	MallID   uint   `json:"mallId"`
	MallName string `json:"mallName"`
	ShopID   string `json:"shopId"`
	ShopName string `json:"shopName"`
	KaiyuPassengerMetrics
}

type KaiyuPassengerItem struct {
	model.KaiyuPassengerHourly
	MallName string `json:"mallName"`
}

type KaiyuPassengerResult struct {
	StartDate string                      `json:"startDate"`
	EndDate   string                      `json:"endDate"`
	TimeZone  string                      `json:"timeZone"`
	Summary   KaiyuPassengerSummary       `json:"summary"`
	Trend     []KaiyuPassengerTrend       `json:"trend"`
	Malls     []KaiyuPassengerMallSummary `json:"malls"`
	Items     []KaiyuPassengerItem        `json:"-"`
}

type KaiyuPassengerMallOption struct {
	MallID   uint   `json:"mallId"`
	MallName string `json:"mallName"`
	ShopID   string `json:"shopId"`
}

type KaiyuPassengerMallListResult struct {
	Items []KaiyuPassengerMallOption `json:"items"`
}

type kaiyuPassengerScope interface {
	ConstrainMallIDs(context.Context, uint, []uint) ([]uint, error)
}

type kaiyuPassengerReader interface {
	QueryKaiyuPassengerHours(context.Context, time.Time, time.Time, []uint) ([]model.KaiyuPassengerHourly, error)
}

type KaiyuPassengerQueryService struct {
	scope  kaiyuPassengerScope
	malls  kaiyuMallReader
	open   func(context.Context) (kaiyuPassengerReader, error)
	mu     sync.Mutex
	reader kaiyuPassengerReader
}

func NewKaiyuPassengerQueryService() *KaiyuPassengerQueryService {
	return newKaiyuPassengerQueryService(auth_svc.NewMallScopeService(), data_dao.NewMallDAO(), func(ctx context.Context) (kaiyuPassengerReader, error) { return OpenKaiyuOracle(ctx) })
}

func newKaiyuPassengerQueryService(scope kaiyuPassengerScope, malls kaiyuMallReader, open func(context.Context) (kaiyuPassengerReader, error)) *KaiyuPassengerQueryService {
	if scope == nil || malls == nil || open == nil {
		panic("kaiyu passenger: required dependencies are missing")
	}
	return &KaiyuPassengerQueryService{scope: scope, malls: malls, open: open}
}

func (service *KaiyuPassengerQueryService) Query(ctx context.Context, actor uint, input KaiyuPassengerQuery) (*KaiyuPassengerResult, error) {
	start, end, err := parseKaiyuPassengerRange(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var requested []uint
	if input.MallID > 0 {
		requested = []uint{input.MallID}
	}
	ids, err := service.constrain(ctx, actor, requested)
	if err != nil {
		return nil, err
	}
	reader, err := service.connection(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := reader.QueryKaiyuPassengerHours(ctx, start, end, ids)
	if errors.Is(err, reportoracle.ErrKaiyuPassengerTooManyRows) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: query hours: %v", ErrKaiyuPassengerUnavailable, err)
	}
	malls, err := service.listScopedMalls(ctx, actor)
	if err != nil {
		return nil, err
	}
	return aggregateKaiyuPassengerHours(input, rows, malls), nil
}

func (service *KaiyuPassengerQueryService) ListMalls(ctx context.Context, actor uint) (*KaiyuPassengerMallListResult, error) {
	if _, err := service.constrain(ctx, actor, nil); err != nil {
		return nil, err
	}
	malls, err := service.listScopedMalls(ctx, actor)
	if err != nil {
		return nil, err
	}
	return &KaiyuPassengerMallListResult{Items: malls}, nil
}

func (service *KaiyuPassengerQueryService) constrain(ctx context.Context, actor uint, requested []uint) ([]uint, error) {
	if actor == 0 {
		return nil, auth_svc.ErrMallScopeForbidden
	}
	ids, err := service.scope.ConstrainMallIDs(ctx, actor, requested)
	if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: constrain malls: %v", ErrKaiyuPassengerUnavailable, err)
	}
	return ids, nil
}

func (service *KaiyuPassengerQueryService) listScopedMalls(ctx context.Context, actor uint) ([]KaiyuPassengerMallOption, error) {
	items := make([]KaiyuPassengerMallOption, 0)
	var after uint
	for {
		rows, err := service.malls.List(ctx, data_dao.MallListFilter{ActorUserID: actor, AfterID: after, Limit: 200})
		if err != nil {
			return nil, fmt.Errorf("%w: list scoped malls: %v", ErrKaiyuPassengerUnavailable, err)
		}
		for _, mall := range rows {
			items = append(items, KaiyuPassengerMallOption{MallID: mall.ID, MallName: mall.NameCN, ShopID: mall.KaiyuShopID})
		}
		if len(rows) < 200 {
			return items, nil
		}
		after = rows[len(rows)-1].ID
	}
}

func (service *KaiyuPassengerQueryService) connection(ctx context.Context) (kaiyuPassengerReader, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.reader != nil {
		return service.reader, nil
	}
	reader, err := service.open(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: open Oracle: %v", ErrKaiyuPassengerUnavailable, err)
	}
	service.reader = reader
	return reader, nil
}

func parseKaiyuPassengerRange(input KaiyuPassengerQuery) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation(time.DateOnly, input.StartDate, kaiyuLocation)
	if err != nil {
		return time.Time{}, time.Time{}, ErrKaiyuPassengerInvalid
	}
	last, err := time.ParseInLocation(time.DateOnly, input.EndDate, kaiyuLocation)
	if err != nil || last.Before(start) {
		return time.Time{}, time.Time{}, ErrKaiyuPassengerInvalid
	}
	end := last.AddDate(0, 0, 1)
	if end.Sub(start) > 31*24*time.Hour {
		return time.Time{}, time.Time{}, ErrKaiyuPassengerInvalid
	}
	return start, end, nil
}

func aggregateKaiyuPassengerHours(input KaiyuPassengerQuery, rows []model.KaiyuPassengerHourly, options []KaiyuPassengerMallOption) *KaiyuPassengerResult {
	result := &KaiyuPassengerResult{StartDate: input.StartDate, EndDate: input.EndDate, TimeZone: "Asia/Shanghai", Trend: make([]KaiyuPassengerTrend, 0), Malls: make([]KaiyuPassengerMallSummary, 0), Items: make([]KaiyuPassengerItem, 0, len(rows))}
	names := make(map[uint]string, len(options))
	for _, mall := range options {
		names[mall.MallID] = mall.MallName
	}
	trends := make(map[time.Time]*KaiyuPassengerTrend)
	malls := make(map[uint]*KaiyuPassengerMallSummary)
	for _, row := range rows {
		row.HourStart = row.HourStart.In(kaiyuLocation)
		name := names[row.MallID]
		if name == "" {
			name = row.ShopName
		}
		result.Items = append(result.Items, KaiyuPassengerItem{KaiyuPassengerHourly: row, MallName: name})
		result.Summary.add(row)
		if result.Summary.LatestHour == nil || row.HourStart.After(*result.Summary.LatestHour) {
			hour := row.HourStart
			result.Summary.LatestHour = &hour
		}
		trend := trends[row.HourStart]
		if trend == nil {
			trend = &KaiyuPassengerTrend{HourStart: row.HourStart}
			trends[row.HourStart] = trend
		}
		trend.add(row)
		mall := malls[row.MallID]
		if mall == nil {
			mall = &KaiyuPassengerMallSummary{MallID: row.MallID, MallName: name}
			malls[row.MallID] = mall
		}
		mall.ShopID = row.ShopID
		mall.ShopName = row.ShopName
		mall.add(row)
	}
	for _, trend := range trends {
		result.Trend = append(result.Trend, *trend)
	}
	for _, mall := range malls {
		result.Malls = append(result.Malls, *mall)
	}
	sort.Slice(result.Trend, func(i, j int) bool { return result.Trend[i].HourStart.Before(result.Trend[j].HourStart) })
	sort.Slice(result.Malls, func(i, j int) bool { return result.Malls[i].MallID < result.Malls[j].MallID })
	result.Summary.MallCount = len(malls)
	return result
}

func (metrics *KaiyuPassengerMetrics) add(row model.KaiyuPassengerHourly) {
	metrics.Passby += row.Passby
	metrics.Traffic += row.Traffic
	metrics.TrafficCount += row.TrafficCount
	metrics.TrafficBatch += row.TrafficBatch
	metrics.RecordCount++
	if metrics.Passby > 0 {
		metrics.CaptureRate = float64(metrics.Traffic) / float64(metrics.Passby)
	}
}
