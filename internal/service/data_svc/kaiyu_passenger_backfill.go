package data_svc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
)

const kaiyuBackfillBatchDays = 5

var (
	ErrKaiyuPassengerBackfillBusy = errors.New("kaiyu passenger: backfill already running")
	kaiyuBackfillMu               sync.Mutex
)

type KaiyuPassengerMissingMall struct {
	KaiyuPassengerMallOption
	MissingHours     int       `json:"missingHours"`
	FirstMissingHour time.Time `json:"firstMissingHour"`
	LastMissingHour  time.Time `json:"lastMissingHour"`
}

type KaiyuPassengerMissingResult struct {
	StartDate     string                      `json:"startDate"`
	EndDate       string                      `json:"endDate"`
	CheckedUntil  time.Time                   `json:"checkedUntil"`
	TimeZone      string                      `json:"timeZone"`
	ExpectedHours int                         `json:"expectedHours"`
	ExistingHours int                         `json:"existingHours"`
	MissingHours  int                         `json:"missingHours"`
	UnboundMalls  int                         `json:"unboundMalls"`
	Malls         []KaiyuPassengerMissingMall `json:"malls"`
}

type KaiyuPassengerBackfillRequest struct {
	KaiyuPassengerQuery
	Cursor string `json:"cursor,omitempty"`
}

type KaiyuPassengerBackfillResult struct {
	SavedHours         int    `json:"savedHours"`
	SourceMissingHours int    `json:"sourceMissingHours"`
	FailedDays         int    `json:"failedDays"`
	FailedHours        int    `json:"failedHours"`
	ScannedDays        int    `json:"scannedDays"`
	RemainingHours     int    `json:"remainingHours"`
	NextCursor         string `json:"nextCursor"`
}

type kaiyuMissingDay struct {
	mall  KaiyuPassengerMallOption
	day   time.Time
	hours []time.Time
}

func (service *KaiyuPassengerQueryService) Missing(ctx context.Context, actor uint, input KaiyuPassengerQuery) (*KaiyuPassengerMissingResult, error) {
	result, _, _, err := service.missingPlan(ctx, actor, input, time.Now())
	return result, err
}

func (service *KaiyuPassengerQueryService) missingPlan(ctx context.Context, actor uint, input KaiyuPassengerQuery, now time.Time) (*KaiyuPassengerMissingResult, []kaiyuMissingDay, kaiyuPassengerReader, error) {
	start, end, err := parseKaiyuPassengerRange(input)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var requested []uint
	if input.MallID > 0 {
		requested = []uint{input.MallID}
	}
	ids, err := service.constrain(ctx, actor, requested)
	if err != nil {
		return nil, nil, nil, err
	}
	malls, err := service.listScopedMalls(ctx, actor)
	if err != nil {
		return nil, nil, nil, err
	}
	selected := make([]KaiyuPassengerMallOption, 0, len(malls))
	for _, mall := range malls {
		if input.MallID == 0 || input.MallID == mall.MallID {
			selected = append(selected, mall)
		}
	}
	end = minTime(end, now.In(kaiyuLocation).Truncate(time.Hour))
	var rows []model.KaiyuPassengerHourly
	var reader kaiyuPassengerReader
	if end.After(start) && len(selected) > 0 {
		reader, err = service.connection(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		rows, err = reader.QueryKaiyuPassengerHours(ctx, start, end, ids)
		if errors.Is(err, reportoracle.ErrKaiyuPassengerTooManyRows) {
			return nil, nil, nil, err
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: query missing hours: %v", ErrKaiyuPassengerUnavailable, err)
		}
	}
	result, days := planKaiyuMissingHours(start, end, selected, rows)
	result.StartDate, result.EndDate = input.StartDate, input.EndDate
	return result, days, reader, nil
}

func planKaiyuMissingHours(start, end time.Time, malls []KaiyuPassengerMallOption, rows []model.KaiyuPassengerHourly) (*KaiyuPassengerMissingResult, []kaiyuMissingDay) {
	result := &KaiyuPassengerMissingResult{CheckedUntil: end, TimeZone: "Asia/Shanghai", Malls: make([]KaiyuPassengerMissingMall, 0)}
	type key struct {
		mall uint
		shop string
		hour int64
	}
	existing := make(map[key]bool, len(rows))
	for _, row := range rows {
		existing[key{row.MallID, row.ShopID, row.HourStart.Unix()}] = true
	}
	groups := make([]kaiyuMissingDay, 0)
	for _, mall := range malls {
		mall.ShopID = strings.TrimSpace(mall.ShopID)
		if mall.ShopID == "" {
			result.UnboundMalls++
			continue
		}
		summary := KaiyuPassengerMissingMall{KaiyuPassengerMallOption: mall}
		var current *kaiyuMissingDay
		for hour := start; hour.Before(end); hour = hour.Add(time.Hour) {
			result.ExpectedHours++
			if existing[key{mall.MallID, mall.ShopID, hour.Unix()}] {
				result.ExistingHours++
				continue
			}
			result.MissingHours++
			summary.MissingHours++
			if summary.FirstMissingHour.IsZero() {
				summary.FirstMissingHour = hour
			}
			summary.LastMissingHour = hour
			day := time.Date(hour.Year(), hour.Month(), hour.Day(), 0, 0, 0, 0, kaiyuLocation)
			if current == nil || !current.day.Equal(day) {
				groups = append(groups, kaiyuMissingDay{mall: mall, day: day})
				current = &groups[len(groups)-1]
			}
			current.hours = append(current.hours, hour)
		}
		if summary.MissingHours > 0 {
			result.Malls = append(result.Malls, summary)
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].day.Equal(groups[j].day) {
			return groups[i].mall.MallID < groups[j].mall.MallID
		}
		return groups[i].day.Before(groups[j].day)
	})
	return result, groups
}

