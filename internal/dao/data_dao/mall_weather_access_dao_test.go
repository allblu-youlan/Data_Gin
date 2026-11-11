package data_dao

import (
	"context"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"

	"gorm.io/gorm"
)

func TestMallWeatherPermissionDAORejectsInvalidLookupBeforeDatabase(t *testing.T) {
	dao := &MallWeatherPermissionDAO{}
	tests := []struct {
		name       string
		ctx        context.Context
		userID     uint
		permission string
	}{
		{name: "missing database", ctx: context.Background(), userID: 1, permission: "mall.read"},
		{name: "missing context", userID: 1, permission: "mall.read"},
		{name: "missing user", ctx: context.Background(), permission: "mall.read"},
		{name: "missing permission", ctx: context.Background(), userID: 1},
		{name: "oversized permission", ctx: context.Background(), userID: 1, permission: string(make([]byte, 65))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := dao.HasPermission(tt.ctx, tt.userID, tt.permission, time.Now()); err == nil {
				t.Fatal("HasPermission() error = nil")
			}
		})
	}
}

func TestMallWeatherPermissionDAOHasPermissionUsesBoundedExistenceQuery(t *testing.T) {
	t.Parallel()
	db := dryRunWeatherDAOTestDB(t)
	type contextKey string
	ctx := context.WithValue(t.Context(), contextKey("request"), "open-weather")
	var statement string
	var statementContext context.Context
	var statementVars []interface{}
	if err := db.Callback().Query().After("gorm:query").Register("test:capture_permission_exists_sql", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
		statementContext = tx.Statement.Context
		statementVars = append([]interface{}{}, tx.Statement.Vars...)
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}

	allowed, err := NewMallWeatherPermissionDAO(db).HasPermission(
		ctx,
		7,
		" weather.read ",
		time.Date(2026, 7, 30, 3, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	)
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if allowed {
		t.Fatal("HasPermission() returned true in dry-run mode")
	}
	for _, fragment := range []string{
		"SELECT 1 AS permission_exists",
		"FROM `mall_weather_user_permissions`",
		"user_id = ? AND permission IN (?,?)",
		"expires_at IS NULL OR expires_at > ?",
		"LIMIT 1",
	} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("statement missing %q: %s", fragment, statement)
		}
	}
	if strings.Contains(strings.ToUpper(statement), "COUNT(") {
		t.Fatalf("permission lookup uses count query: %s", statement)
	}
	if statementContext != ctx {
		t.Fatal("permission lookup did not preserve request context")
	}
	if len(statementVars) != 4 || statementVars[0] != uint(7) || statementVars[1] != "weather.read" || statementVars[2] != model.PermissionWeatherAPIRead {
		t.Fatalf("statement vars = %v", statementVars)
	}
	if expiry, ok := statementVars[3].(time.Time); !ok || expiry.Location() != time.UTC {
		t.Fatalf("expiry variable is not UTC: %#v", statementVars[3])
	}
}

func TestMallWeatherPermissionDAORejectsInvalidPermanentGrantBeforeDatabase(t *testing.T) {
	dao := NewMallWeatherPermissionDAO(dryRunWeatherDAOTestDB(t).Session(&gorm.Session{SkipDefaultTransaction: true}))
	tests := []struct {
		name        string
		userID      uint
		grantedBy   uint
		permissions []string
	}{
		{name: "missing user", grantedBy: 1, permissions: []string{"mall.read"}},
		{name: "missing grantor", userID: 1, permissions: []string{"mall.read"}},
		{name: "empty permission set", userID: 1, grantedBy: 1},
		{name: "blank permission", userID: 1, grantedBy: 1, permissions: []string{" "}},
		{name: "oversized permission", userID: 1, grantedBy: 1, permissions: []string{strings.Repeat("x", 65)}},
		{name: "duplicate permission", userID: 1, grantedBy: 1, permissions: []string{"mall.read", "mall.read"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := dao.GrantPermanentPermissions(context.Background(), tt.userID, tt.grantedBy, tt.permissions); err == nil {
				t.Fatal("GrantPermanentPermissions() error = nil")
			}
		})
	}
}

func TestMallWeatherPermissionDAOGrantPermanentPermissionsUsesConflictUpdate(t *testing.T) {
	db := dryRunWeatherDAOTestDB(t).Session(&gorm.Session{SkipDefaultTransaction: true})
	var generatedSQL string
	if err := db.Callback().Create().After("gorm:create").Register("test:capture_sql", func(tx *gorm.DB) {
		generatedSQL = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("register SQL capture callback: %v", err)
	}

	dao := NewMallWeatherPermissionDAO(db)
	if err := dao.GrantPermanentPermissions(context.Background(), 7, 7, []string{"mall.read", "weather.read"}); err != nil {
		t.Fatalf("GrantPermanentPermissions() error = %v", err)
	}
	for _, fragment := range []string{
		"ON DUPLICATE KEY UPDATE",
		"`granted_by`=VALUES(`granted_by`)",
		"`expires_at`=VALUES(`expires_at`)",
		"`updated_at`=VALUES(`updated_at`)",
	} {
		if !strings.Contains(generatedSQL, fragment) {
			t.Fatalf("generated SQL missing %q: %s", fragment, generatedSQL)
		}
	}
}

func TestAPIIdempotencyDAORejectsInvalidStateBeforeDatabase(t *testing.T) {
	dao := &APIIdempotencyDAO{}
	if _, err := dao.Reserve(context.Background(), nil); err == nil {
		t.Fatal("Reserve(nil) error = nil")
	}
	if _, err := dao.Reserve(context.Background(), &model.APIIdempotencyRecord{
		OperationScope: "mall.create",
		ActorUserID:    1,
		KeyHash:        "short",
		RequestHash:    "short",
	}); err == nil {
		t.Fatal("Reserve(invalid hashes) error = nil")
	}
	if err := dao.Complete(context.Background(), 0, 0, 500, ""); err == nil {
		t.Fatal("Complete(invalid state) error = nil")
	}
}
