package model

import (
	"reflect"
	"testing"
)

func TestOpenAPIPermissionGroupsAreIsolatedAndReadOnly(t *testing.T) {
	groups := OpenAPIPermissionGroups()
	if len(groups) != 7 || groups[0].Code != PermissionTmallRead || len(groups[0].Children) != 4 {
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

func TestFeishuAttendancePermissionHierarchy(t *testing.T) {
	parent := "feishu_attendance.read"
	children := []string{"feishu_attendance.daily.read", "feishu_attendance.punch.read", "feishu_attendance.item.read"}
	for _, child := range children {
		if got := OpenAPIPermissionCandidates(child); !reflect.DeepEqual(got, []string{child, parent}) {
			t.Errorf("candidates(%s) = %v", child, got)
		}
		if OpenAPIPermissionLevel(child) != 2 || OpenAPIPermissionName(child) == "" {
			t.Errorf("missing named child permission %s", child)
		}
	}
	want := append([]string{parent}, children...)
	if got := ExpandOpenAPIPermissions([]string{parent}); !reflect.DeepEqual(got, want) {
		t.Errorf("expanded permissions = %v, want %v", got, want)
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

func TestYouzanParentExpandsForNavigationWithoutSiblingGrants(t *testing.T) {
	for _, test := range []struct {
		codes []string
		want  []string
	}{
		{[]string{"youzan.read", "youzan.orders.read"}, []string{"youzan.read", "youzan.orders.read", "youzan.order_items.read"}},
		{[]string{"youzan.orders.read"}, []string{"youzan.orders.read"}},
		{[]string{"youzan.order_items.read"}, []string{"youzan.order_items.read"}},
	} {
		if got := ExpandOpenAPIPermissions(test.codes); !reflect.DeepEqual(got, test.want) {
			t.Errorf("expanded permissions = %v, want %v", got, test.want)
		}
	}
}
