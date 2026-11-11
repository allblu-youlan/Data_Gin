package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
)

var (
	ErrKaiyuRegionInvalid         = errors.New("kaiyu region: invalid query")
	ErrKaiyuRegionUnavailable     = errors.New("kaiyu region: unavailable")
	ErrKaiyuRegionBusy            = errors.New("kaiyu region: sync busy")
	ErrKaiyuRegionBindingConflict = errors.New("kaiyu region: duplicate shop binding")
)

type KaiyuRegionQuery struct {
	StartDate  string `json:"startDate"`
	EndDate    string `json:"endDate"`
	MallID     uint   `json:"mallId,omitempty"`
	RegionName string `json:"regionName,omitempty"`
}
type KaiyuRegionItem struct {
	MallID        uint      `json:"mallId"`
	MallName      string    `json:"mallName"`
	ShopID        string    `json:"shopId"`
	StatDate      string    `json:"statDate"`
	RegionName    string    `json:"regionName"`
	ExposureCount int64     `json:"exposureCount"`
	StayCount     int64     `json:"stayCount"`
	StayRate      float64   `json:"stayRate"`
	AvgStayTime   float64   `json:"avgStayTime"`
	FetchedAt     time.Time `json:"fetchedAt"`
}
type KaiyuRegionSummary struct {
	ExposureCount   int64      `json:"exposureCount"`
	StayCount       int64      `json:"stayCount"`
	RecordCount     int        `json:"recordCount"`
	MallCount       int        `json:"mallCount"`
	RegionCount     int        `json:"regionCount"`
	LatestFetchedAt *time.Time `json:"latestFetchedAt"`
}
type KaiyuRegionResult struct {
	StartDate string             `json:"startDate"`
	EndDate   string             `json:"endDate"`
	TimeZone  string             `json:"timezone"`
	Items     []KaiyuRegionItem  `json:"items"`
	Summary   KaiyuRegionSummary `json:"summary"`
}
type KaiyuRegionStatusItem struct {
	MallID        uint       `json:"mallId"`
	MallName      string     `json:"mallName"`
	ShopID        string     `json:"shopId"`
	StatDate      string     `json:"statDate"`
	State         string     `json:"state"`
	RecordCount   int        `json:"recordCount"`
	FetchedAt     *time.Time `json:"fetchedAt,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
}
type KaiyuRegionMissingResult struct {
	StartDate     string                  `json:"startDate"`
	EndDate       string                  `json:"endDate"`
	TimeZone      string                  `json:"timezone"`
	ExpectedDays  int                     `json:"expectedDays"`
	SuccessDays   int                     `json:"successDays"`
	EmptyDays     int                     `json:"emptyDays"`
	FailedDays    int                     `json:"failedDays"`
	UnqueriedDays int                     `json:"unqueriedDays"`
	UnboundMalls  int                     `json:"unboundMalls"`
	Items         []KaiyuRegionStatusItem `json:"items"`
}
type kaiyuRegionStore interface {
	QueryKaiyuRegionDays(context.Context, time.Time, time.Time, []uint) ([]model.KaiyuRegionStayDaily, error)
	QueryKaiyuRegionSyncDays(context.Context, time.Time, time.Time, []uint) ([]model.KaiyuRegionSyncDay, error)
	SaveKaiyuRegionDay(context.Context, model.KaiyuRegionSyncDay, []model.KaiyuRegionStayDaily) error
}
type kaiyuRegionSource interface {
	RegionStayByShops(context.Context, time.Time, []string) ([]kaiyu.RegionStayShop, error)
}
type KaiyuRegionService struct {
	access *KaiyuPassengerQueryService
	open   func(context.Context) (kaiyuRegionStore, error)
	source func() (kaiyuRegionSource, error)
	mu     sync.Mutex
	store  kaiyuRegionStore
	now    func() time.Time
}

func NewKaiyuRegionService() *KaiyuRegionService {
	return &KaiyuRegionService{access: NewKaiyuPassengerQueryService(), open: func(ctx context.Context) (kaiyuRegionStore, error) { return OpenKaiyuOracle(ctx) }, source: newKaiyuRegionSource, now: time.Now}
}
func (service *KaiyuRegionService) connection(ctx context.Context) (kaiyuRegionStore, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.store != nil {
		return service.store, nil
	}
	store, err := service.open(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: open Oracle: %v", ErrKaiyuRegionUnavailable, err)
	}
	service.store = store
	return store, nil
}
func parseKaiyuRegionRange(input KaiyuRegionQuery) (time.Time, time.Time, error) {
	start, end, err := parseKaiyuPassengerRange(KaiyuPassengerQuery{StartDate: input.StartDate, EndDate: input.EndDate, MallID: input.MallID})
	if err != nil || utf8.RuneCountInString(input.RegionName) > 255 {
		return time.Time{}, time.Time{}, ErrKaiyuRegionInvalid
	}
	return start, end, nil
}
func (service *KaiyuRegionService) scopedMalls(ctx context.Context, actor uint, input KaiyuRegionQuery) ([]uint, []KaiyuPassengerMallOption, error) {
	var requested []uint
	if input.MallID > 0 {
		requested = []uint{input.MallID}
	}
	ids, err := service.access.constrain(ctx, actor, requested)
	if err != nil {
		return nil, nil, err
	}
	malls, err := service.access.listScopedMalls(ctx, actor)
	if err != nil {
		return nil, nil, err
	}
	return ids, filterKaiyuRegionMalls(malls, input.MallID), nil
}
func filterKaiyuRegionMalls(malls []KaiyuPassengerMallOption, mallID uint) []KaiyuPassengerMallOption {
	filtered := make([]KaiyuPassengerMallOption, 0, len(malls))
	for _, mall := range malls {
		if mallID == 0 || mall.MallID == mallID {
			mall.ShopID = strings.TrimSpace(mall.ShopID)
			filtered = append(filtered, mall)
		}
	}
	return filtered
}
func (service *KaiyuRegionService) Query(ctx context.Context, actor uint, input KaiyuRegionQuery) (*KaiyuRegionResult, error) {
	start, end, err := parseKaiyuRegionRange(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ids, malls, err := service.scopedMalls(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	store, err := service.connection(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := store.QueryKaiyuRegionDays(ctx, start, end, ids)
	if err != nil {
		return nil, err
	}
	result := &KaiyuRegionResult{StartDate: input.StartDate, EndDate: input.EndDate, TimeZone: "Asia/Shanghai", Items: make([]KaiyuRegionItem, 0, len(rows))}
	names := make(map[uint]string, len(malls))
	for _, mall := range malls {
		names[mall.MallID] = mall.MallName
	}
	mallSet := map[uint]bool{}
	regionSet := map[string]bool{}
	for _, row := range rows {
		if input.RegionName != "" && row.RegionName != input.RegionName {
			continue
		}
		name := names[row.MallID]
		if name == "" {
			name = row.ShopName
		}
		item := KaiyuRegionItem{MallID: row.MallID, MallName: name, ShopID: row.ShopID, StatDate: row.StatDate.In(kaiyuLocation).Format(time.DateOnly), RegionName: row.RegionName, ExposureCount: row.ExposureCount, StayCount: row.StayCount, StayRate: row.StayRate, AvgStayTime: row.AvgStayTime, FetchedAt: row.FetchedAt.In(kaiyuLocation)}
		result.Items = append(result.Items, item)
		result.Summary.ExposureCount += row.ExposureCount
		result.Summary.StayCount += row.StayCount
		mallSet[row.MallID] = true
		regionSet[row.ShopID+"\x00"+row.RegionName] = true
		if result.Summary.LatestFetchedAt == nil || item.FetchedAt.After(*result.Summary.LatestFetchedAt) {
			at := item.FetchedAt
			result.Summary.LatestFetchedAt = &at
		}
	}
	result.Summary.RecordCount = len(result.Items)
	result.Summary.MallCount = len(mallSet)
	result.Summary.RegionCount = len(regionSet)
	return result, nil
}
func (service *KaiyuRegionService) Missing(ctx context.Context, actor uint, input KaiyuRegionQuery) (*KaiyuRegionMissingResult, error) {
	start, end, err := parseKaiyuRegionRange(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ids, malls, err := service.scopedMalls(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	store, err := service.connection(ctx)
	if err != nil {
		return nil, err
	}
	return queryKaiyuRegionStatus(ctx, store, input, start, end, ids, malls)
}
func queryKaiyuRegionStatus(ctx context.Context, store kaiyuRegionStore, input KaiyuRegionQuery, start, end time.Time, ids []uint, malls []KaiyuPassengerMallOption) (*KaiyuRegionMissingResult, error) {
	rows, err := store.QueryKaiyuRegionSyncDays(ctx, start, end, ids)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]model.KaiyuRegionSyncDay, len(rows))
	for _, row := range rows {
		existing[row.ShopID+":"+row.StatDate.In(kaiyuLocation).Format(time.DateOnly)] = row
	}
	result := &KaiyuRegionMissingResult{StartDate: input.StartDate, EndDate: input.EndDate, TimeZone: "Asia/Shanghai", Items: make([]KaiyuRegionStatusItem, 0)}
	for _, mall := range malls {
		if mall.ShopID == "" {
			result.UnboundMalls++
		}
	}
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		for _, mall := range malls {
			if mall.ShopID == "" {
				continue
			}
			item := KaiyuRegionStatusItem{MallID: mall.MallID, MallName: mall.MallName, ShopID: mall.ShopID, StatDate: date, State: "unqueried"}
			if row, ok := existing[mall.ShopID+":"+date]; ok && row.MallID == mall.MallID {
				item.State = row.State
				item.RecordCount = row.RecordCount
				at := row.FetchedAt.In(kaiyuLocation)
				item.FetchedAt = &at
				if row.LastSuccessAt != nil {
					at := row.LastSuccessAt.In(kaiyuLocation)
					item.LastSuccessAt = &at
				}
				if row.State == model.KaiyuRegionSyncFailed {
					item.ErrorMessage = "最近一次取数失败，请重试"
				}
			}
			switch item.State {
			case model.KaiyuRegionSyncSuccess:
				result.SuccessDays++
			case model.KaiyuRegionSyncEmpty:
				result.EmptyDays++
			case model.KaiyuRegionSyncFailed:
				result.FailedDays++
			default:
				result.UnqueriedDays++
			}
			result.Items = append(result.Items, item)
		}
	}
	result.ExpectedDays = len(result.Items)
	return result, nil
}
