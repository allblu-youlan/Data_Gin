package bootstrap

import (
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/global"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
)

func TestFeishuMentionMigrationRetainsUpgradeBaseline(t *testing.T) {
	latest, found := preferredSchemaMigrationBaseline(schemaIncrementalMigrationBaselines(), []string{feishuMentionMigrationVersion})
	if !found || latest != feishuMentionMigrationVersion { t.Fatal("missing v23 upgrade baseline") }
	baseline, ok := preferredSchemaMigrationBaseline(schemaIncrementalMigrationBaselines(), []string{"2026-09-08-office-webdav-v22"})
	if !ok || baseline != officeWebDAVMigrationVersion {
		t.Fatalf("missing last deployed baseline: %q", baseline)
	}
	want := []interface{}{&model.FeishuMonitoredChat{}, &model.FeishuSourceMessage{}, &model.FeishuMessageRecipient{}, &model.FeishuDailyReport{}, &model.FeishuDeliveryJob{}, &model.FeishuDigestSchedule{}}
	got := feishuMentionMigrationModels()
	if len(got) != len(want) {
		t.Fatalf("model count=%d", len(got))
	}
	for i := range want {
		if reflect.TypeOf(got[i]) != reflect.TypeOf(want[i]) {
			t.Fatalf("missing model %T", want[i])
		}
	}
}

func TestFeishuMentionSchedulerIndependentOfPersonalAndWeatherFlags(t *testing.T) {
	loadReportCenterConfig(t, "")
	v := config.Instance()
	keys := []string{"cfg.feishu_mention.enabled", "cfg.feishu_mention.personal_reminder_enabled", "cfg.feishu_mention.digest_enabled"}
	for _, key := range keys {
		old := v.Get(key)
		key := key
		t.Cleanup(func() { v.Set(key, old) })
	}
	queue := global.QueueJobClient
	global.QueueJobClient = nil
	t.Cleanup(func() { global.QueueJobClient = queue })
	v.Set(keys[0], false)
	disabled := &fakeScheduledTaskRegistrar{}
	registerFeishuMentionScheduledTasks(disabled)
	if disabled.calls != 0 {
		t.Fatal("disabled module registered planner")
	}
	v.Set(keys[0], true)
	v.Set(keys[1], false)
	v.Set(keys[2], false)
	enabled := &fakeScheduledTaskRegistrar{}
	registerFeishuMentionScheduledTasks(enabled)
	if enabled.calls != 1 || enabled.crons[0] != "* * * * *" || enabled.tasks[0].Type() != job.TypeFeishuMentionPlan {
		t.Fatalf("planner registration=%+v", enabled)
	}
	// The planner still handles recovery/cancellation while digest generation pauses.
	definitions := job.FeishuMentionOutboxTaskDefinitions(5, 60*time.Second)
	registry, err := job.NewOutboxTaskRegistry(definitions...)
	if err != nil || registry == nil || len(definitions) != 1 || definitions[0].MaxRetry != 4 || definitions[0].Queue != "delivery" {
		t.Fatalf("outbox definitions=%+v err=%v", definitions, err)
	}
}
