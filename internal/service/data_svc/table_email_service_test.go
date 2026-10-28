package data_svc

import (
	"encoding/json"
	"gin-biz-web-api/model"
	"strings"
	"testing"
	"time"
)

func TestTableEmailDeliveryFreezesRecipientAndContent(t *testing.T) {
	rule := model.TableEmailRule{BaseModel: model.BaseModel{ID: 7}, Generation: 2, CursorColumn: "ID", RecipientsTemplate: "{{EMAIL}}", SubjectTemplate: "新订单 {{ID}}", BodyTemplate: "{{NAME}}"}
	row := map[string]any{"ID": "9007199254740993", "EMAIL": "buyer@example.com", "NAME": "张三"}
	delivery, err := newTableEmailDelivery(rule, row)
	if err != nil {
		t.Fatal(err)
	}
	row["EMAIL"] = "changed@example.com"
	if delivery.Status != "queued" || string(delivery.RecipientsJSON) != `["buyer@example.com"]` || delivery.Subject != "新订单 9007199254740993" || delivery.Generation != 2 {
		t.Fatalf("delivery: %#v", delivery)
	}
	delivery.ID = 19
	outbox, err := tableEmailOutbox(delivery, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(outbox.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["delivery_id"] != float64(19) || strings.Contains(string(outbox.PayloadJSON), "buyer") {
		t.Fatalf("unsafe task: %#v", outbox)
	}
	delivery.RetryVersion++
	retry, err := tableEmailOutbox(delivery, time.Now())
	if err != nil || retry.TaskKey == outbox.TaskKey {
		t.Fatal("retry must have a new outbox identity")
	}
}

func TestTableEmailBadRecipientBecomesFailedDelivery(t *testing.T) {
	rule := model.TableEmailRule{BaseModel: model.BaseModel{ID: 7}, Generation: 1, CursorColumn: "ID", RecipientsTemplate: "{{EMAIL}}", SubjectTemplate: "新订单", BodyTemplate: "通知"}
	delivery, err := newTableEmailDelivery(rule, map[string]any{"ID": "12", "EMAIL": "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "failed" || delivery.LastError == "" || delivery.SourceValue != "12" {
		t.Fatalf("invalid row not recorded: %#v", delivery)
	}
}

func TestTableEmailResetOnlyOnNewSourceOrReenable(t *testing.T) {
	old := model.TableEmailRule{BaseModel: model.BaseModel{ID: 1}, DatasourceID: 2, Owner: "ERP", TableNameValue: "ORDERS", CursorColumn: "ID", Enabled: true}
	input := tableEmailInput(old)
	if tableEmailNeedsBaseline(old, input) {
		t.Fatal("ordinary edit would skip new records")
	}
	input.BodyTemplate = "new text"
	if tableEmailNeedsBaseline(old, input) {
		t.Fatal("template edit would skip new records")
	}
	input.TableName = "OTHER"
	if !tableEmailNeedsBaseline(old, input) {
		t.Fatal("table switch retained old cursor")
	}
	input = tableEmailInput(old)
	old.Enabled = false
	if !tableEmailNeedsBaseline(old, input) {
		t.Fatal("reenable would send paused records")
	}
}
