package data_svc

import "testing"

func TestNormalizeOpenBojunQueryPagination(t *testing.T) {
	for _, tt := range []struct {
		name       string
		page, size int
		valid      bool
	}{
		{"first", 1, 50, true}, {"limits", 1000, 100, true},
		{"zero page", 0, 50, false}, {"zero size", 1, 0, false},
		{"negative", -1, 50, false}, {"large page", 1001, 50, false}, {"large size", 1, 101, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeOpenBojunQueryPagination(&tt.page, &tt.size)
			if (err == nil) != tt.valid || (tt.valid && (got.Page != tt.page || got.PageSize != tt.size)) {
				t.Fatalf("pagination = %+v, error = %v", got, err)
			}
		})
	}
	got, err := normalizeOpenBojunQueryPagination(nil, nil)
	if err != nil || got.Page != 1 || got.PageSize != 50 || got.HasMore {
		t.Fatalf("defaults = %+v, error = %v", got, err)
	}
}
