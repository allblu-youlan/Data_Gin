package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var (
	ErrFeishuMentionDisabled  = errors.New("个人提醒与确认功能未启用")
	ErrFeishuMentionForbidden = errors.New("无权确认此提醒")
	ErrFeishuMentionInvalid   = errors.New("无效的提醒操作")
)

type FeishuMention struct {
	Key           string `json:"key"`
	OpenID        string `json:"openId"`
	Name          string `json:"name"`
	MentionedType string `json:"mentionedType"`
	TenantKey     string `json:"tenantKey"`
}
type FeishuMentionEvent struct {
	AppID, TenantKey, MessageID, ChatID, ChatType, SenderType, SenderOpenID, SenderName, MessageType, Content string
	CreatedAt                                                                                                 time.Time
	Mentions                                                                                                  []FeishuMention
}
type FeishuMentionConfirmation struct {
	AppID, OperatorOpenID, MessageID, Action string
	RecipientRecordID                        uint
	PayloadVersion                           int
}
type FeishuMentionService struct {
	db    *gorm.DB
	appID string
	cfg   appConfig.FeishuMentionConfig
	now   func() time.Time
}

func NewFeishuMentionService(db *gorm.DB, appID string, cfg appConfig.FeishuMentionConfig) *FeishuMentionService {
	now := func() time.Time { return time.Now().In(mentionLocation()) }
	if db != nil {
		db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent), NowFunc: now})
	}
	return &FeishuMentionService{db: db, appID: appID, cfg: cfg, now: now}
}

func (s *FeishuMentionService) Initialize(ctx context.Context) error {
	if !s.cfg.Enabled {
		return nil
	}
	now := s.now()
	cutoff, err := nextMentionCutoff(now, s.cfg.DigestTime)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		schedule := model.FeishuDigestSchedule{AppID: s.appID, Enabled: true, NextWindowStartAt: now, NextCutoffAt: cutoff}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schedule).Error; err != nil {
			return err
		}
		return s.cancelDisabled(tx)
	})
}
func (s *FeishuMentionService) cancelDisabled(tx *gorm.DB) error {
	q := tx.Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND status IN ?", s.appID, []string{"pending", "retry_wait", "failed"})
	if !s.cfg.PersonalReminderEnabled {
		return q.Where("job_type IN ?", []string{"initial_reminder", "followup_reminder"}).Updates(map[string]any{"status": "cancelled", "cancel_reason": "personal_reminder_disabled"}).Error
	}
	if !s.cfg.FollowupEnabled {
		return q.Where("job_type = ?", "followup_reminder").Updates(map[string]any{"status": "cancelled", "cancel_reason": "followup_disabled"}).Error
	}
	return nil
}

