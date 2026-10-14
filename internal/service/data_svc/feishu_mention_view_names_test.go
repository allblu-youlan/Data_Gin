package data_svc

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"gin-biz-web-api/model"
)

type mentionViewNamesFunc func(context.Context, []string) (map[string]string, error)

func (f mentionViewNamesFunc) UserNames(ctx context.Context, ids []string) (map[string]string, error) {
	return f(ctx, ids)
}

func TestFeishuMentionViewEnrichesLegacySendersOnCurrentPage(t *testing.T) {
	s, _, _ := mentionViewTestService(t)
	var calls atomic.Int32
	s.SetUserNamesClient(mentionViewNamesFunc(func(_ context.Context, ids []string) (map[string]string, error) {
		calls.Add(1)
		if len(ids) > 10 {
			t.Fatal("directory batch exceeds API limit")
		}
		names := make(map[string]string)
		for _, id := range ids {
			names[id] = "姓名" + id
		}
		return names, nil
	}))
	snapshot := model.FeishuDigestSnapshot{MessageCount: 23}
	for i := 0; i < 23; i++ {
		snapshot.Messages = append(snapshot.Messages, model.FeishuDigestMessage{SenderOpenID: fmt.Sprintf("ou_sender%d", i), DisplayText: "原文"})
	}
	snapshot.Messages[1].SenderOpenID = snapshot.Messages[0].SenderOpenID
	snapshot.Messages[2].SenderName = "已有姓名"
	dto := mentionViewReportDTO(42, snapshot, 1)
	s.fillSenderNames(t.Context(), dto)
	if calls.Load() != 2 || dto.Messages[0].SenderName != "姓名ou_sender0" || dto.Messages[1].SenderName != dto.Messages[0].SenderName || dto.Messages[2].SenderName != "已有姓名" || dto.SenderNameNotice != "" {
		t.Fatalf("wrong page enrichment: calls=%d dto=%+v", calls.Load(), dto)
	}
	if snapshot.Messages[0].SenderName != "" {
		t.Fatal("frozen snapshot mutated")
	}
	dto = mentionViewReportDTO(42, snapshot, 1)
	s.fillSenderNames(t.Context(), dto)
	if calls.Load() != 2 {
		t.Fatal("repeat page did not reuse names")
	}
	dto = mentionViewReportDTO(42, snapshot, 2)
	s.fillSenderNames(t.Context(), dto)
	if calls.Load() != 3 || len(dto.Messages) != 3 || dto.Messages[0].SenderName != "姓名ou_sender20" {
		t.Fatal("remaining page not enriched independently")
	}
}

func TestFeishuMentionViewDirectoryFailureKeepsMessageReadable(t *testing.T) {
	s, _, _ := mentionViewTestService(t)
	s.SetUserNamesClient(mentionViewNamesFunc(func(_ context.Context, _ []string) (map[string]string, error) {
		return nil, errors.New("private provider error")
	}))
	dto := &FeishuMentionViewReportDTO{Messages: []FeishuMentionViewMessageDTO{{SenderOpenID: "ou_sender", DisplayText: "完整正文"}, {SenderName: "已保存姓名", DisplayText: "另一条正文"}}}
	s.fillSenderNames(t.Context(), dto)
	if dto.Messages[0].DisplayText != "完整正文" || dto.Messages[1].SenderName != "已保存姓名" || dto.SenderNameNotice == "" {
		t.Fatal("directory failure blocked content")
	}
	other, _, _ := mentionViewTestService(t)
	other.fillSenderNames(t.Context(), dto)
	if dto.Messages[0].SenderName != "" {
		t.Fatal("name cache leaked across services")
	}
}
