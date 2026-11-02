package data_dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrMallNotFound        = errors.New("mall: not found")
	ErrMallVersionConflict = errors.New("mall: version conflict")
)

const maxMallPageSize = 200

type MallListFilter struct {
	ActorUserID    uint
	AfterID        uint
	Limit          int
	City           string
	Status         string
	GeocodeStatus  string
	WeatherEnabled *bool
}

type MallDAO struct {
	db *gorm.DB
}

func NewMallDAO(databases ...*gorm.DB) *MallDAO {
	db := database.DB
	if len(databases) > 0 && databases[0] != nil {
		db = databases[0]
	}
	return &MallDAO{db: db}
}

func (dao *MallDAO) WithDB(db *gorm.DB) *MallDAO {
	return &MallDAO{db: db}
}

func (dao *MallDAO) Create(ctx context.Context, mall *model.Mall) error {
	if mall == nil {
		return fmt.Errorf("mall: create nil model")
	}
	return dao.db.WithContext(ctx).Create(mall).Error
}

func (dao *MallDAO) FindByID(ctx context.Context, id uint) (*model.Mall, error) {
	return dao.findByID(ctx, id, false)
}

func (dao *MallDAO) FindByIDForUpdate(ctx context.Context, id uint) (*model.Mall, error) {
	return dao.findByID(ctx, id, true)
}

func (dao *MallDAO) findByID(ctx context.Context, id uint, forUpdate bool) (*model.Mall, error) {
	var mall model.Mall
	query := dao.db.WithContext(ctx)
	if forUpdate {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&mall, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMallNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mall: find by id: %w", err)
	}
	return &mall, nil
}

func (dao *MallDAO) FindByCode(ctx context.Context, code string) (*model.Mall, error) {
	var mall model.Mall
	err := dao.db.WithContext(ctx).Where("mall_code = ?", code).First(&mall).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMallNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mall: find by code: %w", err)
	}
	return &mall, nil
}

func (dao *MallDAO) List(ctx context.Context, filter MallListFilter) ([]model.Mall, error) {
	query := dao.db.WithContext(ctx).Model(&model.Mall{})
	if filter.ActorUserID > 0 {
		query = applyMallScopeQuery(query, dao.db.WithContext(ctx), filter.ActorUserID, "malls.id")
	}
	if filter.AfterID > 0 {
		query = query.Where("id > ?", filter.AfterID)
	}
	if filter.City != "" {
		query = query.Where("city = ?", filter.City)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.GeocodeStatus != "" {
		query = query.Where("geocode_status = ?", filter.GeocodeStatus)
	}
	if filter.WeatherEnabled != nil {
		query = query.Where("weather_enabled = ?", *filter.WeatherEnabled)
	}

	var malls []model.Mall
	if err := query.Order("id ASC").Limit(normalizePageSize(filter.Limit)).Find(&malls).Error; err != nil {
		return nil, fmt.Errorf("mall: list: %w", err)
	}
	return malls, nil
}

func (dao *MallDAO) ListScopedIdentitiesAfterID(ctx context.Context, actorUserID, afterID uint, limit int) ([]model.Mall, error) {
	var malls []model.Mall
	query := applyMallScopeQuery(
		dao.db.WithContext(ctx).Model(&model.Mall{}).Select("id", "mall_code", "name_cn"),
		dao.db.WithContext(ctx),
		actorUserID,
		"malls.id",
	)
	if afterID > 0 {
		query = query.Where("id > ?", afterID)
	}
	if err := query.Order("id ASC").Limit(normalizePageSize(limit)).Find(&malls).Error; err != nil {
		return nil, fmt.Errorf("mall: list scoped identities: %w", err)
	}
	return malls, nil
}

func (dao *MallDAO) ListEnabledWeatherAfterID(ctx context.Context, afterID uint, limit int) ([]model.Mall, error) {
	confirmed := "confirmed"
	enabled := true
	return dao.List(ctx, MallListFilter{
		AfterID:        afterID,
		Limit:          limit,
		Status:         "active",
		GeocodeStatus:  confirmed,
		WeatherEnabled: &enabled,
	})
}

// ListOpenWeatherMallsAfterID returns only the public identity fields for malls
// whose stored weather coordinates are immediately usable by open queries.
func (dao *MallDAO) ListOpenWeatherMallsAfterID(ctx context.Context, actorUserID, afterID uint, limit int) ([]model.Mall, error) {
	var malls []model.Mall
	query, err := dao.openWeatherMallsQuery(ctx)
	if err != nil {
		return nil, err
	}
	query = query.
		Model(&model.Mall{}).
		Select([]string{
			"id", "mall_code", "name_cn", "name_en", "province", "city", "district", "township",
			"address_raw", "address_standardized", "weather_longitude", "weather_latitude",
			"weather_coordinate_system", "timezone",
		})
	query = applyMallScopeQuery(query, dao.db.WithContext(ctx), actorUserID, "malls.id")
	if afterID > 0 {
		query = query.Where("id > ?", afterID)
	}
	if err := query.Order("id ASC").Limit(normalizePageSize(limit)).Find(&malls).Error; err != nil {
		return nil, fmt.Errorf("mall: list open weather malls: %w", err)
	}
	return malls, nil
}

func (dao *MallDAO) CountOpenWeatherMalls(ctx context.Context, actorUserID uint) (int64, error) {
	query, err := dao.openWeatherMallsQuery(ctx)
	if err != nil {
		return 0, err
	}
	query = applyMallScopeQuery(query.Model(&model.Mall{}), dao.db.WithContext(ctx), actorUserID, "malls.id")
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, fmt.Errorf("mall: count open weather malls: %w", err)
	}
	return total, nil
}

