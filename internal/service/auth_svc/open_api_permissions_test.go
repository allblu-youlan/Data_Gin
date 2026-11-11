package auth_svc

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

func TestParentGrantsCoverOnlyTheirDeclaredChildren(t *testing.T) {
	tests := []struct {
		name, granted, requested string
		want                     bool
	}{
		{"tmall parent", model.PermissionTmallRead, model.PermissionTmallRead, true},
		{"tmall sycm", model.PermissionTmallRead, model.PermissionTmallSycmRead, true},
		{"tmall qztg", model.PermissionTmallRead, model.PermissionTmallQztgRead, true},
		{"tmall total", model.PermissionTmallRead, model.PermissionTmallTotRead, true},
		{"tmall orders", model.PermissionTmallRead, model.PermissionTmallOrdersRead, true},
		{"different module", model.PermissionTmallRead, model.PermissionBojunOrderRead, false},
		{"system management", model.PermissionTmallRead, model.PermissionSystemAccountManage, false},
		{"weather read", model.PermissionWeatherAPIRead, model.PermissionWeatherRead, true},
		{"weather management", model.PermissionWeatherAPIRead, model.PermissionWeatherConfigManage, false},
		{"legacy child", model.PermissionTmallOrdersRead, model.PermissionTmallOrdersRead, true},
		{"child cannot grant parent", model.PermissionTmallOrdersRead, model.PermissionTmallRead, false},
		{"child cannot grant sibling", model.PermissionTmallOrdersRead, model.PermissionTmallSycmRead, false},
	}
	for _, accountType := range []string{model.AccountTypeOpenAPI, model.AccountTypeConsole} {
		t.Run(accountType, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					user := model.User{BaseModel: &model.BaseModel{ID: 7}, AccountType: accountType, Status: model.AccountStatusActive}
					permissions := map[string]bool{test.granted: true}
					repository := &fakeAuthorizationRepository{
						roles:           []authorizationRole{{Code: "custom"}},
						openPermissions: permissions, consolePermissions: permissions,
					}
					allowed, err := newAuthorizer(repository).HasPermission(context.Background(), user, test.requested)
					if err != nil || allowed != test.want {
						t.Fatalf("permission %s = %t, %v; want %t", test.requested, allowed, err, test.want)
					}
				})
			}
		})
	}
}

func TestPermissionDTOInheritanceAndExpiry(t *testing.T) {
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	future, later, past := now.Add(time.Hour), now.Add(2*time.Hour), now.Add(-time.Hour)
	for _, test := range []struct {
		name              string
		grants            map[string]*time.Time
		status, inherited string
		expiresAt         *time.Time
	}{
		{"parent", map[string]*time.Time{model.PermissionTmallRead: &future}, "ACTIVE", model.PermissionTmallRead, &future},
		{"expired parent", map[string]*time.Time{model.PermissionTmallRead: &past}, "NOT_GRANTED", "", nil},
		{"parent expires now", map[string]*time.Time{model.PermissionTmallRead: &now}, "NOT_GRANTED", "", nil},
		{"independent child", map[string]*time.Time{model.PermissionTmallRead: &past, model.PermissionTmallOrdersRead: nil}, "ACTIVE", "", nil},
		{"revoked parent", map[string]*time.Time{}, "NOT_GRANTED", "", nil},
		{"revoked parent retains child", map[string]*time.Time{model.PermissionTmallOrdersRead: &future}, "ACTIVE", "", &future},
		{"longer parent", map[string]*time.Time{model.PermissionTmallRead: &later, model.PermissionTmallOrdersRead: &future}, "ACTIVE", model.PermissionTmallRead, &later},
		{"longer child", map[string]*time.Time{model.PermissionTmallRead: &future, model.PermissionTmallOrdersRead: &later}, "ACTIVE", "", &later},
		{"permanent parent", map[string]*time.Time{model.PermissionTmallRead: nil, model.PermissionTmallOrdersRead: &future}, "ACTIVE", model.PermissionTmallRead, nil},
		{"permanent child", map[string]*time.Time{model.PermissionTmallRead: &future, model.PermissionTmallOrdersRead: nil}, "ACTIVE", "", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, dto := range permissionDTOs(test.grants, now) {
				if dto.Permission != model.PermissionTmallOrdersRead {
					continue
				}
				if dto.Status != test.status || dto.InheritedFrom != test.inherited || dto.Level != 2 || dto.ParentCode != model.PermissionTmallRead || !reflect.DeepEqual(dto.ExpiresAt, test.expiresAt) {
					t.Fatalf("child = %+v", dto)
				}
				return
			}
			t.Fatal("child permission missing from catalog")
		})
	}
}

