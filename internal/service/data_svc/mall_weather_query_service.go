package data_svc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/auth_svc"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"
)

const (
	defaultWeatherQueryPageSize = 200
	maxWeatherQueryPageSize     = 200
	maxWeatherQueryRange        = 31 * 24 * time.Hour
	maxWeatherCursorLength      = 512
	minWeatherCursorUnixMS      = int64(946684800000)
	maxWeatherCursorUnixMS      = int64(4102444800000)
	maxWeatherOverviewMinutely  = 120
	maxWeatherOverviewAlerts    = 20
)

var (
	ErrMallWeatherInvalidQuery          = errors.New("mall weather query: invalid input")
	ErrMallWeatherCoordinateUnconfirmed = errors.New("mall weather query: coordinate unconfirmed")
)

type mallWeatherQueryDAO interface {
	QueryRealtime(ctx context.Context, query data_dao.RealtimeQuery) ([]model.MallWeatherRealtime, error)
	SummarizeRealtimeDay(ctx context.Context, query data_dao.RealtimeDaySummaryQuery) (*data_dao.RealtimeDaySummary, error)
	CountRealtime(ctx context.Context, query data_dao.RealtimeQuery) (int64, error)
	QueryMinutely(ctx context.Context, query data_dao.MinutelyQuery) ([]model.MallWeatherMinutely, error)
	CountMinutely(ctx context.Context, query data_dao.MinutelyQuery) (int64, error)
	QueryHourly(ctx context.Context, query data_dao.HourlyQuery) ([]model.MallWeatherHourly, error)
	CountHourly(ctx context.Context, query data_dao.HourlyQuery) (int64, error)
	QueryDaily(ctx context.Context, query data_dao.DailyQuery) ([]model.MallWeatherDaily, error)
	CountDaily(ctx context.Context, query data_dao.DailyQuery) (int64, error)
	QueryAlerts(ctx context.Context, query data_dao.AlertQuery) ([]model.MallWeatherAlert, error)
	CountAlerts(ctx context.Context, query data_dao.AlertQuery) (int64, error)
	QueryLifeIndices(ctx context.Context, query data_dao.LifeIndexQuery) ([]model.MallWeatherLifeIndex, error)
	CountLifeIndices(ctx context.Context, query data_dao.LifeIndexQuery) (int64, error)
	QueryFetchRuns(ctx context.Context, query data_dao.FetchRunQuery) ([]model.MallWeatherFetchRun, error)
	FindCurrentLatest(ctx context.Context, mallID uint, dataKind string) (*model.MallWeatherLatest, error)
	FindCurrentLatestByKinds(ctx context.Context, mallID uint, dataKinds []string) (map[string]model.MallWeatherLatest, error)
	FindCurrentLatestLifeSource(ctx context.Context, mallID uint, sourceAPI string) (*model.MallWeatherLatest, error)
	FindCurrentRealtime(ctx context.Context, mallID uint) (*data_dao.MallWeatherCurrentRealtime, error)
	ListOverviewMinutely(ctx context.Context, mallID uint, startUTC, endUTC time.Time, limit int) ([]model.MallWeatherMinutely, error)
	ListOverviewAlerts(ctx context.Context, mallID uint, limit int) ([]model.MallWeatherAlert, error)
}

type mallWeatherQueryMallReader interface {
	FindByID(ctx context.Context, id uint) (*model.Mall, error)
}

type MallWeatherQueryService struct {
	malls       mallWeatherQueryMallReader
	weather     mallWeatherQueryDAO
	permissions mallPermissionChecker
	now         func() time.Time
	mallScope   *auth_svc.MallScopeService
}

type MallWeatherWarningDTO struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type MallWeatherHourlyDTO struct {
	ForecastTimeUTC             time.Time               `json:"forecastTimeUtc"`
	ForecastTimeLocal           string                  `json:"forecastTimeLocal"`
	IssuedAtUTC                 time.Time               `json:"issuedAtUtc"`
	IssuedAtLocal               string                  `json:"issuedAtLocal"`
	FetchedAtUTC                time.Time               `json:"fetchedAtUtc"`
	FetchedAtLocal              string                  `json:"fetchedAtLocal"`
	TemperatureC                *float64                `json:"temperatureC,omitempty"`
	ApparentTemperatureC        *float64                `json:"apparentTemperatureC,omitempty"`
	PressurePa                  *float64                `json:"pressurePa,omitempty"`
	HumidityRatio               *float64                `json:"humidityRatio,omitempty"`
	HumidityPct                 *float64                `json:"humidityPct,omitempty"`
	WindSpeedKPH                *float64                `json:"windSpeedKph,omitempty"`
	WindDirectionDeg            *float64                `json:"windDirectionDeg,omitempty"`
	PrecipitationMMH            *float64                `json:"precipitationMmH,omitempty"`
	PrecipitationProbabilityPct *float64                `json:"precipitationProbabilityPct,omitempty"`
	CloudrateRatio              *float64                `json:"cloudrateRatio,omitempty"`
	DSWRFWM2                    *float64                `json:"dswrfWM2,omitempty"`
	VisibilityKM                *float64                `json:"visibilityKm,omitempty"`
	Skycon                      string                  `json:"skycon,omitempty"`
	PM25UGM3                    *float64                `json:"pm25UgM3,omitempty"`
	AQIChn                      *int                    `json:"aqiChn,omitempty"`
	AQIUSA                      *int                    `json:"aqiUsa,omitempty"`
	HourlyDescription           string                  `json:"hourlyDescription,omitempty"`
	ForecastKeypoint            string                  `json:"forecastKeypoint,omitempty"`
	QualityStatus               string                  `json:"qualityStatus"`
	QualityWarnings             []MallWeatherWarningDTO `json:"qualityWarnings"`
}