func applyMallScopeQuery(query, db *gorm.DB, actorUserID uint, mallIDColumn string) *gorm.DB {
	if actorUserID == 0 {
		return query.Where("1 = 0")
	}
	return query.Where(
		"EXISTS (?)",
		db.Table("users AS scope_user").Select("1").
			Where("scope_user.id = ? AND scope_user.status = ?", actorUserID, model.AccountStatusActive).
			Where("scope_user.mall_scope_mode = ? OR (scope_user.mall_scope_mode = ? AND "+mallIDColumn+" IN (?))",
				model.MallScopeAll, model.MallScopeSelected,
				db.Model(&model.UserMallScope{}).Select("mall_id").Where("user_id = ?", actorUserID)),
	)
}

func (dao *MallDAO) openWeatherMallsQuery(ctx context.Context) (*gorm.DB, error) {
	if dao == nil || dao.db == nil || ctx == nil {
		return nil, fmt.Errorf("mall: invalid open weather query")
	}
	return dao.db.WithContext(ctx).
		Where("status = ?", "active").
		Where("geocode_status = ?", "confirmed").
		Where("weather_enabled = ?", true).
		Where("weather_longitude BETWEEN ? AND ?", -180, 180).
		Where("weather_latitude BETWEEN ? AND ?", -90, 90), nil
}

