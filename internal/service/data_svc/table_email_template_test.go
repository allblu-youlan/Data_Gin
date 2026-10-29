package data_svc

import (
	"reflect"
	"strings"
	"testing"
)

func TestTableEmailRendersRecipientsAndRecordFields(t *testing.T) {
	input := TableEmailRuleInput{RecipientsTemplate: "ops@example.com, {{email}}; {{EMAIL}}", SubjectTemplate: "新订单 {{ID}}", BodyTemplate: "客户：{{NAME}}\n金额：{{AMOUNT}}"}
	got, err := renderTableEmail(input, map[string]any{"ID": "9007199254740993", "EMAIL": "buyer@example.com", "NAME": "<张三>", "AMOUNT": "12.50"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Recipients, []string{"ops@example.com", "buyer@example.com"}) || got.Subject != "新订单 9007199254740993" || got.Body != "客户：<张三>\n金额：12.50" {
		t.Fatalf("unexpected rendering: %#v", got)
	}
	if body := tableEmailHTML(got.Body); strings.Contains(body, "<张三>") || !strings.Contains(body, "&lt;张三&gt;") || !strings.Contains(body, "<br>") {
		t.Fatalf("unsafe HTML: %s", body)
	}
}

func TestTableEmailRejectsMissingFieldsAndInvalidRecipients(t *testing.T) {
	for _, value := range []string{"", "not-an-email", "a@example.com\r\nBcc: b@example.com", "a@example.com,b@example.com"} {
		_, err := renderTableEmail(TableEmailRuleInput{RecipientsTemplate: "{{EMAIL}}", SubjectTemplate: "新记录", BodyTemplate: "内容"}, map[string]any{"EMAIL": value})
		if err == nil {
			t.Errorf("accepted row email %q", value)
		}
	}
	for _, body := range []string{"{{MISSING}}", "{{NAME", "{{NAME}} }}", "{{if NAME}}"} {
		_, err := renderTableEmail(TableEmailRuleInput{RecipientsTemplate: "a@example.com", SubjectTemplate: "新记录", BodyTemplate: body}, map[string]any{"NAME": "张三"})
		if err == nil {
			t.Errorf("accepted invalid template %q", body)
		}
	}
}

func TestTableEmailNormalizesAndValidatesRule(t *testing.T) {
	input := TableEmailRuleInput{Name: "提醒", Owner: "erp", TableName: "orders", CursorColumn: "id", RecipientsTemplate: "{{EMAIL}}", SubjectTemplate: "订单 {{ID}}", BodyTemplate: "{{NAME}}", PollIntervalSeconds: 60}
	columns := []OfficeResultColumn{{Name: "ID", DataType: "NUMBER"}, {Name: "EMAIL", DataType: "VARCHAR2"}, {Name: "NAME", DataType: "VARCHAR2"}}
	got, err := normalizeTableEmailRule(input, columns)
	if err != nil {
		t.Fatal(err)
	}
	if got.Owner != "ERP" || got.TableName != "ORDERS" || got.CursorColumn != "ID" {
		t.Fatalf("not normalized: %#v", got)
	}
	input.BodyTemplate = "{{UNKNOWN}}"
	if _, err := normalizeTableEmailRule(input, columns); err == nil {
		t.Fatal("accepted unknown column")
	}
	input.BodyTemplate = "{{NAME}}"
	input.CursorColumn = "NAME"
	if _, err := normalizeTableEmailRule(input, columns); err == nil {
		t.Fatal("accepted nonnumeric cursor")
	}
}
