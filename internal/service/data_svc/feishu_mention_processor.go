package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FeishuMentionCardClient interface {
	SendCard(context.Context, string, string, string) (string, error)
	UpdateCard(context.Context, string, string) error
}
type FeishuMentionTaskInspector interface {
	GetTaskInfo(string, string) (*asynq.TaskInfo, error)
}
type FeishuMentionLimiter interface{ Wait(context.Context) error }
type FeishuMentionProcessor struct {
	service   *FeishuMentionService
	client    FeishuMentionCardClient
	inspector FeishuMentionTaskInspector
	limiter   FeishuMentionLimiter
}

func NewFeishuMentionProcessor(s *FeishuMentionService, client FeishuMentionCardClient, inspector FeishuMentionTaskInspector) *FeishuMentionProcessor {
	return &FeishuMentionProcessor{service: s, client: client, inspector: inspector}
}
func (p *FeishuMentionProcessor) SetLimiter(limiter FeishuMentionLimiter) { p.limiter = limiter }

func (p *FeishuMentionProcessor) HandleDelivery(ctx context.Context, task *asynq.Task) error {
	payload, err := job.DecodeFeishuMentionTaskPayload(task.Payload())
	if err != nil {
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}
	s := p.service
	if !s.cfg.Enabled {
		return nil
	}
	if p.limiter != nil {
		if err := p.limiter.Wait(ctx); err != nil {
			return err
		}
	}
	var row model.FeishuDeliveryJob
	var snapshot model.FeishuDeliverySnapshot
	claimed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND app_id = ?", payload.JobID, s.appID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if row.DispatchGeneration != payload.DispatchGeneration || (row.Status != "pending" && row.Status != "retry_wait") || row.ScheduledAt.After(s.now()) {
			return nil
		}
		allowed, reason, err := s.deliveryAllowed(tx, row)
		if err != nil {
			return err
		}
		if reason != "" {
			return tx.Model(&row).Updates(map[string]any{"status": "cancelled", "cancel_reason": reason}).Error
		}
		if !allowed {
			return nil
		}
		if row.CycleAttempts >= uint(s.cfg.MaxAttempts) {
			return s.markDeliveryFailed(tx, &row, "attempts_exhausted", "本轮发送次数已用尽")
		}
		if err := json.Unmarshal([]byte(row.PayloadSnapshot), &snapshot); err != nil || snapshot.CardJSON == "" || !json.Valid([]byte(snapshot.CardJSON)) {
			return s.markDeliveryFailed(tx, &row, "invalid_snapshot", "发送快照无效")
		}
		if p.client == nil {
			return errors.New("feishu mention: client unavailable")
		}
		row.Status = "sending"
		row.LeaseToken = uuid.NewString()
		until := s.now().Add(time.Duration(s.cfg.DeliveryLeaseSeconds) * time.Second)
		row.LeaseUntil = &until
		row.Attempts++
		row.CycleAttempts++
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil || !claimed {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.APITimeoutSeconds)*2*time.Second)
	defer cancel()
	var messageID string
	if row.JobType == "card_update" {
		err = p.client.UpdateCard(callCtx, row.TargetFeishuMessageID, snapshot.CardJSON)
	} else {
		messageID, err = p.client.SendCard(callCtx, row.RecipientOpenID, snapshot.CardJSON, row.SendUUID)
		if err == nil && messageID == "" {
			err = errors.New("feishu returned no message id")
		}
	}
	return p.finish(ctx, row, messageID, err, officePushRetryable(err))
}

