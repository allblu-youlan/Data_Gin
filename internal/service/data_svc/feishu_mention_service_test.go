package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/model"
)

func TestFeishuMentionParsing(t *testing.T) {
	mentions := []FeishuMention{{Key: "@_user_1", OpenID: "ou_a", Name: "张三", MentionedType: "user"}, {Key: "@_user_10", OpenID: "ou_b", Name: "李四", MentionedType: "user"}}
	tests := []struct {
		name, kind, raw, want string
		invalid               bool
	}{
		{"text placeholders", "text", `{"text":"@_user_10 和 @_user_1 请查看"}`, "@李四 和 @张三 请查看", false},
		{"received post", "post", `{"title":"任务","content":[[{"tag":"at","user_id":"ou_a"},{"tag":"text","text":"查看"},{"tag":"a","text":"链接","href":"https://example.com"}],[{"tag":"img","image_key":"secret"}]]}`, "任务\n@张三查看链接 (https://example.com)\n[图片]", false},
		{"localized post", "post", `{"zh_cn":{"title":"标题","content":[[{"tag":"text","text":"正文"}]]}}`, "标题\n正文", false},
		{"attachment placeholder", "file", `{"file_key":"secret"}`, "[file 消息]", false},
		{"invalid json", "text", `{`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFeishuMentionContent(tt.kind, tt.raw, mentions)
			if (err != nil) != tt.invalid || got != tt.want {
				t.Fatalf("got %q, %v; want %q invalid=%v", got, err, tt.want, tt.invalid)
			}
		})
	}
}
func TestFeishuMentionDeduplicatesOnlyUsers(t *testing.T) {
	got := uniqueFeishuMentions([]FeishuMention{{OpenID: "ou_a", MentionedType: "user"}, {OpenID: "ou_a", MentionedType: "user"}, {OpenID: "all", MentionedType: "user"}, {OpenID: "bot", MentionedType: "bot"}, {OpenID: "ou_b", MentionedType: "user", TenantKey: "external"}})
	if len(got) != 2 || got[1].OpenID != "ou_b" || got[1].TenantKey != "external" {
		t.Fatalf("mentions = %#v", got)
	}
}
func TestFeishuMentionCutoff(t *testing.T) {
	for _, tt := range []struct{ name, now, want string }{{"first activation", "2026-10-13T10:00:00+08:00", "2026-10-13T18:00:00+08:00"}, {"exact cutoff", "2026-10-13T18:00:00+08:00", "2026-10-14T18:00:00+08:00"}, {"year boundary", "2026-12-31T23:00:00+08:00", "2027-01-01T18:00:00+08:00"}} {
		t.Run(tt.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tt.now)
			want, _ := time.Parse(time.RFC3339, tt.want)
			got, err := nextMentionCutoff(now, "18:00")
			if err != nil || !got.Equal(want) {
				t.Fatalf("cutoff=%s err=%v", got, err)
			}
			if _, offset := got.Zone(); offset != 8*60*60 {
				t.Fatalf("cutoff is not in Shanghai: %s", got)
			}
		})
	}
}
func TestFeishuMentionCardsTreatOriginalAsPlainText(t *testing.T) {
	source := model.FeishuSourceMessage{DisplayText: `<at id="all"></at> **正文**`, SenderName: "王五", MessageCreatedAt: time.Now()}
	recipient := model.FeishuMessageRecipient{BaseModel: model.BaseModel{ID: 42}}
	raw, err := mentionReminderCard(source, recipient, "采购群", false, false)
	if err != nil {
		t.Fatal(err)
	}
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatal(err)
	}
	elements := card["elements"].([]any)
	node := elements[0].(map[string]any)["text"].(map[string]any)
	if node["tag"] != "plain_text" || !strings.Contains(node["content"].(string), source.DisplayText) {
		t.Fatalf("card altered content: %s", raw)
	}
	updated, err := confirmedMentionCard(raw)
	if err != nil || strings.Contains(updated, `"tag":"button"`) || !strings.Contains(updated, "已确认查看") {
		t.Fatalf("confirmation card=%s err=%v", updated, err)
	}
}
func TestFeishuMentionDigestSplitsWithoutLosingContent(t *testing.T) {
	body := strings.Repeat("😀中文\"<&\\\n", 7000)
	snapshot := model.FeishuDigestSnapshot{ChatName: "采购群", RecipientName: "张三", RecipientOpenID: "ou_a", MessageCount: 1, WindowStart: time.Now(), WindowEnd: time.Now(), Messages: []model.FeishuDigestMessage{{RecipientRecordID: 42, SenderName: "王五", MessageCreatedAt: time.Now(), DisplayText: body}}}
	parts, err := buildMentionDigestParts(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 {
		t.Fatalf("wanted multiple parts, got %d", len(parts))
	}
	var rebuilt strings.Builder
	expectedFragment := 1
	for i, part := range parts {
		if part.PartNo != uint(i+1) {
			t.Fatalf("wrong part %d", part.PartNo)
		}
		card, err := mentionDigestCard(snapshot, part)
		if err != nil || mentionDigestBodySize(snapshot.RecipientOpenID, card) > mentionDigestMaxBodyBytes {
			t.Fatalf("oversize or invalid card: %v", err)
		}
		if !strings.Contains(card, "王五") {
			t.Fatal("sender omitted")
		}
		for _, fragment := range part.Fragments {
			if fragment.RecipientRecordID != 42 || fragment.FragmentNo != expectedFragment || !utf8.ValidString(fragment.Text) {
				t.Fatalf("bad fragment %#v", fragment)
			}
			rebuilt.WriteString(fragment.Text)
			expectedFragment++
		}
	}
	if rebuilt.String() != body {
		t.Fatal("digest lost source content")
	}
}
func TestFeishuMentionDisabledDoesNotAccessDatabase(t *testing.T) {
	s := NewFeishuMentionService(nil, "app", appConfig.FeishuMentionConfig{})
	if err := s.Archive(context.Background(), FeishuMentionEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanDigests(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Confirm(context.Background(), FeishuMentionConfirmation{}); !errors.Is(err, ErrFeishuMentionDisabled) {
		t.Fatalf("confirm = %v", err)
	}
}

func TestFeishuMentionReminderPreviewIsExplicitAndBounded(t *testing.T) {
	source := model.FeishuSourceMessage{DisplayText: strings.Repeat("<中文>\\\"", 10000), SenderOpenID: "ou_sender", MessageCreatedAt: time.Now()}
	raw, err := mentionReminderCard(source, model.FeishuMessageRecipient{BaseModel: model.BaseModel{ID: 1}}, "来源群", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "内容已截取") || !strings.Contains(raw, "群成员") || mentionDigestBodySize("recipient", raw) > mentionDigestMaxBodyBytes {
		t.Fatalf("invalid reminder preview size=%d err=%v", mentionDigestBodySize("recipient", raw), err)
	}
}

func TestFeishuMentionBusinessClockUsesShanghai(t *testing.T) {
	service := NewFeishuMentionService(nil, "app", appConfig.FeishuMentionConfig{})
	if _, offset := service.now().Zone(); offset != 8*60*60 {
		t.Fatal("business clock must use Shanghai")
	}
}

func TestFeishuMentionChangedDigestCutoff(t *testing.T) {
	start := time.Date(2026, 10, 13, 10, 0, 0, 0, mentionLocation())
	schedule := model.FeishuDigestSchedule{NextWindowStartAt: start, NextCutoffAt: start.Add(8 * time.Hour)}
	for _, tt := range []struct {
		clock string
		want  time.Time
	}{
		{"17:00", start.Add(7 * time.Hour)},
		{"20:00", start.Add(10 * time.Hour)},
		{"08:00", start.Add(22 * time.Hour)},
		{"10:00", start.Add(24 * time.Hour)},
	} {
		t.Run(tt.clock, func(t *testing.T) {
			got, err := rescheduleMentionCutoff(schedule, tt.clock)
			if err != nil || !got.Equal(tt.want) || !schedule.NextWindowStartAt.Equal(start) {
				t.Fatalf("cutoff=%s err=%v want=%s", got, err, tt.want)
			}
		})
	}
	if _, err := rescheduleMentionCutoff(schedule, "25:00"); err == nil {
		t.Fatal("accepted invalid digest clock")
	}
}

func TestFeishuMentionRetryClassification(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"invalid credentials", &feishu.TokenError{Retryable: false}, false},
		{"wrapped token outage", fmt.Errorf("token: %w", &feishu.TokenError{Retryable: true}), true},
		{"missing permission", &feishu.BotError{Retryable: false}, false},
		{"rate limit", &feishu.BotError{Retryable: true}, true},
		{"network failure", errors.New("temporary network failure"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := feishuMentionRetryable(tt.err); got != tt.want {
				t.Fatalf("retry=%t want=%t", got, tt.want)
			}
		})
	}
}
