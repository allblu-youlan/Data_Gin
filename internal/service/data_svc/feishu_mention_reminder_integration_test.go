//go:build integration

package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"github.com/hibiken/asynq"
)

func mentionLifecycleJob(t *testing.T, s *FeishuMentionService, kind string) model.FeishuDeliveryJob {
	t.Helper()
	var row model.FeishuDeliveryJob
	if err := s.db.Where("app_id = ? AND job_type = ?", s.appID, kind).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestFeishuMentionMySQLInitialRetryStartsOnlyOneSnapshottedFollowup(t *testing.T) {
	s, now := mentionMySQLFixture(t, true)
	ctx := context.Background()
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "retry_initial", "u1")); err != nil {
		t.Fatal(err)
	}
	initial := mentionLifecycleJob(t, s, "initial_reminder")
	client := &mentionTestClient{fail: true}
	processor := NewFeishuMentionProcessor(s, client, nil)
	if err := mentionTestDeliver(t, processor, initial); err == nil || errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("temporary first-send failure should retry: %v", err)
	}
	initial = mentionLifecycleJob(t, s, "initial_reminder")
	var recipient model.FeishuMessageRecipient
	if err := s.db.First(&recipient, *initial.RecipientRecordID).Error; err != nil {
		t.Fatal(err)
	}
	var followupCount int64
	if err := s.db.Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND job_type = ?", s.appID, "followup_reminder").Count(&followupCount).Error; err != nil {
		t.Fatal(err)
	}
	if recipient.ViewStatus != "pending" || recipient.InitialSentAt != nil || followupCount != 0 || initial.CycleAttempts != 1 {
		t.Fatal("failed first send started a followup or changed confirmation state")
	}
	if err := s.db.Model(&model.FeishuMonitoredChat{}).Where("app_id = ? AND chat_id = ?", s.appID, "chat_a").Update("followup_minutes", 120).Error; err != nil {
		t.Fatal(err)
	}
	*now = initial.ScheduledAt
	client.fail = false
	if err := mentionTestDeliver(t, processor, initial); err != nil {
		t.Fatal(err)
	}
	followup := mentionLifecycleJob(t, s, "followup_reminder")
	if !followup.ScheduledAt.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("followup ignored its original interval: %s", followup.ScheduledAt)
	}
	*now = followup.ScheduledAt
	for i := 0; i < 2; i++ {
		if err := mentionTestDeliver(t, processor, followup); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.db.First(&recipient, recipient.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND job_type = ?", s.appID, "followup_reminder").Count(&followupCount).Error; err != nil {
		t.Fatal(err)
	}
	if recipient.ViewStatus != "pending" || recipient.InitialSentAt == nil || recipient.FollowupSentAt == nil || followupCount != 1 || len(client.calls) != 3 {
		t.Fatal("reminders did not stop after one followup while keeping pending confirmation")
	}
	if client.calls[0] != client.calls[1] {
		t.Fatal("initial retry changed its send UUID")
	}
}

