package data_svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/requestbody"
	"gin-biz-web-api/internal/service/auth_svc"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const (
	PermissionMallRead            = model.PermissionMallRead
	PermissionMallWrite           = model.PermissionMallWrite
	PermissionMallGeocodeConfirm  = model.PermissionMallGeocodeConfirm
	PermissionWeatherRead         = model.PermissionWeatherRead
	PermissionWeatherRefresh      = model.PermissionWeatherRefresh
	PermissionWeatherExport       = model.PermissionWeatherExport
	PermissionWeatherFeishuPush   = model.PermissionWeatherFeishuPush
	PermissionWeatherConfigManage = model.PermissionWeatherConfigManage

	mallCreateOperationScope = "mall.create"
	maxIdempotencyKeyLength  = 255
	maxMallImportRows        = 200
)

var (
	ErrMallForbidden           = errors.New("mall service: forbidden")
	ErrMallInvalidInput        = errors.New("mall service: invalid input")
	ErrMallConflict            = errors.New("mall service: conflict")
	ErrMallIdempotencyConflict = errors.New("mall service: idempotency conflict")
	ErrMallIdempotencyPending  = errors.New("mall service: idempotency request pending")
	ErrMallWeatherDisabled     = errors.New("mall service: weather feature disabled")

	mallCodePattern       = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,63}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,254}$`)
)

type mallPermissionChecker interface {
	HasPermission(ctx context.Context, userID uint, permission string, now time.Time) (bool, error)
}

type MallService struct {
	db                    *gorm.DB
	malls                 *data_dao.MallDAO
	permissions           mallPermissionChecker
	defaultDetailProfile  string
	defaultCoverageRadius int
	weatherFeatureEnabled func() bool
	now                   func() time.Time
	mallScope             *auth_svc.MallScopeService
}

type MallCreateResult struct {
	ID            uint      `json:"id"`
	MallCode      string    `json:"mallCode"`
	Status        string    `json:"status"`
	GeocodeStatus string    `json:"geocodeStatus"`
	WeatherStatus string    `json:"weatherStatus"`
	Version       uint64    `json:"version"`
	CreatedAt     time.Time `json:"createdAt"`
}

type MallDTO struct {
	ID                uint                                      `json:"id"`
	MallCode          string                                    `json:"mallCode"`
	NameCN            string                                    `json:"nameCn"`
	NameEN            string                                    `json:"nameEn,omitempty"`
	Province          string                                    `json:"province"`
	City              string                                    `json:"city"`
	District          string                                    `json:"district,omitempty"`
	Address           string                                    `json:"address"`
	Longitude         *float64                                  `json:"longitude,omitempty"`
	Latitude          *float64                                  `json:"latitude,omitempty"`
	CoordinateSystem  string                                    `json:"coordinateSystem,omitempty"`
	GeocodeStatus     string                                    `json:"geocodeStatus"`
	BusinessHours     map[string][]requestbody.MallBusinessHour `json:"businessHours,omitempty"`
	GrossFloorAreaSQM *float64                                  `json:"grossFloorAreaSqm,omitempty"`
	ParkingSpaces     *int                                      `json:"parkingSpaces,omitempty"`
	Tags              []string                                  `json:"tags,omitempty"`
	WeatherEnabled    bool                                      `json:"weatherEnabled"`
	WeatherProvider   string                                    `json:"weatherProvider"`
	DetailProfile     string                                    `json:"detailProfile"`
	CoverageRadiusM   int                                       `json:"coverageRadiusM"`
	TimeZone          string                                    `json:"timeZone"`
	Status            string                                    `json:"status"`
	Version           uint64                                    `json:"version"`
	CreatedAt         time.Time                                 `json:"createdAt"`
	UpdatedAt         time.Time                                 `json:"updatedAt"`
}

type MallListResult struct {
	Items       []MallDTO `json:"items"`
	NextAfterID uint      `json:"nextAfterId,omitempty"`
}

type MallImportRowResult struct {
	Row          int               `json:"row"`
	Status       string            `json:"status"`
	ReviewStatus string            `json:"reviewStatus,omitempty"`
	Mall         *MallCreateResult `json:"mall,omitempty"`
	ErrorCode    string            `json:"errorCode,omitempty"`
}

type MallImportResult struct {
	Rows     []MallImportRowResult `json:"rows"`
	Created  int                   `json:"created"`
	Replayed int                   `json:"replayed"`
	Failed   int                   `json:"failed"`
}

func NewMallService() *MallService {
	return &MallService{
		db:                    database.DB,
		malls:                 data_dao.NewMallDAO(),
		permissions:           newAccountPermissionChecker(database.DB),
		defaultDetailProfile:  config.GetString("cfg.mall_weather.default_detail_profile", "full"),
		defaultCoverageRadius: config.GetInt("cfg.mall_weather.coverage_radius_m", 1000),
		weatherFeatureEnabled: func() bool { return global.MallWeatherEnabledAtStartup },
		now:                   time.Now,
		mallScope:             auth_svc.NewMallScopeService(database.DB),
	}
}

func (service *MallService) Create(ctx context.Context, actorUserID uint, idempotencyKey string, request requestbody.MallCreateRequest) (*MallCreateResult, bool, error) {
	if err := service.authorize(ctx, actorUserID, PermissionMallWrite); err != nil {
		return nil, false, err
	}
	return service.createAuthorized(ctx, actorUserID, idempotencyKey, request)
}

func (service *MallService) Import(ctx context.Context, actorUserID uint, batchIdempotencyKey string, items []requestbody.MallCreateRequest) (*MallImportResult, error) {
	if err := service.authorize(ctx, actorUserID, PermissionMallWrite); err != nil {
		return nil, err
	}
	if !validIdempotencyKey(batchIdempotencyKey) {
		return nil, fmt.Errorf("%w: idempotency key is required", ErrMallInvalidInput)
	}
	if len(items) == 0 || len(items) > maxMallImportRows {
		return nil, fmt.Errorf("%w: import items must contain 1 to %d rows", ErrMallInvalidInput, maxMallImportRows)
	}

	result := &MallImportResult{Rows: make([]MallImportRowResult, 0, len(items))}
	for index, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rowKey := sha256Hex([]byte(fmt.Sprintf("%s\x1f%d", batchIdempotencyKey, index+1)))
		mall, replayed, err := service.createAuthorized(ctx, actorUserID, rowKey, item)
		row := MallImportRowResult{Row: index + 1}
		if err != nil {
			row.Status = "FAILED"
			row.ErrorCode = classifyMallImportError(err)
			result.Failed++
		} else {
			row.Mall = mall
			row.ReviewStatus = "PENDING_GEOCODE"
			if replayed {
				row.Status = "REPLAYED"
				result.Replayed++
			} else {
				row.Status = "CREATED"
				result.Created++
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

func (service *MallService) createAuthorized(ctx context.Context, actorUserID uint, idempotencyKey string, request requestbody.MallCreateRequest) (*MallCreateResult, bool, error) {
	normalized, err := normalizeMallCreateRequest(request)
	if err != nil {
		return nil, false, err
	}
	if !validIdempotencyKey(idempotencyKey) {
		return nil, false, fmt.Errorf("%w: idempotency key is required", ErrMallInvalidInput)
	}
	requestHash, err := hashJSON(normalized)
	if err != nil {
		return nil, false, fmt.Errorf("mall service: hash create request: %w", err)
	}
	if err := applyMallWeatherDefaults(&normalized, service.defaultDetailProfile, service.defaultCoverageRadius); err != nil {
		return nil, false, err
	}
	keyHash := sha256Hex([]byte(idempotencyKey))

	var result *MallCreateResult
	var replayed bool
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		idempotencyDAO := data_dao.NewAPIIdempotencyDAO(tx)
		record := &model.APIIdempotencyRecord{
			OperationScope: mallCreateOperationScope,
			ActorUserID:    actorUserID,
			KeyHash:        keyHash,
			RequestHash:    requestHash,
			ResourceType:   "mall",
			ResponseJSON:   model.JSONText(`{}`),
		}
		reserved, err := idempotencyDAO.Reserve(ctx, record)
		if err != nil {
			return err
		}
		if !reserved {
			existing, err := idempotencyDAO.FindForUpdate(ctx, mallCreateOperationScope, actorUserID, keyHash)
			if err != nil {
				return err
			}
			if existing.RequestHash != requestHash {
				return ErrMallIdempotencyConflict
			}
			if existing.ResourceID == 0 || existing.HTTPStatus == 0 || existing.ResponseJSON == "" || existing.ResponseJSON == model.JSONText(`{}`) {
				return ErrMallIdempotencyPending
			}
			var snapshot MallCreateResult
			if err := json.Unmarshal([]byte(existing.ResponseJSON), &snapshot); err != nil {
				return fmt.Errorf("mall service: decode idempotency response: %w", err)
			}
			result = &snapshot
			replayed = true
			return nil
		}

		mall, err := mallFromCreateRequest(normalized, actorUserID, service.now().UTC())
		if err != nil {
			return err
		}
		if err := data_dao.NewMallDAO(tx).Create(ctx, mall); err != nil {
			if isDuplicateKeyError(err) {
				return ErrMallConflict
			}
			return fmt.Errorf("mall service: create mall: %w", err)
		}
		outbox, err := newMallGeocodeOutbox(mall, service.now().UTC())
		if err != nil {
			return err
		}
		if err := data_dao.NewAsyncJobOutboxDAO(tx).Create(ctx, outbox); err != nil {
			return fmt.Errorf("mall service: create geocode outbox: %w", err)
		}

		created := mallCreateResult(mall)
		responseJSON, err := json.Marshal(created)
		if err != nil {
			return fmt.Errorf("mall service: encode create response: %w", err)
		}
		if err := idempotencyDAO.Complete(ctx, record.ID, mall.ID, http.StatusCreated, model.JSONText(responseJSON)); err != nil {
			return err
		}
		result = &created
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return result, replayed, nil
}

func classifyMallImportError(err error) string {
	switch {
	case errors.Is(err, ErrMallInvalidInput):
		return "INVALID_INPUT"
	case errors.Is(err, ErrMallConflict), errors.Is(err, ErrMallIdempotencyConflict), errors.Is(err, ErrMallIdempotencyPending):
		return "CONFLICT"
	default:
		return "UNAVAILABLE"
	}
}

func (service *MallService) Get(ctx context.Context, actorUserID, mallID uint) (*MallDTO, error) {
	if err := service.authorize(ctx, actorUserID, PermissionMallRead); err != nil {
		return nil, err
	}
	if err := service.requireMallScope(ctx, actorUserID, mallID); err != nil {
		return nil, err
	}
	mall, err := service.malls.FindByID(ctx, mallID)
	if err != nil {
		return nil, err
	}
	dto, err := mallDTO(mall)
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

func (service *MallService) List(ctx context.Context, actorUserID uint, request requestbody.MallListRequest) (*MallListResult, error) {
	if err := service.authorize(ctx, actorUserID, PermissionMallRead); err != nil {
		return nil, err
	}
	request.City = strings.TrimSpace(request.City)
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.GeocodeStatus = strings.ToLower(strings.TrimSpace(request.GeocodeStatus))
	if request.Status != "" && !oneOf(request.Status, "draft", "active", "disabled") {
		return nil, fmt.Errorf("%w: invalid status filter", ErrMallInvalidInput)
	}
	if request.GeocodeStatus != "" && !oneOf(request.GeocodeStatus, "pending", "review", "confirmed", "failed") {
		return nil, fmt.Errorf("%w: invalid geocode status filter", ErrMallInvalidInput)
	}
	rows, err := service.malls.List(ctx, data_dao.MallListFilter{
		ActorUserID:    actorUserID,
		AfterID:        request.AfterID,
		Limit:          request.Limit,
		City:           request.City,
		Status:         request.Status,
		GeocodeStatus:  request.GeocodeStatus,
		WeatherEnabled: request.WeatherEnabled,
	})
	if err != nil {
		return nil, err
	}
	result := &MallListResult{Items: make([]MallDTO, 0, len(rows))}
	for index := range rows {
		dto, err := mallDTO(&rows[index])
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, dto)
	}
	if len(rows) > 0 {
		result.NextAfterID = rows[len(rows)-1].ID
	}
	return result, nil
}

func (service *MallService) Update(ctx context.Context, actorUserID, mallID uint, request requestbody.MallPatchRequest) (*MallDTO, error) {
	if err := service.authorize(ctx, actorUserID, PermissionMallWrite); err != nil {
		return nil, err
	}
	if err := service.requireMallScope(ctx, actorUserID, mallID); err != nil {
		return nil, err
	}
	if request.ExpectedMallVersion == 0 {
		return nil, fmt.Errorf("%w: expectedMallVersion is required", ErrMallInvalidInput)
	}

	var updated *model.Mall
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		mallDAO := data_dao.NewMallDAO(tx)
		current, err := mallDAO.FindByID(ctx, mallID)
		if err != nil {
			return err
		}
		if current.Version != request.ExpectedMallVersion {
			return data_dao.ErrMallVersionConflict
		}
		updates, requiresGeocode, err := service.prepareMallPatch(current, request, actorUserID)
		if err != nil {
			return err
		}
		if err := mallDAO.UpdateWithVersion(ctx, mallID, request.ExpectedMallVersion, updates); err != nil {
			return err
		}
		if requiresGeocode {
			candidate := *current
			applyMallUpdatesForOutbox(&candidate, updates)
			candidate.Version = request.ExpectedMallVersion + 1
			outbox, err := newMallGeocodeOutbox(&candidate, service.now().UTC())
			if err != nil {
				return err
			}
			if err := data_dao.NewAsyncJobOutboxDAO(tx).Create(ctx, outbox); err != nil {
				return fmt.Errorf("mall service: create updated geocode outbox: %w", err)
			}
		}
		updated, err = mallDAO.FindByID(ctx, mallID)
		return err
	})
	if err != nil {
		return nil, err
	}
	dto, err := mallDTO(updated)
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

func (service *MallService) prepareMallPatch(
	current *model.Mall,
	request requestbody.MallPatchRequest,
	actorUserID uint,
) (map[string]interface{}, bool, error) {
	updates, requiresGeocode, err := buildMallPatch(current, request, actorUserID)
	if err != nil {
		return nil, false, err
	}
	requiresWeatherWorkers := requiresGeocode || mallPatchEnablesWeather(request)
	if err := service.requireWeatherFeature(requiresWeatherWorkers); err != nil {
		return nil, false, err
	}
	return updates, requiresGeocode, nil
}

func mallPatchEnablesWeather(request requestbody.MallPatchRequest) bool {
	return request.Weather != nil && request.Weather.Enabled != nil && *request.Weather.Enabled
}

func (service *MallService) requireWeatherFeature(required bool) error {
	if !required {
		return nil
	}
	if service == nil || service.weatherFeatureEnabled == nil || !service.weatherFeatureEnabled() {
		return ErrMallWeatherDisabled
	}
	return nil
}

func (service *MallService) Delete(ctx context.Context, actorUserID, mallID uint, expectedVersion uint64) error {
	if err := service.authorize(ctx, actorUserID, PermissionMallWrite); err != nil {
		return err
	}
	if err := service.requireMallScope(ctx, actorUserID, mallID); err != nil {
		return err
	}
	if mallID == 0 || expectedVersion == 0 {
		return fmt.Errorf("%w: mall id and expected version are required", ErrMallInvalidInput)
	}
	if _, err := service.malls.FindByID(ctx, mallID); err != nil {
		return err
	}
	return service.malls.DeleteWithVersion(ctx, mallID, expectedVersion)
}

func (service *MallService) requireMallScope(ctx context.Context, actorUserID, mallID uint) error {
	if service.mallScope == nil {
		return nil // injected unit services retain their existing isolated tests
	}
	if err := service.mallScope.Require(ctx, actorUserID, mallID); err != nil {
		if errors.Is(err, auth_svc.ErrMallScopeForbidden) {
			return ErrMallForbidden
		}
		return fmt.Errorf("mall service: check mall scope: %w", err)
	}
	return nil
}

func (service *MallService) authorize(ctx context.Context, actorUserID uint, permission string) error {
	if actorUserID == 0 {
		return ErrMallForbidden
	}
	allowed, err := service.permissions.HasPermission(ctx, actorUserID, permission, service.now().UTC())
	if err != nil {
		return fmt.Errorf("mall service: check permission: %w", err)
	}
	if !allowed {
		return ErrMallForbidden
	}
	return nil
}

func normalizeMallCreateRequest(request requestbody.MallCreateRequest) (requestbody.MallCreateRequest, error) {
	request.MallCode = strings.ToUpper(strings.TrimSpace(request.MallCode))
	request.NameCN = strings.TrimSpace(request.NameCN)
	request.NameEN = strings.TrimSpace(request.NameEN)
	request.Province = strings.TrimSpace(request.Province)
	request.City = strings.TrimSpace(request.City)
	request.District = strings.TrimSpace(request.District)
	request.Address = strings.TrimSpace(request.Address)
	if !mallCodePattern.MatchString(request.MallCode) {
		return request, fmt.Errorf("%w: invalid mallCode", ErrMallInvalidInput)
	}
	if !validText(request.NameCN, 1, 255) || !validText(request.Province, 1, 128) || !validText(request.City, 1, 128) || !validText(request.Address, 1, 1000) {
		return request, fmt.Errorf("%w: invalid required mall fields", ErrMallInvalidInput)
	}
	if !validOptionalText(request.NameEN, 255) || !validOptionalText(request.District, 128) {
		return request, fmt.Errorf("%w: mall field exceeds length limit", ErrMallInvalidInput)
	}
	if request.GrossFloorAreaSQM != nil && *request.GrossFloorAreaSQM <= 0 {
		return request, fmt.Errorf("%w: grossFloorAreaSqm must be positive", ErrMallInvalidInput)
	}
	if request.ParkingSpaces != nil && *request.ParkingSpaces < 0 {
		return request, fmt.Errorf("%w: parkingSpaces must not be negative", ErrMallInvalidInput)
	}
	tags, err := normalizeTags(request.Tags)
	if err != nil {
		return request, err
	}
	request.Tags = tags
	businessHours, err := normalizeBusinessHours(request.BusinessHours)
	if err != nil {
		return request, err
	}
	request.BusinessHours = businessHours
	if request.Weather.DetailProfile != nil {
		if !oneOf(strings.ToLower(strings.TrimSpace(*request.Weather.DetailProfile)), "full", "standard", "economy") {
			return request, fmt.Errorf("%w: invalid weather detailProfile", ErrMallInvalidInput)
		}
		detailProfile := strings.ToLower(strings.TrimSpace(*request.Weather.DetailProfile))
		request.Weather.DetailProfile = &detailProfile
	}
	if request.Weather.CoverageRadiusM != nil && (*request.Weather.CoverageRadiusM < 100 || *request.Weather.CoverageRadiusM > 10000) {
		return request, fmt.Errorf("%w: invalid weather coverageRadiusM", ErrMallInvalidInput)
	}
	return request, nil
}

func applyMallWeatherDefaults(request *requestbody.MallCreateRequest, defaultDetailProfile string, defaultCoverageRadius int) error {
	if request.Weather.DetailProfile == nil {
		value := defaultDetailProfile
		request.Weather.DetailProfile = &value
	}
	if request.Weather.CoverageRadiusM == nil {
		value := defaultCoverageRadius
		request.Weather.CoverageRadiusM = &value
	}
	if !oneOf(*request.Weather.DetailProfile, "full", "standard", "economy") || *request.Weather.CoverageRadiusM < 100 || *request.Weather.CoverageRadiusM > 10000 {
		return fmt.Errorf("%w: invalid mall weather defaults", ErrMallInvalidInput)
	}
	return nil
}

func mallFromCreateRequest(request requestbody.MallCreateRequest, actorUserID uint, now time.Time) (*model.Mall, error) {
	tagsJSON, err := json.Marshal(request.Tags)
	if err != nil {
		return nil, fmt.Errorf("mall service: encode tags: %w", err)
	}
	businessHoursJSON, err := json.Marshal(request.BusinessHours)
	if err != nil {
		return nil, fmt.Errorf("mall service: encode business hours: %w", err)
	}
	return &model.Mall{
		MallCode:           request.MallCode,
		NameCN:             request.NameCN,
		NameEN:             request.NameEN,
		Country:            "中国",
		Province:           request.Province,
		City:               request.City,
		District:           request.District,
		AddressRaw:         request.Address,
		BusinessHoursJSON:  model.JSONText(businessHoursJSON),
		GrossFloorAreaSQM:  request.GrossFloorAreaSQM,
		ParkingSpaces:      request.ParkingSpaces,
		TagsJSON:           model.JSONText(tagsJSON),
		GeocodeStatus:      "pending",
		Timezone:           "Asia/Shanghai",
		WeatherEnabled:     false,
		WeatherProvider:    "caiyun",
		CoverageRadiusM:    *request.Weather.CoverageRadiusM,
		SamplingMode:       "center",
		DetailProfile:      *request.Weather.DetailProfile,
		FastRefreshMinutes: 10,
		Status:             "draft",
		CreatedBy:          actorUserID,
		UpdatedBy:          actorUserID,
		Version:            1,
		CreatedAt:          now.UTC(),
		UpdatedAt:          now.UTC(),
	}, nil
}

func buildMallPatch(current *model.Mall, request requestbody.MallPatchRequest, actorUserID uint) (map[string]interface{}, bool, error) {
	updates := map[string]interface{}{"updated_by": actorUserID}
	requiresGeocode := false
	setText := func(field string, input *string, maximum int, required bool) error {
		if input == nil {
			return nil
		}
		value := strings.TrimSpace(*input)
		if required && value == "" || utf8.RuneCountInString(value) > maximum {
			return fmt.Errorf("%w: invalid %s", ErrMallInvalidInput, field)
		}
		updates[field] = value
		return nil
	}
	if err := setText("name_cn", request.NameCN, 255, true); err != nil {
		return nil, false, err
	}
	if err := setText("name_en", request.NameEN, 255, false); err != nil {
		return nil, false, err
	}
	if err := setText("province", request.Province, 128, true); err != nil {
		return nil, false, err
	}
	if err := setText("city", request.City, 128, true); err != nil {
		return nil, false, err
	}
	if err := setText("district", request.District, 128, false); err != nil {
		return nil, false, err
	}
	if err := setText("address_raw", request.Address, 1000, true); err != nil {
		return nil, false, err
	}
	requiresGeocode = request.NameCN != nil || request.Province != nil || request.City != nil || request.District != nil || request.Address != nil
	if request.GrossFloorAreaSQM != nil {
		if *request.GrossFloorAreaSQM <= 0 {
			return nil, false, fmt.Errorf("%w: grossFloorAreaSqm must be positive", ErrMallInvalidInput)
		}
		updates["gross_floor_area_sqm"] = *request.GrossFloorAreaSQM
	}
	if request.ParkingSpaces != nil {
		if *request.ParkingSpaces < 0 {
			return nil, false, fmt.Errorf("%w: parkingSpaces must not be negative", ErrMallInvalidInput)
		}
		updates["parking_spaces"] = *request.ParkingSpaces
	}
	if request.Tags != nil {
		tags, err := normalizeTags(*request.Tags)
		if err != nil {
			return nil, false, err
		}
		data, err := json.Marshal(tags)
		if err != nil {
			return nil, false, fmt.Errorf("mall service: encode tags: %w", err)
		}
		updates["tags_json"] = model.JSONText(data)
	}
	if request.BusinessHours != nil {
		businessHours, err := normalizeBusinessHours(*request.BusinessHours)
		if err != nil {
			return nil, false, err
		}
		data, err := json.Marshal(businessHours)
		if err != nil {
			return nil, false, fmt.Errorf("mall service: encode business hours: %w", err)
		}
		updates["business_hours_json"] = model.JSONText(data)
	}
	if request.Weather != nil {
		if request.Weather.DetailProfile != nil {
			profile := strings.ToLower(strings.TrimSpace(*request.Weather.DetailProfile))
			if !oneOf(profile, "full", "standard", "economy") {
				return nil, false, fmt.Errorf("%w: invalid detailProfile", ErrMallInvalidInput)
			}
			updates["detail_profile"] = profile
		}
		if request.Weather.CoverageRadiusM != nil {
			radius := *request.Weather.CoverageRadiusM
			if radius < 100 || radius > 10000 {
				return nil, false, fmt.Errorf("%w: invalid coverageRadiusM", ErrMallInvalidInput)
			}
			updates["coverage_radius_m"] = radius
		}
		if request.Weather.Enabled != nil {
			if *request.Weather.Enabled && current.GeocodeStatus != "confirmed" {
				return nil, false, fmt.Errorf("%w: weather cannot be enabled before coordinate confirmation", ErrMallInvalidInput)
			}
			updates["weather_enabled"] = *request.Weather.Enabled
		}
	}
	if requiresGeocode {
		updates["longitude"] = nil
		updates["latitude"] = nil
		updates["coordinate_system"] = ""
		updates["weather_longitude"] = nil
		updates["weather_latitude"] = nil
		updates["weather_coordinate_system"] = ""
		updates["geocode_status"] = "pending"
		updates["geocoded_at"] = nil
		updates["geocode_confirmed_by"] = 0
		updates["weather_enabled"] = false
		updates["status"] = "draft"
	}
	if len(updates) == 1 {
		return nil, false, fmt.Errorf("%w: patch has no changes", ErrMallInvalidInput)
	}
	return updates, requiresGeocode, nil
}

func newMallGeocodeOutbox(mall *model.Mall, now time.Time) (*model.AsyncJobOutbox, error) {
	addressHash := mallAddressHash(mall)
	payload, err := json.Marshal(job.MallGeocodeTaskPayload{
		MallID:      mall.ID,
		MallVersion: mall.Version,
		AddressHash: addressHash,
	})
	if err != nil {
		return nil, fmt.Errorf("mall service: encode geocode payload: %w", err)
	}
	return &model.AsyncJobOutbox{
		TaskKey:     fmt.Sprintf("mall:geocode:%d:v%d:%s", mall.ID, mall.Version, addressHash),
		TaskType:    job.TypeMallGeocode,
		PayloadJSON: model.JSONText(payload),
		QueueName:   job.MallWeatherQueueName,
		AvailableAt: now.UTC(),
	}, nil
}

func mallAddressHash(mall *model.Mall) string {
	canonical := strings.Join([]string{mall.Province, mall.City, mall.District, mall.AddressRaw, mall.NameCN}, "\x1f")
	return sha256Hex([]byte(canonical))
}

func mallCreateResult(mall *model.Mall) MallCreateResult {
	return MallCreateResult{
		ID:            mall.ID,
		MallCode:      mall.MallCode,
		Status:        strings.ToUpper(mall.Status),
		GeocodeStatus: strings.ToUpper(mall.GeocodeStatus),
		WeatherStatus: "WAITING_FOR_COORDINATE",
		Version:       mall.Version,
		CreatedAt:     mall.CreatedAt,
	}
}

func mallDTO(mall *model.Mall) (MallDTO, error) {
	var businessHours map[string][]requestbody.MallBusinessHour
	if strings.TrimSpace(string(mall.BusinessHoursJSON)) != "" {
		if err := json.Unmarshal([]byte(mall.BusinessHoursJSON), &businessHours); err != nil {
			return MallDTO{}, fmt.Errorf("mall service: decode business hours: %w", err)
		}
	}
	var tags []string
	if strings.TrimSpace(string(mall.TagsJSON)) != "" {
		if err := json.Unmarshal([]byte(mall.TagsJSON), &tags); err != nil {
			return MallDTO{}, fmt.Errorf("mall service: decode tags: %w", err)
		}
	}
	return MallDTO{
		ID: mall.ID, MallCode: mall.MallCode, NameCN: mall.NameCN, NameEN: mall.NameEN,
		Province: mall.Province, City: mall.City, District: mall.District, Address: mall.AddressRaw,
		Longitude: mall.Longitude, Latitude: mall.Latitude, CoordinateSystem: mall.CoordinateSystem,
		GeocodeStatus: strings.ToUpper(mall.GeocodeStatus), BusinessHours: businessHours,
		GrossFloorAreaSQM: mall.GrossFloorAreaSQM, ParkingSpaces: mall.ParkingSpaces, Tags: tags,
		WeatherEnabled: mall.WeatherEnabled, WeatherProvider: mall.WeatherProvider,
		DetailProfile: mall.DetailProfile, CoverageRadiusM: mall.CoverageRadiusM,
		TimeZone: mall.Timezone,
		Status:   strings.ToUpper(mall.Status), Version: mall.Version, CreatedAt: mall.CreatedAt, UpdatedAt: mall.UpdatedAt,
	}, nil
}

func applyMallUpdatesForOutbox(mall *model.Mall, updates map[string]interface{}) {
	if value, ok := updates["name_cn"].(string); ok {
		mall.NameCN = value
	}
	if value, ok := updates["province"].(string); ok {
		mall.Province = value
	}
	if value, ok := updates["city"].(string); ok {
		mall.City = value
	}
	if value, ok := updates["district"].(string); ok {
		mall.District = value
	}
	if value, ok := updates["address_raw"].(string); ok {
		mall.AddressRaw = value
	}
}

func validateBusinessHours(hours map[string][]requestbody.MallBusinessHour) error {
	validDays := map[string]struct{}{"monday": {}, "tuesday": {}, "wednesday": {}, "thursday": {}, "friday": {}, "saturday": {}, "sunday": {}}
	for day, ranges := range hours {
		if _, ok := validDays[strings.ToLower(day)]; !ok {
			return fmt.Errorf("%w: invalid businessHours day", ErrMallInvalidInput)
		}
		if len(ranges) > 4 {
			return fmt.Errorf("%w: too many businessHours ranges", ErrMallInvalidInput)
		}
		for _, interval := range ranges {
			if !validClock(interval.Open) || !validClock(interval.Close) || interval.Open == interval.Close {
				return fmt.Errorf("%w: invalid businessHours interval", ErrMallInvalidInput)
			}
		}
	}
	return nil
}

func normalizeBusinessHours(hours map[string][]requestbody.MallBusinessHour) (map[string][]requestbody.MallBusinessHour, error) {
	if hours == nil {
		return nil, nil
	}
	normalized := make(map[string][]requestbody.MallBusinessHour, len(hours))
	for day, ranges := range hours {
		day = strings.ToLower(strings.TrimSpace(day))
		if _, duplicated := normalized[day]; duplicated {
			return nil, fmt.Errorf("%w: duplicate businessHours day", ErrMallInvalidInput)
		}
		normalized[day] = append([]requestbody.MallBusinessHour(nil), ranges...)
	}
	if err := validateBusinessHours(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func validClock(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	hour, errHour := time.Parse("15:04", value)
	return errHour == nil && hour.Format("15:04") == value
}

func normalizeTags(tags []string) ([]string, error) {
	if len(tags) > 50 {
		return nil, fmt.Errorf("%w: too many tags", ErrMallInvalidInput)
	}
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || utf8.RuneCountInString(tag) > 64 {
			return nil, fmt.Errorf("%w: invalid tag", ErrMallInvalidInput)
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	sort.Strings(result)
	return result, nil
}

func validIdempotencyKey(value string) bool {
	return len(value) <= maxIdempotencyKeyLength && idempotencyKeyPattern.MatchString(value)
}

func hashJSON(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func isDuplicateKeyError(err error) bool {
	var mysqlError *mysqlDriver.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}

func validText(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}

func validOptionalText(value string, maximum int) bool {
	return utf8.RuneCountInString(value) <= maximum
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
