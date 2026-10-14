//go:build integration

package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

func mentionRegenerateFixture(t *testing.T) (*FeishuMentionService, *FeishuMentionAdminService, *time.Time, model.FeishuDailyReport) {
	t.Helper()
	core, now := mentionMySQLFixture(t, false)
	if err := core.db.AutoMigrate(&model.AuthAudit{}); err != nil {
		t.Fatal(err)
	}
	core.publicURL = "https://example.test"
	admin := NewFeishuMentionAdminService(core.db, core.appID, core.cfg, core.publicURL)
	admin.now = core.now
	event := mentionTestEvent(core, "chat_a", "previous-day", "ou_recipient")
	event.Content = `{"text":"昨天的正式日报原文"}`
	if err := core.Archive(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	*now = time.Date(2026, 10, 13, 18, 0, 0, 0, mentionLocation())
	if err := core.PlanDigests(t.Context()); err != nil {
		t.Fatal(err)
	}
	var report model.FeishuDailyReport
	if err := core.db.Where("app_id = ? AND report_kind = ?", core.appID, "daily").First(&report).Error; err != nil {
		t.Fatal(err)
	}
	*now = now.AddDate(0, 0, 1).Add(-6 * time.Hour)
	t.Cleanup(func() {
		var ids []uint
		core.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", core.appID).Pluck("id", &ids)
		if len(ids) > 0 {
			core.db.Where("target_type = ? AND target_id IN ?", "feishu_daily_report", ids).Delete(&model.AuthAudit{})
		}
	})
	return core, admin, now, report
}

func mentionRegenerateArchive(t *testing.T, core *FeishuMentionService, now *time.Time, id, text string, received, created time.Time) {
	t.Helper()
	*now = received
	event := mentionTestEvent(core, "chat_a", id, "ou_recipient")
	event.CreatedAt = created
	raw, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatal(err)
	}
	event.Content = string(raw)
	if err := core.Archive(t.Context(), event); err != nil {
		t.Fatal(err)
	}
}

func mentionRegenerateJob(t *testing.T, core *FeishuMentionService, reportID uint) model.FeishuDeliveryJob {
	t.Helper()
	var jobs []model.FeishuDeliveryJob
	if err := core.db.Where("app_id = ? AND report_id = ?", core.appID, reportID).Find(&jobs).Error; err != nil || len(jobs) != 1 {
		t.Fatalf("manual report must have one job: count=%d error=%v", len(jobs), err)
	}
	return jobs[0]
}

