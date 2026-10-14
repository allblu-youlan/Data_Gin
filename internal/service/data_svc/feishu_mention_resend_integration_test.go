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

func mentionResendFixture(t *testing.T, compact bool) (*FeishuMentionService, *FeishuMentionAdminService, model.FeishuDailyReport, []model.FeishuDeliveryJob) {
	t.Helper()
	core, admin := mentionAdminFixture(t)
	admin.publicURL = "https://example.test"
	if compact {
		core.publicURL = admin.publicURL
	}
	event := mentionTestEvent(core, "chat_a", "resend-message", "ou_recipient")
	body, _ := json.Marshal(map[string]string{"text": strings.Repeat("完整内容保留", 3000)})
	event.Content = string(body)
	ctx := context.Background()
	if err := core.Archive(ctx, event); err != nil {
		t.Fatal(err)
	}
	cutoff := core.now().Add(8 * time.Hour)
	core.now = func() time.Time { return cutoff }
	admin.now = core.now
	if err := core.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var report model.FeishuDailyReport
	if err := core.db.Where("app_id = ?", core.appID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	jobs, err := admin.dao.ReportJobs(ctx, []uint{report.ID})
	if err != nil || len(jobs) == 0 {
		t.Fatalf("missing jobs: %v", err)
	}
	if err := core.db.Model(&model.FeishuDeliveryJob{}).Where("report_id = ?", report.ID).Updates(map[string]any{"status": "sent", "sent_at": cutoff, "feishu_message_id": "om_original", "attempts": 2, "cycle_attempts": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := core.db.Model(&report).Updates(map[string]any{"status": "sent", "sent_at": cutoff}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		core.db.Where("target_type = ? AND target_id = ?", "feishu_daily_report", report.ID).Delete(&model.AuthAudit{})
	})
	return core, admin, report, jobs
}

func TestFeishuMentionMySQLResendRepeatedAndLegacyCompact(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprint(compact), func(t *testing.T) {
			core, admin, report, jobs := mentionResendFixture(t, compact)
			if !compact && len(jobs) < 2 {
				t.Fatal("fixture must cover old multipart reports")
			}
			if compact && len(jobs) != 1 {
				t.Fatal("new reports must have one bounded card")
			}
			originalSnapshot := string(report.ContentSnapshot)
			client := &mentionTestClient{}
			processor := NewFeishuMentionProcessor(core, client, nil)
			previous := jobs[0]
			for cycle := 0; cycle < 2; cycle++ {
				dto, err := admin.ResendReport(context.Background(), report.ID, 7)
				if err != nil || dto.PartCount != 1 || len(dto.Parts) != 1 || dto.Status != "ready" {
					t.Fatalf("resend: %+v %v", dto, err)
				}
				if _, err := admin.ResendReport(context.Background(), report.ID, 7); !errors.Is(err, ErrFeishuMentionAdminConflict) {
					t.Fatalf("active report resent: %v", err)
				}
				var current model.FeishuDeliveryJob
				if err := core.db.First(&current, previous.ID).Error; err != nil {
					t.Fatal(err)
				}
				if current.SendUUID == previous.SendUUID || current.DispatchGeneration != previous.DispatchGeneration+1 || current.CycleAttempts != 0 || current.SentAt != nil || current.FeishuMessageID != "" {
					t.Fatalf("bad new cycle: %+v", current)
				}
				if err := mentionTestDeliver(t, processor, previous); err != nil {
					t.Fatal(err)
				}
				if len(client.calls) != cycle {
					t.Fatal("stale task sent an extra card")
				}
				if err := mentionTestDeliver(t, processor, current); err != nil {
					t.Fatal(err)
				}
				if len(client.calls) != cycle+1 {
					t.Fatal("explicit resend did not send")
				}
				if err := core.db.First(&report, report.ID).Error; err != nil {
					t.Fatal(err)
				}
				if report.Status != "sent" || report.PartCount != 1 || string(report.ContentSnapshot) != originalSnapshot {
					t.Fatal("resend changed frozen content or did not finish")
				}
				previous = current
			}
			for _, old := range jobs[1:] {
				if err := mentionTestDeliver(t, processor, old); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.ReportPart(context.Background(), report.ID, *old.PartNo); !errors.Is(err, ErrFeishuMentionAdminNotFound) {
					t.Fatal("obsolete part exposed as current")
				}
			}
			if len(client.calls) != 2 {
				t.Fatal("obsolete multipart task sent a card")
			}
			if err := core.PlanDigests(context.Background()); err != nil {
				t.Fatal(err)
			}
			var count int64
			core.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", core.appID).Count(&count)
			if count != 1 {
				t.Fatal("resend defeated daily uniqueness")
			}
		})
	}
}

