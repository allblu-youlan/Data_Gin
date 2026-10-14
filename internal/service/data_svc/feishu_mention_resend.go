package data_svc

import (
	"context"
	"encoding/json"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// ResendReport explicitly starts a new delivery cycle. Unlike a failed-job
// retry it needs a new Feishu UUID, so a successfully sent card can be sent again.
func (s *FeishuMentionAdminService) ResendReport(ctx context.Context, id, actor uint) (*FeishuMentionReportDTO, error) {
	if id == 0 || actor == 0 {
		return nil, ErrFeishuMentionAdminInvalid
	}
	if !s.config.Enabled || !s.config.DigestEnabled {
		return nil, ErrFeishuMentionAdminPaused
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	err := s.dao.Transaction(ctx, func(d *data_dao.FeishuMentionAdminDAO) error {
		// Workers lock jobs before updating reports. Use the same order here.
		var jobs []model.FeishuDeliveryJob
		if err := d.DB.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ? AND report_id = ?", d.AppID, id).Order("id").Find(&jobs).Error; err != nil {
			return err
		}
		var report model.FeishuDailyReport
		if err := d.DB.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ? AND id = ?", d.AppID, id).First(&report).Error; err != nil {
			return err
		}
		if !mentionOneOf(report.Status, "sent", "failed") || len(jobs) == 0 {
			return ErrFeishuMentionAdminConflict
		}
		schedule, err := d.Schedule(ctx)
		if err != nil {
			return err
		}
		if schedule == nil || !schedule.Enabled {
			return ErrFeishuMentionAdminPaused
		}
		now := s.now()
		var first *model.FeishuDeliveryJob
		for i := range jobs {
			j := &jobs[i]
			if j.JobType != "daily_digest" || j.PartNo == nil || j.RecipientOpenID != report.RecipientOpenID || j.Status == "sending" || j.Status == "retry_wait" || (j.LeaseUntil != nil && j.LeaseUntil.After(now)) {
				return ErrFeishuMentionAdminConflict
			}
			if j.Status == "pending" && report.Status != "failed" {
				return ErrFeishuMentionAdminConflict
			}
			if *j.PartNo == 1 {
				first = j
			}
		}
		if first == nil {
			return ErrFeishuMentionAdminConflict
		}
		var snapshot model.FeishuDigestSnapshot
		if err := json.Unmarshal([]byte(report.ContentSnapshot), &snapshot); err != nil {
			return err
		}
		card, err := compactMentionDigestCard(snapshot, id, s.publicURL)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(model.FeishuDeliverySnapshot{CardJSON: card, ChatID: report.ChatID})
		if err != nil {
			return err
		}
		before := map[string]any{"status": report.Status, "partCount": report.PartCount, "dispatchGeneration": first.DispatchGeneration, "feishuMessageId": first.FeishuMessageID}
		// Preserve historical parts, but fence queued tasks from their old cycle.
		for _, j := range jobs {
			if j.ID == first.ID {
				continue
			}
			if err := d.DB.Model(&j).Updates(map[string]any{"status": "cancelled", "cancel_reason": "compact_report_resend", "dispatch_generation": j.DispatchGeneration + 1, "lease_token": "", "lease_until": nil}).Error; err != nil {
				return err
			}
		}
		first.Status = "pending"
		first.SendUUID = uuid.NewString()
		first.DispatchGeneration++
		first.ScheduledAt = now
		first.CycleAttempts = 0
		first.PayloadSnapshot = model.JSONText(payload)
		first.LeaseToken, first.CancelReason, first.FeishuMessageID, first.LastErrorCode, first.LastErrorMessage = "", "", "", "", ""
		first.LeaseUntil, first.SentAt = nil, nil
		if err := d.DB.Save(first).Error; err != nil {
			return err
		}
		if err := d.DB.Model(&report).Updates(map[string]any{"status": "ready", "part_count": 1, "sent_at": nil}).Error; err != nil {
			return err
		}
		if err := data_dao.CreateFeishuMentionOutbox(ctx, d.DB, first); err != nil {
			return err
		}
		return mentionAudit(ctx, d, actor, "FEISHU_REPORT_RESEND", "feishu_daily_report", id, before, map[string]any{"status": "ready", "partCount": 1, "dispatchGeneration": first.DispatchGeneration})
	})
	if err != nil {
		return nil, mentionAdminError(err)
	}
	return s.Report(ctx, id)
}