type MallWeatherQueryMeta struct {
	Provider            string  `json:"provider"`
	APIVersion          string  `json:"apiVersion"`
	RepresentativePoint string  `json:"representativePoint"`
	Longitude           float64 `json:"longitude"`
	Latitude            float64 `json:"latitude"`
	CoordinateSystem    string  `json:"coordinateSystem"`
	SamplingMode        string  `json:"samplingMode"`
	CoverageRadiusM     int     `json:"coverageRadiusM"`
	SpatialResolution   string  `json:"spatialResolution"`
	TimeZone            string  `json:"timeZone"`
	Unit                string  `json:"unit"`
	FreshnessStatus     string  `json:"freshnessStatus"`
	DataAgeSeconds      *int64  `json:"dataAgeSeconds,omitempty"`
}

type MallWeatherPagination struct {
	NextCursor string `json:"nextCursor,omitempty"`
	PageSize   int    `json:"pageSize"`
	Page       int    `json:"page,omitempty"`
	TotalItems *int64 `json:"totalItems,omitempty"`
	TotalPages *int64 `json:"totalPages,omitempty"`
}

type MallWeatherHourlyResult struct {
	Items      []MallWeatherHourlyDTO `json:"items"`
	Meta       MallWeatherQueryMeta   `json:"meta"`
	Pagination MallWeatherPagination  `json:"pagination"`
}

type MallWeatherRealtimeDTO struct {
	SnapshotAtUTC              time.Time               `json:"snapshotAtUtc"`
	SnapshotAtLocal            string                  `json:"snapshotAtLocal"`
	ProviderServerTimeUTC      time.Time               `json:"providerServerTimeUtc"`
	ProviderServerTimeLocal    string                  `json:"providerServerTimeLocal"`
	FetchedAtUTC               time.Time               `json:"fetchedAtUtc"`
	FetchedAtLocal             string                  `json:"fetchedAtLocal"`
	TemperatureC               *float64                `json:"temperatureC,omitempty"`
	ApparentTemperatureC       *float64                `json:"apparentTemperatureC,omitempty"`
	HumidityRatio              *float64                `json:"humidityRatio,omitempty"`
	HumidityPct                *float64                `json:"humidityPct,omitempty"`
	PressurePa                 *float64                `json:"pressurePa,omitempty"`
	WindSpeedKPH               *float64                `json:"windSpeedKph,omitempty"`
	WindDirectionDeg           *float64                `json:"windDirectionDeg,omitempty"`
	CloudrateRatio             *float64                `json:"cloudrateRatio,omitempty"`
	VisibilityKM               *float64                `json:"visibilityKm,omitempty"`
	DSWRFWM2                   *float64                `json:"dswrfWM2,omitempty"`
	Skycon                     string                  `json:"skycon,omitempty"`
	LocalPrecipitationStatus   string                  `json:"localPrecipitationStatus,omitempty"`
	LocalPrecipitationMMH      *float64                `json:"localPrecipitationMmH,omitempty"`
	LocalPrecipitationSource   string                  `json:"localPrecipitationSource,omitempty"`
	NearestPrecipitationStatus string                  `json:"nearestPrecipitationStatus,omitempty"`
	NearestPrecipDistanceKM    *float64                `json:"nearestPrecipitationDistanceKm,omitempty"`
	NearestPrecipitationMMH    *float64                `json:"nearestPrecipitationMmH,omitempty"`
	PM25UGM3                   *float64                `json:"pm25UgM3,omitempty"`
	PM10UGM3                   *float64                `json:"pm10UgM3,omitempty"`
	O3UGM3                     *float64                `json:"o3UgM3,omitempty"`
	SO2UGM3                    *float64                `json:"so2UgM3,omitempty"`
	NO2UGM3                    *float64                `json:"no2UgM3,omitempty"`
	COMGM3                     *float64                `json:"coMgM3,omitempty"`
	AQIChn                     *int                    `json:"aqiChn,omitempty"`
	AQIUSA                     *int                    `json:"aqiUsa,omitempty"`
	AQIDescriptionChn          string                  `json:"aqiDescriptionChn,omitempty"`
	AQIDescriptionUSA          string                  `json:"aqiDescriptionUsa,omitempty"`
	ComfortIndex               *int                    `json:"comfortIndex,omitempty"`
	ComfortDescription         string                  `json:"comfortDescription,omitempty"`
	UltravioletIndex           *int                    `json:"ultravioletIndex,omitempty"`
	UltravioletDescription     string                  `json:"ultravioletDescription,omitempty"`
	QualityStatus              string                  `json:"qualityStatus"`
	QualityWarnings            []MallWeatherWarningDTO `json:"qualityWarnings"`
}

