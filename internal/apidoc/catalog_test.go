package apidoc

import (
	"encoding/json"
	"reflect"
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
	if source.Title != "allblu开放接口文档" || len(source.Operations) != 41 {
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

func TestFeishuAttendanceContracts(t *testing.T) {
	common := "ID USER_ID USER_NAME EMPLOYEE_NO DEPARTMENT_NAME WORK_DATE MODIFIED_TIME"
	extras := map[string]string{
		"daily": "ATTENDANCE_GROUP_NAME SHIFT_NAME TIME_ZONE RESIGN_DATE REQUIRED_MINUTES ACTUAL_MINUTES OVERTIME_MINUTES LEAVE_MINUTES",
		"punch": "SOURCE_ROW_NO PUNCH_INDEX PUNCH_TYPE TIME_CODE RESULT_CODE PUNCH_TIME_TEXT RESULT_TEXT FEATURE_DATE GROUP_NAME GROUP_TYPE DAY_TYPE SOURCE_PUNCH_NO PUNCH_TITLE FEATURE_PUNCH_TIME_TEXT SHIFT_TIME_TEXT PUNCH_SHIFT_TIME_TEXT PUNCH_STATUS PUNCH_SUB_STATUS STATUS_MESSAGE PUNCH_LOCATION_NAME REMARK",
		"item":  "SOURCE_ROW_NO ITEM_SEQ METRIC_CODE METRIC_TITLE VALUE_TEXT DURATION_DAY DURATION_HALF_DAY DURATION_HOUR DURATION_HALF_HOUR DURATION_MINUTE",
	}
	registered := map[string]bool{}
	for kind := range extras {
		registered["POST /api/open/feishu/attendance/"+kind+"/query"] = true
	}
	document, err := Build(registered)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Groups) != 1 || document.Groups[0].Code != "feishu_attendance.read" || len(document.Groups[0].Children) != 3 {
		t.Fatalf("missing attendance documentation group: %+v", document.Groups)
	}
	for _, category := range document.Groups[0].Children {
		if len(category.Operations) != 1 {
			t.Fatalf("expected one operation per attendance permission: %+v", category)
		}
		op := category.Operations[0]
		kind := strings.TrimSuffix(strings.TrimPrefix(op.Permission, "feishu_attendance."), ".read")
		t.Run(kind, func(t *testing.T) {
			if op.Method != "POST" || op.Path != "/api/open/feishu/attendance/"+kind+"/query" || !strings.Contains(op.Authentication, "token") {
				t.Fatalf("incorrect route or authentication: %+v", op)
			}
			for _, name := range []string{"startDate", "endDate", "filters"} {
				field := findField(op.RequestFields, name)
				if field == nil || !field.Required || field.Location != "body" {
					t.Errorf("missing required body field: %s", name)
				}
			}
			filters := findField(op.RequestFields, "filters")
			if filters == nil || len(filters.Children) != 2 {
				t.Fatal("expected exactly the name and department filters")
			}
			for _, name := range []string{"USER_NAME", "DEPARTMENT_NAME"} {
				field := findField(filters.Children, name)
				if field == nil || !field.Required || field.Type != "string" {
					t.Errorf("missing required exact filter %s", name)
				}
			}
			items := findField(op.ResponseFields, "data", "items")
			if items == nil {
				t.Fatal("missing items")
			}
			var names []string
			for _, field := range items.Children {
				names = append(names, field.Name)
				if field.Type != "string" || !field.Nullable || field.Description == "" {
					t.Errorf("source values must retain string/null and description: %+v", field)
				}
			}
			if want := strings.Fields(common + " " + extras[kind]); !reflect.DeepEqual(names, want) {
				t.Errorf("source field contract = %v, want %v", names, want)
			}
			pagination := findField(op.ResponseFields, "data", "pagination")
			if pagination == nil || len(pagination.Children) != 3 || findField(pagination.Children, "hasMore") == nil || findField(op.ResponseFields, "data", "total") != nil {
				t.Error("expected page/pageSize/hasMore pagination without total")
			}
			errors := map[int]string{}
			for _, entry := range op.Errors {
				errors[entry.Status] = entry.Code
			}
			if want := (map[int]string{401: "100401", 403: "100403", 422: "100422", 429: "100429", 500: "100500", 503: "100503"}); !reflect.DeepEqual(errors, want) {
				t.Errorf("errors = %v", errors)
			}
			var notes strings.Builder
			for _, note := range op.Notes {
				notes.WriteString(note.Title + note.Body)
			}
			for _, term := range []string{"31", "300", "30", "手机号", "USER_ID", "部门全称", "数据范围授权", "MODIFIED_TIME", "02:00", "非实时", "URL", "未知字段"} {
				if !strings.Contains(notes.String(), term) {
					t.Errorf("missing contract explanation: %s", term)
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

func TestYouzanCatalogPublishesIndependentQueryContracts(t *testing.T) {
	document, err := Build(map[string]bool{
		"POST /api/open/youzan/orders/query":       true,
		"POST /api/open/youzan/orders/items/query": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Groups) != 1 || document.Groups[0].Code != "youzan.read" || len(document.Groups[0].Children) != 2 {
		t.Fatalf("expected Youzan parent with two query permissions: %+v", document.Groups)
	}
	contracts := map[string]struct {
		path    string
		fields  string
		filters string
	}{
		"youzan.orders.read": {
			path:    "/api/open/youzan/orders/query",
			fields:  "ID TID STATUS STATUS_STR TYPE NODE_KDT_ID ROOT_KDT_ID SHOP_NAME CREATED UPDATE_TIME PAY_TIME SUCCESS_TIME CONSIGN_TIME CONFIRM_TIME TOTAL_FEE POST_FEE PAYMENT REFUND_STATE CREATED_DWD MODIFIED_DWD",
			filters: "TID STATUS NODE_KDT_ID ROOT_KDT_ID",
		},
		"youzan.order_items.read": {
			path:    "/api/open/youzan/orders/items/query",
			fields:  "ID TID OID TITLE NUM ITEM_ID SKU_ID ITEM_NO SKU_NO ITEM_BARCODE SKU_BARCODE PRICE DISCOUNT_PRICE TOTAL_FEE PAYMENT CREATED UPDATE_TIME CREATED_DWD MODIFIED_DWD",
			filters: "TID OID ITEM_ID SKU_ID SKU_NO",
		},
	}
	for _, category := range document.Groups[0].Children {
		t.Run(category.Code, func(t *testing.T) {
			contract, ok := contracts[category.Code]
			if !ok || len(category.Operations) != 1 {
				t.Fatalf("unexpected permission operations: %+v", category)
			}
			operation := category.Operations[0]
			if operation.Method != "POST" || operation.Path != contract.path || operation.ParentPermission != "youzan.read" {
				t.Fatalf("unexpected route contract: %+v", operation)
			}
			items := findField(operation.ResponseFields, "data", "items")
			if items == nil || len(items.Children) != len(strings.Fields(contract.fields)) {
				t.Fatalf("unexpected Youzan projection: %+v", items)
			}
			for _, name := range strings.Fields(contract.fields) {
				field := findField(items.Children, name)
				if field == nil || field.Type != "string" || !field.Nullable || !field.Required {
					t.Errorf("%s must be a required string-or-null field: %+v", name, field)
				}
			}
			filters := findField(operation.RequestFields, "filters")
			if filters == nil || len(filters.Children) != len(strings.Fields(contract.filters)) {
				t.Fatalf("unexpected filters: %+v", filters)
			}
			for _, name := range strings.Fields(contract.filters) {
				if findField(filters.Children, name) == nil {
					t.Errorf("missing exact filter %s", name)
				}
			}
			for _, name := range []string{"startTime", "endTime"} {
				field := findField(operation.RequestFields, name)
				if field == nil || field.Required {
					t.Errorf("%s must allow precise TID requests without a time range", name)
				}
			}
			var request struct {
				TimeField string `json:"timeField"`
			}
			if err := json.Unmarshal(operation.RequestExample, &request); err != nil || request.TimeField != "modifiedDwd" {
				t.Errorf("example should demonstrate warehouse incremental time: %+v, %v", request, err)
			}
			var response struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
				Data struct {
					Items []map[string]any `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(operation.ResponseExample, &response); err != nil || response.Code != 0 || response.Msg != "请求成功" || len(response.Data.Items) != 1 {
				t.Fatalf("unexpected response example: %+v, %v", response, err)
			}
			for name, value := range response.Data.Items[0] {
				if value != nil {
					if _, ok := value.(string); !ok {
						t.Errorf("%s is not string or null", name)
					}
				}
			}
		})
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
