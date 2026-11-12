package model

import (
	"reflect"
	"testing"
)

func TestOpenAPIPermissionGroupsAreIsolatedAndReadOnly(t *testing.T) {
	groups := OpenAPIPermissionGroups()
	if len(groups) != 5 || groups[0].Code != PermissionTmallRead || len(groups[0].Children) != 4 {
		t.Fatalf("permission groups = %+v", groups)
	}
	for _, code := range []string{PermissionWeatherRefresh, PermissionWeatherConfigManage, PermissionMallWrite, PermissionSystemAccountManage} {
		if parent := OpenAPIPermissionParent(code); parent != "" {
			t.Fatalf("administrative permission %s inherited from %s", code, parent)
		}
	}
	groups[0].Children[0].Code = "changed"
	if OpenAPIPermissionGroups()[0].Children[0].Code != PermissionTmallSycmRead {
		t.Fatal("caller changed shared permission catalog")
	}
}

func TestOpenAPIPermissionCandidatesOnlyInheritDeclaredParent(t *testing.T) {
	for _, test := range []struct {
		code string
		want []string
	}{
		{PermissionTmallOrdersRead, []string{PermissionTmallOrdersRead, PermissionTmallRead}},
		{PermissionWeatherRead, []string{PermissionWeatherRead, PermissionWeatherAPIRead}},
		{PermissionMonitoringHourlyRead, []string{PermissionMonitoringHourlyRead, PermissionMonitoringAPIRead}},
		{PermissionMonitoringDailyRead, []string{PermissionMonitoringDailyRead, PermissionMonitoringAPIRead}},
		{PermissionMonitoringAreaRead, []string{PermissionMonitoringAreaRead, PermissionMonitoringAPIRead}},
		{PermissionTmallRead, []string{PermissionTmallRead}},
		{PermissionWeatherConfigManage, []string{PermissionWeatherConfigManage}},
		{"tmall.unknown.read", []string{"tmall.unknown.read"}},
	} {
		if got := OpenAPIPermissionCandidates(test.code); !reflect.DeepEqual(got, test.want) {
			t.Errorf("candidates(%s) = %v, want %v", test.code, got, test.want)
		}
	}
}
