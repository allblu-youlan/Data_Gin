//go:build integration

package data_svc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gin-biz-web-api/model"
)

func TestFeishuMentionMySQLRetentionPassesBlockedReportsAndKeepsConnectedSets(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	archivedAt := *now
	windowEnd := archivedAt.Add(8 * time.Hour)
	sources := make([]model.FeishuSourceMessage, 1001)
	for i := range sources {
		sources[i] = model.FeishuSourceMessage{
			AppID: s.appID, MessageID: fmt.Sprintf("retention_%d", i), ChatID: "chat_a", SenderOpenID: "sender",
			MessageType: "text", MessageCreatedAt: archivedAt, ReceivedAt: archivedAt,
			RawContent: model.JSONText(`{"text":"旧消息"}`), DisplayText: "旧消息", MentionsSnapshot: model.JSONText(`[]`),
		}
	}
	if err := s.db.CreateInBatches(&sources, 100).Error; err != nil {
		t.Fatal(err)
	}
	reports := make([]model.FeishuDailyReport, 1003)
	for i := range reports {
		reports[i] = model.FeishuDailyReport{
			AppID: s.appID, ChatID: "chat_a", RecipientOpenID: fmt.Sprintf("recipient_%d", i), ReportDate: "2026-10-13",
			WindowStart: archivedAt, WindowEnd: windowEnd, GeneratedAt: windowEnd, Status: "sent", SentAt: &windowEnd,
			ContentSnapshot: model.JSONText(`{}`),
		}
	}
	if err := s.db.CreateInBatches(&reports, 100).Error; err != nil {
		t.Fatal(err)
	}
	recipients := make([]model.FeishuMessageRecipient, len(reports))
	for i := range recipients {
		sourceIndex := i
		status := "pending"
		switch {
		case i == 999 || i == 1000:
			// Two eligible reports share a source across the 1000-report page.
			sourceIndex, status = 999, "not_required"
		case i == 1001:
			// This independent report must be visited by the next page.
			sourceIndex, status = 1000, "not_required"
		case i == 1002:
			// A confirmed recipient cannot release the source of pending person 0.
			sourceIndex, status = 0, "confirmed"
		}
		recipients[i] = model.FeishuMessageRecipient{
			SourceMessageID: sources[sourceIndex].ID, RecipientOpenID: reports[i].RecipientOpenID,
			ViewStatus: status, FirstReportID: &reports[i].ID,
		}
	}
	if err := s.db.CreateInBatches(&recipients, 100).Error; err != nil {
		t.Fatal(err)
	}
	*now = archivedAt.AddDate(0, 0, 91)
	for attempt := 0; attempt < 2; attempt++ {
		if err := s.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		var reportCount, sourceCount, recipientCount, dangling int64
		if err := s.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", s.appID).Count(&reportCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.db.Model(&model.FeishuSourceMessage{}).Where("app_id = ?", s.appID).Count(&sourceCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.db.Table("feishu_message_recipients r").Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").Where("m.app_id = ?", s.appID).Count(&recipientCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.db.Table("feishu_message_recipients r").Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").Joins("LEFT JOIN feishu_daily_reports report ON report.id = r.first_report_id").Where("m.app_id = ? AND r.first_report_id IS NOT NULL AND report.id IS NULL", s.appID).Count(&dangling).Error; err != nil {
			t.Fatal(err)
		}
		if reportCount != 1000 || sourceCount != 999 || recipientCount != 1000 || dangling != 0 {
			t.Fatalf("cleanup %d: reports=%d sources=%d recipients=%d dangling=%d", attempt, reportCount, sourceCount, recipientCount, dangling)
		}
	}
}
