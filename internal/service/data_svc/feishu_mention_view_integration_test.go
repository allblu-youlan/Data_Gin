//go:build integration

package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/model"
)

func TestFeishuMentionViewMySQLAuthorizesBeforeLoadingSnapshot(t *testing.T) {
	fixture, now := mentionMySQLFixture(t, false)
	s, err := NewFeishuMentionViewService(fixture.db, fixture.appID, "test-shared-secret", "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	create := func(appID, recipient string) model.FeishuDailyReport {
		t.Helper()
		snapshot := model.FeishuDigestSnapshot{
			SchemaVersion: 1, AppID: appID, ChatID: "chat_a", ChatName: "冻结群名", RecipientOpenID: recipient, RecipientName: recipient,
			ReportDate: now.Format("2006-01-02"), WindowStart: now.Add(-time.Hour), WindowEnd: *now, GeneratedAt: *now, MessageCount: 1,
			Messages: []model.FeishuDigestMessage{{MessageID: "om_one", SenderOpenID: "ou_sender", MessageCreatedAt: *now, DisplayText: "该收件人的全文"}},
		}
		body, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		report := model.FeishuDailyReport{AppID: appID, ChatID: "chat_a", RecipientOpenID: recipient, ReportDate: snapshot.ReportDate, WindowStart: snapshot.WindowStart, WindowEnd: *now, GeneratedAt: *now, ContentSnapshot: model.JSONText(body)}
		if err := fixture.db.Create(&report).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fixture.db.Delete(&model.FeishuDailyReport{}, report.ID) })
		return report
	}
	own := create(fixture.appID, "ou_viewer")
	otherRecipient := create(fixture.appID, "ou_other")
	otherApp := create(fixture.appID+"_other", "ou_viewer")
	lookupCalls := 0
	messageCalls := 0
	s.SetMessageDetailsClient(mentionViewDetailsFunc(func(_ context.Context, ids []string) (map[string]feishu.MessageDetail, error) {
		messageCalls++
		return map[string]feishu.MessageDetail{"om_one": {OriginalURL: "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=42"}}, nil
	}))
	s.SetUserNamesClient(mentionViewNamesFunc(func(_ context.Context, ids []string) (map[string]string, error) {
		lookupCalls++
		return map[string]string{"ou_sender": "李慧娜"}, nil
	}))
	if dto, err := s.Report(t.Context(), "ou_viewer", own.ID, 1); err != nil || len(dto.Messages) != 1 || dto.Messages[0].DisplayText != "该收件人的全文" || dto.Messages[0].SenderName != "李慧娜" || dto.Messages[0].OriginalURL == nil {
		t.Fatalf("own report: %+v err=%v", dto, err)
	}
	for _, report := range []model.FeishuDailyReport{otherRecipient, otherApp} {
		// Poison the snapshot to prove denial occurs at the SQL boundary, not after parsing.
		if err := fixture.db.Model(&report).Update("content_snapshot", model.JSONText(`{"unexpected":"private"}`)).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.Report(t.Context(), "ou_viewer", report.ID, 1); !errors.Is(err, ErrFeishuMentionViewNotFound) {
			t.Fatalf("unauthorized snapshot was loaded: %v", err)
		}
	}
	if lookupCalls != 1 {
		t.Fatal("unauthorized report reached directory lookup")
	}
	if messageCalls != 1 {
		t.Fatal("unauthorized report reached message metadata lookup")
	}
}