func TestFeishuMentionMySQLReminderSwitchPreservesDefaultRecordsAndPausedUpdates(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "default_record", "u1")); err != nil {
		t.Fatal(err)
	}
	s.cfg.PersonalReminderEnabled, s.cfg.FollowupEnabled = true, true
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "enabled_record", "u1")); err != nil {
		t.Fatal(err)
	}
	initial := mentionLifecycleJob(t, s, "initial_reminder")
	client := &mentionTestClient{}
	processor := NewFeishuMentionProcessor(s, client, mentionTestInspector{missing: true})
	if err := mentionTestDeliver(t, processor, initial); err != nil {
		t.Fatal(err)
	}
	initial = mentionLifecycleJob(t, s, "initial_reminder")
	input := FeishuMentionConfirmation{AppID: s.appID, OperatorOpenID: "u1", MessageID: initial.FeishuMessageID, Action: "confirm_view", RecipientRecordID: *initial.RecipientRecordID, PayloadVersion: 1}
	if _, err := s.Confirm(ctx, input); err != nil {
		t.Fatal(err)
	}
	update := mentionLifecycleJob(t, s, "card_update")
	s.cfg.PersonalReminderEnabled = false
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mentionTestDeliver(t, processor, update); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&model.AsyncJobOutbox{}).Where("task_key = ?", job.FeishuMentionTaskKey(update.ID, update.DispatchGeneration)).Update("published_at", *now).Error; err != nil {
		t.Fatal(err)
	}
	if err := processor.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	update = mentionLifecycleJob(t, s, "card_update")
	if update.Status != "pending" || update.Attempts != 0 || update.DispatchGeneration != 1 || len(client.calls) != 1 {
		t.Fatal("disabled confirmation spent an update attempt or resumed its queue task")
	}
	s.cfg.PersonalReminderEnabled = true
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := processor.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	update = mentionLifecycleJob(t, s, "card_update")
	if update.DispatchGeneration != 2 || update.Attempts != 0 {
		t.Fatal("enabled confirmation did not recover its paused card update")
	}
	if err := mentionTestDeliver(t, processor, update); err != nil {
		t.Fatal(err)
	}
	var recipient model.FeishuMessageRecipient
	if err := s.db.First(&recipient, *initial.RecipientRecordID).Error; err != nil {
		t.Fatal(err)
	}
	update = mentionLifecycleJob(t, s, "card_update")
	if recipient.ViewStatus != "confirmed" || recipient.ConfirmedAt == nil || !recipient.ConfirmedAt.Equal(*now) || update.Status != "sent" {
		t.Fatal("pause/resume lost confirmation or failed to update the card")
	}
	var defaultRecipient model.FeishuMessageRecipient
	if err := s.db.Table("feishu_message_recipients AS r").Select("r.*").Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").Where("m.app_id = ? AND m.message_id = ?", s.appID, "default_record").First(&defaultRecipient).Error; err != nil {
		t.Fatal(err)
	}
	var defaultJobs int64
	if err := s.db.Model(&model.FeishuDeliveryJob{}).Where("recipient_record_id = ?", defaultRecipient.ID).Count(&defaultJobs).Error; err != nil {
		t.Fatal(err)
	}
	if defaultRecipient.ViewStatus != "not_required" || defaultJobs != 0 {
		t.Fatal("enabling reminders retroactively scheduled the default-mode record")
	}
}

func TestFeishuMentionMySQLConfirmationDuringFollowupUpdatesBothSentCards(t *testing.T) {
	s, now := mentionMySQLFixture(t, true)
	ctx := context.Background()
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "confirm_during_followup", "u1")); err != nil {
		t.Fatal(err)
	}
	initial := mentionLifecycleJob(t, s, "initial_reminder")
	client := &mentionTestClient{}
	processor := NewFeishuMentionProcessor(s, client, nil)
	if err := mentionTestDeliver(t, processor, initial); err != nil {
		t.Fatal(err)
	}
	initial = mentionLifecycleJob(t, s, "initial_reminder")
	followup := mentionLifecycleJob(t, s, "followup_reminder")
	*now = followup.ScheduledAt
	var confirmErr error
	client.onSend = func() {
		_, confirmErr = s.Confirm(ctx, FeishuMentionConfirmation{AppID: s.appID, OperatorOpenID: "u1", MessageID: initial.FeishuMessageID, Action: "confirm_view", RecipientRecordID: *initial.RecipientRecordID, PayloadVersion: 1})
	}
	if err := mentionTestDeliver(t, processor, followup); err != nil {
		t.Fatal(err)
	}
	if confirmErr != nil {
		t.Fatal(confirmErr)
	}
	followup = mentionLifecycleJob(t, s, "followup_reminder")
	var updates []model.FeishuDeliveryJob
	if err := s.db.Where("app_id = ? AND job_type = ?", s.appID, "card_update").Find(&updates).Error; err != nil {
		t.Fatal(err)
	}
	if followup.Status != "sent" || len(updates) != 2 {
		t.Fatalf("in-flight completion lost sent state or confirmation update: status=%s updates=%d", followup.Status, len(updates))
	}
	targets := map[string]bool{}
	for _, update := range updates {
		targets[update.TargetFeishuMessageID] = true
	}
	if !targets[initial.FeishuMessageID] || !targets[followup.FeishuMessageID] {
		t.Fatal("confirmation did not cover both successfully sent reminder cards")
	}
}
