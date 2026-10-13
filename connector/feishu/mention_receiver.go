package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type Mention struct{ Key, OpenID, Name, MentionedType, TenantKey string }
type MentionEvent struct {
	AppID, TenantKey, MessageID, ChatID, ChatType, SenderType, SenderOpenID, SenderName, MessageType, Content string
	CreatedAt                                                                                                 time.Time
	Mentions                                                                                                  []Mention
}
type MentionConfirmation struct {
	AppID, OperatorOpenID, MessageID, Action string
	RecipientRecordID                        uint
	PayloadVersion                           int
}
type MentionReceiverStatus struct {
	Connected                bool
	LastEventAt, LastErrorAt *time.Time
}
type MentionReceiver struct {
	appID   string
	client  *larkws.Client
	archive func(context.Context, MentionEvent) error
	confirm func(context.Context, MentionConfirmation) (string, error)
	mu      sync.RWMutex
	status  MentionReceiverStatus
}

// Business work has a 2.5-second deadline, leaving room for SDK response
// serialization inside Feishu's callback deadline. Network sends happen in workers.
const mentionCallbackTimeout = 2500 * time.Millisecond

func NewMentionReceiver(appID, appSecret string, archive func(context.Context, MentionEvent) error, confirm func(context.Context, MentionConfirmation) (string, error)) (*MentionReceiver, error) {
	if appID == "" || appSecret == "" || archive == nil || confirm == nil {
		return nil, fmt.Errorf("feishu mention: invalid receiver configuration")
	}
	r := &MentionReceiver{appID: appID, archive: archive, confirm: confirm}
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(r.onMessage).OnP2CardActionTrigger(r.onCard)
	r.client = larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(handler), larkws.WithLogger(mentionSDKLogger{}),
		larkws.WithOnReady(func() { r.setConnected(true) }),
		larkws.WithOnReconnected(func() { r.setConnected(true) }),
		larkws.WithOnReconnecting(func() { r.setConnected(false) }),
		larkws.WithOnDisconnected(func() { r.setConnected(false) }),
		larkws.WithOnError(func(error) { r.recordError() }),
	)
	return r, nil
}

func (r *MentionReceiver) Run(ctx context.Context) error {
	defer r.setConnected(false)
	return r.client.Start(ctx)
}
func (r *MentionReceiver) Close(ctx context.Context) error { return r.client.CloseAndWait(ctx) }
func (r *MentionReceiver) Status() MentionReceiverStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := r.status
	if result.LastEventAt != nil {
		stamp := *result.LastEventAt
		result.LastEventAt = &stamp
	}
	if result.LastErrorAt != nil {
		stamp := *result.LastErrorAt
		result.LastErrorAt = &stamp
	}
	return result
}
func (r *MentionReceiver) setConnected(connected bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Connected = connected
}
func (r *MentionReceiver) recordError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.status.LastErrorAt = &now
}
func (r *MentionReceiver) recordEvent() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.status.LastEventAt = &now
}

func (r *MentionReceiver) onMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	input, err := adaptMentionEvent(r.appID, event)
	if err != nil {
		r.recordError()
		return err
	}
	r.recordEvent()
	ctx, cancel := context.WithTimeout(ctx, mentionCallbackTimeout)
	defer cancel()
	if err := r.archive(ctx, input); err != nil {
		r.recordError()
		return fmt.Errorf("feishu mention: archive failed")
	}
	return nil
}

