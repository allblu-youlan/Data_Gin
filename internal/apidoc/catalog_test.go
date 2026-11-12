package apidoc

import (
	"encoding/json"
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

func TestCatalogIncludesCompleteReferenceFields(t *testing.T) {
	var source struct {
		Title      string      `json:"title"`
		Operations []Operation `json:"operations"`
	}
	if err := json.Unmarshal(catalogJSON, &source); err != nil {
		t.Fatal(err)
	}
	if source.Title != "allblu开放接口文档" || len(source.Operations) != 36 {
		t.Fatalf("unexpected reference: %s, %d operations", source.Title, len(source.Operations))
	}
	seen := map[string]bool{}
	counts := map[string]int{"tmall-orders-realtime": 18, "tmall-sycm": 35, "tmall-sycm-hourly": 47, "tmall-qztg": 90, "tmall-qztg-hourly": 92, "tmall-tot": 31}
	for _, operation := range source.Operations {
		t.Run(operation.ID, func(t *testing.T) {
			if seen[operation.ID] || model.OpenAPIPermissionParent(operation.Permission) == "" {
				t.Fatalf("duplicate ID or unknown permission: %s", operation.ID)
			}
			seen[operation.ID] = true
			var example any
			if err := json.Unmarshal(operation.ResponseExample, &example); err != nil {
				t.Fatal(err)
			}
			assertExampleFields(t, example, operation.ResponseFields, "")
			if count, ok := counts[operation.ID]; ok {
				items := findField(operation.ResponseFields, "data", "items")
				if items == nil || len(items.Children) != count {
					t.Fatalf("expected all %d source fields inline", count)
				}
				for _, field := range items.Children {
					if field.Type != "string" || !strings.Contains(field.Description, "单位/格式与使用说明") {
						t.Errorf("lost source type or units: %s", field.Name)
					}
				}
			}
		})
	}
}

func TestBuildUsesRegisteredRoutesAndSharedPermissionHierarchy(t *testing.T) {
	document, err := Build(map[string]bool{
		"POST /api/open/tmall/qztg/hourly/query": true,
		"GET /api/open/bojun/orders/query":       true,
		"POST /api/unknown":                      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Groups) != 1 || document.Groups[0].Code != model.PermissionTmallRead {
		t.Fatalf("unexpected groups: %+v", document.Groups)
	}
	children := document.Groups[0].Children
	if len(children) != 1 || len(children[0].Operations) != 1 {
		t.Fatalf("unregistered routes leaked: %+v", children)
	}
	if children[0].Operations[0].ParentPermission != model.PermissionTmallRead {
		t.Fatal("missing inherited permission")
	}
	empty, err := Build(nil)
	if err != nil || empty.Groups == nil || len(empty.Groups) != 0 {
		t.Fatalf("empty catalog: %+v, %v", empty, err)
	}
}

func findField(fields []Field, path ...string) *Field {
	for i := range fields {
		if fields[i].Name == path[0] {
			if len(path) == 1 {
				return &fields[i]
			}
			return findField(fields[i].Children, path[1:]...)
		}
	}
	return nil
}

func assertExampleFields(t *testing.T, example any, fields []Field, path string) {
	t.Helper()
	switch value := example.(type) {
	case map[string]any:
		for name, child := range value {
			field := findField(fields, name)
			if field == nil {
				t.Errorf("example field has no inline definition: %s%s", path, name)
				continue
			}
			assertExampleFields(t, child, field.Children, path+name+".")
		}
	case []any:
		for _, child := range value {
			assertExampleFields(t, child, fields, path+"[].")
		}
	}
}
