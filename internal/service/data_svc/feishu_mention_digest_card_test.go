package data_svc

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

func TestFeishuMentionCompactDigestBoundsPreviewAndUsesProfile(t *testing.T) {
	now := time.Date(2026, 10, 14, 18, 0, 0, 0, mentionLocation())
	snapshot := model.FeishuDigestSnapshot{ChatName: "测试群", RecipientOpenID: "ou_recipient", ReportDate: "2026-10-14", WindowStart: now.Add(-24 * time.Hour), WindowEnd: now, MessageCount: 1000}
	for i := 0; i < 1000; i++ {
		snapshot.Messages = append(snapshot.Messages, model.FeishuDigestMessage{SenderOpenID: "ou_sender", MessageCreatedAt: now, DisplayText: "@易理志"})
	}
	snapshot.Messages[1].DisplayText = strings.Repeat("长消息\n", 5000) + "完整内容末尾"
	raw, err := compactMentionDigestCard(snapshot, 42, "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(raw, `"tag":"person"`) != 3 || !strings.Contains(raw, `"show_name":true`) || !strings.Contains(raw, "@易理志") || !strings.Contains(raw, "997 条消息") || strings.Contains(raw, "完整内容末尾") {
		t.Fatalf("invalid bounded preview: %s", raw)
	}
	if !strings.Contains(snapshot.Messages[1].DisplayText, "完整内容末尾") {
		t.Fatal("full snapshot was truncated")
	}
	var card struct {
		Config struct {
			Width string `json:"width_mode"`
		}
		Elements []struct{ Actions []struct{ URL string } }
	}
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatal(err)
	}
	if card.Config.Width != "compact" || len(raw) > 8000 {
		t.Fatalf("unbounded card: width=%s size=%d", card.Config.Width, len(raw))
	}
	link, err := url.Parse(card.Elements[len(card.Elements)-1].Actions[0].URL)
	if err != nil || link.Host != "applink.feishu.cn" || link.Query().Get("mode") != "sidebar-semi" || link.Query().Get("url") != "https://example.test/api/v1/feishu-mention-view/reports/42" {
		t.Fatalf("invalid sidebar link: %v %v", link, err)
	}
}

func TestFeishuMentionCompactDigestRejectsInvalidPublicURL(t *testing.T) {
	for _, base := range []string{"", "javascript:alert(1)", "https://user:pass@example.test", "https://example.test/?secret=x", "https://example.test/#x"} {
		if _, err := compactMentionDigestCard(model.FeishuDigestSnapshot{}, 1, base); err == nil {
			t.Fatalf("accepted %q", base)
		}
	}
}