func TestPermissionDTOReportsDirectGrantAlongsideEffectiveInheritance(t *testing.T) {
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	childExpiry, parentExpiry, past := now.Add(time.Hour), now.Add(2*time.Hour), now.Add(-time.Hour)
	for _, test := range []struct {
		name                          string
		grants                        map[string]*time.Time
		status, inherited             string
		effectiveExpiry, directExpiry *time.Time
		directGranted, permanent      bool
	}{
		{"longer parent retains child expiry", map[string]*time.Time{model.PermissionTmallRead: &parentExpiry, model.PermissionTmallOrdersRead: &childExpiry}, "ACTIVE", model.PermissionTmallRead, &parentExpiry, &childExpiry, true, false},
		{"permanent parent retains child expiry", map[string]*time.Time{model.PermissionTmallRead: nil, model.PermissionTmallOrdersRead: &childExpiry}, "ACTIVE", model.PermissionTmallRead, nil, &childExpiry, true, false},
		{"permanent independent child", map[string]*time.Time{model.PermissionTmallRead: &parentExpiry, model.PermissionTmallOrdersRead: nil}, "ACTIVE", "", nil, nil, true, true},
		{"expired independent child remains revocable", map[string]*time.Time{model.PermissionTmallRead: &parentExpiry, model.PermissionTmallOrdersRead: &past}, "ACTIVE", model.PermissionTmallRead, &parentExpiry, &past, true, false},
		{"revoke child retains parent", map[string]*time.Time{model.PermissionTmallRead: &parentExpiry}, "ACTIVE", model.PermissionTmallRead, &parentExpiry, nil, false, false},
		{"revoke parent retains child", map[string]*time.Time{model.PermissionTmallOrdersRead: &childExpiry}, "ACTIVE", "", &childExpiry, &childExpiry, true, false},
		{"revoke both removes access", map[string]*time.Time{}, "NOT_GRANTED", "", nil, nil, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dto := effectivePermissionDTO(model.PermissionTmallOrdersRead, test.grants, now)
			if dto.Status != test.status || dto.InheritedFrom != test.inherited || !reflect.DeepEqual(dto.ExpiresAt, test.effectiveExpiry) {
				t.Fatalf("effective permission = %+v", dto)
			}
			payload, err := json.Marshal(dto)
			if err != nil {
				t.Fatal(err)
			}
			var direct struct {
				Granted   *bool      `json:"directGranted"`
				Permanent *bool      `json:"directPermanent"`
				ExpiresAt *time.Time `json:"directExpiresAt"`
			}
			if err := json.Unmarshal(payload, &direct); err != nil {
				t.Fatal(err)
			}
			if direct.Granted == nil || *direct.Granted != test.directGranted || direct.Permanent == nil || *direct.Permanent != test.permanent || !reflect.DeepEqual(direct.ExpiresAt, test.directExpiry) {
				t.Fatalf("direct grant fields missing or incorrect: %s", payload)
			}
		})
	}
}

func TestPermissionCatalogMatchesGrantableHierarchy(t *testing.T) {
	grantable := make(map[string]bool)
	for _, code := range model.GrantableDataPermissions() {
		grantable[code] = true
	}
	for _, permission := range PermissionCatalog() {
		if permission.APIGrantable != grantable[permission.Code] {
			t.Fatalf("permission grantability differs between catalogs: %+v", permission)
		}
		if permission.APIGrantable && (permission.Level == 0 || permission.ParentCode != model.OpenAPIPermissionParent(permission.Code)) {
			t.Fatalf("permission hierarchy missing: %+v", permission)
		}
		delete(grantable, permission.Code)
	}
	if len(grantable) != 0 {
		t.Fatalf("grantable permissions missing from role catalog: %v", grantable)
	}
}

func TestAccountProfileExpandsParentPermissionForNavigation(t *testing.T) {
	repository := &fakeAccountAuthRepository{profile: &consoleProfile{
		Account:     *testConsoleAccount(t),
		Permissions: []string{model.PermissionTmallRead, model.PermissionTmallOrdersRead, model.PermissionBojunOrderRead},
	}}
	service := NewAccountAuthService(repository, nil, &fakeTokenIssuer{})
	profile, err := service.Profile(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{model.PermissionTmallRead, model.PermissionTmallSycmRead, model.PermissionTmallQztgRead, model.PermissionTmallTotRead, model.PermissionTmallOrdersRead, model.PermissionBojunOrderRead}
	sort.Strings(want)
	if !reflect.DeepEqual(profile.Permissions, want) {
		t.Fatalf("profile permissions = %v, want %v", profile.Permissions, want)
	}
	if len(repository.profile.Permissions) != 3 {
		t.Fatal("profile expansion mutated stored grants")
	}
}
