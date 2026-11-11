package data_svc

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
	"gin-biz-web-api/pkg/providerhttp"
	"go.uber.org/zap"
)

var kaiyuRegionSyncMu sync.Mutex

type KaiyuRegionBackfillRequest struct {
	KaiyuRegionQuery
	Cursor string `json:"cursor,omitempty"`
	Force  bool   `json:"force,omitempty"`
}
type KaiyuRegionBackfillResult struct {
	ProcessedDays     int    `json:"processedDays"`
	SavedDays         int    `json:"savedDays"`
	EmptyDays         int    `json:"emptyDays"`
	FailedDays        int    `json:"failedDays"`
	SourceMissingDays int    `json:"sourceMissingDays"`
	Done              bool   `json:"done"`
	NextCursor        string `json:"nextCursor"`
}

func (service *KaiyuRegionService) Backfill(ctx context.Context, actor uint, input KaiyuRegionBackfillRequest) (*KaiyuRegionBackfillResult, error) {
	start, end, err := parseKaiyuRegionRange(input.KaiyuRegionQuery)
	if err != nil {
		return nil, err
	}
	if err := validateKaiyuRegionCursor(input.Cursor, start, end); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	ids, malls, err := service.scopedMalls(ctx, actor, input.KaiyuRegionQuery)
	if err != nil {
		return nil, err
	}
	if !kaiyuRegionSyncMu.TryLock() {
		return nil, ErrKaiyuRegionBusy
	}
	defer kaiyuRegionSyncMu.Unlock()
	store, err := service.connection(ctx)
	if err != nil {
		return nil, err
	}
	missing, err := queryKaiyuRegionStatus(ctx, store, input.KaiyuRegionQuery, start, end, ids, malls)
	if err != nil {
		return nil, err
	}
	source, err := service.source()
	if err != nil {
		return nil, err
	}
	return fillKaiyuRegionBatch(ctx, source, store, missing.Items, input, service.now())
}
func validateKaiyuRegionCursor(cursor string, start, end time.Time) error {
	if cursor == "" {
		return nil
	}
	date, id, ok := strings.Cut(cursor, ":")
	day, err := time.ParseInLocation(time.DateOnly, date, kaiyuLocation)
	if !ok || err != nil || day.Before(start) || !day.Before(end) {
		return ErrKaiyuRegionInvalid
	}
	number, err := strconv.ParseUint(id, 10, 64)
	if err != nil || number == 0 {
		return ErrKaiyuRegionInvalid
	}
	return nil
}
func kaiyuRegionCursor(item KaiyuRegionStatusItem) string {
	return item.StatDate + ":" + strconv.FormatUint(uint64(item.MallID), 10)
}
func afterKaiyuRegionCursor(item KaiyuRegionStatusItem, cursor string) bool {
	if cursor == "" {
		return true
	}
	date, id, _ := strings.Cut(cursor, ":")
	number, _ := strconv.ParseUint(id, 10, 64)
	return item.StatDate > date || (item.StatDate == date && uint64(item.MallID) > number)
}
func fillKaiyuRegionBatch(ctx context.Context, source kaiyuRegionSource, store kaiyuRegionStore, statuses []KaiyuRegionStatusItem, input KaiyuRegionBackfillRequest, now time.Time) (*KaiyuRegionBackfillResult, error) {
	candidates := make([]KaiyuRegionStatusItem, 0, len(statuses))
	bindings := make(map[string]uint)
	for _, status := range statuses {
		if mallID, exists := bindings[status.ShopID]; exists && mallID != status.MallID {
			return nil, ErrKaiyuRegionBindingConflict
		}
		bindings[status.ShopID] = status.MallID
		if afterKaiyuRegionCursor(status, input.Cursor) && (input.Force || status.State != model.KaiyuRegionSyncSuccess) {
			candidates = append(candidates, status)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].StatDate == candidates[j].StatDate {
			return candidates[i].MallID < candidates[j].MallID
		}
		return candidates[i].StatDate < candidates[j].StatDate
	})
	result := &KaiyuRegionBackfillResult{Done: true}
	if len(candidates) == 0 {
		return result, nil
	}
	batch := candidates[:1]
	for len(batch) < len(candidates) && len(batch) < 20 && candidates[len(batch)].StatDate == batch[0].StatDate {
		batch = candidates[:len(batch)+1]
	}
	shopIDs := make([]string, 0, len(batch))
	for _, item := range batch {
		shopIDs = append(shopIDs, item.ShopID)
	}
	day, _ := time.ParseInLocation(time.DateOnly, batch[0].StatDate, kaiyuLocation)
	rows, fetchErr := fetchKaiyuRegionShops(ctx, source, day, shopIDs)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if fetchErr != nil {
		zap.L().Warn("开域区域数据请求失败", zap.String("date", batch[0].StatDate), zap.String("error", providerhttp.RedactText(fetchErr.Error())))
	}
	byShop := make(map[string]kaiyu.RegionStayShop, len(rows))
	duplicate := make(map[string]bool)
	for _, row := range rows {
		if _, exists := byShop[row.ShopID]; exists {
			duplicate[row.ShopID] = true
		}
		byShop[row.ShopID] = row
	}
	for _, item := range batch {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		status := model.KaiyuRegionSyncDay{MallID: item.MallID, ShopID: item.ShopID, ShopName: item.MallName, StatDate: day, FetchedAt: now.In(kaiyuLocation), State: model.KaiyuRegionSyncFailed}
		var records []model.KaiyuRegionStayDaily
		response, found := byShop[item.ShopID]
		shopName := strings.TrimSpace(response.ShopName)
		if shopName == "" {
			shopName = item.MallName
		}
		switch {
		case fetchErr != nil:
			status.ErrorMessage = "上游查询失败"
		case !found:
			status.ErrorMessage = "上游未返回店铺日期数据"
			result.SourceMissingDays++
		case duplicate[item.ShopID]:
			status.ErrorMessage = "上游返回重复店铺数据"
		case len(response.Regions) == 0:
			status.State = model.KaiyuRegionSyncEmpty
			status.ShopName = shopName
		default:
			status.State = model.KaiyuRegionSyncSuccess
			status.ShopName = shopName
			status.RecordCount = len(response.Regions)
			for _, region := range response.Regions {
				records = append(records, model.KaiyuRegionStayDaily{MallID: item.MallID, ShopID: item.ShopID, ShopName: shopName, StatDate: day, RegionName: region.RegionName, ExposureCount: region.ExposureCount, StayCount: region.StayCount, StayRate: region.StayRate, AvgStayTime: region.AvgStayTime, FetchedAt: status.FetchedAt})
			}
		}
		if err := store.SaveKaiyuRegionDay(ctx, status, records); err != nil {
			return result, fmt.Errorf("%w: save daily snapshot: %v", ErrKaiyuRegionUnavailable, err)
		}
		result.ProcessedDays++
		switch status.State {
		case model.KaiyuRegionSyncSuccess:
			result.SavedDays++
		case model.KaiyuRegionSyncEmpty:
			result.EmptyDays++
		default:
			result.FailedDays++
		}
		result.NextCursor = kaiyuRegionCursor(item)
	}
	result.Done = len(batch) == len(candidates)
	if result.Done {
		result.NextCursor = ""
	}
	return result, nil
}
func fetchKaiyuRegionShops(ctx context.Context, source kaiyuRegionSource, day time.Time, ids []string) ([]kaiyu.RegionStayShop, error) {
	var rows []kaiyu.RegionStayShop
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		rows, err = source.RegionStayByShops(requestCtx, day, ids)
		cancel()
		if err == nil {
			return rows, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, err
}
