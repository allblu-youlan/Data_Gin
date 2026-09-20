package data_svc

import (
	"context"
	"errors"
	"math"
	"time"

	"gin-biz-web-api/pkg/database"
)

const (
	defaultMallWeatherCapacityMinutelyRows                 = 120
	defaultMallWeatherCapacityHourlyBatch                  = 200
	defaultMallWeatherCapacityAlertBatch                   = 200
	defaultMallWeatherCapacityWeatherV26CallsPerMallPerDay = 168
	maxMallWeatherCapacityMallCount                        = 100000
	maxMallWeatherCapacityProviderQPS                      = 10000
	maxMallWeatherCapacityAlertsPerMall                    = 256
)

var ErrMallWeatherInvalidCapacityPlan = errors.New("mall weather capacity plan: invalid input")

type MallWeatherCapacityPlanService struct {
	permissions mallPermissionChecker
	now         func() time.Time
}

func NewMallWeatherCapacityPlanService() *MallWeatherCapacityPlanService {
	return &MallWeatherCapacityPlanService{
		permissions: newAccountPermissionChecker(database.DB),
		now:         time.Now,
	}
}

func newMallWeatherCapacityPlanService(
	permissions mallPermissionChecker,
	now func() time.Time,
) (*MallWeatherCapacityPlanService, error) {
	if permissions == nil || now == nil {
		return nil, ErrMallWeatherInvalidCapacityPlan
	}
	return &MallWeatherCapacityPlanService{permissions: permissions, now: now}, nil
}

type MallWeatherCapacityPlanInput struct {
	MallCount       int
	ProviderQPS     float64
	HourlySteps     int
	DailySteps      int
	LifeIndexDays   int
	AlertsPerMall   int
	FeishuBatchRows int
}

type MallWeatherCapacityDatasetPlan struct {
	Kind            string `json:"kind"`
	Rows            int64  `json:"rows"`
	DatabaseBatches int64  `json:"databaseBatches"`
	FeishuBatches   int64  `json:"feishuBatches"`
}

type MallWeatherCapacityPlan struct {
	MallCount                         int                              `json:"mallCount"`
	ProviderQPS                       float64                          `json:"providerQps"`
	WeatherV26ProviderRequestsPerDay  int64                            `json:"weatherV26ProviderRequestsPerDay"`
	LifeIndexV3ProviderRequestsPerDay int64                            `json:"lifeIndexV3ProviderRequestsPerDay"`
	ProviderRequests                  int64                            `json:"providerRequests"`
	ProviderDrainSeconds              float64                          `json:"providerDrainSeconds"`
	MinimumQPSForOneHourDrain         float64                          `json:"minimumQpsForOneHourDrain"`
	TotalDatabaseRows                 int64                            `json:"totalDatabaseRows"`
	TotalDatabaseBatches              int64                            `json:"totalDatabaseBatches"`
	FeishuBatchRows                   int                              `json:"feishuBatchRows"`
	TotalFeishuBatches                int64                            `json:"totalFeishuBatches"`
	Datasets                          []MallWeatherCapacityDatasetPlan `json:"datasets"`
}