func (dao *MallDAO) UpdateWithVersion(ctx context.Context, id uint, expectedVersion uint64, updates map[string]interface{}) error {
	safeUpdates, err := sanitizeMallUpdates(updates)
	if err != nil {
		return err
	}
	safeUpdates["version"] = gorm.Expr("version + 1")
	result := dao.db.WithContext(ctx).
		Model(&model.Mall{}).
		Where("id = ? AND version = ?", id, expectedVersion).
		Updates(safeUpdates)
	if result.Error != nil {
		return fmt.Errorf("mall: update: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrMallVersionConflict
	}
	return nil
}

func (dao *MallDAO) DeleteWithVersion(ctx context.Context, id uint, expectedVersion uint64) error {
	result := dao.db.WithContext(ctx).
		Where("id = ? AND version = ?", id, expectedVersion).
		Delete(&model.Mall{})
	if result.Error != nil {
		return fmt.Errorf("mall: delete: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrMallVersionConflict
	}
	return nil
}

func (dao *MallDAO) AdvanceLastWeatherSuccessAt(ctx context.Context, id uint, observedAt time.Time) error {
	return dao.advanceWeatherObservedAt(ctx, id, observedAt, true)
}

func (dao *MallDAO) AdvanceLastWeatherErrorAt(ctx context.Context, id uint, observedAt time.Time) error {
	return dao.advanceWeatherObservedAt(ctx, id, observedAt, false)
}

func (dao *MallDAO) advanceWeatherObservedAt(ctx context.Context, id uint, observedAt time.Time, success bool) error {
	if dao == nil || dao.db == nil || ctx == nil || id == 0 || observedAt.IsZero() {
		return fmt.Errorf("mall: invalid weather observation")
	}
	observedAt = observedAt.UTC()
	column := "last_weather_error_at"
	if success {
		column = "last_weather_success_at"
	}
	expression, err := monotonicWeatherObservedAtExpr(column, observedAt)
	if err != nil {
		return err
	}
	result := dao.db.WithContext(ctx).Model(&model.Mall{}).
		Where("id = ?", id).
		Update(column, expression)
	if result.Error != nil {
		return fmt.Errorf("mall: advance weather observation: %w", result.Error)
	}
	return nil
}

func monotonicWeatherObservedAtExpr(column string, observedAt time.Time) (clause.Expr, error) {
	if observedAt.IsZero() || (column != "last_weather_success_at" && column != "last_weather_error_at") {
		return clause.Expr{}, fmt.Errorf("mall: invalid weather observation expression")
	}
	// Security: column is restricted to the two internal weather-health fields;
	// observedAt remains a bound parameter.
	return clause.Expr{
		SQL:  "CASE WHEN `" + column + "` IS NULL OR `" + column + "` < ? THEN ? ELSE `" + column + "` END",
		Vars: []interface{}{observedAt.UTC(), observedAt.UTC()},
	}, nil
}

func normalizePageSize(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxMallPageSize {
		return maxMallPageSize
	}
	return limit
}

func sanitizeMallUpdates(updates map[string]interface{}) (map[string]interface{}, error) {
	allowed := map[string]struct{}{
		"kaiyu_shop_id": {}, "name_cn": {}, "name_en": {}, "aliases_json": {}, "brand_name": {}, "group_name": {},
		"management_company": {}, "business_status": {}, "opening_date": {}, "renovation_date": {},
		"mall_type": {}, "positioning": {}, "tags_json": {}, "country": {}, "province": {},
		"city": {}, "district": {}, "township": {}, "street": {}, "street_number": {},
		"postal_code": {}, "address_raw": {}, "address_standardized": {}, "adcode": {}, "citycode": {},
		"longitude": {}, "latitude": {}, "coordinate_system": {}, "weather_longitude": {},
		"weather_latitude": {}, "weather_coordinate_system": {}, "geocode_level": {},
		"geocode_confidence": {}, "geocode_status": {}, "geocoded_at": {}, "geocode_confirmed_by": {},
		"timezone": {}, "gross_floor_area_sqm": {}, "retail_area_sqm": {}, "floor_count_above": {},
		"floor_count_below": {}, "store_count": {}, "anchor_store_count": {}, "parking_spaces": {},
		"ev_charging_spaces": {}, "business_hours_json": {}, "service_phone": {}, "website_url": {},
		"metro_lines_json": {}, "metro_stations_json": {}, "bus_stops_json": {}, "indoor_outdoor_type": {},
		"contact_name": {}, "contact_phone": {}, "contact_email": {}, "operator_department": {},
		"data_owner_user_id": {}, "source_type": {}, "source_reference": {}, "remark": {},
		"custom_fields_json": {}, "weather_enabled": {}, "weather_provider": {}, "coverage_radius_m": {},
		"sampling_mode": {}, "detail_profile": {}, "fast_refresh_minutes": {}, "retention_policy_code": {},
		"last_weather_success_at": {}, "last_weather_error_at": {}, "status": {}, "updated_by": {},
	}

	result := make(map[string]interface{}, len(updates)+1)
	for field, value := range updates {
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("mall: update field %q is not allowed", field)
		}
		result[field] = value
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("mall: no fields to update")
	}
	return result, nil
}
