package model

import "testing"

func TestMallWeatherAdminPermissions(t *testing.T) {
	permissions := MallWeatherAdminPermissions()
	if len(permissions) != 15 {
		t.Fatalf("MallWeatherAdminPermissions() len = %d, want 15", len(permissions))
	}

	seen := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		if permission == "" || len(permission) > 64 {
			t.Fatalf("invalid permission %q", permission)
		}
		if _, exists := seen[permission]; exists {
			t.Fatalf("duplicate permission %q", permission)
		}
		seen[permission] = struct{}{}
	}
	if _, exists := seen[PermissionWeatherRawRead]; !exists {
		t.Fatalf("missing reserved permission %q", PermissionWeatherRawRead)
	}
	if _, exists := seen[PermissionBojunOrderRead]; !exists {
		t.Fatalf("missing permission %q", PermissionBojunOrderRead)
	}
	for _, permission := range []string{"tmall.sycm.read", "tmall.qztg.read", "tmall.tot.read"} {
		if _, exists := seen[permission]; !exists {
			t.Fatalf("missing tmall permission %q", permission)
		}
	}

	permissions[0] = "modified"
	if MallWeatherAdminPermissions()[0] == "modified" {
		t.Fatal("MallWeatherAdminPermissions() returned shared mutable state")
	}
}
