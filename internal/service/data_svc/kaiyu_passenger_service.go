package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/kaiyu"
)

var kaiyuLocation = time.FixedZone("Asia/Shanghai", 8*3600)

var ErrKaiyuPassengerSourceMissing = errors.New("kaiyu passenger: source hour missing")

type kaiyuMallReader interface {
	List(context.Context, data_dao.MallListFilter) ([]model.Mall, error)
}
type kaiyuPassengerClient interface {
	HourlyPassengers(context.Context, string, time.Time) ([]kaiyu.HourlyPassenger, error)
}
type kaiyuPassengerWriter interface {
	UpsertKaiyuPassengerHour(context.Context, model.KaiyuPassengerHourly) error
}

type KaiyuPassengerService struct {
	malls  kaiyuMallReader
	client kaiyuPassengerClient
	store  kaiyuPassengerWriter
}
type KaiyuSyncResult struct {
	HourStart time.Time `json:"hourStart"`
	Saved     int       `json:"saved"`
	Failed    int       `json:"failed"`
	Skipped   int       `json:"skipped"`
}

func NewKaiyuPassengerService(malls kaiyuMallReader, client kaiyuPassengerClient, store kaiyuPassengerWriter) *KaiyuPassengerService {
	return &KaiyuPassengerService{malls: malls, client: client, store: store}
}

func KaiyuPreviousHour(now time.Time) time.Time {
	return now.In(kaiyuLocation).Truncate(time.Hour).Add(-time.Hour)
}

// SyncHour is independent of weather enablement and geocoding. A missing source
// hour is reported as a failure, rather than fabricated as zero traffic.
func (s *KaiyuPassengerService) SyncHour(ctx context.Context, hour time.Time) (KaiyuSyncResult, error) {
	result := KaiyuSyncResult{HourStart: hour.In(kaiyuLocation)}
	if hour.IsZero() || hour.Minute() != 0 || hour.Second() != 0 || hour.Nanosecond() != 0 {
		return result, fmt.Errorf("客流同步时间必须为整点")
	}
	var failures []error
	seen := map[string]bool{}
	var afterID uint
	for {
		malls, err := s.malls.List(ctx, data_dao.MallListFilter{AfterID: afterID, Limit: 200})
		if err != nil {
			return result, errors.Join(append(failures, err)...)
		}
		if len(malls) == 0 {
			break
		}
		for _, mall := range malls {
			if err := ctx.Err(); err != nil {
				return result, errors.Join(append(failures, err)...)
			}
			id := strings.TrimSpace(mall.KaiyuShopID)
			if id == "" {
				result.Skipped++
				continue
			}
			if seen[id] {
				result.Failed++
				failures = append(failures, fmt.Errorf("门店 %d 重复绑定开域店铺 %s", mall.ID, id))
				continue
			}
			seen[id] = true
			err := s.syncMallHour(ctx, mall, id, result.HourStart)
			if err != nil {
				result.Failed++
				failures = append(failures, fmt.Errorf("门店 %d (%s): %w", mall.ID, id, err))
			} else {
				result.Saved++
			}
		}
		nextID := malls[len(malls)-1].ID
		if nextID <= afterID {
			return result, fmt.Errorf("客流门店分页未推进")
		}
		afterID = nextID
	}
	return result, errors.Join(failures...)
}

func (s *KaiyuPassengerService) syncMallHour(ctx context.Context, mall model.Mall, id string, hour time.Time) error {
	var rows []kaiyu.HourlyPassenger
	var err error
	// Bounded retries are safe: provider calls only read, and the Oracle write
	// replaces the same shop-hour key, including after an uncertain commit.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		rows, err = s.client.HourlyPassengers(ctx, id, hour)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	return s.saveMallHour(ctx, mall, id, hour, rows)
}

// saveMallHour also serves day-based backfills, so all hours from one provider
// response use the same validation and idempotent Oracle write.
func (s *KaiyuPassengerService) saveMallHour(ctx context.Context, mall model.Mall, id string, hour time.Time, rows []kaiyu.HourlyPassenger) error {
	expected := hour.Format("2006-01-02 15:04")
	var matched *kaiyu.HourlyPassenger
	for i := range rows {
		row := &rows[i]
		if row.ShopID != id {
			continue
		}
		if row.Dateline != expected && row.Dateline != expected+":00" {
			continue
		}
		if matched != nil {
			return fmt.Errorf("接口返回重复小时 %s", expected)
		}
		matched = row
	}
	if matched == nil {
		return fmt.Errorf("%w: %s", ErrKaiyuPassengerSourceMissing, expected)
	}
	row := model.KaiyuPassengerHourly{
		MallID: mall.ID, ShopID: id, ShopName: matched.ShopName, HourStart: hour,
		Passby: matched.Passby, Traffic: matched.Traffic, TrafficCount: matched.TrafficCount, TrafficBatch: matched.TrafficBatch,
		CaptureRate: matched.CaptureRate, AvgVisitDuration: matched.AvgVisitDuration, NewVisitorsRatio: matched.NewVisitorsRatio, DeepVisitRatio: matched.DeepVisitRatio,
		NetSales: matched.NetSales, SalesTransactions: matched.SalesTransactions, ATV: matched.ATV, ShopperYield: matched.ShopperYield,
	}
	if row.ShopName == "" {
		row.ShopName = mall.NameCN
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		writeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = s.store.UpsertKaiyuPassengerHour(writeCtx, row)
		cancel()
		if err == nil {
			return nil
		}
	}
	return err
}
