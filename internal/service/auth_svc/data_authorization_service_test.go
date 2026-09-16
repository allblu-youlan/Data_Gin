package auth_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/requests/auth_request"
	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestNormalizeDataAuthorizationGrantEnforcesAllowlistAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		permission string
		expiresAt  string
		permanent  bool
		wantExpiry bool
		wantError  bool
	}{
		{name: "weather", permission: model.PermissionWeatherRead, expiresAt: now.Add(30 * 24 * time.Hour).Format(time.RFC3339), wantExpiry: true},
		{name: "bojun", permission: model.PermissionBojunOrderRead, expiresAt: now.Add(time.Hour).Format(time.RFC3339), wantExpiry: true},
		{name: "permanent", permission: model.PermissionBojunOrderRead, permanent: true},
		{name: "permanent conflicts with expiry", permission: model.PermissionWeatherRead, expiresAt: now.Add(time.Hour).Format(time.RFC3339), permanent: true, wantError: true},
		{name: "non permanent requires expiry", permission: model.PermissionWeatherRead, wantError: true},
		{name: "write permission rejected", permission: model.PermissionMallWrite, expiresAt: now.Add(time.Hour).Format(time.RFC3339), wantError: true},
		{name: "too soon", permission: model.PermissionWeatherRead, expiresAt: now.Add(time.Minute).Format(time.RFC3339), wantError: true},
		{name: "too long", permission: model.PermissionWeatherRead, expiresAt: now.Add(366 * 24 * time.Hour).Format(time.RFC3339), wantError: true},
		{name: "invalid timestamp", permission: model.PermissionWeatherRead, expiresAt: "2026-07-28", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, expiry, _, err := normalizeDataAuthorizationGrant(tt.permission, tt.expiresAt, tt.permanent, "业务接入", now)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError %t", err, tt.wantError)
			}
			if err == nil && (expiry != nil) != tt.wantExpiry {
				t.Fatalf("expiry = %v, wantExpiry %t", expiry, tt.wantExpiry)
			}
		})
	}
}

func TestNormalizeDataAuthorizationCreateRejectsReservedAndDuplicatePermissions(t *testing.T) {
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	service := &DataAuthorizationService{now: func() time.Time { return now }}
	valid := auth_request.DataAuthorizationAccountCreateRequest{
		Account: "partner_weather_01", Nickname: "合作方账号", Reason: "天气数据接入",
		Permissions: []auth_request.DataAuthorizationPermissionInput{{Permission: model.PermissionWeatherRead, ExpiresAt: now.Add(30 * 24 * time.Hour).Format(time.RFC3339)}},
	}
	if _, grants, err := service.normalizeCreate(valid); err != nil || len(grants) != 1 {
		t.Fatalf("normalizeCreate(valid) grants=%v error=%v", grants, err)
	}
	permanent := valid
	permanent.Permissions = []auth_request.DataAuthorizationPermissionInput{{
		Permission: model.PermissionBojunOrderRead,
		Permanent:  true,
	}}
	normalized, grants, err := service.normalizeCreate(permanent)
	if err != nil || len(grants) != 1 || grants[0].ExpiresAt != nil ||
		len(normalized.Permissions) != 1 || !normalized.Permissions[0].Permanent || normalized.Permissions[0].ExpiresAt != "" {
		t.Fatalf("normalizeCreate(permanent) normalized=%+v grants=%+v error=%v", normalized, grants, err)
	}
	reserved := valid
	reserved.Account = " Admin "
	if _, _, err := service.normalizeCreate(reserved); err == nil {
		t.Fatal("normalizeCreate() accepted reserved admin")
	}
	duplicate := valid
	duplicate.Permissions = append(duplicate.Permissions, duplicate.Permissions[0])
	if _, _, err := service.normalizeCreate(duplicate); err == nil {
		t.Fatal("normalizeCreate() accepted duplicate permission")
	}
}

