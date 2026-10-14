//go:build integration

package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

func mentionSettingsFixture(t *testing.T) (*FeishuMentionService, *FeishuMentionAdminService, *time.Time) {
	t.Helper()
	core, now := mentionMySQLFixture(t, false)
	if err := core.db.AutoMigrate(&model.AuthAudit{}); err != nil {
		t.Fatal(err)
	}
	admin := NewFeishuMentionAdminService(core.db, core.appID, core.cfg)
	admin.now = core.now
	t.Cleanup(func() {
		var setting model.RuntimeConfig
		if err := core.db.Where("config_key = ?", mentionDigestSettingsKey(core.appID)).First(&setting).Error; err == nil {
			core.db.Where("target_type = ? AND target_id = ?", "runtime_config", setting.ID).Delete(&model.AuthAudit{})
		}
	})
	return core, admin, now
}

func saveMentionTestDigestTime(t *testing.T, admin *FeishuMentionAdminService, value string) {
	t.Helper()
	result, err := admin.SaveDigestSettings(context.Background(), 7, FeishuMentionDigestSettingsInput{DigestTime: value})
	if err != nil || result == nil || result.DigestTime != value || result.Timezone != "Asia/Shanghai" {
		t.Fatalf("save=%+v error=%v", result, err)
	}
}

