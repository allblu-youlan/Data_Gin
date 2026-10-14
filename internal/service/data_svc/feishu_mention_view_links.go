package data_svc

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/model"
)

type FeishuMentionMessageDetailsClient interface {
	MessageDetails(context.Context, []string) (map[string]feishu.MessageDetail, error)
}

// SetMessageDetailsClient is called at startup. The client only supplements
// display metadata after report authorization, never the archived message text.
func (s *FeishuMentionViewService) SetMessageDetailsClient(client FeishuMentionMessageDetailsClient) {
	s.messages = client
}

var mentionChatIDPattern = regexp.MustCompile(`^oc_[A-Za-z0-9_-]{1,125}$`)

func (s *FeishuMentionViewService) fillMessageDetails(ctx context.Context, dto *FeishuMentionViewReportDTO, snapshot model.FeishuDigestSnapshot, page int) {
	if mentionChatIDPattern.MatchString(snapshot.ChatID) {
		link := "https://applink.feishu.cn/client/chat/open?" + url.Values{"openChatId": {snapshot.ChatID}}.Encode()
		dto.Report.ChatURL = &link
	}
	if s.messages == nil || len(dto.Messages) == 0 {
		return
	}
	start := (page - 1) * mentionViewPageSize
	ids := make([]string, 0, len(dto.Messages))
	seen := make(map[string]bool)
	for i, message := range dto.Messages {
		id := snapshot.Messages[start+i].MessageID
		if (message.OriginalURL == nil || strings.TrimSpace(message.SenderName) == "") && id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	details, err := s.messages.MessageDetails(ctx, ids)
	if err != nil {
		return
	}
	for i := range dto.Messages {
		message := &dto.Messages[i]
		detail := details[snapshot.Messages[start+i].MessageID]
		if strings.TrimSpace(message.SenderName) == "" {
			message.SenderName = strings.TrimSpace(detail.SenderName)
		}
		if message.OriginalURL == nil && detail.OriginalURL != "" {
			message.OriginalURL = mentionViewOriginalURL(&detail.OriginalURL)
		}
	}
}
