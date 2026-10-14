//go:build integration

package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func mentionMySQLFixture(t *testing.T, personal bool) (*FeishuMentionService, *time.Time) {
	t.Helper()
	dsn := os.Getenv("FEISHU_MENTION_MYSQL_DSN")
	if dsn == "" {
		t.Skip("FEISHU_MENTION_MYSQL_DSN is required for MySQL 8 integration tests")
	}
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.DBName, "_test") && !strings.Contains(parsed.DBName, "integration") {
		t.Fatal("integration database name must contain _test or integration")
	}
	parsed.ParseTime = true
	parsed.ClientFoundRows = true
	parsed.Loc = mentionLocation()
	db, err := gorm.Open(mysql.Open(parsed.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "8.") {
		t.Fatalf("MySQL 8 is required, got %s", version)
	}
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin").AutoMigrate(&model.FeishuMonitoredChat{}, &model.FeishuSourceMessage{}, &model.FeishuMessageRecipient{}, &model.FeishuDailyReport{}, &model.FeishuDeliveryJob{}, &model.FeishuDigestSchedule{}, &model.AsyncJobOutbox{}, &model.RuntimeConfig{}); err != nil {
		t.Fatal(err)
	}
	cfg := appConfig.FeishuMentionConfig{Enabled: true, ReceiverEnabled: true, PersonalReminderEnabled: personal, FollowupEnabled: personal, DigestEnabled: true, DigestTime: "18:00", MaxAttempts: 3, APITimeoutSeconds: 1, DeliveryLeaseSeconds: 10, RetentionDays: 90}
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, mentionLocation())
	s := NewFeishuMentionService(db, "test_"+uuid.NewString(), cfg)
	s.now = func() time.Time { return now }
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, chatID := range []string{"chat_a", "chat_b"} {
		if err := db.Create(&model.FeishuMonitoredChat{AppID: s.appID, ChatID: chatID, ChatName: chatID, Enabled: true, FollowupMinutes: 30}).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = db.Transaction(func(tx *gorm.DB) error {
			var jobs []model.FeishuDeliveryJob
			tx.Where("app_id = ?", s.appID).Find(&jobs)
			for _, row := range jobs {
				tx.Where("task_key LIKE ?", fmt.Sprintf("feishu-mention:%d:g:%%", row.ID)).Delete(&model.AsyncJobOutbox{})
			}
			var sources []model.FeishuSourceMessage
			tx.Where("app_id = ?", s.appID).Find(&sources)
			for _, source := range sources {
				tx.Where("source_message_id = ?", source.ID).Delete(&model.FeishuMessageRecipient{})
			}
			for _, value := range []any{&model.FeishuDeliveryJob{}, &model.FeishuDailyReport{}, &model.FeishuSourceMessage{}, &model.FeishuMonitoredChat{}, &model.FeishuDigestSchedule{}} {
				tx.Where("app_id = ?", s.appID).Delete(value)
			}
			tx.Where("config_key = ?", mentionDigestSettingsKey(s.appID)).Delete(&model.RuntimeConfig{})
			return nil
		})
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return s, &now
}
func mentionTestEvent(s *FeishuMentionService, chat, id string, users ...string) FeishuMentionEvent {
	event := FeishuMentionEvent{AppID: s.appID, MessageID: id, ChatID: chat, ChatType: "group", SenderType: "user", SenderOpenID: "sender", SenderName: "发送人", MessageType: "text", Content: `{"text":"本期内容"}`, CreatedAt: s.now()}
	for _, user := range users {
		event.Mentions = append(event.Mentions, FeishuMention{OpenID: user, Name: user, MentionedType: "user"})
	}
	return event
}
func mentionTestDeliver(t *testing.T, p *FeishuMentionProcessor, row model.FeishuDeliveryJob) error {
	t.Helper()
	raw, _ := json.Marshal(job.FeishuMentionTaskPayload{JobID: row.ID, DispatchGeneration: row.DispatchGeneration})
	task, _ := job.NewFeishuMentionTask(raw)
	return p.HandleDelivery(context.Background(), task)
}

type mentionTestClient struct {
	mu     sync.Mutex
	calls  []string
	fail   bool
	onSend func()
}