func (service *KaiyuPassengerQueryService) Backfill(ctx context.Context, actor uint, input KaiyuPassengerBackfillRequest) (*KaiyuPassengerBackfillResult, error) {
	if _, _, err := parseKaiyuPassengerRange(input.KaiyuPassengerQuery); err != nil {
		return nil, err
	}
	if _, _, err := parseKaiyuBackfillCursor(input.Cursor); err != nil {
		return nil, err
	}
	if !kaiyuBackfillMu.TryLock() {
		return nil, ErrKaiyuPassengerBackfillBusy
	}
	defer kaiyuBackfillMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	missing, groups, reader, err := service.missingPlan(ctx, actor, input.KaiyuPassengerQuery, time.Now())
	if err != nil {
		return nil, err
	}
	if missing.MissingHours == 0 {
		return &KaiyuPassengerBackfillResult{}, nil
	}
	writer, ok := reader.(kaiyuPassengerWriter)
	if !ok {
		return nil, ErrKaiyuPassengerUnavailable
	}
	client, err := newKaiyuBackfillClient()
	if err != nil {
		return nil, fmt.Errorf("%w: create client: %v", ErrKaiyuPassengerUnavailable, err)
	}
	return fillKaiyuMissingDays(ctx, groups, missing.MissingHours, input.Cursor, kaiyuBackfillBatchDays, client, writer)
}

func newKaiyuBackfillClient() (kaiyuPassengerClient, error) {
	cfg, err := appConfig.LoadKaiyuConfig()
	if err != nil {
		return nil, err
	}
	return kaiyu.NewClient(cfg.Client)
}

func fillKaiyuMissingDays(ctx context.Context, groups []kaiyuMissingDay, missing int, cursor string, limit int, client kaiyuPassengerClient, writer kaiyuPassengerWriter) (*KaiyuPassengerBackfillResult, error) {
	day, mallID, err := parseKaiyuBackfillCursor(cursor)
	if err != nil {
		return nil, err
	}
	result := &KaiyuPassengerBackfillResult{RemainingHours: missing}
	service := NewKaiyuPassengerService(nil, client, writer)
	processed := 0
	last := cursor
	for _, group := range groups {
		if !day.IsZero() && (group.day.Before(day) || (group.day.Equal(day) && group.mall.MallID <= mallID)) {
			continue
		}
		if ctx.Err() != nil && processed == 0 {
			return nil, fmt.Errorf("%w: %v", ErrKaiyuPassengerUnavailable, ctx.Err())
		}
		if processed >= limit || ctx.Err() != nil {
			result.NextCursor = last
			return result, nil
		}
		processed++
		result.ScannedDays++
		last = group.day.Format(time.DateOnly) + ":" + strconv.FormatUint(uint64(group.mall.MallID), 10)
		groupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		rows, readErr := client.HourlyPassengers(groupCtx, group.mall.ShopID, group.day)
		if readErr != nil {
			result.FailedDays++
			result.FailedHours += len(group.hours)
			cancel()
			continue
		}
		mall := model.Mall{BaseModel: model.BaseModel{ID: group.mall.MallID}, NameCN: group.mall.MallName, KaiyuShopID: group.mall.ShopID}
		failedDay := false
		for _, hour := range group.hours {
			err := service.saveMallHour(groupCtx, mall, mall.KaiyuShopID, hour, rows)
			switch {
			case err == nil:
				result.SavedHours++
				result.RemainingHours--
			case errors.Is(err, ErrKaiyuPassengerSourceMissing):
				result.SourceMissingHours++
			default:
				result.FailedHours++
				failedDay = true
			}
		}
		if failedDay {
			result.FailedDays++
		}
		cancel()
	}
	return result, nil
}

func parseKaiyuBackfillCursor(cursor string) (time.Time, uint, error) {
	if cursor == "" {
		return time.Time{}, 0, nil
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return time.Time{}, 0, ErrKaiyuPassengerInvalid
	}
	day, err := time.ParseInLocation(time.DateOnly, parts[0], kaiyuLocation)
	id, idErr := strconv.ParseUint(parts[1], 10, strconv.IntSize)
	if err != nil || idErr != nil || id == 0 {
		return time.Time{}, 0, ErrKaiyuPassengerInvalid
	}
	return day, uint(id), nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
