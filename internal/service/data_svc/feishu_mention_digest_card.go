package data_svc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"gin-biz-web-api/model"
)

// A single compact card keeps chat height bounded. The report snapshot remains
// the complete source for the authenticated, paginated detail view.
func compactMentionDigestCard(snapshot model.FeishuDigestSnapshot, reportID uint, publicURL string) (string, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(publicURL), "/"))
	if err != nil || base.Hostname() == "" || (base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1" || base.Hostname() == "::1"))) || base.User != nil || base.RawQuery != "" || base.Fragment != "" || reportID == 0 {
		return "", errors.New("feishu mention: configure a valid public App.Url for report details")
	}
	base.Path += fmt.Sprintf("/api/v1/feishu-mention-view/reports/%d", reportID)
	base.RawPath = ""
	detailURL := "https://applink.feishu.cn/client/web_url/open?" + url.Values{"mode": {"sidebar-semi"}, "url": {base.String()}}.Encode()
	plain := func(text string) map[string]any {
		return map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": text}}
	}
	elements := []any{plain(fmt.Sprintf("%s · 共 %d 条消息\n%s 至 %s（上海时间）", snapshot.ReportDate, snapshot.MessageCount, snapshot.WindowStart.In(mentionLocation()).Format("01-02 15:04"), snapshot.WindowEnd.In(mentionLocation()).Format("01-02 15:04")))}
	for _, message := range snapshot.Messages[:min(3, len(snapshot.Messages))] {
		elements = append(elements, map[string]any{"tag": "hr"})
		if message.SenderOpenID != "" {
			elements = append(elements, map[string]any{"tag": "person", "user_id": message.SenderOpenID, "show_name": true, "show_avatar": true})
		} else {
			name := message.SenderName
			if name == "" {
				name = "发送人"
			}
			elements = append(elements, plain(mentionDigestPreview(name, 50)))
		}
		elements = append(elements, plain(message.MessageCreatedAt.In(mentionLocation()).Format("15:04:05")+"\n"+mentionDigestPreview(message.DisplayText, 100)))
	}
	if len(snapshot.Messages) > 3 {
		elements = append(elements, plain(fmt.Sprintf("另有 %d 条消息，点击查看详情阅读全部内容", len(snapshot.Messages)-3)))
	}
	elements = append(elements, map[string]any{"tag": "action", "actions": []any{map[string]any{"tag": "button", "type": "primary", "text": map[string]any{"tag": "plain_text", "content": "查看详情"}, "url": detailURL}}})
	raw, err := json.Marshal(map[string]any{
		"config":   map[string]any{"width_mode": "compact", "update_multi": true},
		"header":   map[string]any{"title": map[string]any{"tag": "plain_text", "content": mentionDigestPreview(snapshot.ChatName, 40) + "｜@ 消息日报"}},
		"elements": elements,
	})
	if err != nil {
		return "", err
	}
	if mentionDigestBodySize(snapshot.RecipientOpenID, string(raw)) > mentionDigestMaxBodyBytes {
		return "", errors.New("feishu mention: digest card too large")
	}
	return string(raw), nil
}

func mentionDigestPreview(text string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