func (c *mentionTestClient) SendCard(_ context.Context, id, card, key string) (string, error) {
	c.mu.Lock()
	c.calls = append(c.calls, key)
	fail := c.fail
	hook := c.onSend
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	if fail {
		return "", errors.New("temporary send failure")
	}
	return "om_" + key, nil
}
func (c *mentionTestClient) UpdateCard(context.Context, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "update")
	return nil
}

type mentionTestInspector struct {
	state   asynq.TaskState
	missing bool
}

func (i mentionTestInspector) GetTaskInfo(string, string) (*asynq.TaskInfo, error) {
	if i.missing {
		return nil, asynq.ErrTaskNotFound
	}
	return &asynq.TaskInfo{State: i.state}, nil
}

func TestFeishuMentionMySQLDefaultArchiveAndDigest(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	event := mentionTestEvent(s, "chat_a", "m1", "u1", "u1", "u2")
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Archive(ctx, event) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_b", "m2", "u1")); err != nil {
		t.Fatal(err)
	}
	var sourceCount, recipientCount, jobCount int64
	s.db.Model(&model.FeishuSourceMessage{}).Where("app_id = ?", s.appID).Count(&sourceCount)
	s.db.Table("feishu_message_recipients r").Joins("JOIN feishu_source_messages m ON m.id=r.source_message_id").Where("m.app_id = ? AND r.view_status = ?", s.appID, "not_required").Count(&recipientCount)
	s.db.Model(&model.FeishuDeliveryJob{}).Where("app_id = ?", s.appID).Count(&jobCount)
	if sourceCount != 2 || recipientCount != 3 || jobCount != 0 {
		t.Fatalf("sources=%d recipients=%d jobs=%d", sourceCount, recipientCount, jobCount)
	}
	*now = now.Add(9 * time.Hour)
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var reports []model.FeishuDailyReport
	s.db.Where("app_id = ?", s.appID).Find(&reports)
	if len(reports) != 3 {
		t.Fatalf("reports=%d", len(reports))
	}
	for _, report := range reports {
		var snapshot model.FeishuDigestSnapshot
		if err := json.Unmarshal([]byte(report.ContentSnapshot), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.MessageCount != 1 || snapshot.ChatID != report.ChatID || snapshot.RecipientOpenID != report.RecipientOpenID {
			t.Fatalf("wrong scope: %#v", snapshot)
		}
	}
	*now = now.Add(24 * time.Hour)
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var count int64
	s.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", s.appID).Count(&count)
	if count != 3 {
		t.Fatal("historical messages rolled into next report")
	}
}
func TestFeishuMentionMySQLConfirmAndLease(t *testing.T) {
	s, now := mentionMySQLFixture(t, true)
	ctx := context.Background()
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "m1", "u1")); err != nil {
		t.Fatal(err)
	}
	var initial model.FeishuDeliveryJob
	s.db.Where("app_id = ? AND job_type = ?", s.appID, "initial_reminder").First(&initial)
	client := &mentionTestClient{}
	p := NewFeishuMentionProcessor(s, client, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- mentionTestDeliver(t, p, initial) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(client.calls) != 1 {
		t.Fatalf("duplicate send: %v", client.calls)
	}
	s.db.First(&initial, initial.ID)
	var followup model.FeishuDeliveryJob
	s.db.Where("app_id = ? AND job_type = ?", s.appID, "followup_reminder").First(&followup)
	if !followup.ScheduledAt.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("followup scheduled at %s", followup.ScheduledAt)
	}
	input := FeishuMentionConfirmation{AppID: s.appID, OperatorOpenID: "other", MessageID: initial.FeishuMessageID, Action: "confirm_view", RecipientRecordID: *initial.RecipientRecordID, PayloadVersion: 1}
	if _, err := s.Confirm(ctx, input); !errors.Is(err, ErrFeishuMentionForbidden) {
		t.Fatalf("foreign confirmation=%v", err)
	}
	input.OperatorOpenID = "u1"
	already, err := s.Confirm(ctx, input)
	if err != nil || already {
		t.Fatalf("first confirm=%v %v", already, err)
	}
	already, err = s.Confirm(ctx, input)
	if err != nil || !already {
		t.Fatalf("repeat confirm=%v %v", already, err)
	}
	s.db.First(&followup, followup.ID)
	if followup.Status != "cancelled" || followup.CancelReason != "confirmed" {
		t.Fatalf("followup=%#v", followup)
	}
	var updates int64
	s.db.Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND job_type = ?", s.appID, "card_update").Count(&updates)
	if updates != 1 {
		t.Fatalf("card update duplicates: %d", updates)
	}
	s.cfg.PersonalReminderEnabled = false
	if _, err := s.Confirm(ctx, input); !errors.Is(err, ErrFeishuMentionDisabled) {
		t.Fatalf("disabled old card=%v", err)
	}
}
func TestFeishuMentionMySQLOutboxRollbackAndQueueRecovery(t *testing.T) {
	s, _ := mentionMySQLFixture(t, true)
	ctx := context.Background()
	callback := "mention_test_reject_outbox"
	if err := s.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "async_job_outbox" {
			tx.AddError(errors.New("test outbox unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "m1", "u1"))
	s.db.Callback().Create().Remove(callback)
	if err == nil {
		t.Fatal("expected failed transaction")
	}
	var count int64
	s.db.Model(&model.FeishuSourceMessage{}).Where("app_id = ?", s.appID).Count(&count)
	if count != 0 {
		t.Fatal("source survived outbox rollback")
	}
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "m1", "u1")); err != nil {
		t.Fatal(err)
	}
	var initial model.FeishuDeliveryJob
	s.db.Where("app_id = ?", s.appID).First(&initial)
	s.db.Model(&model.AsyncJobOutbox{}).Where("task_key = ?", job.FeishuMentionTaskKey(initial.ID, 1)).Update("published_at", s.now())
	p := NewFeishuMentionProcessor(s, &mentionTestClient{}, mentionTestInspector{state: asynq.TaskStateRetry})
	if err := p.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.db.First(&initial, initial.ID)
	if initial.DispatchGeneration != 1 {
		t.Fatal("recovery duplicated live retry")
	}
	p.inspector = mentionTestInspector{missing: true}
	if err := p.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.db.First(&initial, initial.ID)
	if initial.DispatchGeneration != 2 || initial.CycleAttempts != 0 {
		t.Fatalf("recovery=%#v", initial)
	}
}
func TestFeishuMentionMySQLDigestSerialAndRetention(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	event := mentionTestEvent(s, "chat_a", "m1", "u1")
	raw, _ := json.Marshal(map[string]string{"text": strings.Repeat("这是很长的群消息。", 6000)})
	event.Content = string(raw)
	if err := s.Archive(ctx, event); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(9 * time.Hour)
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var jobs []model.FeishuDeliveryJob
	s.db.Where("app_id = ?", s.appID).Order("part_no").Find(&jobs)
	if len(jobs) < 3 {
		t.Fatalf("parts=%d", len(jobs))
	}
	client := &mentionTestClient{}
	p := NewFeishuMentionProcessor(s, client, nil)
	if err := mentionTestDeliver(t, p, jobs[1]); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 0 {
		t.Fatal("second part sent before first")
	}
	if err := mentionTestDeliver(t, p, jobs[0]); err != nil {
		t.Fatal(err)
	}
	client.fail = true
	if err := mentionTestDeliver(t, p, jobs[1]); err == nil {
		t.Fatal("expected retry error")
	}
	if err := mentionTestDeliver(t, p, jobs[2]); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 {
		t.Fatal("third part sent after second failed")
	}
	*now = now.Add(91 * 24 * time.Hour)
	if err := s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var reportCount int64
	s.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", s.appID).Count(&reportCount)
	if reportCount != 1 {
		t.Fatal("retention deleted incomplete report")
	}
	client.fail = false
	for _, row := range jobs[1:] {
		if err := mentionTestDeliver(t, p, row); err != nil {
			t.Fatal(err)
		}
	}
	// Mimic outbox dispatch, then a fully successful default-mode report is eligible.
	for _, row := range jobs {
		s.db.Model(&model.AsyncJobOutbox{}).Where("task_key = ?", job.FeishuMentionTaskKey(row.ID, 1)).Update("published_at", s.now())
	}
	if err := s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	s.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", s.appID).Count(&reportCount)
	if reportCount != 0 {
		t.Fatal("retention kept completed not_required records")
	}
}

func TestFeishuMentionMySQLPlannerCommitsPairsAndResumes(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	for _, chat := range []string{"chat_a", "chat_b"} {
		if err := s.Archive(ctx, mentionTestEvent(s, chat, "m_"+chat, "u1")); err != nil {
			t.Fatal(err)
		}
	}
	*now = now.Add(9 * time.Hour)
	var original model.FeishuDigestSchedule
	s.db.Where("app_id = ?", s.appID).First(&original)
	callback := "mention_test_fail_second_report"
	created := 0
	if err := s.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "feishu_daily_reports" {
			created++
			if created == 2 {
				tx.AddError(errors.New("test planner interruption"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := s.PlanDigests(ctx)
	s.db.Callback().Create().Remove(callback)
	if err == nil {
		t.Fatal("expected interrupted planner")
	}
	var count int64
	s.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", s.appID).Count(&count)
	if count != 1 {
		t.Fatalf("first pair was not independently committed: %d", count)
	}
	var cursor model.FeishuDigestSchedule
	s.db.First(&cursor, original.ID)
	if !cursor.NextCutoffAt.Equal(original.NextCutoffAt) {
		t.Fatal("cursor advanced before all pairs persisted")
	}
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	s.db.Model(&model.FeishuDailyReport{}).Where("app_id = ?", s.appID).Count(&count)
	if count != 2 {
		t.Fatalf("resume duplicated or skipped a pair: %d", count)
	}
	s.db.First(&cursor, original.ID)
	if !cursor.NextWindowStartAt.Equal(original.NextCutoffAt) {
		t.Fatal("cursor did not advance after completed window")
	}
}

func TestFeishuMentionMySQLSlowDigestDoesNotBlockIntake(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "m_before", "u1")); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(9 * time.Hour)
	entered := make(chan struct{})
	release := make(chan struct{})
	callback := "mention_test_slow_report"
	if err := s.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "feishu_daily_reports" {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	planned := make(chan error, 1)
	go func() { planned <- s.PlanDigests(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("planner did not reach report")
	}
	intakeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err := s.Archive(intakeCtx, mentionTestEvent(s, "chat_b", "m_after", "u1"))
	cancel()
	close(release)
	planErr := <-planned
	s.db.Callback().Create().Remove(callback)
	if err != nil {
		t.Fatalf("slow digest blocked new intake: %v", err)
	}
	if planErr != nil {
		t.Fatal(planErr)
	}
	var reports []model.FeishuDailyReport
	s.db.Where("app_id = ?", s.appID).Find(&reports)
	if len(reports) != 1 || reports[0].ChatID != "chat_a" {
		t.Fatalf("late intake leaked into closed window: %#v", reports)
	}
}

func TestFeishuMentionMySQLRecoveryPassesPausedRows(t *testing.T) {
	s, now := mentionMySQLFixture(t, false)
	ctx := context.Background()
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "m1", "u1")); err != nil {
		t.Fatal(err)
	}
	var source model.FeishuSourceMessage
	s.db.Where("app_id = ?", s.appID).First(&source)
	var recipient model.FeishuMessageRecipient
	s.db.Where("source_message_id = ?", source.ID).First(&recipient)
	paused := make([]model.FeishuDeliveryJob, 501)
	for i := range paused {
		paused[i] = newMentionJob(s.appID, "u1", "card_update", fmt.Sprintf("paused:%s:%d", s.appID, i), s.now(), model.JSONText(`{}`))
		paused[i].RecipientRecordID = &recipient.ID
		paused[i].TargetFeishuMessageID = fmt.Sprintf("om_%d", i)
	}
	if err := s.db.CreateInBatches(&paused, 100).Error; err != nil {
		t.Fatal(err)
	}
	*now = now.Add(9 * time.Hour)
	if err := s.PlanDigests(ctx); err != nil {
		t.Fatal(err)
	}
	var digest model.FeishuDeliveryJob
	s.db.Where("app_id = ? AND job_type = ?", s.appID, "daily_digest").First(&digest)
	s.db.Model(&model.AsyncJobOutbox{}).Where("task_key = ?", job.FeishuMentionTaskKey(digest.ID, 1)).Update("published_at", s.now())
	processor := NewFeishuMentionProcessor(s, &mentionTestClient{}, mentionTestInspector{missing: true})
	if err := processor.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.db.First(&digest, digest.ID)
	if digest.DispatchGeneration != 2 {
		t.Fatal("paused card updates starved the later missing digest task")
	}
	var resumed int64
	s.db.Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND job_type = ? AND dispatch_generation > ?", s.appID, "card_update", 1).Count(&resumed)
	if resumed != 0 {
		t.Fatal("recovery resumed disabled card updates")
	}
}
