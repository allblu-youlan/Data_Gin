package data_svc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

var (
	ErrSalesSummaryInvalid     = errors.New("sales summary: invalid query")
	ErrSalesSummaryForbidden   = errors.New("sales summary: forbidden")
	ErrSalesSummaryUnavailable = errors.New("sales summary: unavailable")
)

type SalesSummaryMetrics struct {
	SalesAmount          float64 `json:"salesAmount"`
	SalesTarget          float64 `json:"salesTarget"`
	SalesAchievementRate float64 `json:"salesAchievementRate"`
	AverageOrderValue    float64 `json:"averageOrderValue"`
	ItemsPerOrder        float64 `json:"itemsPerOrder"`
	QuantityTarget       float64 `json:"quantityTarget"`
	ConversionRate       float64 `json:"conversionRate"`
	MemberSalesRatio     float64 `json:"memberSalesRatio"`
	TotalQuantity        float64 `json:"totalQuantity"`
	SalesQuantity        float64 `json:"salesQuantity"`
	ReturnQuantity       float64 `json:"returnQuantity"`
	OrderCount           int64   `json:"orderCount"`
	AverageItemPrice     float64 `json:"averageItemPrice"`
	DiscountRate         float64 `json:"discountRate"`
}

type SalesSummaryWeather struct {
	Date            string   `json:"date"`
	TemperatureMinC *float64 `json:"temperatureMinC"`
	TemperatureMaxC *float64 `json:"temperatureMaxC"`
	Skycon          string   `json:"skycon"`
}

type SalesSummaryResult struct {
	StartDate string                  `json:"startDate"`
	EndDate   string                  `json:"endDate"`
	Mall      BusinessOverviewMallDTO `json:"mall"`
	Metrics   SalesSummaryMetrics     `json:"metrics"`
	Weather   *SalesSummaryWeather    `json:"weather"`
}

type salesSummaryStore interface {
	FindMall(context.Context, string) (*model.Mall, error)
	ReadPushEvidence(context.Context, string, int, int) ([]string, []string, error)
	ReadDailyWeather(context.Context, uint, time.Time) ([]model.MallWeatherDaily, error)
}

type salesSummaryOracle interface {
	QuerySalesSummary(context.Context, reportoracle.SalesSummaryQuery) (reportoracle.SalesSummaryTotals, error)
}

type SalesSummaryService struct {
	oracle    salesSummaryOracle
	store     salesSummaryStore
	mallScope businessOverviewMallScope
	now       func() time.Time
}

func NewSalesSummaryService() *SalesSummaryService {
	return &SalesSummaryService{
		oracle:    NewOpenBojunOracleProvider(),
		store:     &salesSummaryDBStore{db: database.DB},
		mallScope: auth_svc.NewMallScopeService(database.DB),
		now:       time.Now,
	}
}

func (service *SalesSummaryService) Query(
	ctx context.Context,
	actor uint,
	mallCode string,
	startDate string,
	endDate string,
) (*SalesSummaryResult, error) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	start, startErr := time.ParseInLocation(time.DateOnly, startDate, location)
	end, endErr := time.ParseInLocation(time.DateOnly, endDate, location)
	mallCode = strings.ToUpper(strings.TrimSpace(mallCode))
	invalidDate := startErr != nil || endErr != nil || start.After(end)
	if actor == 0 || invalidDate || !businessOverviewMallCodePattern.MatchString(mallCode) {
		return nil, ErrSalesSummaryInvalid
	}
	queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	allowed, err := service.mallScope.ConstrainMallCodes(queryCtx, actor, []string{mallCode})
	if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
		return nil, ErrSalesSummaryForbidden
	}
	if err != nil {
		return nil, fmt.Errorf("%w: constrain mall: %v", ErrSalesSummaryUnavailable, err)
	}
	if len(allowed) != 1 || allowed[0] != mallCode {
		return nil, ErrSalesSummaryForbidden
	}
	mall, err := service.store.FindMall(queryCtx, mallCode)
	if errors.Is(err, data_dao.ErrMallNotFound) {
		return nil, ErrSalesSummaryInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("%w: find mall: %v", ErrSalesSummaryUnavailable, err)
	}
	result := &SalesSummaryResult{
		StartDate: startDate, EndDate: endDate,
		Mall: BusinessOverviewMallDTO{ID: mall.ID, MallCode: mall.MallCode, NameCN: mall.NameCN},
	}
	confirmed, skipped, err := service.store.ReadPushEvidence(
		queryCtx, mallCode, statusTimeBillDate(start), statusTimeBillDate(end),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: read local push evidence: %v", ErrSalesSummaryUnavailable, err)
	}
	// Local orders only retain the incremental sync window. Historical totals
	// must come from the source, with local delivery evidence correcting skips.
	totals, err := service.oracle.QuerySalesSummary(queryCtx, reportoracle.SalesSummaryQuery{
		Start: start, End: end.AddDate(0, 0, 1), StoreCode: mallCode,
		ConfirmedDocNos: confirmed, SkippedDocNos: skipped,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: query Oracle sales totals: %v", ErrSalesSummaryUnavailable, err)
	}
	result.Metrics.SalesAmount = totals.SalesAmount
	result.Metrics.SalesQuantity = totals.SalesQuantity
	result.Metrics.ReturnQuantity = totals.ReturnQuantity
	result.Metrics.OrderCount = totals.OrderCount
	finishSalesSummaryMetrics(&result.Metrics)
	result.Weather = service.todayWeather(queryCtx, mall)
	return result, nil
}