// deliveryAllowed never uses confirmation state to filter a daily report.
func (s *FeishuMentionService) deliveryAllowed(tx *gorm.DB, row model.FeishuDeliveryJob) (bool, string, error) {
	if !s.cfg.Enabled {
		return false, "", nil
	}
	if row.JobType == "daily_digest" {
		if !s.cfg.DigestEnabled {
			return false, "", nil
		}
		var schedule model.FeishuDigestSchedule
		if err := tx.Where("app_id = ?", s.appID).First(&schedule).Error; err != nil {
			return false, "", err
		}
		if !schedule.Enabled {
			return false, "", nil
		}
		if row.ReportID == nil || row.PartNo == nil || *row.PartNo == 0 {
			return false, "", errors.New("invalid digest association")
		}
		var report model.FeishuDailyReport
		if err := tx.Where("id = ? AND app_id = ? AND recipient_open_id = ?", *row.ReportID, s.appID, row.RecipientOpenID).First(&report).Error; err != nil {
			return false, "", err
		}
		var count int64
		err := tx.Model(&model.FeishuDeliveryJob{}).Where("report_id = ? AND part_no < ? AND status <> ?", *row.ReportID, *row.PartNo, "sent").Count(&count).Error
		return count == 0, "", err
	}
	if row.RecipientRecordID == nil {
		return false, "", errors.New("invalid recipient association")
	}
	var recipient model.FeishuMessageRecipient
	if err := tx.First(&recipient, *row.RecipientRecordID).Error; err != nil {
		return false, "", err
	}
	var source model.FeishuSourceMessage
	if err := tx.Where("id = ? AND app_id = ?", recipient.SourceMessageID, s.appID).First(&source).Error; err != nil {
		return false, "", err
	}
	if recipient.RecipientOpenID != row.RecipientOpenID {
		return false, "", ErrFeishuMentionForbidden
	}
	if row.JobType == "card_update" {
		return s.cfg.PersonalReminderEnabled && recipient.ViewStatus == "confirmed" && row.TargetFeishuMessageID != "", "", nil
	}
	if row.JobType != "initial_reminder" && row.JobType != "followup_reminder" {
		return false, "", errors.New("invalid delivery job type")
	}
	if !s.cfg.PersonalReminderEnabled {
		return false, "personal_reminder_disabled", nil
	}
	if row.JobType == "followup_reminder" && !s.cfg.FollowupEnabled {
		return false, "followup_disabled", nil
	}
	if recipient.ViewStatus != "pending" {
		return false, "confirmed", nil
	}
	var group model.FeishuMonitoredChat
	err := tx.Where("app_id = ? AND chat_id = ?", s.appID, source.ChatID).First(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && !group.Enabled {
		return false, "group_disabled", nil
	}
	if err != nil {
		return false, "", err
	}
	if row.JobType == "followup_reminder" && recipient.InitialSentAt == nil {
		return false, "", errors.New("followup lacks successful initial delivery")
	}
	return true, "", nil
}

func (p *FeishuMentionProcessor) finish(ctx context.Context, claimed model.FeishuDeliveryJob, messageID string, sendErr error, retryable bool) error {
	s := p.service
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	retry := sendErr != nil && retryable && claimed.CycleAttempts < uint(s.cfg.MaxAttempts)
	var retryDelay time.Duration
	err := s.db.WithContext(stateCtx).Transaction(func(tx *gorm.DB) error {
		// Confirmation and completion both lock the recipient before touching its jobs.
		var recipient model.FeishuMessageRecipient
		if claimed.RecipientRecordID != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&recipient, *claimed.RecipientRecordID).Error; err != nil {
				return err
			}
		}
		var row model.FeishuDeliveryJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND lease_token = ? AND status = ?", claimed.ID, claimed.LeaseToken, "sending").First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		now := s.now()
		if sendErr != nil {
			code := "send_failed"
			safe := "飞书请求失败，请核对权限、目标用户与服务状态"
			var botErr *feishu.BotError
			if errors.As(sendErr, &botErr) {
				code = string(botErr.Class)
				if botErr.Code != 0 {
					code = fmt.Sprintf("feishu_%d", botErr.Code)
				}
			}
			if !retry {
				return s.markDeliveryFailed(tx, &row, code, safe)
			}
			delay := time.Duration(1<<min(row.CycleAttempts, 6)) * time.Second
			var delayErr interface{ RetryDelay() time.Duration }
			if errors.As(sendErr, &delayErr) && delayErr.RetryDelay() > delay {
				delay = delayErr.RetryDelay()
			}
			retryDelay = delay
			return tx.Model(&row).Updates(map[string]any{"status": "retry_wait", "scheduled_at": now.Add(delay), "lease_token": "", "lease_until": nil, "last_error_code": code, "last_error_message": safe}).Error
		}
		if err := tx.Model(&row).Updates(map[string]any{"status": "sent", "sent_at": now, "feishu_message_id": messageID, "lease_token": "", "lease_until": nil, "last_error_code": "", "last_error_message": ""}).Error; err != nil {
			return err
		}
		row.SentAt = &now
		row.FeishuMessageID = messageID
		row.Status = "sent"
		switch row.JobType {
		case "initial_reminder", "followup_reminder":
			field := "initial_sent_at"
			if row.JobType == "followup_reminder" {
				field = "followup_sent_at"
			}
			if err := tx.Model(&recipient).Update(field, now).Error; err != nil {
				return err
			}
			var source model.FeishuSourceMessage
			if err := tx.First(&source, recipient.SourceMessageID).Error; err != nil {
				return err
			}
			if recipient.ViewStatus == "confirmed" {
				return s.createCardUpdate(stateCtx, tx, row, recipient, source)
			}
			if row.JobType == "initial_reminder" && s.cfg.PersonalReminderEnabled && s.cfg.FollowupEnabled && recipient.ViewStatus == "pending" {
				var group model.FeishuMonitoredChat
				if err := tx.Where("app_id = ? AND chat_id = ?", s.appID, source.ChatID).First(&group).Error; err != nil {
					return err
				}
				if !group.Enabled {
					return nil
				}
				var snapshot model.FeishuDeliverySnapshot
				if err := json.Unmarshal([]byte(row.PayloadSnapshot), &snapshot); err != nil {
					return err
				}
				card, err := mentionFollowupCard(snapshot.CardJSON)
				if err != nil {
					return err
				}
				snapshot.CardJSON = card
				data, _ := json.Marshal(snapshot)
				followup := newMentionJob(s.appID, row.RecipientOpenID, "followup_reminder", fmt.Sprintf("followup:%d", recipient.ID), now.Add(time.Duration(snapshot.FollowupMinutes)*time.Minute), model.JSONText(data))
				followup.RecipientRecordID = &recipient.ID
				return createMentionJob(stateCtx, tx, &followup, true)
			}
		case "daily_digest":
			var next model.FeishuDeliveryJob
			err := tx.Where("report_id = ? AND part_no = ?", row.ReportID, *row.PartNo+1).First(&next).Error
			if err == nil {
				return data_dao.CreateFeishuMentionOutbox(stateCtx, tx, &next)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Model(&model.FeishuDailyReport{}).Where("id = ?", *row.ReportID).Updates(map[string]any{"status": "sent", "sent_at": now}).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	if sendErr != nil {
		if retry {
			return feishuMentionRetryError{cause: sendErr, delay: retryDelay}
		}
		return fmt.Errorf("%w: feishu delivery failed", asynq.SkipRetry)
	}
	return nil
}
func (s *FeishuMentionService) markDeliveryFailed(tx *gorm.DB, row *model.FeishuDeliveryJob, code, message string) error {
	if err := tx.Model(row).Updates(map[string]any{"status": "failed", "lease_token": "", "lease_until": nil, "last_error_code": code, "last_error_message": message}).Error; err != nil {
		return err
	}
	if row.ReportID != nil {
		return tx.Model(&model.FeishuDailyReport{}).Where("id = ?", *row.ReportID).Update("status", "failed").Error
	}
	return nil
}
func mentionFollowupCard(raw string) (string, error) {
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		return "", err
	}
	header, _ := card["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	title["content"] = fmt.Sprint(title["content"]) + "（再次提醒查看）"
	value, err := json.Marshal(card)
	return string(value), err
}

func (p *FeishuMentionProcessor) HandlePlan(ctx context.Context, task *asynq.Task) error {
	if err := job.DecodeOfficePushScheduleTaskPayload(task.Payload()); err != nil {
		return fmt.Errorf("%w: invalid mention planner payload", asynq.SkipRetry)
	}
	s := p.service
	if !s.cfg.Enabled {
		return nil
	}
	if err := s.Initialize(ctx); err != nil {
		return err
	}
	if err := s.PlanDigests(ctx); err != nil {
		return err
	}
	if err := p.Recover(ctx); err != nil {
		return err
	}
	// The first daily minute performs conservative, transactional retention; no pending references are removed.
	if s.now().In(mentionLocation()).Hour() == 3 && s.now().Minute() == 0 {
		return s.Cleanup(ctx)
	}
	return nil
}

func (p *FeishuMentionProcessor) Recover(ctx context.Context) error {
	s := p.service
	if !s.cfg.Enabled || p.inspector == nil {
		return nil
	}
	var afterID uint
	for {
		var candidates []model.FeishuDeliveryJob
		if err := s.db.WithContext(ctx).Where("id > ? AND app_id = ? AND ((status IN ? AND scheduled_at <= ?) OR (status = ? AND lease_until <= ?))", afterID, s.appID, []string{"pending", "retry_wait"}, s.now(), "sending", s.now()).Order("id").Limit(500).Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			key := job.FeishuMentionTaskKey(candidate.ID, candidate.DispatchGeneration)
			var outbox model.AsyncJobOutbox
			err := s.db.WithContext(ctx).Where("task_key = ?", key).First(&outbox).Error
			if err == nil && outbox.PublishedAt == nil {
				continue
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			info, err := p.inspector.GetTaskInfo(job.FeishuMentionQueueName, key)
			if err != nil && !errors.Is(err, asynq.ErrTaskNotFound) && !errors.Is(err, asynq.ErrQueueNotFound) {
				return fmt.Errorf("feishu mention: inspect recovery queue: %w", err)
			}
			if info != nil && (info.State == asynq.TaskStateActive || info.State == asynq.TaskStatePending || info.State == asynq.TaskStateRetry || info.State == asynq.TaskStateScheduled || info.State == asynq.TaskStateAggregating) {
				continue
			}
			err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var row model.FeishuDeliveryJob
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, candidate.ID).Error; err != nil {
					return err
				}
				if row.DispatchGeneration != candidate.DispatchGeneration || (row.Status != "pending" && row.Status != "retry_wait" && row.Status != "sending") || row.LeaseUntil != nil && row.LeaseUntil.After(s.now()) {
					return nil
				}
				allowed, reason, err := s.deliveryAllowed(tx, row)
				if err != nil {
					return err
				}
				if reason != "" {
					return tx.Model(&row).Updates(map[string]any{"status": "cancelled", "cancel_reason": reason}).Error
				}
				if !allowed {
					return nil
				}
				// An unpublished current generation may have been created after the initial read.
				var n int64
				if err := tx.Model(&model.AsyncJobOutbox{}).Where("task_key = ? AND published_at IS NULL", key).Count(&n).Error; err != nil {
					return err
				}
				if n > 0 {
					return nil
				}
				if row.CycleAttempts >= uint(s.cfg.MaxAttempts) {
					return s.markDeliveryFailed(tx, &row, "attempts_exhausted", "本轮发送次数已用尽")
				}
				row.DispatchGeneration++
				row.Status = "pending"
				row.LeaseToken = ""
				row.LeaseUntil = nil
				if err := tx.Save(&row).Error; err != nil {
					return err
				}
				return data_dao.CreateFeishuMentionOutbox(ctx, tx, &row)
			})
			if err != nil {
				return err
			}
		}
		if len(candidates) < 500 {
			return nil
		}
		afterID = candidates[len(candidates)-1].ID
	}
}

type feishuMentionRetryError struct {
	cause error
	delay time.Duration
}

func (e feishuMentionRetryError) Error() string             { return "feishu mention: temporary delivery failure" }
func (e feishuMentionRetryError) Unwrap() error             { return e.cause }
func (e feishuMentionRetryError) RetryDelay() time.Duration { return e.delay }