func TestFeishuMentionMySQLDigestSettingsSharedAndIsolated(t *testing.T) {
	core, admin, _ := mentionSettingsFixture(t)
	otherCore, otherAdmin, _ := mentionSettingsFixture(t)
	ctx := context.Background()
	readerConfig := core.cfg
	readerConfig.DigestTime = "22:00"
	reader := NewFeishuMentionAdminService(core.db, core.appID, readerConfig)
	for _, value := range []string{"17:25", "19:20"} {
		saveMentionTestDigestTime(t, admin, value)
		status, err := reader.Status(ctx)
		if err != nil || status["digestTime"] != value {
			t.Fatalf("another instance retained its local config: status=%+v error=%v", status, err)
		}
		otherStatus, err := otherAdmin.Status(ctx)
		if err != nil || otherStatus["digestTime"] != otherCore.cfg.DigestTime {
			t.Fatalf("saved setting leaked into another application: status=%+v error=%v", otherStatus, err)
		}
	}
	var setting model.RuntimeConfig
	if err := core.db.Where("config_key = ?", mentionDigestSettingsKey(core.appID)).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	var audits []model.AuthAudit
	if err := core.db.Where("target_type = ? AND target_id = ?", "runtime_config", setting.ID).Order("id").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 2 || audits[1].ActorUserID != 7 || audits[1].Action != "FEISHU_DIGEST_SETTINGS_UPDATE" {
		t.Fatalf("settings audit=%+v", audits)
	}
	var schedule model.FeishuDigestSchedule
	if err := core.db.Where("app_id = ?", core.appID).First(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	if schedule.NextCutoffAt.Hour() != 18 || !schedule.NextWindowStartAt.Equal(core.now()) {
		t.Fatal("saving settings directly changed the scheduler cursor")
	}
}

func TestFeishuMentionMySQLDigestSettingsCanSaveBeforeInitializationWhileDisabled(t *testing.T) {
	core, admin, now := mentionSettingsFixture(t)
	ctx := context.Background()
	if err := core.db.Where("app_id = ?", core.appID).Delete(&model.FeishuDigestSchedule{}).Error; err != nil {
		t.Fatal(err)
	}
	admin.config.Enabled, admin.config.DigestEnabled = false, false
	saveMentionTestDigestTime(t, admin, "08:30")
	var schedules, jobs int64
	core.db.Model(&model.FeishuDigestSchedule{}).Where("app_id = ?", core.appID).Count(&schedules)
	core.db.Model(&model.FeishuDeliveryJob{}).Where("app_id = ?", core.appID).Count(&jobs)
	if schedules != 0 || jobs != 0 {
		t.Fatal("saving a paused module initialized scheduling or delivery")
	}
	status, err := admin.Status(ctx)
	if err != nil || status["enabled"] != false || status["digestTime"] != "08:30" {
		t.Fatalf("disabled status=%+v error=%v", status, err)
	}
	if err := core.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	var schedule model.FeishuDigestSchedule
	if err := core.db.Where("app_id = ?", core.appID).First(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	if !schedule.NextWindowStartAt.Equal(*now) || !schedule.NextCutoffAt.Equal(now.Add(22*time.Hour+30*time.Minute)) {
		t.Fatalf("initialization ignored saved setting: %+v", schedule)
	}
	missingApp := NewFeishuMentionAdminService(core.db, "", core.cfg)
	if _, err := missingApp.SaveDigestSettings(ctx, 7, FeishuMentionDigestSettingsInput{DigestTime: "18:00"}); !errors.Is(err, ErrFeishuMentionAdminInvalid) {
		t.Fatalf("empty application accepted: %v", err)
	}
}

func TestFeishuMentionMySQLDigestSettingsAuditFailureRollsBack(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "first save"
		if existing {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			core, admin, _ := mentionSettingsFixture(t)
			if existing {
				saveMentionTestDigestTime(t, admin, "17:00")
			}
			callback := "mention_settings_audit_failure"
			if err := core.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "auth_audits" {
					tx.AddError(errors.New("test audit unavailable"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			_, err := admin.SaveDigestSettings(context.Background(), 7, FeishuMentionDigestSettingsInput{DigestTime: "20:00"})
			core.db.Callback().Create().Remove(callback)
			if err == nil {
				t.Fatal("save succeeded without its audit")
			}
			settings, err := loadMentionDigestSettings(context.Background(), core.db, core.appID, core.cfg.DigestTime)
			want := "18:00"
			if existing {
				want = "17:00"
			}
			if err != nil || settings.DigestTime != want {
				t.Fatalf("rolled-back setting=%+v error=%v", settings, err)
			}
			var count int64
			if err := core.db.Model(&model.RuntimeConfig{}).Where("config_key = ?", mentionDigestSettingsKey(core.appID)).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if !existing && count != 0 {
				t.Fatal("failed first save left a runtime setting")
			}
		})
	}
}

func TestFeishuMentionMySQLDigestSettingsReadErrorsDoNotUseLocalDefault(t *testing.T) {
	core, admin, _ := mentionSettingsFixture(t)
	ctx := context.Background()
	saveMentionTestDigestTime(t, admin, "17:00")
	if err := core.db.Model(&model.RuntimeConfig{}).Where("config_key = ?", mentionDigestSettingsKey(core.appID)).Update("config_json", `{}`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Status(ctx); err == nil {
		t.Fatal("status hid invalid persisted settings behind local defaults")
	}
	if err := core.Initialize(ctx); err == nil {
		t.Fatal("initialization ignored invalid persisted settings")
	}
	if err := core.PlanDigests(ctx); err == nil {
		t.Fatal("planner ignored invalid persisted settings")
	}
}

func TestFeishuMentionMySQLWebDigestTimePreservesWindowsAndBacklog(t *testing.T) {
	core, admin, now := mentionSettingsFixture(t)
	ctx := context.Background()
	start := *now
	if err := core.Archive(ctx, mentionTestEvent(core, "chat_a", "first_day", "u1")); err != nil {
		t.Fatal(err)
	}
	saveMentionTestDigestTime(t, admin, "17:00")
	if err := core.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	*now = start.Add(7 * time.Hour)
	if err := core.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var frozen model.FeishuDailyReport
	if err := core.db.Where("app_id = ?", core.appID).First(&frozen).Error; err != nil {
		t.Fatal(err)
	}
	if !frozen.WindowStart.Equal(start) || !frozen.WindowEnd.Equal(*now) {
		t.Fatal("earlier web cutoff discarded the first unreported interval")
	}
	core.cfg.DigestEnabled = false
	for _, item := range []struct {
		hours time.Duration
		id    string
	}{{30*time.Hour + 45*time.Minute, "before_old_cutoff"}, {31*time.Hour + 30*time.Minute, "after_old_cutoff"}} {
		*now = start.Add(item.hours)
		if err := core.Archive(ctx, mentionTestEvent(core, "chat_a", item.id, "u1")); err != nil {
			t.Fatal(err)
		}
	}
	*now = start.Add(56 * time.Hour)
	admin.config.DigestEnabled = false
	saveMentionTestDigestTime(t, admin, "20:00")
	if err := core.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	workerConfig := core.cfg
	workerConfig.DigestEnabled, workerConfig.DigestTime = true, "08:00"
	worker := NewFeishuMentionService(core.db, core.appID, workerConfig)
	worker.now = core.now
	if err := worker.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var reports []model.FeishuDailyReport
	if err := core.db.Where("app_id = ?", core.appID).Order("id").Find(&reports).Error; err != nil {
		t.Fatal(err)
	}
	if len(reports) != 3 || string(reports[0].ContentSnapshot) != string(frozen.ContentSnapshot) {
		t.Fatal("saved time changed an immutable report or lost a backlog window")
	}
	for i, report := range reports {
		if !report.WindowEnd.Equal(start.Add(time.Duration(7+24*i) * time.Hour)) {
			t.Fatalf("overdue window changed to the new time: %s", report.WindowEnd)
		}
	}
	var schedule model.FeishuDigestSchedule
	if err := core.db.Where("app_id = ?", core.appID).First(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	if !schedule.NextWindowStartAt.Equal(start.Add(55*time.Hour)) || !schedule.NextCutoffAt.Equal(start.Add(82*time.Hour)) {
		t.Fatalf("new time did not follow old windows: %+v", schedule)
	}
}