type MallWeatherMinutelyDTO struct {
	ForecastMinuteUTC   time.Time               `json:"forecastMinuteUtc"`
	ForecastMinuteLocal string                  `json:"forecastMinuteLocal"`
	IssuedAtUTC         time.Time               `json:"issuedAtUtc"`
	IssuedAtLocal       string                  `json:"issuedAtLocal"`
	FetchedAtUTC        time.Time               `json:"fetchedAtUtc"`
	FetchedAtLocal      string                  `json:"fetchedAtLocal"`
	MinuteOffset        int                     `json:"minuteOffset"`
	PrecipitationMMH    *float64                `json:"precipitationMmH,omitempty"`
	ProbabilityRatio    *float64                `json:"probabilityRatio,omitempty"`
	ProbabilityPct      *float64                `json:"probabilityPct,omitempty"`
	ProbabilityWindow   *int                    `json:"probabilityWindow,omitempty"`
	Datasource          string                  `json:"datasource,omitempty"`
	Description         string                  `json:"description,omitempty"`
	ForecastKeypoint    string                  `json:"forecastKeypoint,omitempty"`
	QualityStatus       string                  `json:"qualityStatus"`
	QualityWarnings     []MallWeatherWarningDTO `json:"qualityWarnings"`
}

type MallWeatherAlertDTO struct {
	AlertID          string                  `json:"alertId"`
	Status           string                  `json:"status"`
	Code             string                  `json:"code,omitempty"`
	AlertTypeCode    string                  `json:"alertTypeCode,omitempty"`
	AlertLevelCode   string                  `json:"alertLevelCode,omitempty"`
	AlertTypeName    string                  `json:"alertTypeName,omitempty"`
	AlertLevelName   string                  `json:"alertLevelName,omitempty"`
	Title            string                  `json:"title"`
	Description      string                  `json:"description,omitempty"`
	Source           string                  `json:"source,omitempty"`
	PublishedAtUTC   *time.Time              `json:"publishedAtUtc,omitempty"`
	PublishedAtLocal *string                 `json:"publishedAtLocal,omitempty"`
	Province         string                  `json:"province,omitempty"`
	City             string                  `json:"city,omitempty"`
	County           string                  `json:"county,omitempty"`
	Location         string                  `json:"location,omitempty"`
	RegionID         string                  `json:"regionId,omitempty"`
	Adcode           string                  `json:"adcode,omitempty"`
	Latitude         *float64                `json:"latitude,omitempty"`
	Longitude        *float64                `json:"longitude,omitempty"`
	FirstSeenAtUTC   time.Time               `json:"firstSeenAtUtc"`
	FirstSeenAtLocal string                  `json:"firstSeenAtLocal"`
	LastSeenAtUTC    time.Time               `json:"lastSeenAtUtc"`
	LastSeenAtLocal  string                  `json:"lastSeenAtLocal"`
	EndedAtUTC       *time.Time              `json:"endedAtUtc,omitempty"`
	EndedAtLocal     *string                 `json:"endedAtLocal,omitempty"`
	QualityStatus    string                  `json:"qualityStatus"`
	QualityWarnings  []MallWeatherWarningDTO `json:"qualityWarnings"`
}

type MallWeatherOverviewResult struct {
	Realtime *MallWeatherRealtimeDTO  `json:"realtime,omitempty"`
	Minutely []MallWeatherMinutelyDTO `json:"minutely"`
	Hourly   []MallWeatherHourlyDTO   `json:"hourly"`
	Alerts   []MallWeatherAlertDTO    `json:"alerts"`
	Meta     MallWeatherQueryMeta     `json:"meta"`
}

type weatherHourlyCursor struct {
	Version            int   `json:"v"`
	ForecastTimeUnixMS int64 `json:"forecastTimeUnixMs"`
	IssuedAtUnixMS     int64 `json:"issuedAtUnixMs,omitempty"`
	ID                 uint  `json:"id"`
	Page               int   `json:"page,omitempty"`
}

type weatherTimeSeriesRequest struct {
	StartUTC      time.Time
	EndUTC        time.Time
	TimeZone      string
	Latest        bool
	AsOfUTC       *time.Time
	QualityStatus string
	Cursor        string
	PageSize      int
	IncludeTotals bool
}

func NewMallWeatherQueryService() *MallWeatherQueryService {
	return &MallWeatherQueryService{
		malls: data_dao.NewMallDAO(database.DB), weather: data_dao.NewMallWeatherDAO(database.DB),
		permissions: newAccountPermissionChecker(database.DB), now: time.Now,
		mallScope: auth_svc.NewMallScopeService(database.DB),
	}
}

func newMallWeatherQueryService(
	malls mallWeatherQueryMallReader,
	weather mallWeatherQueryDAO,
	permissions mallPermissionChecker,
	now func() time.Time,
) (*MallWeatherQueryService, error) {
	if malls == nil || weather == nil || permissions == nil || now == nil {
		return nil, fmt.Errorf("mall weather query: invalid service configuration")
	}
	return &MallWeatherQueryService{malls: malls, weather: weather, permissions: permissions, now: now}, nil
}

