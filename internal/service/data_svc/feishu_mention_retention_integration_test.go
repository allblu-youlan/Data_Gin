//go:build integration

package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
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

func TestFeishuMentionMySQLRetentionCleansManualWithoutSources(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	archivedAt := *now
	*now = archivedAt.AddDate(0, 0, s.cfg.RetentionDays+1)
	cutoff := now.AddDate(0, 0, -s.cfg.RetentionDays)
	cases := []struct {
		name          string
		kind          string
		reportStatus  string
		jobStatus     string
		windowEnd     time.Time
		messageCount  int
		outboxPending bool
		wantDeleted   bool
	}{
		{name: "empty_manual", kind: "manual", reportStatus: "sent", jobStatus: "sent", windowEnd: archivedAt, wantDeleted: true},
		{name: "snapshot_without_sources", kind: "manual", reportStatus: "sent", jobStatus: "sent", windowEnd: archivedAt, messageCount: 1, wantDeleted: true},
		{name: "cancelled_delivery", kind: "manual", reportStatus: "sent", jobStatus: "cancelled", windowEnd: archivedAt, wantDeleted: true},
		{name: "at_retention_boundary", kind: "manual", reportStatus: "sent", jobStatus: "sent", windowEnd: cutoff},
		{name: "ready_report", kind: "manual", reportStatus: "ready", jobStatus: "pending", windowEnd: archivedAt},
		{name: "pending_delivery", kind: "manual", reportStatus: "sent", jobStatus: "pending", windowEnd: archivedAt},
		{name: "sending_delivery", kind: "manual", reportStatus: "sent", jobStatus: "sending", windowEnd: archivedAt},
		{name: "retrying_delivery", kind: "manual", reportStatus: "sent", jobStatus: "retry_wait", windowEnd: archivedAt},
		{name: "failed_report", kind: "manual", reportStatus: "failed", jobStatus: "failed", windowEnd: archivedAt},
		{name: "unpublished_outbox", kind: "manual", reportStatus: "sent", jobStatus: "sent", windowEnd: archivedAt, outboxPending: true},
		{name: "daily_without_sources", kind: "daily", reportStatus: "sent", jobStatus: "sent", windowEnd: archivedAt},
	}
	type fixture struct {
		reportID uint
		jobID    uint
		outboxes []uint
	}
	fixtures := make([]fixture, len(cases))
	for i, tc := range cases {
		snapshot := model.FeishuDigestSnapshot{SchemaVersion: 2, ReportKind: tc.kind, MessageCount: tc.messageCount}
		if tc.messageCount > 0 {
			snapshot.Messages = []model.FeishuDigestMessage{{DisplayText: "独立手动快照仍可查看正文"}}
		}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		report := model.FeishuDailyReport{
			AppID: s.appID, ChatID: "chat_a", RecipientOpenID: tc.name,
			ReportDate: tc.windowEnd.Format("2006-01-02"), ReportKind: tc.kind,
			WindowStart: tc.windowEnd.Add(-time.Hour), WindowEnd: tc.windowEnd, GeneratedAt: tc.windowEnd,
			Status: tc.reportStatus, PartCount: 1, ContentSnapshot: model.JSONText(raw),
		}
		if err := s.db.Create(&report).Error; err != nil {
			t.Fatal(err)
		}
		delivery := newMentionJob(s.appID, report.RecipientOpenID, "daily_digest", fmt.Sprintf("digest:%d:part:1", report.ID), tc.windowEnd, model.JSONText(`{}`))
		partNo := uint(1)
		delivery.ReportID, delivery.PartNo = &report.ID, &partNo
		delivery.Status, delivery.DispatchGeneration = tc.jobStatus, 2
		if err := s.db.Create(&delivery).Error; err != nil {
			t.Fatal(err)
		}
		fixtures[i] = fixture{reportID: report.ID, jobID: delivery.ID}
		for generation := uint(1); generation <= 2; generation++ {
			outboxJob := delivery
			outboxJob.DispatchGeneration = generation
			if err := data_dao.CreateFeishuMentionOutbox(ctx, s.db, &outboxJob); err != nil {
				t.Fatal(err)
			}
			var outbox model.AsyncJobOutbox
			if err := s.db.Where("task_key = ?", job.FeishuMentionTaskKey(delivery.ID, generation)).Take(&outbox).Error; err != nil {
				t.Fatal(err)
			}
			fixtures[i].outboxes = append(fixtures[i].outboxes, outbox.ID)
			if !(tc.outboxPending && generation == 2) {
				if err := s.db.Model(&outbox).Update("published_at", tc.windowEnd).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := s.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		for i, tc := range cases {
			want := int64(1)
			if tc.wantDeleted {
				want = 0
			}
			var reports, jobs, outboxes int64
			if err := s.db.Model(&model.FeishuDailyReport{}).Where("id = ?", fixtures[i].reportID).Count(&reports).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.Model(&model.FeishuDeliveryJob{}).Where("id = ?", fixtures[i].jobID).Count(&jobs).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.Model(&model.AsyncJobOutbox{}).Where("id IN ?", fixtures[i].outboxes).Count(&outboxes).Error; err != nil {
				t.Fatal(err)
			}
			if reports != want || jobs != want || outboxes != 2*want {
				t.Fatalf("cleanup %d %s: reports=%d jobs=%d outboxes=%d, want %d/%d/%d", attempt, tc.name, reports, jobs, outboxes, want, want, 2*want)
			}
		}
	}
}