func adaptMentionEvent(appID string, event *larkim.P2MessageReceiveV1) (MentionEvent, error) {
	if event == nil || event.EventV2Base == nil || event.EventV2Base.Header == nil || event.EventV2Base.Header.AppID != appID || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return MentionEvent{}, fmt.Errorf("feishu mention: invalid message event")
	}
	m, s := event.Event.Message, event.Event.Sender
	ms, err := strconv.ParseInt(mentionString(m.CreateTime), 10, 64)
	if err != nil || ms <= 0 {
		return MentionEvent{}, fmt.Errorf("feishu mention: invalid message timestamp")
	}
	input := MentionEvent{AppID: appID, TenantKey: event.EventV2Base.Header.TenantKey, MessageID: mentionString(m.MessageId), ChatID: mentionString(m.ChatId), ChatType: mentionString(m.ChatType), SenderType: mentionString(s.SenderType), MessageType: mentionString(m.MessageType), Content: mentionString(m.Content), CreatedAt: time.UnixMilli(ms).UTC()}
	if s.SenderId != nil {
		input.SenderOpenID = mentionString(s.SenderId.OpenId)
	}
	for _, m := range m.Mentions {
		if m == nil || m.Id == nil {
			continue
		}
		kind, openID := mentionString(m.MentionedType), mentionString(m.Id.OpenId)
		// Older event payloads omit mentioned_type. Only explicit user open IDs
		// qualify; bot application IDs and @all are never expanded.
		if kind == "" && strings.HasPrefix(openID, "ou_") {
			kind = "user"
		}
		input.Mentions = append(input.Mentions, Mention{Key: mentionString(m.Key), OpenID: openID, Name: mentionString(m.Name), MentionedType: kind, TenantKey: mentionString(m.TenantKey)})
	}
	return input, nil
}

func (r *MentionReceiver) onCard(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	input, err := adaptMentionConfirmation(r.appID, event)
	if err != nil {
		return mentionToast("error", "无效的提醒操作"), nil
	}
	r.recordEvent()
	ctx, cancel := context.WithTimeout(ctx, mentionCallbackTimeout)
	defer cancel()
	message, err := r.confirm(ctx, input)
	if err != nil {
		r.recordError()
		return mentionToast("error", message), nil
	}
	return mentionToast("success", message), nil
}

func adaptMentionConfirmation(appID string, event *callback.CardActionTriggerEvent) (MentionConfirmation, error) {
	if event == nil || event.EventV2Base == nil || event.EventV2Base.Header == nil || event.EventV2Base.Header.AppID != appID || event.Event == nil || event.Event.Operator == nil || event.Event.Action == nil || event.Event.Context == nil {
		return MentionConfirmation{}, fmt.Errorf("feishu mention: invalid callback")
	}
	var value struct {
		Action            string `json:"action"`
		RecipientRecordID string `json:"recipient_record_id"`
		PayloadVersion    int    `json:"payload_version"`
	}
	raw, err := json.Marshal(event.Event.Action.Value)
	if err != nil {
		return MentionConfirmation{}, fmt.Errorf("feishu mention: invalid action")
	}
	if err = json.Unmarshal(raw, &value); err != nil {
		return MentionConfirmation{}, fmt.Errorf("feishu mention: invalid action")
	}
	id, err := strconv.ParseUint(value.RecipientRecordID, 10, 64)
	if err != nil || id == 0 || uint64(uint(id)) != id || value.Action != "confirm_view" || value.PayloadVersion != 1 || event.Event.Operator.OpenID == "" || event.Event.Context.OpenMessageID == "" {
		return MentionConfirmation{}, fmt.Errorf("feishu mention: invalid action")
	}
	return MentionConfirmation{AppID: appID, OperatorOpenID: event.Event.Operator.OpenID, MessageID: event.Event.Context.OpenMessageID, Action: value.Action, RecipientRecordID: uint(id), PayloadVersion: value.PayloadVersion}, nil
}

func mentionString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func mentionToast(kind, text string) *callback.CardActionTriggerResponse {
	if text == "" {
		text = "操作失败，请稍后重试"
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: text}}
}

// The SDK logs raw event bodies and credential-bearing connection URLs at
// several levels. Observe lifecycle timestamps instead of forwarding its text.
type mentionSDKLogger struct{}

func (mentionSDKLogger) Debug(context.Context, ...interface{}) {}
func (mentionSDKLogger) Info(context.Context, ...interface{})  {}
func (mentionSDKLogger) Warn(context.Context, ...interface{})  {}
func (mentionSDKLogger) Error(context.Context, ...interface{}) {}