func (service *MallWeatherQueryService) Hourly(
	ctx context.Context,
	actorUserID uint,
	mallID uint,
	request requestbody.MallWeatherHourlyQueryRequest,
) (*MallWeatherHourlyResult, error) {
	if service == nil || ctx == nil || mallID == 0 {
		return nil, fmt.Errorf("%w: invalid request", ErrMallWeatherInvalidQuery)
	}
	if err := service.authorize(ctx, actorUserID); err != nil {
		return nil, err
	}
	if err := service.requireMallScope(ctx, actorUserID, mallID); err != nil {
		return nil, err
	}
	mall, err := service.malls.FindByID(ctx, mallID)
	if err != nil {
		return nil, err
	}
	location, normalized, err := normalizeHourlyWeatherRequest(request, mall)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeWeatherHourlyCursor(normalized.Cursor)
	if err != nil {
		return nil, err
	}
	page := 1
	if cursor != nil {
		page = openCursorPage(cursor.Page, true)
	}
	query := data_dao.HourlyQuery{
		MallID: mallID, StartUTC: normalized.StartUTC, EndUTC: normalized.EndUTC,
		AsOfUTC: normalized.AsOfUTC, Latest: normalized.Latest, QualityStatus: normalized.QualityStatus,
		PreferNonNullTemperature: normalized.Latest && normalized.AsOfUTC == nil,
		Limit:                    normalized.PageSize + 1,
	}
	if cursor != nil {
		forecastTime := time.UnixMilli(cursor.ForecastTimeUnixMS).UTC()
		query.AfterForecastTime = &forecastTime
		query.AfterID = cursor.ID
		if cursor.IssuedAtUnixMS != 0 {
			issuedAt := time.UnixMilli(cursor.IssuedAtUnixMS).UTC()
			query.AfterIssuedAtUTC = &issuedAt
		}
	}
	rows, err := service.weather.QueryHourly(ctx, query)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > normalized.PageSize
	if hasMore {
		rows = rows[:normalized.PageSize]
	}
	items := make([]MallWeatherHourlyDTO, len(rows))
	for index := range rows {
		items[index], err = hourlyWeatherDTO(&rows[index], location)
		if err != nil {
			return nil, err
		}
	}
	result := &MallWeatherHourlyResult{
		Items:      items,
		Meta:       weatherQueryMeta(mall, location, model.MallWeatherFreshnessFresh, nil),
		Pagination: MallWeatherPagination{PageSize: normalized.PageSize},
	}
	if normalized.IncludeTotals {
		totalItems, err := service.weather.CountHourly(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("mall weather query: count hourly rows: %w", err)
		}
		applyOpenWeatherPagination(&result.Pagination, page, totalItems)
	}
	latest, err := service.weather.FindCurrentLatest(ctx, mallID, model.MallWeatherDataKindHourly)
	if err != nil && !errors.Is(err, data_dao.ErrMallWeatherLatestNotFound) {
		return nil, err
	}
	if latest == nil {
		result.Meta.FreshnessStatus = "UNAVAILABLE"
	} else {
		status, age, err := currentWeatherFreshness(model.MallWeatherDataKindHourly, latest, service.now().UTC())
		if err != nil {
			return nil, err
		}
		result.Meta.FreshnessStatus = strings.ToUpper(status)
		result.Meta.DataAgeSeconds = &age
	}
	if hasMore && len(rows) > 0 {
		nextPage, pageErr := nextOpenCursorPage(page)
		if pageErr != nil {
			return nil, pageErr
		}
		result.Pagination.NextCursor, err = encodeWeatherHourlyCursor(&rows[len(rows)-1], nextPage)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (service *MallWeatherQueryService) Overview(ctx context.Context, actorUserID, mallID uint, timeZone string) (*MallWeatherOverviewResult, error) {
	if service == nil || ctx == nil || mallID == 0 {
		return nil, fmt.Errorf("%w: invalid request", ErrMallWeatherInvalidQuery)
	}
	if err := service.authorize(ctx, actorUserID); err != nil {
		return nil, err
	}
	if err := service.requireMallScope(ctx, actorUserID, mallID); err != nil {
		return nil, err
	}
	mall, err := service.malls.FindByID(ctx, mallID)
	if err != nil {
		return nil, err
	}
	location, err := weatherMallLocation(mall, timeZone)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	minutelyStartUTC := now.Truncate(time.Minute)
	hourlyStartUTC := now.Truncate(time.Hour)
	currentRealtime, err := service.weather.FindCurrentRealtime(ctx, mallID)
	if err != nil && !errors.Is(err, data_dao.ErrMallWeatherLatestNotFound) {
		return nil, fmt.Errorf("mall weather query: overview realtime: %w", err)
	}
	var realtime *model.MallWeatherRealtime
	preloadedLatest := make(map[string]model.MallWeatherLatest, 1)
	if currentRealtime != nil {
		realtime = &currentRealtime.Weather
		preloadedLatest[model.MallWeatherDataKindRealtime] = currentRealtime.Latest
	}
	minutely, err := service.weather.ListOverviewMinutely(ctx, mallID, minutelyStartUTC, minutelyStartUTC.Add(2*time.Hour), maxWeatherOverviewMinutely)
	if err != nil {
		return nil, fmt.Errorf("mall weather query: overview minutely: %w", err)
	}
	hourly, err := service.weather.QueryHourly(ctx, data_dao.HourlyQuery{
		MallID: mallID, StartUTC: hourlyStartUTC, EndUTC: hourlyStartUTC.Add(25 * time.Hour),
		Latest: true, PreferNonNullTemperature: true, Limit: 24,
	})
	if err != nil {
		return nil, fmt.Errorf("mall weather query: overview hourly: %w", err)
	}
	alerts, err := service.weather.ListOverviewAlerts(ctx, mallID, maxWeatherOverviewAlerts)
	if err != nil {
		return nil, fmt.Errorf("mall weather query: overview alerts: %w", err)
	}
	result := &MallWeatherOverviewResult{
		Minutely: make([]MallWeatherMinutelyDTO, len(minutely)),
		Hourly:   make([]MallWeatherHourlyDTO, len(hourly)),
		Alerts:   make([]MallWeatherAlertDTO, len(alerts)),
		Meta:     weatherQueryMeta(mall, location, model.MallWeatherFreshnessFresh, nil),
	}
	if realtime != nil {
		dto, err := realtimeWeatherDTO(realtime, location)
		if err != nil {
			return nil, err
		}
		result.Realtime = &dto
	}
	for index := range minutely {
		result.Minutely[index], err = minutelyWeatherDTO(&minutely[index], location)
		if err != nil {
			return nil, err
		}
	}
	for index := range hourly {
		result.Hourly[index], err = hourlyWeatherDTO(&hourly[index], location)
		if err != nil {
			return nil, err
		}
	}
	for index := range alerts {
		result.Alerts[index], err = alertWeatherDTO(&alerts[index], location)
		if err != nil {
			return nil, err
		}
	}
	status, age, err := service.overviewFreshness(ctx, mallID, now, preloadedLatest)
	if err != nil {
		return nil, err
	}
	result.Meta.FreshnessStatus = strings.ToUpper(status)
	result.Meta.DataAgeSeconds = age
	return result, nil
}

func (service *MallWeatherQueryService) authorize(ctx context.Context, actorUserID uint) error {
	if actorUserID == 0 {
		return ErrMallForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, PermissionWeatherRead, service.now().UTC())
	if err != nil {
		return fmt.Errorf("mall weather query: authorize: %w", err)
	}
	if !allowed {
		return ErrMallForbidden
	}
	return nil
}

func (service *MallWeatherQueryService) requireMallScope(ctx context.Context, actorUserID, mallID uint) error {
	if service.mallScope == nil {
		return nil
	}
	if err := service.mallScope.Require(ctx, actorUserID, mallID); err != nil {
		if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
			return ErrMallForbidden
		}
		return fmt.Errorf("mall weather query: check mall scope: %w", err)
	}
	return nil
}

func normalizeHourlyWeatherRequest(request requestbody.MallWeatherHourlyQueryRequest, mall *model.Mall) (*time.Location, requestbody.MallWeatherHourlyQueryRequest, error) {
	location, normalized, err := normalizeWeatherTimeSeriesRequest(weatherTimeSeriesRequest{
		StartUTC: request.StartUTC, EndUTC: request.EndUTC, TimeZone: request.TimeZone,
		Latest: request.Latest, AsOfUTC: request.AsOfUTC, QualityStatus: request.QualityStatus,
		Cursor: request.Cursor, PageSize: request.PageSize,
	}, mall)
	if err != nil {
		return nil, request, err
	}
	request.StartUTC = normalized.StartUTC
	request.EndUTC = normalized.EndUTC
	request.TimeZone = normalized.TimeZone
	request.Latest = normalized.Latest
	request.AsOfUTC = normalized.AsOfUTC
	request.QualityStatus = normalized.QualityStatus
	request.Cursor = normalized.Cursor
	request.PageSize = normalized.PageSize
	return location, request, nil
}

func normalizeWeatherTimeSeriesRequest(request weatherTimeSeriesRequest, mall *model.Mall) (*time.Location, weatherTimeSeriesRequest, error) {
	location, err := weatherMallLocation(mall, request.TimeZone)
	if err != nil {
		return nil, request, err
	}
	if request.StartUTC.IsZero() || request.EndUTC.IsZero() || !request.StartUTC.Before(request.EndUTC) ||
		request.EndUTC.Sub(request.StartUTC) > maxWeatherQueryRange {
		return nil, request, fmt.Errorf("%w: invalid time range", ErrMallWeatherInvalidQuery)
	}
	request.StartUTC = request.StartUTC.UTC()
	request.EndUTC = request.EndUTC.UTC()
	if request.AsOfUTC != nil {
		asOf := request.AsOfUTC.UTC()
		request.AsOfUTC = &asOf
	}
	request.QualityStatus = strings.ToLower(strings.TrimSpace(request.QualityStatus))
	if request.QualityStatus != "" && request.QualityStatus != weatherdomain.QualityStatusValid && request.QualityStatus != weatherdomain.QualityStatusWarning {
		return nil, request, fmt.Errorf("%w: invalid quality status", ErrMallWeatherInvalidQuery)
	}
	if request.PageSize == 0 {
		request.PageSize = defaultWeatherQueryPageSize
	}
	if request.PageSize < 1 || request.PageSize > maxWeatherQueryPageSize || len(request.Cursor) > maxWeatherCursorLength {
		return nil, request, fmt.Errorf("%w: invalid pagination", ErrMallWeatherInvalidQuery)
	}
	request.TimeZone = location.String()
	return location, request, nil
}

func weatherMallLocation(mall *model.Mall, timeZone string) (*time.Location, error) {
	if mall == nil || mall.ID == 0 || mall.WeatherLongitude == nil || mall.WeatherLatitude == nil || mall.GeocodeStatus != "confirmed" ||
		*mall.WeatherLongitude < -180 || *mall.WeatherLongitude > 180 || *mall.WeatherLatitude < -90 || *mall.WeatherLatitude > 90 {
		return nil, ErrMallWeatherCoordinateUnconfirmed
	}
	zoneName := strings.TrimSpace(timeZone)
	if zoneName == "" {
		zoneName = strings.TrimSpace(mall.Timezone)
	}
	if zoneName == "" {
		zoneName = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(zoneName)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid time zone", ErrMallWeatherInvalidQuery)
	}
	return location, nil
}

func hourlyWeatherDTO(row *model.MallWeatherHourly, location *time.Location) (MallWeatherHourlyDTO, error) {
	if row == nil || row.ID == 0 || row.ForecastTimeUTC.IsZero() || row.IssuedAtUTC.IsZero() || row.FetchedAtUTC.IsZero() || location == nil {
		return MallWeatherHourlyDTO{}, fmt.Errorf("mall weather query: invalid hourly row")
	}
	warnings, err := weatherQualityWarnings(row.QualityFlagsJSON)
	if err != nil {
		return MallWeatherHourlyDTO{}, err
	}
	return MallWeatherHourlyDTO{
		ForecastTimeUTC: row.ForecastTimeUTC.UTC(), ForecastTimeLocal: formatWeatherLocalTime(row.ForecastTimeUTC, location),
		IssuedAtUTC: row.IssuedAtUTC.UTC(), IssuedAtLocal: formatWeatherLocalTime(row.IssuedAtUTC, location),
		FetchedAtUTC: row.FetchedAtUTC.UTC(), FetchedAtLocal: formatWeatherLocalTime(row.FetchedAtUTC, location),
		TemperatureC: row.TemperatureC, ApparentTemperatureC: row.ApparentTemperatureC,
		PressurePa: row.PressurePa, HumidityRatio: row.HumidityRatio, HumidityPct: ratioPercent(row.HumidityRatio),
		WindSpeedKPH: row.WindSpeedKPH, WindDirectionDeg: row.WindDirectionDeg,
		PrecipitationMMH: row.PrecipitationMMH, PrecipitationProbabilityPct: row.PrecipProbabilityPct,
		CloudrateRatio: row.CloudrateRatio, DSWRFWM2: row.DSWRFWM2, VisibilityKM: row.VisibilityKM,
		Skycon: row.Skycon, PM25UGM3: row.PM25UGM3, AQIChn: row.AQIChn, AQIUSA: row.AQIUSA,
		HourlyDescription: row.HourlyDescription, ForecastKeypoint: row.ForecastKeypoint,
		QualityStatus: strings.ToUpper(row.QualityStatus), QualityWarnings: warnings,
	}, nil
}

func realtimeWeatherDTO(row *model.MallWeatherRealtime, location *time.Location) (MallWeatherRealtimeDTO, error) {
	if row == nil || row.ID == 0 || row.SnapshotAtUTC.IsZero() || row.ProviderServerTimeUTC.IsZero() || row.FetchedAtUTC.IsZero() || location == nil {
		return MallWeatherRealtimeDTO{}, fmt.Errorf("mall weather query: invalid realtime row")
	}
	warnings, err := weatherQualityWarnings(row.QualityFlagsJSON)
	if err != nil {
		return MallWeatherRealtimeDTO{}, err
	}
	return MallWeatherRealtimeDTO{
		SnapshotAtUTC: row.SnapshotAtUTC.UTC(), SnapshotAtLocal: formatWeatherLocalTime(row.SnapshotAtUTC, location),
		ProviderServerTimeUTC: row.ProviderServerTimeUTC.UTC(), ProviderServerTimeLocal: formatWeatherLocalTime(row.ProviderServerTimeUTC, location),
		FetchedAtUTC: row.FetchedAtUTC.UTC(), FetchedAtLocal: formatWeatherLocalTime(row.FetchedAtUTC, location),
		TemperatureC: row.TemperatureC, ApparentTemperatureC: row.ApparentTemperatureC,
		HumidityRatio: row.HumidityRatio, HumidityPct: ratioPercent(row.HumidityRatio), PressurePa: row.PressurePa,
		WindSpeedKPH: row.WindSpeedKPH, WindDirectionDeg: row.WindDirectionDeg, CloudrateRatio: row.CloudrateRatio,
		VisibilityKM: row.VisibilityKM, DSWRFWM2: row.DSWRFWM2, Skycon: row.Skycon,
		LocalPrecipitationStatus: row.LocalPrecipStatus, LocalPrecipitationMMH: row.LocalPrecipMMH,
		LocalPrecipitationSource: row.LocalPrecipDatasource, NearestPrecipitationStatus: row.NearestPrecipStatus,
		NearestPrecipDistanceKM: row.NearestPrecipDistanceKM, NearestPrecipitationMMH: row.NearestPrecipMMH,
		PM25UGM3: row.PM25UGM3, PM10UGM3: row.PM10UGM3, O3UGM3: row.O3UGM3, SO2UGM3: row.SO2UGM3,
		NO2UGM3: row.NO2UGM3, COMGM3: row.COMGM3, AQIChn: row.AQIChn, AQIUSA: row.AQIUSA,
		AQIDescriptionChn: row.AQIDescChn, AQIDescriptionUSA: row.AQIDescUSA,
		ComfortIndex: row.ComfortIndex, ComfortDescription: row.ComfortDesc,
		UltravioletIndex: row.UltravioletIndex, UltravioletDescription: row.UltravioletDesc,
		QualityStatus: strings.ToUpper(row.QualityStatus), QualityWarnings: warnings,
	}, nil
}

func minutelyWeatherDTO(row *model.MallWeatherMinutely, location *time.Location) (MallWeatherMinutelyDTO, error) {
	if row == nil || row.ID == 0 || row.ForecastMinuteUTC.IsZero() || row.IssuedAtUTC.IsZero() || row.FetchedAtUTC.IsZero() || location == nil {
		return MallWeatherMinutelyDTO{}, fmt.Errorf("mall weather query: invalid minutely row")
	}
	warnings, err := weatherQualityWarnings(row.QualityFlagsJSON)
	if err != nil {
		return MallWeatherMinutelyDTO{}, err
	}
	return MallWeatherMinutelyDTO{
		ForecastMinuteUTC: row.ForecastMinuteUTC.UTC(), ForecastMinuteLocal: formatWeatherLocalTime(row.ForecastMinuteUTC, location),
		IssuedAtUTC: row.IssuedAtUTC.UTC(), IssuedAtLocal: formatWeatherLocalTime(row.IssuedAtUTC, location),
		FetchedAtUTC: row.FetchedAtUTC.UTC(), FetchedAtLocal: formatWeatherLocalTime(row.FetchedAtUTC, location),
		MinuteOffset: row.MinuteOffset, PrecipitationMMH: row.PrecipitationMMH,
		ProbabilityRatio: row.ProbabilityRatio, ProbabilityPct: ratioPercent(row.ProbabilityRatio),
		ProbabilityWindow: row.ProbabilityWindow, Datasource: row.Datasource,
		Description: row.Description, ForecastKeypoint: row.ForecastKeypoint,
		QualityStatus: strings.ToUpper(row.QualityStatus), QualityWarnings: warnings,
	}, nil
}

func alertWeatherDTO(row *model.MallWeatherAlert, location *time.Location) (MallWeatherAlertDTO, error) {
	if row == nil || row.ID == 0 || strings.TrimSpace(row.AlertID) == "" || row.FirstSeenAt.IsZero() || row.LastSeenAt.IsZero() || location == nil {
		return MallWeatherAlertDTO{}, fmt.Errorf("mall weather query: invalid alert row")
	}
	warnings, err := weatherQualityWarnings(row.QualityFlagsJSON)
	if err != nil {
		return MallWeatherAlertDTO{}, err
	}
	dto := MallWeatherAlertDTO{
		AlertID: row.AlertID, Status: strings.ToUpper(row.Status), Code: row.Code,
		AlertTypeCode: row.AlertTypeCode, AlertLevelCode: row.AlertLevelCode,
		AlertTypeName: row.AlertTypeName, AlertLevelName: row.AlertLevelName,
		Title: row.Title, Description: row.Description, Source: row.Source,
		Province: row.Province, City: row.City, County: row.County, Location: row.Location,
		RegionID: row.RegionID, Adcode: row.Adcode, Latitude: row.Latitude, Longitude: row.Longitude,
		FirstSeenAtUTC: row.FirstSeenAt.UTC(), FirstSeenAtLocal: formatWeatherLocalTime(row.FirstSeenAt, location),
		LastSeenAtUTC: row.LastSeenAt.UTC(), LastSeenAtLocal: formatWeatherLocalTime(row.LastSeenAt, location),
		QualityStatus: strings.ToUpper(row.QualityStatus), QualityWarnings: warnings,
	}
	if row.PublishedAtUTC != nil {
		publishedAtUTC := row.PublishedAtUTC.UTC()
		publishedAtLocal := formatWeatherLocalTime(publishedAtUTC, location)
		dto.PublishedAtUTC = &publishedAtUTC
		dto.PublishedAtLocal = &publishedAtLocal
	}
	if row.EndedAt != nil {
		endedAtUTC := row.EndedAt.UTC()
		endedAtLocal := formatWeatherLocalTime(endedAtUTC, location)
		dto.EndedAtUTC = &endedAtUTC
		dto.EndedAtLocal = &endedAtLocal
	}
	return dto, nil
}

func weatherQualityWarnings(raw model.JSONText) ([]MallWeatherWarningDTO, error) {
	parsed := make([]caiyun.ParseWarning, 0)
	if strings.TrimSpace(string(raw)) != "" {
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, fmt.Errorf("mall weather query: decode quality warnings: %w", err)
		}
	}
	warnings := make([]MallWeatherWarningDTO, len(parsed))
	for index := range parsed {
		warnings[index] = MallWeatherWarningDTO{Code: parsed[index].Code, Path: parsed[index].Path}
	}
	return warnings, nil
}

