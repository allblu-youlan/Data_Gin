package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PlanDigests briefly fences intake at the persisted cutoff, then commits each
// group/person snapshot independently. Slow reports cannot hold the intake lock.
func (s *FeishuMentionService) PlanDigests(ctx context.Context) error {
	if !s.cfg.Enabled || !s.cfg.DigestEnabled {
		return nil
	}
	for windows := 0; windows < 31; windows++ {
		settings, err := loadMentionDigestSettings(ctx, s.db, s.appID, s.cfg.DigestTime)
		if err != nil {
			return err
		}
		var schedule model.FeishuDigestSchedule
		due := false
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ?", s.appID).First(&schedule).Error; err != nil {
				return err
			}
			if !schedule.Enabled {
				return nil
			}
			now := s.now()
			// Finish every overdue window using its persisted clock before
			// applying a changed clock to the next unplanned cutoff date.
			if schedule.NextCutoffAt.After(now) {
				next, err := rescheduleMentionCutoff(schedule, settings.DigestTime)
				if err != nil {
					return err
				}
				if !next.Equal(schedule.NextCutoffAt) {
					if err := tx.Model(&schedule).Update("next_cutoff_at", next).Error; err != nil {
						return err
					}
					schedule.NextCutoffAt = next
				}
			}
			due = !schedule.NextCutoffAt.After(now)
			return nil
		}); err != nil {
			return err
		}
		if !due {
			return nil
		}
		// Every intake transaction with received_at before this cutoff committed
		// before the fence above. Newly received events belong to a later window.
		var pairs []struct {
			ChatID          string
			RecipientOpenID string
		}
		if err := s.db.WithContext(ctx).Table("feishu_message_recipients AS r").Select("DISTINCT m.chat_id, r.recipient_open_id").Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").Where("m.app_id = ? AND m.received_at >= ? AND m.received_at < ?", s.appID, schedule.NextWindowStartAt, schedule.NextCutoffAt).Order("m.chat_id, r.recipient_open_id").Scan(&pairs).Error; err != nil {
			return err
		}
		for _, pair := range pairs {
			if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return s.freezeDigest(ctx, tx, pair.ChatID, pair.RecipientOpenID, schedule.NextWindowStartAt, schedule.NextCutoffAt)
			}); err != nil {
				return err
			}
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current model.FeishuDigestSchedule
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, schedule.ID).Error; err != nil {
				return err
			}
			if !current.Enabled || !current.NextWindowStartAt.Equal(schedule.NextWindowStartAt) || !current.NextCutoffAt.Equal(schedule.NextCutoffAt) {
				return nil
			}
			now := s.now()
			next := schedule.NextCutoffAt.In(mentionLocation()).AddDate(0, 0, 1)
			return tx.Model(&current).Updates(map[string]any{"next_window_start_at": schedule.NextCutoffAt, "next_cutoff_at": next, "last_planned_at": now}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func rescheduleMentionCutoff(schedule model.FeishuDigestSchedule, clock string) (time.Time, error) {
	parsed, err := time.Parse("15:04", clock)
	if err != nil {
		return time.Time{}, fmt.Errorf("feishu mention: invalid digest time: %w", err)
	}
	date := schedule.NextCutoffAt.In(mentionLocation())
	next := time.Date(date.Year(), date.Month(), date.Day(), parsed.Hour(), parsed.Minute(), 0, 0, mentionLocation())
	// A first activation can begin after the newly configured cutoff time.
	// Move only the cutoff forward; never discard the unreported interval.
	for !next.After(schedule.NextWindowStartAt) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

func (s *FeishuMentionService) freezeDigest(ctx context.Context, tx *gorm.DB, chatID, openID string, start, end time.Time) error {
	date := end.In(mentionLocation()).Format("2006-01-02")
	var existing model.FeishuDailyReport
	err := tx.Where("app_id = ? AND chat_id = ? AND recipient_open_id = ? AND report_date = ?", s.appID, chatID, openID, date).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var messages []model.FeishuDigestMessage
	if err := tx.Table("feishu_message_recipients r").Select("r.id AS recipient_record_id, m.id AS source_message_id, m.message_id, m.sender_open_id, m.sender_name, m.message_created_at, m.received_at, m.display_text, NULLIF(m.original_url, '') AS original_url").Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").Where("m.app_id = ? AND m.chat_id = ? AND r.recipient_open_id = ? AND m.received_at >= ? AND m.received_at < ?", s.appID, chatID, openID, start, end).Order("m.message_created_at, r.id").Scan(&messages).Error; err != nil {
		return err
	}
	if len(messages) == 0 {
		return nil
	}
	var group model.FeishuMonitoredChat
	if err := tx.Where("app_id = ? AND chat_id = ?", s.appID, chatID).First(&group).Error; err != nil {
		return err
	}
	var recipient model.FeishuMessageRecipient
	if err := tx.First(&recipient, messages[0].RecipientRecordID).Error; err != nil {
		return err
	}
	loc := mentionLocation()
	now := s.now()
	snapshot := model.FeishuDigestSnapshot{SchemaVersion: 1, AppID: s.appID, ChatID: chatID, ChatName: group.ChatName, RecipientOpenID: openID, RecipientName: recipient.RecipientName, ReportDate: date, Timezone: "Asia/Shanghai", WindowStart: start.In(loc), WindowEnd: end.In(loc), GeneratedAt: now.In(loc), MessageCount: len(messages), Messages: messages}
	for i := range snapshot.Messages {
		snapshot.Messages[i].MessageCreatedAt = snapshot.Messages[i].MessageCreatedAt.In(loc)
		snapshot.Messages[i].ReceivedAt = snapshot.Messages[i].ReceivedAt.In(loc)
	}
	parts, err := buildMentionDigestParts(snapshot)
	if err != nil {
		return err
	}
	snapshot.Parts = parts
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	report := model.FeishuDailyReport{AppID: s.appID, ChatID: chatID, RecipientOpenID: openID, ReportDate: date, WindowStart: start, WindowEnd: end, GeneratedAt: now, Status: "ready", PartCount: uint(len(parts)), ContentSnapshot: model.JSONText(raw)}
	if err := tx.Create(&report).Error; err != nil {
		// Another planner may have committed this exact immutable report.
		if isFeishuMentionDuplicate(err) {
			return nil
		}
		return err
	}
	for _, message := range messages {
		result := tx.Model(&model.FeishuMessageRecipient{}).Where("id = ? AND (first_report_id IS NULL OR first_report_id = ?)", message.RecipientRecordID, report.ID).Update("first_report_id", report.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("feishu mention: recipient %d already belongs to another report", message.RecipientRecordID)
		}
	}
	for _, part := range parts {
		card, err := mentionDigestCard(snapshot, part)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(model.FeishuDeliverySnapshot{CardJSON: card, ChatID: chatID})
		row := newMentionJob(s.appID, openID, "daily_digest", fmt.Sprintf("digest:%d:part:%d", report.ID, part.PartNo), now, model.JSONText(raw))
		partNo := part.PartNo
		row.PartNo = &partNo
		row.ReportID = &report.ID
		if err := createMentionJob(ctx, tx, &row, part.PartNo == 1); err != nil {
			return err
		}
	}
	return nil
}

// The serialized outer content includes escaping of the JSON card string.
const mentionDigestMaxBodyBytes = 28000

func mentionDigestBodySize(openID, card string) int {
	body, _ := json.Marshal(map[string]string{"receive_id": openID, "msg_type": "interactive", "content": card, "uuid": "00000000-0000-0000-0000-000000000000"})
	return len(body)
}
func buildMentionDigestParts(snapshot model.FeishuDigestSnapshot) ([]model.FeishuDigestPart, error) {
	var fragments []model.FeishuDigestFragment
	for _, message := range snapshot.Messages {
		chunks := splitMentionText(message.DisplayText, 3000)
		for i, chunk := range chunks {
			fragments = append(fragments, model.FeishuDigestFragment{RecipientRecordID: message.RecipientRecordID, FragmentNo: i + 1, FragmentCount: len(chunks), Text: chunk})
		}
	}
	var parts []model.FeishuDigestPart
	current := model.FeishuDigestPart{PartNo: 1, Title: mentionDigestTitle(snapshot, 999999, 999999)}
	for _, fragment := range fragments {
		candidate := current
		candidate.Fragments = append(append([]model.FeishuDigestFragment{}, current.Fragments...), fragment)
		card, err := mentionDigestCard(snapshot, candidate)
		if err != nil {
			return nil, err
		}
		if mentionDigestBodySize(snapshot.RecipientOpenID, card) > mentionDigestMaxBodyBytes {
			if len(current.Fragments) == 0 {
				return nil, errors.New("feishu mention: digest metadata exceeds card limit")
			}
			parts = append(parts, current)
			current = model.FeishuDigestPart{PartNo: uint(len(parts) + 1), Title: mentionDigestTitle(snapshot, 999999, 999999), Fragments: []model.FeishuDigestFragment{fragment}}
		} else {
			current = candidate
		}
	}
	if len(current.Fragments) > 0 {
		parts = append(parts, current)
	}
	for i := range parts {
		parts[i].PartNo = uint(i + 1)
		parts[i].Title = mentionDigestTitle(snapshot, i+1, len(parts))
		card, err := mentionDigestCard(snapshot, parts[i])
		if err != nil {
			return nil, err
		}
		if mentionDigestBodySize(snapshot.RecipientOpenID, card) > mentionDigestMaxBodyBytes {
			return nil, errors.New("feishu mention: digest card too large")
		}
	}
	return parts, nil
}
func splitMentionText(text string, maxBytes int) []string {
	if text == "" {
		return []string{""}
	}
	var result []string
	start := 0
	for index := range text {
		if index-start >= maxBytes {
			result = append(result, text[start:index])
			start = index
		}
	}
	if start < len(text) {
		result = append(result, text[start:])
	}
	return result
}
func mentionDigestTitle(snapshot model.FeishuDigestSnapshot, part, total int) string {
	return fmt.Sprintf("%s｜%s的 @ 消息每日总结（%d/%d）", snapshot.ChatName, snapshot.RecipientName, part, total)
}
func mentionDigestCard(snapshot model.FeishuDigestSnapshot, part model.FeishuDigestPart) (string, error) {
	lookup := make(map[uint]model.FeishuDigestMessage, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		lookup[message.RecipientRecordID] = message
	}
	var body strings.Builder
	fmt.Fprintf(&body, "统计期：%s 至 %s\n消息数：%d\n", snapshot.WindowStart.Format("2006-01-02 15:04"), snapshot.WindowEnd.Format("2006-01-02 15:04"), snapshot.MessageCount)
	for _, fragment := range part.Fragments {
		message := lookup[fragment.RecipientRecordID]
		fmt.Fprintf(&body, "\n发送人：%s\n时间：%s", mentionSenderName(message.SenderName, message.SenderOpenID), message.MessageCreatedAt.Format("2006-01-02 15:04:05"))
		if fragment.FragmentCount > 1 {
			fmt.Fprintf(&body, "（片段 %d/%d）", fragment.FragmentNo, fragment.FragmentCount)
		}
		fmt.Fprintf(&body, "\n%s\n", fragment.Text)
		if message.OriginalURL != nil {
			fmt.Fprintf(&body, "原文：%s\n", *message.OriginalURL)
		}
	}
	raw, err := json.Marshal(map[string]any{"config": map[string]bool{"wide_screen_mode": true, "update_multi": true}, "header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": part.Title}}, "elements": []any{map[string]any{"tag": "div", "text": map[string]string{"tag": "plain_text", "content": body.String()}}}})
	return string(raw), err
}
