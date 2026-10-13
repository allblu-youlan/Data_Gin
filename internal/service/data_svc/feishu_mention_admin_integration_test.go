//go:build integration

package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

func mentionAdminFixture(t *testing.T) (*FeishuMentionService, *FeishuMentionAdminService) {
	t.Helper()
	core, _ := mentionMySQLFixture(t, true)
	if err := core.db.AutoMigrate(&model.AuthAudit{}); err != nil {
		t.Fatal(err)
	}
	admin := NewFeishuMentionAdminService(core.db, core.appID, core.cfg)
	admin.config.DefaultFollowupMinutes = 30
	admin.now = core.now
	return core, admin
}
func TestFeishuMentionAdminMySQLScopePaginationAndPause(t *testing.T) {
	core, admin := mentionAdminFixture(t)
	ctx := context.Background()
	if err := core.Archive(ctx, mentionTestEvent(core, "chat_a", "admin-message", "user_a")); err != nil {
		t.Fatal(err)
	}
	rows, p, err := admin.Recipients(ctx, data_dao.FeishuMentionAdminFilter{Page: 1, PerPage: 20})
	if err != nil || len(rows) != 1 || p.TotalCount != 1 {
		t.Fatalf("rows=%v pagination=%+v err=%v", rows, p, err)
	}
	if rows[0].InitialReminder == nil || rows[0].ViewStatus != "pending" {
		t.Fatalf("missing reminder details %+v", rows[0])
	}
	empty, p, err := admin.Recipients(ctx, data_dao.FeishuMentionAdminFilter{Page: 99, PerPage: 20})
	if err != nil || len(empty) != 0 || p.Page != 99 {
		t.Fatalf("empty=%v pagination=%+v error=%v", empty, p, err)
	}
	other := NewFeishuMentionAdminService(core.db, "different_application", core.cfg)
	if _, err = other.Recipient(ctx, rows[0].ID); !errors.Is(err, ErrFeishuMentionAdminNotFound) {
		t.Fatalf("cross-app recipient visible: %v", err)
	}
	chats, _, err := admin.Chats(ctx, data_dao.FeishuMentionAdminFilter{Page: 1, PerPage: 20, ChatID: "chat_a"})
	if err != nil || len(chats) != 1 {
		t.Fatal(err)
	}
	chat := chats[0]
	t.Cleanup(func() {
		core.db.Where("target_type = ? AND target_id = ?", "feishu_chat", chat.ID).Delete(&model.AuthAudit{})
	})
	name, external, disabled, interval := chat.ChatName, false, false, 30
	if _, err = other.SaveChat(ctx, 7, chat.ID, FeishuMentionChatInput{ChatName: &name, IsExternal: &external, Enabled: &disabled, FollowupMinutes: &interval}); !errors.Is(err, ErrFeishuMentionAdminNotFound) {
		t.Fatalf("cross-app write accepted %v", err)
	}
	if _, err = admin.SaveChat(ctx, 7, chat.ID, FeishuMentionChatInput{ChatName: &name, IsExternal: &external, Enabled: &disabled, FollowupMinutes: &interval}); err != nil {
		t.Fatal(err)
	}
	detail, err := admin.Recipient(ctx, rows[0].ID)
	if err != nil || detail.InitialReminder.Status != "cancelled" || detail.InitialReminder.CancelReason != "group_disabled" {
		t.Fatalf("disable did not cancel unsent reminders: %+v %v", detail, err)
	}
	var count int64
	core.db.Model(&model.AuthAudit{}).Where("target_type = ? AND target_id = ? AND action = ?", "feishu_chat", chat.ID, "FEISHU_CHAT_UPDATE").Count(&count)
	if count != 1 {
		t.Fatalf("audit count %d", count)
	}
}
func TestFeishuMentionAdminMySQLRetryAtomicAndConcurrent(t *testing.T) {
	core, admin := mentionAdminFixture(t)
	ctx := context.Background()
	if err := core.Archive(ctx, mentionTestEvent(core, "chat_a", "retry-message", "user_a")); err != nil {
		t.Fatal(err)
	}
	var job model.FeishuDeliveryJob
	if err := core.db.Where("app_id = ?", core.appID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		core.db.Where("target_type = ? AND target_id = ?", "feishu_delivery_job", job.ID).Delete(&model.AuthAudit{})
	})
	if err := core.db.Model(&job).Updates(map[string]interface{}{"status": "failed", "attempts": 5, "cycle_attempts": 5, "last_error_code": "permission_denied", "last_error_message": "无发送权限"}).Error; err != nil {
		t.Fatal(err)
	}
	originalUUID, generation := job.SendUUID, job.DispatchGeneration
	injected := errors.New("audit write failed")
	if err := core.db.Callback().Create().Before("gorm:create").Register("mention_admin_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "auth_audits" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := admin.Retry(ctx, 7, job.ID)
	core.db.Callback().Create().Remove("mention_admin_audit_failure")
	if !errors.Is(err, injected) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	var unchanged model.FeishuDeliveryJob
	core.db.First(&unchanged, job.ID)
	if unchanged.Status != "failed" || unchanged.DispatchGeneration != generation {
		t.Fatalf("audit failure did not roll back %+v", unchanged)
	}
	var leaked int64
	core.db.Model(&model.AsyncJobOutbox{}).Where("task_key = ?", fmt.Sprintf("feishu-mention:%d:g:%d", job.ID, generation+1)).Count(&leaked)
	if leaked != 0 {
		t.Fatal("outbox escaped failed audit transaction")
	}
	other := NewFeishuMentionAdminService(core.db, "different_application", core.cfg)
	if _, err = other.Retry(ctx, 7, job.ID); !errors.Is(err, ErrFeishuMentionAdminNotFound) {
		t.Fatalf("cross-app retry accepted: %v", err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := admin.Retry(ctx, 7, job.ID); results <- err }()
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
	var retried model.FeishuDeliveryJob
	core.db.First(&retried, job.ID)
	if retried.SendUUID != originalUUID || retried.Attempts != 5 || retried.CycleAttempts != 0 || retried.DispatchGeneration != generation+1 || retried.Status != "pending" || retried.LastErrorCode != "" {
		t.Fatalf("retry changed logical identity/budget: %+v", retried)
	}
	var audit model.AuthAudit
	if err := core.db.Where("target_type = ? AND target_id = ?", "feishu_delivery_job", job.ID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit.BeforeJSON), "permission_denied") || strings.Contains(string(audit.BeforeJSON), "cardJSON") {
		t.Fatalf("unsafe or incomplete audit %s", audit.BeforeJSON)
	}
}

