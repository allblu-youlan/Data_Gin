package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestMentionReceiverMapsSDKEventAndApplicationIdentity(t *testing.T) {
	raw := `{"schema":"2.0","header":{"app_id":"cli_app","tenant_key":"tenant_a"},"event":{"sender":{"sender_type":"user","sender_id":{"open_id":"ou_sender"}},"message":{"message_id":"om_source","chat_id":"oc_group","chat_type":"group","create_time":"1791856800123","message_type":"text","content":"{\"text\":\"@_user_1 hi\"}","mentions":[{"key":"@_user_1","id":{"open_id":"ou_person"},"name":"张三","tenant_key":"external_tenant"},{"key":"@_user_2","id":{"open_id":"cli_bot"},"mentioned_type":"bot"}]}}}`
	var event larkim.P2MessageReceiveV1
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatal(err)
	}
	input, err := adaptMentionEvent("cli_app", &event)
	if err != nil {
		t.Fatal(err)
	}
	if input.CreatedAt.UnixMilli() != 1791856800123 || input.SenderOpenID != "ou_sender" || input.Mentions[0].MentionedType != "user" || input.Mentions[0].TenantKey != "external_tenant" || input.Mentions[1].MentionedType != "bot" {
		t.Fatalf("mapped=%+v", input)
	}
	if _, err := adaptMentionEvent("other_app", &event); err == nil {
		t.Fatal("cross-app event accepted")
	}
	called := false
	r := &MentionReceiver{appID: "cli_app", archive: func(ctx context.Context, _ MentionEvent) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > mentionCallbackTimeout {
			t.Fatal("missing deadline")
		}
		return errors.New("database detail must not escape")
	}}
	if err := r.onMessage(t.Context(), &event); err == nil || err.Error() != "feishu mention: archive failed" || !called {
		t.Fatalf("archive err=%v", err)
	}
	if r.Status().LastErrorAt == nil || r.Status().LastEventAt == nil {
		t.Fatal("missing lifecycle observation")
	}
}

func TestMentionReceiverCallbackContract(t *testing.T) {
	raw := `{"schema":"2.0","header":{"app_id":"cli_app"},"event":{"operator":{"open_id":"ou_person"},"action":{"value":{"action":"confirm_view","recipient_record_id":"125","payload_version":1}},"context":{"open_message_id":"om_card"}}}`
	var event callback.CardActionTriggerEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatal(err)
	}
	input, err := adaptMentionConfirmation("cli_app", &event)
	if err != nil || input.RecipientRecordID != 125 || input.MessageID != "om_card" || input.OperatorOpenID != "ou_person" {
		t.Fatalf("callback=%+v err=%v", input, err)
	}
	event.Event.Action.Value["recipient_record_id"] = float64(125)
	if _, err := adaptMentionConfirmation("cli_app", &event); err == nil {
		t.Fatal("numeric callback ID accepted instead of decimal string")
	}
	event.Event.Action.Value["recipient_record_id"] = "125"
	r := &MentionReceiver{appID: "cli_app", confirm: func(context.Context, MentionConfirmation) (string, error) {
		return "个人提醒与确认功能未启用", errors.New("disabled")
	}}
	response, err := r.onCard(t.Context(), &event)
	if err != nil || response.Toast.Type != "error" || response.Toast.Content != "个人提醒与确认功能未启用" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
