package model

import (
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestMallWeatherModelSchemas(t *testing.T) {
	tests := []struct {
		name      string
		model     interface{}
		tableName string
	}{
		{"mall", &Mall{}, "malls"},
		{"geocode run", &MallGeocodeRun{}, "mall_geocode_runs"},
		{"geocode candidate", &MallGeocodeCandidate{}, "mall_geocode_candidates"},
		{"coordinate audit", &MallCoordinateAudit{}, "mall_coordinate_audits"},
		{"raw snapshot", &ProviderRawSnapshot{}, "provider_raw_snapshots"},
		{"fetch run", &MallWeatherFetchRun{}, "mall_weather_fetch_runs"},
		{"fetch attempt", &MallWeatherFetchAttempt{}, "mall_weather_fetch_attempts"},
		{"realtime", &MallWeatherRealtime{}, "mall_weather_realtime"},
		{"minutely", &MallWeatherMinutely{}, "mall_weather_minutely"},
		{"hourly", &MallWeatherHourly{}, "mall_weather_hourly"},
		{"daily", &MallWeatherDaily{}, "mall_weather_daily"},
		{"alert", &MallWeatherAlert{}, "mall_weather_alerts"},
		{"alert relation", &MallWeatherAlertRelation{}, "mall_weather_alert_relations"},
		{"life index", &MallWeatherLifeIndex{}, "mall_weather_life_indices"},
		{"latest", &MallWeatherLatest{}, "mall_weather_latest"},
		{"export profile", &MallWeatherExportProfile{}, "mall_weather_export_profiles"},
		{"export job", &MallWeatherExportJob{}, "mall_weather_export_jobs"},
		{"feishu run", &MallWeatherFeishuRun{}, "mall_weather_feishu_runs"},
		{"sheet row", &MallWeatherSheetRow{}, "mall_weather_sheet_rows"},
		{"outbox", &AsyncJobOutbox{}, "async_job_outbox"},
		{"user permission", &MallWeatherUserPermission{}, "mall_weather_user_permissions"},
		{"API idempotency", &APIIdempotencyRecord{}, "api_idempotency_records"},
		{"open API credential", &OpenAPICredential{}, "open_api_credentials"},
		{"data authorization audit", &DataAuthorizationAudit{}, "data_authorization_audits"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := schema.Parse(tt.model, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatalf("schema.Parse() error = %v", err)
			}
			if parsed.Table != tt.tableName {
				t.Fatalf("table = %q, want %q", parsed.Table, tt.tableName)
			}
			if parsed.LookUpField("ID") == nil {
				t.Fatal("model does not embed the project base id")
			}
		})
	}
}

func TestMallWeatherBusinessIndexes(t *testing.T) {
	tests := []struct {
		name       string
		model      interface{}
		indexName  string
		fieldNames []string
	}{
		{"mall code", &Mall{}, "uk_malls_code", []string{"mall_code"}},
		{"geocode candidate", &MallGeocodeCandidate{}, "uk_geocode_candidate", []string{"run_id", "candidate_no"}},
		{"fetch task", &MallWeatherFetchRun{}, "uk_fetch_task", []string{"mall_id", "endpoint_kind", "task_kind", "task_window"}},
		{"fetch attempt", &MallWeatherFetchAttempt{}, "uk_fetch_attempt", []string{"fetch_run_id", "attempt_no"}},
		{"realtime version", &MallWeatherRealtime{}, "uk_realtime_version", []string{"mall_id", "provider", "snapshot_at_utc"}},
		{"minutely version", &MallWeatherMinutely{}, "uk_minutely_version", []string{"mall_id", "provider", "forecast_minute_utc", "issued_at_utc"}},
		{"hourly version", &MallWeatherHourly{}, "uk_hourly_version", []string{"mall_id", "provider", "forecast_time_utc", "issued_at_utc"}},
		{"daily version", &MallWeatherDaily{}, "uk_daily_version", []string{"mall_id", "provider", "forecast_date_local", "issued_at_utc"}},
		{"alert", &MallWeatherAlert{}, "uk_weather_alert", []string{"provider", "alert_id"}},
		{"life index", &MallWeatherLifeIndex{}, "uk_life_version", []string{"mall_id", "provider", "source_api", "forecast_date_local", "index_type", "issued_at_utc"}},
		{"latest", &MallWeatherLatest{}, "uk_weather_latest", []string{"mall_id", "data_kind", "business_key"}},
		{"feishu pipeline run", &MallWeatherFeishuRun{}, "uk_weather_feishu_run", []string{"pipeline_run_id"}},
		{"sheet row", &MallWeatherSheetRow{}, "uk_weather_sheet_row", []string{"destination_id", "dataset_kind", "business_key"}},
		{"sheet row number", &MallWeatherSheetRow{}, "uk_weather_sheet_row_number", []string{"destination_id", "dataset_kind", "row_number"}},
		{"user permission", &MallWeatherUserPermission{}, "uk_mall_weather_permission", []string{"user_id", "permission"}},
		{"API idempotency", &APIIdempotencyRecord{}, "uk_api_idempotency", []string{"operation_scope", "actor_user_id", "key_hash"}},
		{"open API credential hash", &OpenAPICredential{}, "uk_open_api_credential_hash", []string{"token_hash"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := schema.Parse(tt.model, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatalf("schema.Parse() error = %v", err)
			}
			index, ok := parsed.ParseIndexes()[tt.indexName]
			if !ok {
				t.Fatalf("index %q not found", tt.indexName)
			}
			if index.Class != "UNIQUE" {
				t.Fatalf("index %q class = %q, want UNIQUE", tt.indexName, index.Class)
			}
			got := make([]string, 0, len(index.Fields))
			for _, field := range index.Fields {
				got = append(got, field.DBName)
			}
			if !reflect.DeepEqual(got, tt.fieldNames) {
				t.Fatalf("index %q fields = %v, want %v", tt.indexName, got, tt.fieldNames)
			}
		})
	}
}

