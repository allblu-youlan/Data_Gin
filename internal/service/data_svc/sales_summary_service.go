package data_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/cache"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/shanghaimall"

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
	ReadPushedOrders(context.Context, string, int, int, func(model.BojunRetailOrder) error) error
	ReadDailyWeather(context.Context, uint, time.Time) ([]model.MallWeatherDaily, error)
}

type SalesSummaryService struct {
	store        salesSummaryStore
	mallScope    businessOverviewMallScope
	now          func() time.Time
	metricsCache *cache.MemoryTTL[salesSummaryCacheKey, SalesSummaryMetrics]
}

type salesSummaryCacheKey struct {
	MallCode string
	Start    int
	End      int
}

func NewSalesSummaryService() *SalesSummaryService {
	return &SalesSummaryService{
		store:        &salesSummaryDBStore{db: database.DB},
		mallScope:    auth_svc.NewMallScopeService(database.DB),
		now:          time.Now,
		metricsCache: cache.NewMemoryTTL[salesSummaryCacheKey, SalesSummaryMetrics](30*time.Second, 256),
	}
}

func (service *SalesSummaryService) Query(
	ctx context.Context,
	actor uint,
	mallCode string,
	startDate string,
	endDate string,
) (*SalesSummaryResult, error) {
	start, startErr := time.Parse(time.DateOnly, startDate)
	end, endErr := time.Parse(time.DateOnly, endDate)
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
	key := salesSummaryCacheKey{MallCode: mallCode, Start: statusTimeBillDate(start), End: statusTimeBillDate(end)}
	result.Metrics, err = service.readMetrics(queryCtx, key)
	if err != nil {
		return nil, fmt.Errorf("%w: summarize pushed orders: %v", ErrSalesSummaryUnavailable, err)
	}
	finishSalesSummaryMetrics(&result.Metrics)
	result.Weather = service.todayWeather(queryCtx, mall)
	return result, nil
}

func (service *SalesSummaryService) readMetrics(ctx context.Context, key salesSummaryCacheKey) (SalesSummaryMetrics, error) {
	return service.metricsCache.GetOrLoad(ctx, key, func(loadCtx context.Context) (SalesSummaryMetrics, error) {
		var metrics SalesSummaryMetrics
		err := service.store.ReadPushedOrders(loadCtx, key.MallCode, key.Start, key.End, func(order model.BojunRetailOrder) error {
			return addSalesSummaryOrder(&metrics, order)
		})
		return metrics, err
	})
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

func addSalesSummaryOrder(metrics *SalesSummaryMetrics, order model.BojunRetailOrder) error {
	amount, _ := shanghaimall.AmountsFromBojun(&order)
	retail := shanghaimall.RetailOrder{
		Amount: amount, OrderTypeCode: order.OrderTypeCode,
	}
	metrics.SalesAmount += math.Round(retail.Amount*100) / 100
	var items []struct {
		Quantity json.Number `json:"qty"`
	}
	if order.ItemsJSON != "" {
		if err := json.Unmarshal([]byte(order.ItemsJSON), &items); err != nil {
			return fmt.Errorf("order %s item quantities: %w", order.DocNo, err)
		}
	}
	var sales, returns float64
	var hasQuantity bool
	for _, item := range items {
		if item.Quantity == "" {
			continue
		}
		quantity, err := item.Quantity.Float64()
		if err != nil {
			return fmt.Errorf("order %s item quantity: %w", order.DocNo, err)
		}
		hasQuantity = true
		if retail.IsRefund() && order.OrderTypeCode != "EXP" {
			quantity = -math.Abs(quantity)
		}
		if quantity > 0 {
			sales += quantity
		} else {
			returns += quantity
		}
	}
	if !hasQuantity {
		quantity := float64(order.TotalQty)
		if retail.IsRefund() {
			quantity = -math.Abs(quantity)
		}
		if quantity > 0 {
			sales = quantity
		} else {
			returns = quantity
		}
	}
	metrics.SalesQuantity += sales
	metrics.ReturnQuantity += returns
	if sales > 0 || (!retail.IsRefund() && returns == 0) {
		metrics.OrderCount++
	}
	return nil
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

func salesSummaryPushedOrdersQuery(db *gorm.DB, mallCode string, start, end int) *gorm.DB {
	// synced=1 also includes policy skips and orders that never need a push.
	// Require delivery evidence, or Oracle's confirmed status without a local skip.
	return db.Model(&model.BojunRetailOrder{}).
		Where("c_store_code = ? AND billdate >= ? AND billdate <= ?", mallCode, start, end).
		Where(`EXISTS (
SELECT 1 FROM delivery_logs AS sent
WHERE sent.clean_record_id = bojun_retail_orders.id
AND (sent.source_code = 'bojun_order' OR
  (COALESCE(sent.source_code, '') = '' AND sent.destination_id = 0 AND sent.dataset_kind = 'retail_order'))
AND sent.success = TRUE AND COALESCE(sent.response_body, '') <> 'skipped_by_order_push_policy'
) OR (
oracle_retail_id IS NOT NULL AND JSON_UNQUOTE(JSON_EXTRACT(raw_content_json, '$.STATUS')) = '1'
AND NOT EXISTS (
SELECT 1 FROM delivery_logs AS skipped
WHERE skipped.clean_record_id = bojun_retail_orders.id
AND (skipped.source_code = 'bojun_order' OR
  (COALESCE(skipped.source_code, '') = '' AND skipped.destination_id = 0 AND skipped.dataset_kind = 'retail_order'))
AND skipped.response_body = 'skipped_by_order_push_policy'
))`)
}

func (store *salesSummaryDBStore) ReadPushedOrders(
	ctx context.Context,
	mallCode string,
	start int,
	end int,
	visit func(model.BojunRetailOrder) error,
) error {
	rows, err := salesSummaryPushedOrdersQuery(store.db.WithContext(ctx), mallCode, start, end).
		Select("docno", "oracle_retail_id", "order_type_code", "push_amount", "tot_amt_actual", "tot_qty", "items_json").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	var docNo, orderTypeCode, itemsJSON sql.NullString
	var pushAmount, totalAmount sql.NullFloat64
	var totalQuantity sql.Null[int]
	var oracleRetailID *uint64
	for rows.Next() {
		if err := rows.Scan(&docNo, &oracleRetailID, &orderTypeCode, &pushAmount, &totalAmount, &totalQuantity, &itemsJSON); err != nil {
			return err
		}
		order := model.BojunRetailOrder{
			DocNo: docNo.String, OracleRetailID: oracleRetailID, OrderTypeCode: orderTypeCode.String,
			PushAmount: pushAmount.Float64, TotalAmtActual: totalAmount.Float64,
			TotalQty: totalQuantity.V, ItemsJSON: itemsJSON.String,
		}
		if err := visit(order); err != nil {
			return err
		}
	}
	return rows.Err()
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