func TestFeishuMentionAdminMySQLDailyPauseAndFrozenPart(t *testing.T) {
	core, admin := mentionAdminFixture(t)
	ctx := context.Background()
	if err := core.Archive(ctx, mentionTestEvent(core, "chat_a", "digest-admin-message", "user_a")); err != nil {
		t.Fatal(err)
	}
	later := core.now().Add(9 * time.Hour)
	core.now = func() time.Time { return later }
	admin.now = core.now
	if err := core.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var report model.FeishuDailyReport
	if err := core.db.Where("app_id = ?", core.appID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	var job model.FeishuDeliveryJob
	if err := core.db.Where("report_id = ?", report.ID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		core.db.Where("target_type = ? AND target_id = ?", "feishu_delivery_job", job.ID).Delete(&model.AuthAudit{})
	})
	if err := core.db.Model(&job).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	if err := core.db.Model(&model.FeishuDigestSchedule{}).Where("app_id = ?", core.appID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Retry(ctx, 7, job.ID); !errors.Is(err, ErrFeishuMentionAdminPaused) {
		t.Fatalf("paused schedule accepted retry: %v", err)
	}
	if err := core.db.Model(&model.FeishuDigestSchedule{}).Where("app_id = ?", core.appID).Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	part, err := admin.ReportPart(ctx, report.ID, 1)
	if err != nil || !part.Job.Retryable || len(part.Fragments) == 0 || len(part.Content) == 0 {
		t.Fatalf("part=%+v error=%v", part, err)
	}
	core.db.Model(&model.FeishuMonitoredChat{}).Where("app_id = ?", core.appID).Updates(map[string]interface{}{"enabled": false, "chat_name": "renamed"})
	if _, err := admin.Retry(ctx, 7, job.ID); err != nil {
		t.Fatalf("current group state must not block frozen digest retry: %v", err)
	}
	fixed, err := admin.Report(ctx, report.ID)
	if err != nil || fixed.ChatName != "chat_a" || fixed.MessageCount != 1 || fixed.Status != "ready" {
		t.Fatalf("snapshot changed: %+v error=%v", fixed, err)
	}
}