func (service *MallWeatherQueryService) overviewFreshness(
	ctx context.Context,
	mallID uint,
	now time.Time,
	preloaded map[string]model.MallWeatherLatest,
) (string, *int64, error) {
	if service == nil || service.weather == nil || ctx == nil || mallID == 0 || now.IsZero() {
		return "", nil, fmt.Errorf("mall weather query: invalid overview freshness request")
	}
	kinds := []string{
		model.MallWeatherDataKindRealtime,
		model.MallWeatherDataKindMinutely,
		model.MallWeatherDataKindHourly,
	}
	latestByKind := make(map[string]model.MallWeatherLatest, len(kinds))
	for dataKind, latest := range preloaded {
		latestByKind[dataKind] = latest
	}
	missing := make([]string, 0, len(kinds))
	for _, dataKind := range kinds {
		if _, exists := latestByKind[dataKind]; !exists {
			missing = append(missing, dataKind)
		}
	}
	if len(missing) > 0 {
		batch, err := service.weather.FindCurrentLatestByKinds(ctx, mallID, missing)
		if err != nil {
			return "", nil, fmt.Errorf("mall weather query: find overview freshness: %w", err)
		}
		for dataKind, latest := range batch {
			latestByKind[dataKind] = latest
		}
	}
	worstStatus := model.MallWeatherFreshnessFresh
	worstRank := weatherFreshnessRank(worstStatus)
	var maxAge *int64
	unavailable := false
	for _, dataKind := range kinds {
		latest, exists := latestByKind[dataKind]
		if !exists {
			unavailable = true
			continue
		}
		status, age, err := currentWeatherFreshness(dataKind, &latest, now)
		if err != nil {
			return "", nil, err
		}
		if maxAge == nil || age > *maxAge {
			ageCopy := age
			maxAge = &ageCopy
		}
		if rank := weatherFreshnessRank(status); rank > worstRank {
			worstStatus = status
			worstRank = rank
		}
	}
	if unavailable {
		return "unavailable", maxAge, nil
	}
	return worstStatus, maxAge, nil
}