func TestCreateDataAuthorizationUserOmitsNullablePhone(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      &sql.DB{},
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	db = db.Session(&gorm.Session{SkipDefaultTransaction: true})

	var insertSQL string
	if err := db.Callback().Create().After("gorm:create").Register("test:capture_open_api_user", func(tx *gorm.DB) {
		insertSQL = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}
	user := &model.User{
		BaseModel:             &model.BaseModel{},
		CommonTimestampsField: &model.CommonTimestampsField{},
		Account:               "partner_weather_01",
		Nickname:              "合作方账号",
		Password:              strings.Repeat("x", 60),
	}
	if err := createDataAuthorizationUser(context.Background(), db, user); err != nil {
		t.Fatalf("createDataAuthorizationUser() error = %v", err)
	}
	if insertSQL == "" || !strings.Contains(insertSQL, "`account`") {
		t.Fatalf("user INSERT was not captured: %q", insertSQL)
	}
	if strings.Contains(insertSQL, "`phone`") {
		t.Fatalf("user INSERT contains nullable phone zero value: %q", insertSQL)
	}
}

func TestDataAuthorizationReasonRejectsLogInjection(t *testing.T) {
	for _, reason := range []string{"", "line one\nline two", "carriage\rreturn", "nul\x00byte", strings.Repeat("理", 501)} {
		if _, err := normalizeDataAuthorizationReason(reason); err == nil {
			t.Fatalf("reason %q was accepted", reason)
		}
	}
}

func TestNormalizeDataAuthorizationAuditTimeRange(t *testing.T) {
	start, end, err := normalizeDataAuthorizationAuditTimeRange("2026-07-30T09:00:00+08:00", "2026-07-30T01:00:00Z")
	if err != nil || start == nil || end == nil || !start.Equal(*end) {
		t.Fatalf("normalizeDataAuthorizationAuditTimeRange() = %v, %v, %v", start, end, err)
	}
	if start.Location() != time.UTC || end.Location() != time.UTC {
		t.Fatalf("time range must be normalized to UTC: %v, %v", start.Location(), end.Location())
	}
	if start, end, err := normalizeDataAuthorizationAuditTimeRange("", ""); err != nil || start != nil || end != nil {
		t.Fatalf("empty time range = %v, %v, %v", start, end, err)
	}
	for _, values := range [][2]string{{"2026-07-30", ""}, {"2026-07-30T02:00:00Z", "2026-07-30T01:00:00Z"}} {
		if _, _, err := normalizeDataAuthorizationAuditTimeRange(values[0], values[1]); !errors.Is(err, ErrDataAuthorizationInvalidInput) {
			t.Fatalf("range %q-%q error = %v, want invalid input", values[0], values[1], err)
		}
	}
}

func TestBuildDataAuthorizationAuditQueryUsesBoundTimeParameters(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      &sql.DB{},
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	start := time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	query, err := buildDataAuthorizationAuditQuery(db, auth_request.DataAuthorizationAuditQueryRequest{
		TargetUserID: 9,
		Action:       model.DataAuthorizationActionGrant,
		BeforeID:     41,
	}, &start, &end)
	if err != nil {
		t.Fatalf("buildDataAuthorizationAuditQuery() error = %v", err)
	}
	statement := query.Order("id DESC").Limit(31).Find(&[]model.DataAuthorizationAudit{}).Statement
	if !strings.Contains(statement.SQL.String(), "created_at >= ?") || !strings.Contains(statement.SQL.String(), "created_at <= ?") {
		t.Fatalf("time predicates are missing from SQL: %q", statement.SQL.String())
	}
	if len(statement.Vars) != 5 || statement.Vars[3] != start || statement.Vars[4] != end {
		t.Fatalf("time predicates must use bound parameters, SQL=%q vars=%#v", statement.SQL.String(), statement.Vars)
	}
}

func TestGenerateOpenAPITokenProducesOpaqueCredential(t *testing.T) {
	token, err := generateOpenAPIToken()
	if err != nil {
		t.Fatalf("generateOpenAPIToken() error = %v", err)
	}
	if !strings.HasPrefix(token, "dg_open_") || len(tokenDigest(token)) != 64 || tokenDisplayPrefix(token) == token {
		t.Fatalf("token shape is invalid")
	}
	data, err := json.Marshal(model.OpenAPICredential{TokenHash: tokenDigest(token), TokenPrefix: tokenDisplayPrefix(token)})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(data), token) || strings.Contains(string(data), tokenDigest(token)) {
		t.Fatal("credential JSON leaked token or hash")
	}
}

func TestPermissionDTOStates(t *testing.T) {
	now := time.Now().UTC()
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	if got := permissionDTO(model.PermissionWeatherRead, nil, false, now); got.Status != "NOT_GRANTED" || got.Permanent {
		t.Fatalf("missing permission = %+v", got)
	}
	if got := permissionDTO(model.PermissionWeatherRead, nil, true, now); got.Status != "ACTIVE" || !got.Permanent || got.ExpiresAt != nil {
		t.Fatalf("permanent permission = %+v", got)
	}
	if got := permissionDTO(model.PermissionWeatherRead, &future, true, now); got.Status != "ACTIVE" || got.Permanent {
		t.Fatalf("future permission = %+v", got)
	}
	if got := permissionDTO(model.PermissionWeatherRead, &past, true, now); got.Status != "EXPIRED" || got.Permanent {
		t.Fatalf("expired permission = %+v", got)
	}
	permissions := permissionDTOs(map[string]*time.Time{model.PermissionBojunOrderRead: nil}, now)
	if len(permissions) != len(model.GrantableDataPermissions()) || permissions[1].Status != "ACTIVE" || !permissions[1].Permanent {
		t.Fatalf("permissionDTOs(permanent) = %+v", permissions)
	}
	if got := permissionDTO(model.PermissionBusinessOverviewRead, nil, true, now); got.Label != "营业金额查询" || got.Status != "ACTIVE" {
		t.Fatalf("business overview permission = %+v", got)
	}
}