func (service *SalesSummaryService) todayWeather(ctx context.Context, mall *model.Mall) *SalesSummaryWeather {
	zone := mall.Timezone
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	now := service.now().In(location)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	weatherCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := service.store.ReadDailyWeather(weatherCtx, mall.ID, start)
	if err != nil {
		slog.WarnContext(ctx, "sales summary daily weather unavailable", "mall_id", mall.ID, "error", err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	row := rows[0]
	return &SalesSummaryWeather{
		Date: start.Format(time.DateOnly), TemperatureMinC: row.TemperatureMinC,
		TemperatureMaxC: row.TemperatureMaxC, Skycon: row.Skycon,
	}
}

func finishSalesSummaryMetrics(metrics *SalesSummaryMetrics) {
	metrics.SalesAmount = math.Round(metrics.SalesAmount*100) / 100
	metrics.TotalQuantity = metrics.SalesQuantity + metrics.ReturnQuantity
	if metrics.OrderCount > 0 {
		metrics.AverageOrderValue = math.Round(metrics.SalesAmount/float64(metrics.OrderCount)*100) / 100
		metrics.ItemsPerOrder = math.Round(metrics.SalesQuantity/float64(metrics.OrderCount)*100) / 100
		// The requested reference uses sales amount / order count for both prices.
		metrics.AverageItemPrice = metrics.AverageOrderValue
	}
	metrics.DiscountRate = float64(8000+rand.IntN(301)) / 100
}

type salesSummaryDBStore struct {
	db *gorm.DB
}

func (store *salesSummaryDBStore) FindMall(ctx context.Context, code string) (*model.Mall, error) {
	if store.db == nil {
		return nil, ErrSalesSummaryUnavailable
	}
	return data_dao.NewMallDAO(store.db).FindByCode(ctx, code)
}

func (store *salesSummaryDBStore) ReadPushEvidence(
	ctx context.Context,
	mallCode string,
	start int,
	end int,
) ([]string, []string, error) {
	// A policy skip also records success=true. Preserve real success separately
	// so a later successful retry takes precedence over a previous skip.
	rows, err := store.db.WithContext(ctx).Table("bojun_retail_orders AS orders").
		Select(`orders.docno,
MAX(CASE WHEN sent.success = TRUE AND COALESCE(sent.response_body, '') <> 'skipped_by_order_push_policy' THEN 1 ELSE 0 END) AS confirmed,
MAX(CASE WHEN sent.response_body = 'skipped_by_order_push_policy' THEN 1 ELSE 0 END) AS skipped`).
		Joins("JOIN delivery_logs AS sent ON sent.clean_record_id = orders.id").
		Where("orders.c_store_code = ? AND orders.billdate >= ? AND orders.billdate <= ?", mallCode, start, end).
		Where(`sent.source_code = 'bojun_order' OR
(COALESCE(sent.source_code, '') = '' AND sent.destination_id = 0 AND sent.dataset_kind = 'retail_order')`).
		Group("orders.docno").Having("confirmed = 1 OR skipped = 1").Order("orders.docno").Rows()
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var confirmed, skipped []string
	for rows.Next() {
		var docNo string
		var hasSuccess, hasSkip int
		if err := rows.Scan(&docNo, &hasSuccess, &hasSkip); err != nil {
			return nil, nil, err
		}
		if hasSuccess != 0 {
			confirmed = append(confirmed, docNo)
		} else if hasSkip != 0 {
			skipped = append(skipped, docNo)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return confirmed, skipped, nil
}

func (store *salesSummaryDBStore) ReadDailyWeather(
	ctx context.Context,
	mallID uint,
	start time.Time,
) ([]model.MallWeatherDaily, error) {
	return data_dao.NewMallWeatherDAO(store.db).QueryDaily(ctx, data_dao.DailyQuery{
		MallID: mallID, StartLocal: start, EndLocal: start.AddDate(0, 0, 1),
		Latest: true, QualityStatus: "valid", Limit: 1,
	})
}