func weatherFreshnessRank(status string) int {
	switch status {
	case model.MallWeatherFreshnessWarning:
		return 1
	case model.MallWeatherFreshnessCritical:
		return 2
	case model.MallWeatherFreshnessStale:
		return 3
	default:
		return 0
	}
}

func weatherQueryMeta(mall *model.Mall, location *time.Location, freshness string, age *int64) MallWeatherQueryMeta {
	return MallWeatherQueryMeta{
		Provider: "CAIYUN", APIVersion: "v2.6", RepresentativePoint: "MALL_CENTER",
		Longitude: *mall.WeatherLongitude, Latitude: *mall.WeatherLatitude,
		CoordinateSystem: strings.ToUpper(mall.WeatherCoordinateSystem), SamplingMode: strings.ToUpper(mall.SamplingMode),
		CoverageRadiusM:   mall.CoverageRadiusM,
		SpatialResolution: "9-13km; precipitation within first 2h may be 1km",
		TimeZone:          location.String(), Unit: "metric:v2", FreshnessStatus: strings.ToUpper(freshness), DataAgeSeconds: age,
	}
}

func currentWeatherFreshness(dataKind string, latest *model.MallWeatherLatest, now time.Time) (string, int64, error) {
	if latest == nil || latest.FetchedAtUTC.IsZero() || now.IsZero() {
		return "", 0, fmt.Errorf("mall weather query: invalid freshness pointer")
	}
	status := latest.FreshnessStatus
	if status != model.MallWeatherFreshnessStale {
		var err error
		status, err = weatherdomain.FreshnessStatus(dataKind, latest.FetchedAtUTC, now)
		if err != nil {
			return "", 0, err
		}
	}
	age := now.UTC().Sub(latest.FetchedAtUTC.UTC())
	if age < 0 {
		age = 0
	}
	return status, int64(age / time.Second), nil
}

