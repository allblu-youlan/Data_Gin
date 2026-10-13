package data_svc

import (
	"context"
	"time"

	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cleanup removes only closed connected sets of reports and source messages.
// A shared source or report with a pending recipient keeps the entire set intact.
func (s *FeishuMentionService) Cleanup(ctx context.Context) error {
	if !s.cfg.Enabled {
		return nil
	}
	cutoff := s.now().AddDate(0, 0, -s.cfg.RetentionDays)
	var afterID uint
	for {
		nextID, err := s.cleanupMentionBatch(ctx, cutoff, afterID)
		if err != nil || nextID == afterID {
			return err
		}
		afterID = nextID
	}
}

func (s *FeishuMentionService) cleanupMentionBatch(ctx context.Context, cutoff time.Time, afterID uint) (uint, error) {
	nextID := afterID
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var reports []model.FeishuDailyReport
		if err := tx.Where("id > ? AND app_id = ? AND status = ? AND window_end < ?", afterID, s.appID, "sent", cutoff).Order("id").Limit(1000).Find(&reports).Error; err != nil {
			return err
		}
		allowed := map[uint]bool{}
		for _, report := range reports {
			allowed[report.ID] = true
			nextID = report.ID
		}
		if len(allowed) == 0 {
			return nil
		}
		// A message may mention people whose reports straddle this page.
		// Expand to the connected reports before deciding what can be removed.
		for frontier := mentionMapIDs(allowed); len(frontier) > 0; {
			var linked []uint
			if err := tx.Table("feishu_message_recipients AS selected").
				Select("DISTINCT report.id").
				Joins("JOIN feishu_message_recipients related ON related.source_message_id = selected.source_message_id").
				Joins("JOIN feishu_daily_reports report ON report.id = related.first_report_id").
				Where("selected.first_report_id IN ? AND report.app_id = ? AND report.status = ? AND report.window_end < ?", frontier, s.appID, "sent", cutoff).
				Scan(&linked).Error; err != nil {
				return err
			}
			frontier = nil
			for _, id := range linked {
				if !allowed[id] {
					allowed[id] = true
					frontier = append(frontier, id)
				}
			}
		}
		var recipients []model.FeishuMessageRecipient
		ids := mentionMapIDs(allowed)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("first_report_id IN ?", ids).Order("id").Find(&recipients).Error; err != nil {
			return err
		}
		sources := map[uint]bool{}
		for _, recipient := range recipients {
			sources[recipient.SourceMessageID] = true
		}
		if len(sources) == 0 {
			return nil
		}
		var all []model.FeishuMessageRecipient
		if err := tx.Where("source_message_id IN ?", mentionMapIDs(sources)).Find(&all).Error; err != nil {
			return err
		}
		var sourceRows []model.FeishuSourceMessage
		if err := tx.Where("id IN ? AND app_id = ?", mentionMapIDs(sources), s.appID).Find(&sourceRows).Error; err != nil {
			return err
		}
		oldSources := map[uint]bool{}
		for _, source := range sourceRows {
			oldSources[source.ID] = source.ReceivedAt.Before(cutoff)
		}
		var jobs []model.FeishuDeliveryJob
		recipientIDs := make([]uint, 0, len(all))
		for _, recipient := range all {
			recipientIDs = append(recipientIDs, recipient.ID)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ? AND (report_id IN ? OR recipient_record_id IN ?)", s.appID, ids, recipientIDs).Order("id").Find(&jobs).Error; err != nil {
			return err
		}
		blockedRecipients := map[uint]bool{}
		for _, row := range jobs {
			if row.Status != "sent" && row.Status != "cancelled" {
				if row.ReportID != nil {
					delete(allowed, *row.ReportID)
				}
				if row.RecipientRecordID != nil {
					blockedRecipients[*row.RecipientRecordID] = true
				}
				continue
			}
			// Keep jobs whose current outbox has not finished publishing.
			var pending int64
			if err := tx.Model(&model.AsyncJobOutbox{}).Where("task_key = ? AND published_at IS NULL", job.FeishuMentionTaskKey(row.ID, row.DispatchGeneration)).Count(&pending).Error; err != nil {
				return err
			}
			if pending > 0 {
				if row.ReportID != nil {
					delete(allowed, *row.ReportID)
				}
				if row.RecipientRecordID != nil {
					blockedRecipients[*row.RecipientRecordID] = true
				}
			}
		}
		for changed := true; changed; {
			changed = false
			blockedSources := map[uint]bool{}
			for _, recipient := range all {
				if recipient.FirstReportID == nil || !allowed[*recipient.FirstReportID] || (recipient.ViewStatus != "not_required" && recipient.ViewStatus != "confirmed") || blockedRecipients[recipient.ID] || !oldSources[recipient.SourceMessageID] {
					blockedSources[recipient.SourceMessageID] = true
				}
			}
			for _, recipient := range all {
				if blockedSources[recipient.SourceMessageID] && recipient.FirstReportID != nil && allowed[*recipient.FirstReportID] {
					delete(allowed, *recipient.FirstReportID)
					changed = true
				}
			}
		}
		if len(allowed) == 0 {
			return nil
		}
		deleteRecipients := []uint{}
		deleteSources := map[uint]bool{}
		for _, recipient := range all {
			if recipient.FirstReportID != nil && allowed[*recipient.FirstReportID] {
				deleteRecipients = append(deleteRecipients, recipient.ID)
				deleteSources[recipient.SourceMessageID] = true
			}
		}
		for _, row := range jobs {
			included := row.ReportID != nil && allowed[*row.ReportID]
			if !included && row.RecipientRecordID != nil {
				for _, id := range deleteRecipients {
					if id == *row.RecipientRecordID {
						included = true
						break
					}
				}
			}
			if !included {
				continue
			}
			for generation := uint(1); generation <= row.DispatchGeneration; generation++ {
				if err := tx.Where("task_key = ? AND published_at IS NOT NULL", job.FeishuMentionTaskKey(row.ID, generation)).Delete(&model.AsyncJobOutbox{}).Error; err != nil {
					return err
				}
			}
			if err := tx.Delete(&model.FeishuDeliveryJob{}, row.ID).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("id IN ?", deleteRecipients).Delete(&model.FeishuMessageRecipient{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id IN ?", mentionMapIDs(deleteSources)).Delete(&model.FeishuSourceMessage{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", mentionMapIDs(allowed)).Delete(&model.FeishuDailyReport{}).Error
	})
	return nextID, err
}
func mentionMapIDs(values map[uint]bool) []uint {
	ids := make([]uint, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	return ids
}
