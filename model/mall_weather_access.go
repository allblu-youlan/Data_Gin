package model

import "time"

const (
	PermissionMallRead            = "mall.read"
	PermissionMallWrite           = "mall.write"
	PermissionMallGeocodeConfirm  = "mall.geocode.confirm"
	PermissionWeatherRead         = "weather.read"
	PermissionWeatherRefresh      = "weather.refresh"
	PermissionWeatherExport       = "weather.export"
	PermissionWeatherFeishuPush   = "weather.feishu.push"
	PermissionWeatherConfigManage = "weather.config.manage"
	PermissionWeatherRawRead      = "weather.raw.read"
	PermissionBojunOrderRead      = "bojun.order.read"
)

var mallWeatherAdminPermissions = [...]string{
	PermissionMallRead,
	PermissionMallWrite,
	PermissionMallGeocodeConfirm,
	PermissionWeatherRead,
	PermissionWeatherRefresh,
	PermissionWeatherExport,
	PermissionWeatherFeishuPush,
	PermissionWeatherConfigManage,
	PermissionWeatherRawRead,
	PermissionBojunOrderRead,
}

var grantableDataPermissions = [...]string{
	PermissionWeatherRead,
	PermissionBojunOrderRead,
	PermissionBusinessOverviewRead,
}

// GrantableDataPermissions returns the fixed permissions an administrator may
// delegate to an open API account.
func GrantableDataPermissions() []string {
	permissions := make([]string, len(grantableDataPermissions))
	copy(permissions, grantableDataPermissions[:])
	return permissions
}

// MallWeatherAdminPermissions returns the complete permanent permission set
// assigned to the console admin. The returned slice is safe for callers to
// modify.
func MallWeatherAdminPermissions() []string {
	permissions := make([]string, len(mallWeatherAdminPermissions))
	copy(permissions, mallWeatherAdminPermissions[:])
	return permissions
}

// MallWeatherUserPermission is an explicit, fail-closed grant. The module does
// not infer administrative access from usernames or user IDs.
type MallWeatherUserPermission struct {
	BaseModel
	UserID     uint       `gorm:"column:user_id;not null;uniqueIndex:uk_mall_weather_permission,priority:1;index" json:"user_id"`
	Permission string     `gorm:"column:permission;size:64;not null;uniqueIndex:uk_mall_weather_permission,priority:2;index" json:"permission"`
	GrantedBy  uint       `gorm:"column:granted_by;not null;default:0" json:"granted_by"`
	ExpiresAt  *time.Time `gorm:"column:expires_at;type:datetime(3);index" json:"expires_at"`
	WeatherTimestamps
}

func (MallWeatherUserPermission) TableName() string { return "mall_weather_user_permissions" }

// APIIdempotencyRecord stores a one-way hash of the caller's key and the
// non-sensitive response snapshot needed for exact replay.
type APIIdempotencyRecord struct {
	BaseModel
	OperationScope string   `gorm:"column:operation_scope;size:64;not null;uniqueIndex:uk_api_idempotency,priority:1" json:"operation_scope"`
	ActorUserID    uint     `gorm:"column:actor_user_id;not null;uniqueIndex:uk_api_idempotency,priority:2;index" json:"actor_user_id"`
	KeyHash        string   `gorm:"column:key_hash;type:char(64);not null;uniqueIndex:uk_api_idempotency,priority:3" json:"-"`
	RequestHash    string   `gorm:"column:request_hash;type:char(64);not null" json:"-"`
	ResourceType   string   `gorm:"column:resource_type;size:32;not null" json:"resource_type"`
	ResourceID     uint     `gorm:"column:resource_id;not null;default:0;index" json:"resource_id"`
	HTTPStatus     int      `gorm:"column:http_status;not null;default:0" json:"http_status"`
	ResponseJSON   JSONText `gorm:"column:response_json;type:json;not null" json:"-"`
	WeatherTimestamps
}

func (APIIdempotencyRecord) TableName() string { return "api_idempotency_records" }
