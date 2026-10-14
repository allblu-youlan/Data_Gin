package data_svc

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/model"
)

type mentionViewDetailsFunc func(context.Context, []string) (map[string]feishu.MessageDetail, error)

func (f mentionViewDetailsFunc) MessageDetails(ctx context.Context, ids []string) (map[string]feishu.MessageDetail, error) {
	return f(ctx, ids)
}

func TestFeishuMentionViewEnrichesLegacyMessageLinksOnCurrentPage(t *testing.T) {
	s, _, _ := mentionViewTestService(t)
	link := "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=42"
	snapshot := model.FeishuDigestSnapshot{ChatID: "oc_group", MessageCount: 24}
	for i := 0; i < 24; i++ {
		snapshot.Messages = append(snapshot.Messages, model.FeishuDigestMessage{MessageID: fmt.Sprintf("om_%d", i), SenderOpenID: "ou_sender", DisplayText: "冻结的原文"})
	}
	snapshot.Messages[21].MessageID = "om_20"
	snapshot.Messages[22].SenderName = "已保存姓名"
	snapshot.Messages[22].OriginalURL = &link
	snapshot.Messages[23].OriginalURL = &link
	calls := 0
	s.SetMessageDetailsClient(mentionViewDetailsFunc(func(ctx context.Context, ids []string) (map[string]feishu.MessageDetail, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("metadata request has no deadline")
		}
		if !reflect.DeepEqual(ids, []string{"om_20", "om_23"}) {
			t.Fatalf("requested messages outside current page or duplicated IDs: %v", ids)
		}
		return map[string]feishu.MessageDetail{
			"om_20": {SenderName: " 李慧娜 ", OriginalURL: link},
			"om_23": {SenderName: "易理志", OriginalURL: "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=99"},
		}, nil
	}))
	dto := mentionViewReportDTO(42, snapshot, 2)
	s.fillMessageDetails(t.Context(), dto, snapshot, 2)
	s.fillSenderNames(t.Context(), dto)
	if calls != 1 || len(dto.Messages) != 4 || dto.SenderNameNotice != "" {
		t.Fatalf("incorrect enrichment: calls=%d dto=%+v", calls, dto)
	}
	for i, message := range dto.Messages {
		if message.OriginalURL == nil || *message.OriginalURL != link || message.DisplayText != "冻结的原文" {
			t.Fatalf("wrong metadata or modified content at %d: %+v", i, message)
		}
	}
	if dto.Messages[0].SenderName != "李慧娜" || dto.Messages[1].SenderName != "李慧娜" || dto.Messages[2].SenderName != "已保存姓名" || dto.Messages[3].SenderName != "易理志" {
		t.Fatalf("sender names not retained or enriched: %+v", dto.Messages)
	}
	if snapshot.Messages[20].OriginalURL != nil || snapshot.Messages[20].SenderName != "" {
		t.Fatal("frozen snapshot was modified")
	}
	if dto.Report.ChatURL == nil || *dto.Report.ChatURL != "https://applink.feishu.cn/client/chat/open?openChatId=oc_group" {
		t.Fatal("missing original group fallback")
	}
}

func TestFeishuMentionViewMessageLookupFailureKeepsContentAndDirectoryFallback(t *testing.T) {
	s, _, _ := mentionViewTestService(t)
	s.SetMessageDetailsClient(mentionViewDetailsFunc(func(context.Context, []string) (map[string]feishu.MessageDetail, error) {
		return nil, errors.New("provider unavailable")
	}))
	s.SetUserNamesClient(mentionViewNamesFunc(func(context.Context, []string) (map[string]string, error) {
		return map[string]string{"ou_sender": "易理志"}, nil
	}))
	snapshot := model.FeishuDigestSnapshot{ChatID: "oc_group", Messages: []model.FeishuDigestMessage{{MessageID: "om_one", SenderOpenID: "ou_sender", DisplayText: "完整正文"}}}
	dto := mentionViewReportDTO(42, snapshot, 1)
	s.fillMessageDetails(t.Context(), dto, snapshot, 1)
	s.fillSenderNames(t.Context(), dto)
	if dto.Messages[0].DisplayText != "完整正文" || dto.Messages[0].SenderName != "易理志" || dto.Messages[0].OriginalURL != nil || dto.Report.ChatURL == nil {
		t.Fatalf("metadata failure must preserve content and group fallback: %+v", dto)
	}
}

func TestFeishuMentionViewDoesNotInventMessagePositions(t *testing.T) {
	s, _, _ := mentionViewTestService(t)
	calls := 0
	s.SetMessageDetailsClient(mentionViewDetailsFunc(func(context.Context, []string) (map[string]feishu.MessageDetail, error) {
		calls++
		return map[string]feishu.MessageDetail{"om_one": {SenderName: "李慧娜"}}, nil
	}))
	snapshot := model.FeishuDigestSnapshot{ChatID: "oc_group", Messages: []model.FeishuDigestMessage{{MessageID: "om_one"}}}
	dto := mentionViewReportDTO(42, snapshot, 1)
	s.fillMessageDetails(t.Context(), dto, snapshot, 1)
	if dto.Messages[0].OriginalURL != nil || dto.Report.ChatURL == nil {
		t.Fatal("group fallback must not masquerade as a message location")
	}
	dto = mentionViewReportDTO(42, snapshot, 2)
	s.fillMessageDetails(t.Context(), dto, snapshot, 2)
	if calls != 1 {
		t.Fatal("empty page made an unnecessary metadata call")
	}
}