func TestDataAuthorizationSecretsAreNotSerialized(t *testing.T) {
	data, err := json.Marshal(struct {
		Credential OpenAPICredential      `json:"credential"`
		Audit      DataAuthorizationAudit `json:"audit"`
	}{
		Credential: OpenAPICredential{TokenHash: strings.Repeat("a", 64)},
		Audit:      DataAuthorizationAudit{IdempotencyKeyHash: strings.Repeat("b", 64)},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, secret := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("serialized authorization model leaked secret hash")
		}
	}
}

func TestGrantableDataPermissionsReturnsDefensiveAllowlist(t *testing.T) {
	permissions := GrantableDataPermissions()
	if !reflect.DeepEqual(permissions, []string{PermissionWeatherRead, PermissionBojunOrderRead, PermissionBusinessOverviewRead}) {
		t.Fatalf("permissions = %v", permissions)
	}
	permissions[0] = "mall.write"
	if GrantableDataPermissions()[0] != PermissionWeatherRead {
		t.Fatal("GrantableDataPermissions() returned shared storage")
	}
}

func TestAPIIdempotencySecretsAreNotSerialized(t *testing.T) {
	data, err := json.Marshal(APIIdempotencyRecord{
		KeyHash:      "private-key-hash",
		RequestHash:  "private-request-hash",
		ResponseJSON: JSONText(`{"id":123}`),
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, value := range []string{"private-key-hash", "private-request-hash", `\"id\":123`} {
		if strings.Contains(string(data), value) {
			t.Fatalf("serialized idempotency record leaked %q", value)
		}
	}
}

func TestMallContactFieldsAreNotSerialized(t *testing.T) {
	data, err := json.Marshal(Mall{
		ContactName:  "private name",
		ContactPhone: "private phone",
		ContactEmail: "private email",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, value := range []string{"private name", "private phone", "private email"} {
		if string(data) != "" && containsJSONValue(data, value) {
			t.Fatalf("serialized mall leaked contact value %q", value)
		}
	}
}

func TestJSONText(t *testing.T) {
	tests := []struct {
		name      string
		value     JSONText
		wantValue driver.Value
		wantError bool
	}{
		{"unset becomes sql null", "", nil, false},
		{"object remains json", `{"status":"ok"}`, `{"status":"ok"}`, false},
		{"invalid json is rejected", `{`, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.Value()
			if (err != nil) != tt.wantError {
				t.Fatalf("Value() error = %v, wantError %t", err, tt.wantError)
			}
			if !reflect.DeepEqual(got, tt.wantValue) {
				t.Fatalf("Value() = %#v, want %#v", got, tt.wantValue)
			}
		})
	}
}

func containsJSONValue(data []byte, value string) bool {
	var object map[string]interface{}
	if err := json.Unmarshal(data, &object); err != nil {
		return true
	}
	for _, candidate := range object {
		if candidate == value {
			return true
		}
	}
	return false
}