func TestFeishuMentionMySQLResendAtomicConcurrentAndScoped(t *testing.T) {
	core, admin, report, jobs := mentionResendFixture(t, true)
	ctx := context.Background()
	other := NewFeishuMentionAdminService(core.db, "other-app", core.cfg, admin.publicURL)
	if _, err := other.ResendReport(ctx, report.ID, 7); !errors.Is(err, ErrFeishuMentionAdminNotFound) {
		t.Fatalf("cross-app resend: %v", err)
	}
	injected := errors.New("audit unavailable")
	if err := core.db.Callback().Create().Before("gorm:create").Register("resend_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "auth_audits" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := admin.ResendReport(ctx, report.ID, 7)
	core.db.Callback().Create().Remove("resend_audit_failure")
	if !errors.Is(err, injected) {
		t.Fatalf("audit failure: %v", err)
	}
	var unchanged model.FeishuDeliveryJob
	core.db.First(&unchanged, jobs[0].ID)
	if unchanged.Status != "sent" || unchanged.SendUUID != jobs[0].SendUUID || unchanged.DispatchGeneration != jobs[0].DispatchGeneration {
		t.Fatal("audit failure leaked reset")
	}
	var leaked int64
	core.db.Model(&model.AsyncJobOutbox{}).Where("task_key = ?", fmt.Sprintf("feishu-mention:%d:g:%d", unchanged.ID, unchanged.DispatchGeneration+1)).Count(&leaked)
	if leaked != 0 {
		t.Fatal("audit failure leaked outbox")
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := admin.ResendReport(ctx, report.ID, 7); results <- err }()
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
}

func TestFeishuMentionMySQLResendFailedReportRespectsPauseAndActiveLease(t *testing.T) {
	core, admin, report, jobs := mentionResendFixture(t, false)
	ctx := context.Background()
	if err := core.db.Model(&report).Updates(map[string]any{"status": "failed", "sent_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	if err := core.db.Model(&model.FeishuDeliveryJob{}).Where("report_id = ? AND part_no > 1", report.ID).Updates(map[string]any{"status": "pending", "sent_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	if err := core.db.Model(&jobs[0]).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	admin.config.DigestEnabled = false
	if _, err := admin.ResendReport(ctx, report.ID, 7); !errors.Is(err, ErrFeishuMentionAdminPaused) {
		t.Fatalf("module pause ignored: %v", err)
	}
	admin.config.DigestEnabled = true
	lease := core.now().Add(time.Minute)
	if err := core.db.Model(&jobs[0]).Update("lease_until", lease).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ResendReport(ctx, report.ID, 7); !errors.Is(err, ErrFeishuMentionAdminConflict) {
		t.Fatalf("active lease reset: %v", err)
	}
	if err := core.db.Model(&jobs[0]).Update("lease_until", nil).Error; err != nil {
		t.Fatal(err)
	}
	dto, err := admin.ResendReport(ctx, report.ID, 7)
	if err != nil || dto.PartCount != 1 || dto.Status != "ready" {
		t.Fatalf("failed multipart resend: %+v %v", dto, err)
	}
}
