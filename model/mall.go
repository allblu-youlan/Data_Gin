package model

import (
	"time"

	"gorm.io/gorm"
)

// Mall stores the managed business profile and the confirmed representative
// point used for weather requests.
type Mall struct {
	BaseModel

	KaiyuShopID       string     `gorm:"column:kaiyu_shop_id;size:64;not null;default:'';index" json:"kaiyu_shop_id"`
	MallCode          string     `gorm:"column:mall_code;size:64;not null;uniqueIndex:uk_malls_code" json:"mall_code"`
	NameCN            string     `gorm:"column:name_cn;size:255;not null" json:"name_cn"`
	NameEN            string     `gorm:"column:name_en;size:255" json:"name_en"`
	AliasesJSON       JSONText   `gorm:"column:aliases_json;type:json" json:"aliases_json"`
	BrandName         string     `gorm:"column:brand_name;size:255" json:"brand_name"`
	GroupName         string     `gorm:"column:group_name;size:255" json:"group_name"`
	ManagementCompany string     `gorm:"column:management_company;size:255" json:"management_company"`
	BusinessStatus    string     `gorm:"column:business_status;size:32;index" json:"business_status"`
	OpeningDate       *time.Time `gorm:"column:opening_date;type:date" json:"opening_date"`
	RenovationDate    *time.Time `gorm:"column:renovation_date;type:date" json:"renovation_date"`
	MallType          string     `gorm:"column:mall_type;size:64" json:"mall_type"`
	Positioning       string     `gorm:"column:positioning;size:255" json:"positioning"`
	TagsJSON          JSONText   `gorm:"column:tags_json;type:json" json:"tags_json"`

	Country                 string     `gorm:"column:country;size:128" json:"country"`
	Province                string     `gorm:"column:province;size:128" json:"province"`
	City                    string     `gorm:"column:city;size:128;index:idx_malls_city_status,priority:1" json:"city"`
	District                string     `gorm:"column:district;size:128" json:"district"`
	Township                string     `gorm:"column:township;size:128" json:"township"`
	Street                  string     `gorm:"column:street;size:255" json:"street"`
	StreetNumber            string     `gorm:"column:street_number;size:64" json:"street_number"`
	PostalCode              string     `gorm:"column:postal_code;size:32" json:"postal_code"`
	AddressRaw              string     `gorm:"column:address_raw;size:1000;not null" json:"address_raw"`
	AddressStandardized     string     `gorm:"column:address_standardized;size:1000" json:"address_standardized"`
	Adcode                  string     `gorm:"column:adcode;size:32;index" json:"adcode"`
	Citycode                string     `gorm:"column:citycode;size:32" json:"citycode"`
	Longitude               *float64   `gorm:"column:longitude;type:decimal(10,7);index:idx_malls_coordinate,priority:1" json:"longitude"`
	Latitude                *float64   `gorm:"column:latitude;type:decimal(10,7);index:idx_malls_coordinate,priority:2" json:"latitude"`
	CoordinateSystem        string     `gorm:"column:coordinate_system;size:16" json:"coordinate_system"`
	WeatherLongitude        *float64   `gorm:"column:weather_longitude;type:decimal(10,7)" json:"weather_longitude"`
	WeatherLatitude         *float64   `gorm:"column:weather_latitude;type:decimal(10,7)" json:"weather_latitude"`
	WeatherCoordinateSystem string     `gorm:"column:weather_coordinate_system;size:16" json:"weather_coordinate_system"`
	GeocodeLevel            string     `gorm:"column:geocode_level;size:64" json:"geocode_level"`
	GeocodeConfidence       *float64   `gorm:"column:geocode_confidence;type:decimal(5,2)" json:"geocode_confidence"`
	GeocodeStatus           string     `gorm:"column:geocode_status;size:32;not null;default:'pending';index:idx_malls_geocode_status,priority:1" json:"geocode_status"`
	GeocodedAt              *time.Time `gorm:"column:geocoded_at;type:datetime(3)" json:"geocoded_at"`
	GeocodeConfirmedBy      uint       `gorm:"column:geocode_confirmed_by;default:0" json:"geocode_confirmed_by"`
	Timezone                string     `gorm:"column:timezone;size:64;not null;default:'Asia/Shanghai'" json:"timezone"`

	GrossFloorAreaSQM *float64 `gorm:"column:gross_floor_area_sqm;type:decimal(14,2)" json:"gross_floor_area_sqm"`
	RetailAreaSQM     *float64 `gorm:"column:retail_area_sqm;type:decimal(14,2)" json:"retail_area_sqm"`
	FloorCountAbove   *int     `gorm:"column:floor_count_above" json:"floor_count_above"`
	FloorCountBelow   *int     `gorm:"column:floor_count_below" json:"floor_count_below"`
	StoreCount        *int     `gorm:"column:store_count" json:"store_count"`
	AnchorStoreCount  *int     `gorm:"column:anchor_store_count" json:"anchor_store_count"`
	ParkingSpaces     *int     `gorm:"column:parking_spaces" json:"parking_spaces"`
	EVChargingSpaces  *int     `gorm:"column:ev_charging_spaces" json:"ev_charging_spaces"`
	BusinessHoursJSON JSONText `gorm:"column:business_hours_json;type:json" json:"business_hours_json"`
	ServicePhone      string   `gorm:"column:service_phone;size:64" json:"service_phone"`
	WebsiteURL        string   `gorm:"column:website_url;size:1000" json:"website_url"`
	MetroLinesJSON    JSONText `gorm:"column:metro_lines_json;type:json" json:"metro_lines_json"`
	MetroStationsJSON JSONText `gorm:"column:metro_stations_json;type:json" json:"metro_stations_json"`
	BusStopsJSON      JSONText `gorm:"column:bus_stops_json;type:json" json:"bus_stops_json"`
	IndoorOutdoorType string   `gorm:"column:indoor_outdoor_type;size:32" json:"indoor_outdoor_type"`

	ContactName        string   `gorm:"column:contact_name;size:128" json:"-"`
	ContactPhone       string   `gorm:"column:contact_phone;size:64" json:"-"`
	ContactEmail       string   `gorm:"column:contact_email;size:255" json:"-"`
	OperatorDepartment string   `gorm:"column:operator_department;size:255" json:"operator_department"`
	DataOwnerUserID    uint     `gorm:"column:data_owner_user_id;default:0;index" json:"data_owner_user_id"`
	SourceType         string   `gorm:"column:source_type;size:64" json:"source_type"`
	SourceReference    string   `gorm:"column:source_reference;size:1000" json:"source_reference"`
	Remark             string   `gorm:"column:remark;type:text" json:"remark"`
	CustomFieldsJSON   JSONText `gorm:"column:custom_fields_json;type:json" json:"custom_fields_json"`

	WeatherEnabled       bool       `gorm:"column:weather_enabled;not null;default:false;index:idx_malls_weather_scan,priority:2" json:"weather_enabled"`
	WeatherProvider      string     `gorm:"column:weather_provider;size:16;not null;default:'caiyun'" json:"weather_provider"`
	CoverageRadiusM      int        `gorm:"column:coverage_radius_m;not null;default:1000" json:"coverage_radius_m"`
	SamplingMode         string     `gorm:"column:sampling_mode;size:16;not null;default:'center'" json:"sampling_mode"`
	DetailProfile        string     `gorm:"column:detail_profile;size:16;not null;default:'full'" json:"detail_profile"`
	FastRefreshMinutes   int        `gorm:"column:fast_refresh_minutes;not null;default:10" json:"fast_refresh_minutes"`
	RetentionPolicyCode  string     `gorm:"column:retention_policy_code;size:64" json:"retention_policy_code"`
	LastWeatherSuccessAt *time.Time `gorm:"column:last_weather_success_at;type:datetime(3)" json:"last_weather_success_at"`
	LastWeatherErrorAt   *time.Time `gorm:"column:last_weather_error_at;type:datetime(3)" json:"last_weather_error_at"`

	Status    string         `gorm:"column:status;size:16;not null;default:'draft';index:idx_malls_weather_scan,priority:1;index:idx_malls_city_status,priority:2" json:"status"`
	CreatedBy uint           `gorm:"column:created_by;default:0" json:"created_by"`
	UpdatedBy uint           `gorm:"column:updated_by;default:0" json:"updated_by"`
	Version   uint64         `gorm:"column:version;not null;default:1" json:"version"`
	CreatedAt time.Time      `gorm:"column:created_at;type:datetime(3);not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;type:datetime(3);index" json:"-"`
}

func (Mall) TableName() string { return "malls" }