func (s *FeishuMentionService) Archive(ctx context.Context, event FeishuMentionEvent) error {
	if !s.cfg.Enabled || !s.cfg.ReceiverEnabled {
		return nil
	}
	if event.AppID != s.appID {
		return ErrFeishuMentionForbidden
	}
	if event.ChatType != "group" || event.SenderType != "user" {
		return nil
	}
	mentions := uniqueFeishuMentions(event.Mentions)
	if len(mentions) == 0 {
		return nil
	}
	if event.MessageID == "" || event.ChatID == "" || event.CreatedAt.IsZero() || event.SenderOpenID == "" {
		return ErrFeishuMentionInvalid
	}
	display, err := ParseFeishuMentionContent(event.MessageType, event.Content, event.Mentions)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group model.FeishuMonitoredChat
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("app_id = ? AND chat_id = ? AND enabled = ?", s.appID, event.ChatID, true).First(&group).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		// The schedule lock ensures a planner cannot finish a cutoff while an older receive transaction is uncommitted.
		var schedule model.FeishuDigestSchedule
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("app_id = ?", s.appID).First(&schedule).Error; err != nil {
			return err
		}
		now := s.now()
		encoded, _ := json.Marshal(mentions)
		source := model.FeishuSourceMessage{AppID: s.appID, EventTenantKey: event.TenantKey, MessageID: event.MessageID, ChatID: event.ChatID, SenderOpenID: event.SenderOpenID, SenderName: event.SenderName, MessageType: event.MessageType, MessageCreatedAt: event.CreatedAt.In(mentionLocation()), ReceivedAt: now, RawContent: model.JSONText(event.Content), DisplayText: display, MentionsSnapshot: model.JSONText(encoded)}
		// A duplicate-key statement rolls back only that statement in MySQL.
		// Avoid affected-row heuristics, which change with clientFoundRows.
		if err := tx.Create(&source).Error; err != nil {
			if isFeishuMentionDuplicate(err) {
				return nil
			}
			return err
		}
		for _, m := range mentions {
			status := "not_required"
			if s.cfg.PersonalReminderEnabled {
				status = "pending"
			}
			recipient := model.FeishuMessageRecipient{SourceMessageID: source.ID, RecipientOpenID: m.OpenID, RecipientName: m.Name, RecipientTenantKey: m.TenantKey, ViewStatus: status}
			if err := tx.Create(&recipient).Error; err != nil {
				return err
			}
			if !s.cfg.PersonalReminderEnabled {
				continue
			}
			card, err := mentionReminderCard(source, recipient, group.ChatName, false, false)
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(model.FeishuDeliverySnapshot{CardJSON: card, ChatID: event.ChatID, FollowupMinutes: group.FollowupMinutes})
			row := newMentionJob(s.appID, m.OpenID, "initial_reminder", fmt.Sprintf("initial:%d", recipient.ID), now, model.JSONText(payload))
			row.RecipientRecordID = &recipient.ID
			if err := createMentionJob(ctx, tx, &row, true); err != nil {
				return err
			}
		}
		return nil
	})
}

func uniqueFeishuMentions(mentions []FeishuMention) []FeishuMention {
	result := make([]FeishuMention, 0, len(mentions))
	seen := map[string]bool{}
	for _, m := range mentions {
		if m.OpenID == "" || m.OpenID == "all" || m.MentionedType != "user" || seen[m.OpenID] {
			continue
		}
		seen[m.OpenID] = true
		result = append(result, m)
	}
	return result
}

// ParseFeishuMentionContent displays text and post nodes without interpreting source text as card markup.
func ParseFeishuMentionContent(messageType, raw string, mentions []FeishuMention) (string, error) {
	var content map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return "", fmt.Errorf("feishu mention: invalid content JSON: %w", err)
	}
	names := map[string]string{}
	for _, m := range mentions {
		names[m.Key] = m.Name
		names[m.OpenID] = m.Name
	}
	var display string
	switch messageType {
	case "text":
		if err := json.Unmarshal(content["text"], &display); err != nil {
			return "", fmt.Errorf("feishu mention: invalid text: %w", err)
		}
	case "post":
		if _, ok := content["content"]; !ok {
			for _, lang := range []string{"zh_cn", "en_us", "ja_jp"} {
				if value, ok := content[lang]; ok {
					if err := json.Unmarshal(value, &content); err != nil {
						return "", err
					}
					break
				}
			}
		}
		var title string
		_ = json.Unmarshal(content["title"], &title)
		var rows [][]map[string]any
		if err := json.Unmarshal(content["content"], &rows); err != nil {
			return "", fmt.Errorf("feishu mention: invalid post: %w", err)
		}
		var lines []string
		if title != "" {
			lines = append(lines, title)
		}
		for _, row := range rows {
			var line strings.Builder
			for _, node := range row {
				str := func(key string) string { v, _ := node[key].(string); return v }
				switch str("tag") {
				case "text", "md", "code_block":
					line.WriteString(str("text"))
				case "a":
					line.WriteString(str("text"))
					if href := str("href"); href != "" {
						line.WriteString(" (" + href + ")")
					}
				case "at":
					name := names[str("user_id")]
					if name == "" {
						name = str("user_name")
					}
					if name == "" {
						name = "用户"
					}
					line.WriteString("@" + name)
				case "img":
					line.WriteString("[图片]")
				case "media":
					line.WriteString("[视频]")
				case "emotion":
					line.WriteString("[表情]")
				case "hr":
					line.WriteString("\n—\n")
				default:
					line.WriteString("[附件]")
				}
			}
			lines = append(lines, line.String())
		}
		display = strings.Join(lines, "\n")
	default:
		display = "[" + messageType + " 消息]"
	}
	keys := make([]string, 0, len(names))
	for key := range names {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		display = strings.ReplaceAll(display, key, "@"+names[key])
	}
	return display, nil
}