func encodeWeatherHourlyCursor(row *model.MallWeatherHourly, page int) (string, error) {
	if row == nil || row.ID == 0 || row.ForecastTimeUTC.IsZero() || row.IssuedAtUTC.IsZero() {
		return "", fmt.Errorf("mall weather query: invalid cursor row")
	}
	payload, err := json.Marshal(weatherHourlyCursor{
		Version: 1, ForecastTimeUnixMS: row.ForecastTimeUTC.UTC().UnixMilli(),
		IssuedAtUnixMS: row.IssuedAtUTC.UTC().UnixMilli(), ID: row.ID, Page: page,
	})
	if err != nil {
		return "", fmt.Errorf("mall weather query: encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeWeatherHourlyCursor(value string) (*weatherHourlyCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if len(value) > maxWeatherCursorLength {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherInvalidQuery)
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherInvalidQuery)
	}
	var cursor weatherHourlyCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.ID == 0 || invalidOpenCursorPage(cursor.Page) ||
		cursor.ForecastTimeUnixMS < minWeatherCursorUnixMS || cursor.ForecastTimeUnixMS >= maxWeatherCursorUnixMS ||
		cursor.IssuedAtUnixMS < minWeatherCursorUnixMS || cursor.IssuedAtUnixMS >= maxWeatherCursorUnixMS {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherInvalidQuery)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: invalid cursor", ErrMallWeatherInvalidQuery)
	}
	return &cursor, nil
}

func formatWeatherLocalTime(value time.Time, location *time.Location) string {
	return value.UTC().In(location).Format(time.RFC3339Nano)
}

func ratioPercent(value *float64) *float64 {
	if value == nil {
		return nil
	}
	percent := *value * 100
	return &percent
}