func TestFeishuMentionMySQLRegenerateTodayIncludesNewArchivedMessages(t *testing.T) {
	core, admin, now, source := mentionRegenerateFixture(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, mentionLocation())
	clickedAt := start.Add(12 * time.Hour)
	mentionRegenerateArchive(t, core, now, "before-midnight", "不能包含昨天", start.Add(-time.Millisecond), start.Add(-time.Hour))
	mentionRegenerateArchive(t, core, now, "at-midnight", "包含今天零点", start, start)
	mentionRegenerateArchive(t, core, now, "late-message", "按收到时间纳入，发送时间是昨天", start.Add(11*time.Hour), start.Add(-2*time.Hour))
	mentionRegenerateArchive(t, core, now, "at-click", "截止瞬间下一次纳入", clickedAt, clickedAt)
	mentionRegenerateArchive(t, core, now, "after-click", "点击之后下一次纳入", clickedAt.Add(time.Minute), clickedAt)
	*now = clickedAt
	var scheduleBefore model.FeishuDigestSchedule
	if err := core.db.Where("app_id = ?", core.appID).First(&scheduleBefore).Error; err != nil {
		t.Fatal(err)
	}
	originalSnapshot := string(source.ContentSnapshot)
	// The formal report is still ready; an explicit manual request is independent.
	dto, err := admin.ResendReport(ctx, source.ID, 7)
	if err != nil || dto.ID == source.ID || dto.ReportKind != "manual" || dto.MessageCount != 2 || dto.PartCount != 1 {
		t.Fatalf("today report=%+v error=%v", dto, err)
	}
	var manual model.FeishuDailyReport
	if err := core.db.First(&manual, dto.ID).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot model.FeishuDigestSnapshot
	if err := json.Unmarshal([]byte(manual.ContentSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if manual.ReportDate != "2026-10-14" || !manual.WindowStart.Equal(start) || !manual.WindowEnd.Equal(clickedAt) || !manual.GeneratedAt.Equal(clickedAt) ||
		snapshot.SchemaVersion != 2 || snapshot.ReportKind != "manual" || snapshot.Timezone != "Asia/Shanghai" || snapshot.MessageCount != 2 || !snapshot.WindowStart.Equal(start) || !snapshot.WindowEnd.Equal(clickedAt) {
		t.Fatalf("wrong manual interval: report=%+v snapshot=%+v", manual, snapshot)
	}
	body := string(manual.ContentSnapshot)
	for _, excluded := range []string{"不能包含昨天", "截止瞬间下一次纳入", "点击之后下一次纳入", "昨天的正式日报原文"} {
		if strings.Contains(body, excluded) {
			t.Fatalf("out-of-window content included: %s", excluded)
		}
	}
	for _, included := range []string{"包含今天零点", "按收到时间纳入，发送时间是昨天"} {
		if !strings.Contains(body, included) {
			t.Fatalf("missing today content: %s", included)
		}
	}
	first := mentionRegenerateJob(t, core, manual.ID)
	var outboxCount int64
	core.db.Model(&model.AsyncJobOutbox{}).Where("task_key = ?", fmt.Sprintf("feishu-mention:%d:g:%d", first.ID, first.DispatchGeneration)).Count(&outboxCount)
	if outboxCount != 1 {
		t.Fatal("manual send was not atomically queued")
	}
	client := &mentionTestClient{}
	processor := NewFeishuMentionProcessor(core, client, nil)
	if err := mentionTestDeliver(t, processor, first); err != nil {
		t.Fatal(err)
	}
	mentionRegenerateArchive(t, core, now, "new-after-generation", "重新统计必须出现新增完整正文", clickedAt.Add(5*time.Minute), clickedAt.Add(5*time.Minute))
	*now = clickedAt.Add(time.Hour)
	rebuilt, err := admin.ResendReport(ctx, source.ID, 7)
	if err != nil || rebuilt.ID != manual.ID || rebuilt.MessageCount != 5 {
		t.Fatalf("manual report was not refreshed: %+v error=%v", rebuilt, err)
	}
	current := mentionRegenerateJob(t, core, manual.ID)
	if current.SendUUID == first.SendUUID || current.DispatchGeneration != first.DispatchGeneration+1 || current.CycleAttempts != 0 || current.SentAt != nil || current.FeishuMessageID != "" {
		t.Fatal("new statistics did not start a fresh fenced send cycle")
	}
	if err := mentionTestDeliver(t, processor, first); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 {
		t.Fatal("previous generation sent after rebuild")
	}
	if err := mentionTestDeliver(t, processor, current); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 {
		t.Fatal("new statistics were not sent")
	}
	if err := core.db.First(&manual, manual.ID).Error; err != nil {
		t.Fatal(err)
	}
	if manual.Status != "sent" || !manual.GeneratedAt.Equal(*now) || !strings.Contains(string(manual.ContentSnapshot), "重新统计必须出现新增完整正文") || string(manual.ContentSnapshot) == body {
		t.Fatal("manual report still contains the old frozen snapshot")
	}
	var original model.FeishuDailyReport
	if err := core.db.First(&original, source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if string(original.ContentSnapshot) != originalSnapshot || original.Status != source.Status || original.ReportDate != source.ReportDate {
		t.Fatal("manual regeneration changed the source daily report")
	}
	var scheduleAfter model.FeishuDigestSchedule
	core.db.First(&scheduleAfter, scheduleBefore.ID)
	if !scheduleAfter.NextWindowStartAt.Equal(scheduleBefore.NextWindowStartAt) || !scheduleAfter.NextCutoffAt.Equal(scheduleBefore.NextCutoffAt) {
		t.Fatal("manual regeneration advanced the formal schedule")
	}
	var assigned int64
	core.db.Model(&model.FeishuMessageRecipient{}).Where("first_report_id = ?", manual.ID).Count(&assigned)
	if assigned != 0 {
		t.Fatal("manual report consumed formal first_report_id assignments")
	}
}

func TestFeishuMentionMySQLManualBeforeDailyDoesNotConsumeFormalReport(t *testing.T) {
	core, admin, now, source := mentionRegenerateFixture(t)
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, mentionLocation())
	mentionRegenerateArchive(t, core, now, "today-before-daily", "今天仍需进入正式日报", start.Add(10*time.Hour), start.Add(10*time.Hour))
	*now = start.Add(12 * time.Hour)
	manual, err := admin.ResendReport(t.Context(), source.ID, 7)
	if err != nil || manual.MessageCount != 1 {
		t.Fatalf("manual=%+v error=%v", manual, err)
	}
	var pendingRecipient model.FeishuMessageRecipient
	if err := core.db.Table("feishu_message_recipients r").Select("r.*").Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").
		Where("m.app_id = ? AND m.message_id = ?", core.appID, "today-before-daily").Take(&pendingRecipient).Error; err != nil {
		t.Fatal(err)
	}
	if pendingRecipient.FirstReportID != nil {
		t.Fatal("manual creation consumed the daily assignment")
	}
	*now = start.Add(18 * time.Hour)
	if err := core.PlanDigests(t.Context()); err != nil {
		t.Fatal(err)
	}
	var daily model.FeishuDailyReport
	if err := core.db.Where("app_id = ? AND report_date = ? AND report_kind = ?", core.appID, "2026-10-14", "daily").Take(&daily).Error; err != nil {
		t.Fatal(err)
	}
	if daily.ID == manual.ID || !strings.Contains(string(daily.ContentSnapshot), "今天仍需进入正式日报") {
		t.Fatal("manual row suppressed the formal daily report")
	}
	if err := core.db.First(&pendingRecipient, pendingRecipient.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pendingRecipient.FirstReportID == nil || *pendingRecipient.FirstReportID != daily.ID {
		t.Fatal("first_report_id does not point exclusively at the formal daily report")
	}
	var count int64
	core.db.Model(&model.FeishuDailyReport{}).Where("app_id = ? AND report_date = ?", core.appID, "2026-10-14").Count(&count)
	if count != 2 {
		t.Fatal("manual and daily must coexist for the same day, group and recipient")
	}
}

func TestFeishuMentionMySQLRegenerateAtomicConcurrentAndScoped(t *testing.T) {
	core, admin, _, source := mentionRegenerateFixture(t)
	ctx := context.Background()
	other := NewFeishuMentionAdminService(core.db, "other-app", core.cfg, admin.publicURL)
	if _, err := other.ResendReport(ctx, source.ID, 7); !errors.Is(err, ErrFeishuMentionAdminNotFound) {
		t.Fatalf("cross-app regeneration accepted: %v", err)
	}
	injected := errors.New("audit unavailable")
	if err := core.db.Callback().Create().Before("gorm:create").Register("regenerate_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "auth_audits" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := admin.ResendReport(ctx, source.ID, 7)
	core.db.Callback().Create().Remove("regenerate_audit_failure")
	if !errors.Is(err, injected) {
		t.Fatalf("audit failure=%v", err)
	}
	var count int64
	core.db.Model(&model.FeishuDailyReport{}).Where("app_id = ? AND report_kind = ?", core.appID, "manual").Count(&count)
	if count != 0 {
		t.Fatal("audit rollback leaked a manual report")
	}
	core.db.Model(&model.FeishuDeliveryJob{}).Where("app_id = ?", core.appID).Count(&count)
	if count != 1 {
		t.Fatal("audit rollback leaked a manual delivery")
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := admin.ResendReport(ctx, source.ID, 7)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrFeishuMentionAdminConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	core.db.Model(&model.FeishuDailyReport{}).Where("app_id = ? AND report_kind = ?", core.appID, "manual").Count(&count)
	if count != 1 {
		t.Fatal("concurrent clicks created multiple manual reports")
	}
}

func TestFeishuMentionMySQLRegenerateEmptyTodayRespectsPauseAndLease(t *testing.T) {
	core, admin, now, source := mentionRegenerateFixture(t)
	ctx := context.Background()
	admin.config.DigestEnabled = false
	if _, err := admin.ResendReport(ctx, source.ID, 7); !errors.Is(err, ErrFeishuMentionAdminPaused) {
		t.Fatalf("module pause ignored: %v", err)
	}
	admin.config.DigestEnabled = true
	dto, err := admin.ResendReport(ctx, source.ID, 7)
	if err != nil || dto.MessageCount != 0 || dto.ReportDate != now.Format("2006-01-02") || dto.ReportKind != "manual" {
		t.Fatalf("empty today must not replay yesterday: %+v %v", dto, err)
	}
	delivery := mentionRegenerateJob(t, core, dto.ID)
	var snapshot model.FeishuDeliverySnapshot
	if err := json.Unmarshal([]byte(delivery.PayloadSnapshot), &snapshot); err != nil || !strings.Contains(snapshot.CardJSON, "0 条消息") {
		t.Fatal("empty result must explicitly show zero messages")
	}
	if _, err := admin.ResendReport(ctx, dto.ID, 7); !errors.Is(err, ErrFeishuMentionAdminConflict) {
		t.Fatal("active manual statistics were replaced")
	}
	lease := now.Add(time.Minute)
	if err := core.db.Model(&model.FeishuDailyReport{}).Where("id = ?", dto.ID).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := core.db.Model(&delivery).Updates(map[string]any{"status": "failed", "lease_until": lease}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ResendReport(ctx, dto.ID, 7); !errors.Is(err, ErrFeishuMentionAdminConflict) {
		t.Fatal("manual active lease replaced")
	}
	if err := core.db.Model(&delivery).Update("lease_until", nil).Error; err != nil {
		t.Fatal(err)
	}
	rebuilt, err := admin.ResendReport(ctx, dto.ID, 7)
	if err != nil || rebuilt.ID != dto.ID || rebuilt.Status != "ready" || rebuilt.MessageCount != 0 {
		t.Fatalf("failed manual could not be regenerated: %+v %v", rebuilt, err)
	}
}