func (s *FeishuMentionService) Confirm(ctx context.Context, input FeishuMentionConfirmation) (bool, error) {
	if !s.cfg.Enabled || !s.cfg.PersonalReminderEnabled {
		return false, ErrFeishuMentionDisabled
	}
	if input.Action != "confirm_view" || input.PayloadVersion != 1 || input.RecipientRecordID == 0 || input.MessageID == "" {
		return false, ErrFeishuMentionInvalid
	}
	if input.AppID != s.appID {
		return false, ErrFeishuMentionForbidden
	}
	already := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var recipient model.FeishuMessageRecipient
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&recipient, input.RecipientRecordID).Error; err != nil {
			return err
		}
		var source model.FeishuSourceMessage
		if err := tx.Where("id = ? AND app_id = ?", recipient.SourceMessageID, s.appID).First(&source).Error; err != nil {
			return ErrFeishuMentionForbidden
		}
		if recipient.RecipientOpenID != input.OperatorOpenID || recipient.ViewStatus == "not_required" {
			return ErrFeishuMentionForbidden
		}
		var sent []model.FeishuDeliveryJob
		if err := tx.Where("app_id = ? AND recipient_record_id = ? AND job_type IN ? AND status = ?", s.appID, recipient.ID, []string{"initial_reminder", "followup_reminder"}, "sent").Find(&sent).Error; err != nil {
			return err
		}
		valid := false
		for _, row := range sent {
			if row.FeishuMessageID == input.MessageID {
				valid = true
				break
			}
		}
		if !valid {
			return ErrFeishuMentionForbidden
		}
		already = recipient.ViewStatus == "confirmed"
		if !already {
			now := s.now()
			if err := tx.Model(&recipient).Updates(map[string]any{"view_status": "confirmed", "confirmed_at": now}).Error; err != nil {
				return err
			}
			recipient.ViewStatus = "confirmed"
			recipient.ConfirmedAt = &now
		}
		if err := tx.Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND recipient_record_id = ? AND job_type IN ? AND status IN ?", s.appID, recipient.ID, []string{"initial_reminder", "followup_reminder"}, []string{"pending", "retry_wait", "failed"}).Updates(map[string]any{"status": "cancelled", "cancel_reason": "confirmed"}).Error; err != nil {
			return err
		}
		for _, row := range sent {
			if err := s.createCardUpdate(ctx, tx, row, recipient, source); err != nil {
				return err
			}
		}
		return nil
	})
	return already, err
}
func newMentionJob(appID, openID, kind, key string, at time.Time, payload model.JSONText) model.FeishuDeliveryJob {
	return model.FeishuDeliveryJob{AppID: appID, RecipientOpenID: openID, JobType: kind, BusinessKey: key, SendUUID: uuid.NewString(), Status: "pending", ScheduledAt: at, DispatchGeneration: 1, PayloadSnapshot: payload}
}
func createMentionJob(ctx context.Context, tx *gorm.DB, row *model.FeishuDeliveryJob, publish bool) error {
	if err := tx.Create(row).Error; err != nil {
		if isFeishuMentionDuplicate(err) {
			return nil
		}
		return err
	}
	if publish {
		return data_dao.CreateFeishuMentionOutbox(ctx, tx, row)
	}
	return nil
}
func (s *FeishuMentionService) createCardUpdate(ctx context.Context, tx *gorm.DB, sent model.FeishuDeliveryJob, recipient model.FeishuMessageRecipient, source model.FeishuSourceMessage) error {
	var old model.FeishuDeliverySnapshot
	if err := json.Unmarshal([]byte(sent.PayloadSnapshot), &old); err != nil {
		return err
	}
	// Preserve the card's original group name and body; only replace its action.
	card, err := confirmedMentionCard(old.CardJSON)
	if err != nil {
		return err
	}
	old.CardJSON = card
	payload, _ := json.Marshal(old)
	row := newMentionJob(s.appID, recipient.RecipientOpenID, "card_update", fmt.Sprintf("card_update:%s:%s:confirmed", s.appID, sent.FeishuMessageID), s.now(), model.JSONText(payload))
	row.RecipientRecordID = &recipient.ID
	row.TargetFeishuMessageID = sent.FeishuMessageID
	return createMentionJob(ctx, tx, &row, true)
}
func mentionReminderCard(source model.FeishuSourceMessage, recipient model.FeishuMessageRecipient, chatName string, followup, confirmed bool) (string, error) {
	title := chatName + "｜有人 @ 你"
	if followup {
		title = chatName + "｜再次提醒查看"
	}
	preview := source.DisplayText
	if len(preview) > 3000 {
		preview = splitMentionText(preview, 3000)[0] + "\n【内容已截取，完整原文见群消息或管理详情】"
	}
	body := fmt.Sprintf("发送人：%s\n发送时间：%s\n状态：待确认查看\n\n%s", mentionSenderName(source.SenderName, source.SenderOpenID), source.MessageCreatedAt.In(mentionLocation()).Format("2006-01-02 15:04:05"), preview)
	elements := []any{map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": body}}}
	if confirmed {
		elements = append(elements, mentionConfirmedElement())
	} else {
		elements = append(elements, map[string]any{"tag": "action", "actions": []any{map[string]any{"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "我已查看"}, "type": "primary", "value": map[string]any{"action": "confirm_view", "recipient_record_id": fmt.Sprint(recipient.ID), "payload_version": 1}}}})
	}
	raw, err := json.Marshal(map[string]any{"config": map[string]any{"wide_screen_mode": true, "update_multi": true}, "header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": title}}, "elements": elements})
	return string(raw), err
}
func mentionConfirmedElement() any {
	return map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "已确认查看（不代表事项已处理完成）"}}
}
func confirmedMentionCard(raw string) (string, error) {
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		return "", err
	}
	elements, _ := card["elements"].([]any)
	for i, element := range elements {
		if node, ok := element.(map[string]any); ok && node["tag"] == "action" {
			elements[i] = mentionConfirmedElement()
		}
	}
	for _, element := range elements {
		if node, ok := element.(map[string]any); ok && node["tag"] == "div" {
			if text, ok := node["text"].(map[string]any); ok {
				if content, ok := text["content"].(string); ok {
					text["content"] = strings.Replace(content, "状态：待确认查看", "状态：已确认查看", 1)
				}
			}
		}
	}
	card["elements"] = elements
	value, err := json.Marshal(card)
	return string(value), err
}
func mentionLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return loc
}
func nextMentionCutoff(now time.Time, clock string) (time.Time, error) {
	parsed, err := time.Parse("15:04", clock)
	if err != nil {
		return time.Time{}, err
	}
	local := now.In(mentionLocation())
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), parsed.Hour(), parsed.Minute(), 0, 0, local.Location())
	if !cutoff.After(now) {
		cutoff = cutoff.AddDate(0, 0, 1)
	}
	return cutoff, nil
}

func mentionSenderName(name, openID string) string {
	if name != "" {
		return name
	}
	return "群成员（" + openID + "）"
}

func isFeishuMentionDuplicate(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