func (service *MallWeatherCapacityPlanService) Plan(
	ctx context.Context,
	actorUserID uint,
	input MallWeatherCapacityPlanInput,
) (*MallWeatherCapacityPlan, error) {
	if service == nil || service.permissions == nil || service.now == nil || ctx == nil {
		return nil, ErrMallWeatherInvalidCapacityPlan
	}
	if actorUserID == 0 {
		return nil, ErrMallForbidden
	}
	allowed, err := service.permissions.HasPermission(
		ctx,
		actorUserID,
		PermissionWeatherConfigManage,
		service.now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrMallForbidden
	}
	plan, err := BuildMallWeatherCapacityPlan(input)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func BuildMallWeatherCapacityPlan(input MallWeatherCapacityPlanInput) (MallWeatherCapacityPlan, error) {
	normalized, err := normalizeMallWeatherCapacityPlanInput(input)
	if err != nil {
		return MallWeatherCapacityPlan{}, err
	}
	malls := int64(normalized.MallCount)
	weatherRequests := malls * defaultMallWeatherCapacityWeatherV26CallsPerMallPerDay
	providerRequests := weatherRequests
	datasets := []MallWeatherCapacityDatasetPlan{
		mallWeatherCapacityDataset("realtime", malls, malls, normalized.FeishuBatchRows),
		mallWeatherCapacityDataset("minutely", malls*defaultMallWeatherCapacityMinutelyRows, malls, normalized.FeishuBatchRows),
		mallWeatherCapacityDataset("hourly", malls*int64(normalized.HourlySteps), malls*ceilInt64(int64(normalized.HourlySteps), defaultMallWeatherCapacityHourlyBatch), normalized.FeishuBatchRows),
		mallWeatherCapacityDataset("daily", malls*int64(normalized.DailySteps), malls, normalized.FeishuBatchRows),
		mallWeatherCapacityDataset("alerts", malls*int64(normalized.AlertsPerMall), malls*ceilInt64(int64(normalized.AlertsPerMall), defaultMallWeatherCapacityAlertBatch), normalized.FeishuBatchRows),
		mallWeatherCapacityDataset("life_indices", malls*int64(normalized.LifeIndexDays), malls, normalized.FeishuBatchRows),
	}
	plan := MallWeatherCapacityPlan{
		MallCount:                         normalized.MallCount,
		ProviderQPS:                       normalized.ProviderQPS,
		WeatherV26ProviderRequestsPerDay:  weatherRequests,
		LifeIndexV3ProviderRequestsPerDay: 0,
		ProviderRequests:                  providerRequests,
		ProviderDrainSeconds:              float64(providerRequests) / normalized.ProviderQPS,
		MinimumQPSForOneHourDrain:         float64(providerRequests) / 3600,
		FeishuBatchRows:                   normalized.FeishuBatchRows,
		Datasets:                          datasets,
	}
	for _, dataset := range datasets {
		plan.TotalDatabaseRows += dataset.Rows
		plan.TotalDatabaseBatches += dataset.DatabaseBatches
		plan.TotalFeishuBatches += dataset.FeishuBatches
	}
	return plan, nil
}

func normalizeMallWeatherCapacityPlanInput(input MallWeatherCapacityPlanInput) (MallWeatherCapacityPlanInput, error) {
	if input.FeishuBatchRows == 0 {
		input.FeishuBatchRows = defaultMallWeatherFeishuBatchRows
	}
	if input.MallCount < 1 || input.MallCount > maxMallWeatherCapacityMallCount ||
		input.ProviderQPS <= 0 || math.IsNaN(input.ProviderQPS) || math.IsInf(input.ProviderQPS, 0) ||
		input.ProviderQPS > maxMallWeatherCapacityProviderQPS ||
		input.HourlySteps < 1 || input.HourlySteps > 360 ||
		input.DailySteps < 1 || input.DailySteps > 15 ||
		input.LifeIndexDays < 1 || input.LifeIndexDays > 15 ||
		input.AlertsPerMall < 0 || input.AlertsPerMall > maxMallWeatherCapacityAlertsPerMall ||
		input.FeishuBatchRows < 1 || input.FeishuBatchRows > maxMallWeatherFeishuBatchRows {
		return MallWeatherCapacityPlanInput{}, ErrMallWeatherInvalidCapacityPlan
	}
	return input, nil
}

func mallWeatherCapacityDataset(kind string, rows, databaseBatches int64, feishuBatchRows int) MallWeatherCapacityDatasetPlan {
	return MallWeatherCapacityDatasetPlan{
		Kind:            kind,
		Rows:            rows,
		DatabaseBatches: databaseBatches,
		FeishuBatches:   ceilInt64(rows, int64(feishuBatchRows)),
	}
}

func ceilInt64(value, divisor int64) int64 {
	if value <= 0 {
		return 0
	}
	return (value + divisor - 1) / divisor
}
