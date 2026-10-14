package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResendReport rebuilds today's manual report from archived messages. The source
// report identifies the group and recipient; its frozen daily content is untouched.
func (s *FeishuMentionAdminService) ResendReport(ctx context.Context, id, actor uint) (*FeishuMentionReportDTO, error) {
	if id == 0 || actor == 0 {
		return nil, ErrFeishuMentionAdminInvalid
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !s.config.Enabled || !s.config.DigestEnabled {
		return nil, ErrFeishuMentionAdminPaused
	}
	clickedAt := s.now().In(mentionLocation())
	start := time.Date(clickedAt.Year(), clickedAt.Month(), clickedAt.Day(), 0, 0, 0, 0, mentionLocation())
	date := clickedAt.Format("2006-01-02")
	var source model.FeishuDailyReport
	if err := s.dao.DB.WithContext(ctx).Select("id", "chat_id", "recipient_open_id").Where("app_id = ? AND id = ?", s.dao.AppID, id).Take(&source).Error; err != nil {
		return nil, mentionAdminError(err)
	}
	var reportID uint
	err := s.dao.Transaction(ctx, func(d *data_dao.FeishuMentionAdminDAO) error {
		// Intake holds this group's shared lock until it commits. The exclusive
		// lock also serializes manual creation before its report/job rows exist.
		var group model.FeishuMonitoredChat
		if err := d.DB.Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ? AND chat_id = ?", d.AppID, source.ChatID).Take(&group).Error; err != nil {
			return err
		}
		schedule, err := d.Schedule(ctx)
		if err != nil {
			return err
		}
		if schedule == nil || !schedule.Enabled {
			return ErrFeishuMentionAdminPaused
		}
		var report model.FeishuDailyReport
		err = d.DB.Select("id").Where("app_id = ? AND chat_id = ? AND recipient_open_id = ? AND report_date = ? AND report_kind = ?", d.AppID, source.ChatID, source.RecipientOpenID, date, "manual").Take(&report).Error
		create := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !create {
			return err
		}
		var delivery model.FeishuDeliveryJob
		var before any
		if !create {
			// Match worker ordering: lock the delivery before its report.
			var jobs []model.FeishuDeliveryJob
			if err := d.DB.Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ? AND report_id = ?", d.AppID, report.ID).Order("id").Find(&jobs).Error; err != nil {
				return err
			}
			if err := d.DB.Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ? AND id = ?", d.AppID, report.ID).First(&report).Error; err != nil {
				return err
			}
			if len(jobs) != 1 || !mentionOneOf(report.Status, "sent", "failed") {
				return ErrFeishuMentionAdminConflict
			}
			delivery = jobs[0]
			if delivery.JobType != "daily_digest" || delivery.PartNo == nil || *delivery.PartNo != 1 || delivery.RecipientOpenID != source.RecipientOpenID ||
				!mentionOneOf(delivery.Status, "sent", "failed", "cancelled") || delivery.LeaseUntil != nil && delivery.LeaseUntil.After(s.now()) {
				return ErrFeishuMentionAdminConflict
			}
			before = map[string]any{"status": report.Status, "windowEnd": report.WindowEnd, "dispatchGeneration": delivery.DispatchGeneration}
		}
		snapshot, err := mentionManualSnapshot(d.DB, d.AppID, group, source.RecipientOpenID, start, clickedAt)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if create {
			report = model.FeishuDailyReport{
				AppID: d.AppID, ChatID: source.ChatID, RecipientOpenID: source.RecipientOpenID, ReportDate: date, ReportKind: "manual",
				WindowStart: start, WindowEnd: clickedAt, GeneratedAt: clickedAt, Status: "ready", PartCount: 1, ContentSnapshot: model.JSONText(raw),
			}
			if err := d.DB.Create(&report).Error; err != nil {
				return err
			}
		} else if err := d.DB.Model(&report).Updates(map[string]any{
			"window_start": start, "window_end": clickedAt, "generated_at": clickedAt, "status": "ready", "part_count": 1,
			"content_snapshot": model.JSONText(raw), "sent_at": nil,
		}).Error; err != nil {
			return err
		}
		card, err := compactMentionDigestCard(snapshot, report.ID, s.publicURL)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(model.FeishuDeliverySnapshot{CardJSON: card, ChatID: source.ChatID})
		if err != nil {
			return err
		}
		if create {
			delivery = newMentionJob(d.AppID, source.RecipientOpenID, "daily_digest", fmt.Sprintf("digest:%d:part:1", report.ID), clickedAt, model.JSONText(payload))
			partNo := uint(1)
			delivery.ReportID, delivery.PartNo = &report.ID, &partNo
			if err := createMentionJob(ctx, d.DB, &delivery, true); err != nil {
				return err
			}
		} else {
			delivery.Status, delivery.SendUUID = "pending", uuid.NewString()
			delivery.DispatchGeneration++
			delivery.ScheduledAt, delivery.CycleAttempts = clickedAt, 0
			delivery.PayloadSnapshot = model.JSONText(payload)
			delivery.LeaseToken, delivery.CancelReason, delivery.FeishuMessageID, delivery.LastErrorCode, delivery.LastErrorMessage = "", "", "", "", ""
			delivery.LeaseUntil, delivery.SentAt = nil, nil
			if err := d.DB.Save(&delivery).Error; err != nil {
				return err
			}
			if err := data_dao.CreateFeishuMentionOutbox(ctx, d.DB, &delivery); err != nil {
				return err
			}
		}
		reportID = report.ID
		return mentionAudit(ctx, d, actor, "FEISHU_REPORT_REGENERATE", "feishu_daily_report", report.ID, before, map[string]any{
			"sourceReportId": id, "reportKind": "manual", "status": "ready", "windowStart": start, "windowEnd": clickedAt,
			"messageCount": snapshot.MessageCount, "dispatchGeneration": delivery.DispatchGeneration,
		})
	})
	if err != nil {
		return nil, mentionAdminError(err)
	}
	return s.Report(ctx, reportID)
}

func mentionManualSnapshot(db *gorm.DB, appID string, group model.FeishuMonitoredChat, openID string, start, end time.Time) (model.FeishuDigestSnapshot, error) {
	var messages []model.FeishuDigestMessage
	if err := db.Table("feishu_message_recipients r").
		Select("r.id AS recipient_record_id, m.id AS source_message_id, m.message_id, m.sender_open_id, m.sender_name, m.message_created_at, m.received_at, m.display_text, NULLIF(m.original_url, '') AS original_url").
		Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").
		Where("m.app_id = ? AND m.chat_id = ? AND r.recipient_open_id = ? AND m.received_at >= ? AND m.received_at < ?", appID, group.ChatID, openID, start, end).
		Order("m.message_created_at, r.id").Scan(&messages).Error; err != nil {
		return model.FeishuDigestSnapshot{}, err
	}
	var identity struct{ RecipientName string }
	if err := db.Table("feishu_message_recipients r").Select("r.recipient_name").
		Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").
		Where("m.app_id = ? AND m.chat_id = ? AND r.recipient_open_id = ?", appID, group.ChatID, openID).
		Order("m.received_at DESC, r.id DESC").Limit(1).Scan(&identity).Error; err != nil {
		return model.FeishuDigestSnapshot{}, err
	}
	snapshot := model.FeishuDigestSnapshot{
		SchemaVersion: 2, ReportKind: "manual", AppID: appID, ChatID: group.ChatID, ChatName: group.ChatName,
		RecipientOpenID: openID, RecipientName: identity.RecipientName, ReportDate: end.Format("2006-01-02"), Timezone: "Asia/Shanghai",
		WindowStart: start, WindowEnd: end, GeneratedAt: end, MessageCount: len(messages), Messages: messages,
	}
	for i := range snapshot.Messages {
		snapshot.Messages[i].MessageCreatedAt = snapshot.Messages[i].MessageCreatedAt.In(mentionLocation())
		snapshot.Messages[i].ReceivedAt = snapshot.Messages[i].ReceivedAt.In(mentionLocation())
	}
	snapshot.Parts = []model.FeishuDigestPart{{PartNo: 1, Title: mentionDigestTitle(snapshot, 1, 1)}}
	return snapshot, nil
}
