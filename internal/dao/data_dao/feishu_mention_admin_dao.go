package data_dao

import (
	"context"
	"errors"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/paginator"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeishuMentionAdminFilter contains only validated, bound query values.
type FeishuMentionAdminFilter struct {
	Page, PerPage                                                 int
	ChatID, Keyword, RecipientOpenID, ViewStatus, Status, JobType string
	Enabled                                                       *bool
	ReceivedFrom, ReceivedTo                                      *time.Time
	ReportDateFrom, ReportDateTo                                  string
	RecipientRecordID, ReportID                                   uint
}

type FeishuMentionAdminDAO struct {
	DB    *gorm.DB
	AppID string
}

func (d *FeishuMentionAdminDAO) WithTx(tx *gorm.DB) *FeishuMentionAdminDAO {
	return &FeishuMentionAdminDAO{DB: tx, AppID: d.AppID}
}
func (d *FeishuMentionAdminDAO) Transaction(ctx context.Context, fn func(*FeishuMentionAdminDAO) error) error {
	if d.DB == nil {
		return errors.New("feishu mention database unavailable")
	}
	return d.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(d.WithTx(tx)) })
}

func mentionPage(query *gorm.DB, f FeishuMentionAdminFilter, dest interface{}) (paginator.Pagination, error) {
	var count int64
	if err := query.Session(&gorm.Session{}).Count(&count).Error; err != nil {
		return paginator.Pagination{}, err
	}
	p := paginator.Pagination{Page: f.Page, PerPage: f.PerPage, TotalCount: int(count), TotalPage: (int(count) + f.PerPage - 1) / f.PerPage}
	if err := query.Limit(f.PerPage).Offset((f.Page - 1) * f.PerPage).Find(dest).Error; err != nil {
		return p, err
	}
	return p, nil
}
func (d *FeishuMentionAdminDAO) Chats(ctx context.Context, f FeishuMentionAdminFilter) ([]model.FeishuMonitoredChat, paginator.Pagination, error) {
	rows := make([]model.FeishuMonitoredChat, 0)
	q := d.DB.WithContext(ctx).Model(&model.FeishuMonitoredChat{}).Where("app_id = ?", d.AppID)
	if f.ChatID != "" {
		q = q.Where("chat_id = ?", f.ChatID)
	}
	if f.Keyword != "" {
		q = q.Where("chat_name LIKE ?", "%"+f.Keyword+"%")
	}
	if f.Enabled != nil {
		q = q.Where("enabled = ?", *f.Enabled)
	}
	p, err := mentionPage(q.Order("id DESC"), f, &rows)
	return rows, p, err
}
func (d *FeishuMentionAdminDAO) Chat(ctx context.Context, id uint, lock bool) (*model.FeishuMonitoredChat, error) {
	q := d.DB.WithContext(ctx).Where("app_id = ? AND id = ?", d.AppID, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row model.FeishuMonitoredChat
	err := q.First(&row).Error
	return &row, err
}
func (d *FeishuMentionAdminDAO) SaveChat(ctx context.Context, row *model.FeishuMonitoredChat, create bool) error {
	if create {
		return d.DB.WithContext(ctx).Create(row).Error
	}
	return d.DB.WithContext(ctx).Model(&model.FeishuMonitoredChat{}).Where("app_id = ? AND id = ?", d.AppID, row.ID).Updates(map[string]interface{}{"chat_name": row.ChatName, "is_external": row.IsExternal, "enabled": row.Enabled, "followup_minutes": row.FollowupMinutes}).Error
}
func (d *FeishuMentionAdminDAO) CancelChatReminders(ctx context.Context, chatID string) error {
	ids := d.DB.Model(&model.FeishuMessageRecipient{}).Select("feishu_message_recipients.id").Joins("JOIN feishu_source_messages s ON s.id = feishu_message_recipients.source_message_id").Where("s.app_id = ? AND s.chat_id = ?", d.AppID, chatID)
	return d.DB.WithContext(ctx).Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND recipient_record_id IN (?) AND job_type IN ? AND status IN ?", d.AppID, ids, []string{"initial_reminder", "followup_reminder"}, []string{"pending", "retry_wait", "failed"}).Updates(map[string]interface{}{"status": "cancelled", "cancel_reason": "group_disabled"}).Error
}

type FeishuMentionAdminRecipientRow struct {
	model.FeishuMessageRecipient
	AppID, MessageID, ChatID, ChatName, SenderOpenID, SenderName, MessageType, DisplayText string
	MessageCreatedAt, ReceivedAt                                                           time.Time
	OriginalURL                                                                            *string
}

func (d *FeishuMentionAdminDAO) recipientQuery(ctx context.Context) *gorm.DB {
	return d.DB.WithContext(ctx).Table("feishu_message_recipients r").Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").Joins("LEFT JOIN feishu_monitored_chats g ON g.app_id = m.app_id AND g.chat_id = m.chat_id").Where("m.app_id = ?", d.AppID)
}

const mentionRecipientColumns = "r.*, m.app_id, m.message_id, m.chat_id, g.chat_name, m.sender_open_id, m.sender_name, m.message_type, m.message_created_at, m.received_at, m.display_text, m.original_url"

func (d *FeishuMentionAdminDAO) Recipients(ctx context.Context, f FeishuMentionAdminFilter) ([]FeishuMentionAdminRecipientRow, paginator.Pagination, error) {
	rows := make([]FeishuMentionAdminRecipientRow, 0)
	q := d.recipientQuery(ctx)
	if f.ChatID != "" {
		q = q.Where("m.chat_id = ?", f.ChatID)
	}
	if f.RecipientOpenID != "" {
		q = q.Where("r.recipient_open_id = ?", f.RecipientOpenID)
	}
	if f.ViewStatus != "" {
		q = q.Where("r.view_status = ?", f.ViewStatus)
	}
	if f.ReceivedFrom != nil {
		q = q.Where("m.received_at >= ? AND m.received_at < ?", *f.ReceivedFrom, *f.ReceivedTo)
	}
	p, err := mentionPage(q.Select(mentionRecipientColumns).Order("m.received_at DESC, r.id DESC"), f, &rows)
	return rows, p, err
}
func (d *FeishuMentionAdminDAO) Recipient(ctx context.Context, id uint, lock bool) (*FeishuMentionAdminRecipientRow, error) {
	q := d.recipientQuery(ctx).Select(mentionRecipientColumns).Where("r.id = ?", id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row FeishuMentionAdminRecipientRow
	err := q.Take(&row).Error
	return &row, err
}
func (d *FeishuMentionAdminDAO) RecipientJobs(ctx context.Context, ids []uint) ([]model.FeishuDeliveryJob, error) {
	rows := make([]model.FeishuDeliveryJob, 0)
	if len(ids) == 0 {
		return rows, nil
	}
	err := d.DB.WithContext(ctx).Where("app_id = ? AND recipient_record_id IN ? AND job_type IN ?", d.AppID, ids, []string{"initial_reminder", "followup_reminder"}).Order("id ASC").Find(&rows).Error
	return rows, err
}
func (d *FeishuMentionAdminDAO) Reports(ctx context.Context, f FeishuMentionAdminFilter) ([]model.FeishuDailyReport, paginator.Pagination, error) {
	rows := make([]model.FeishuDailyReport, 0)
	q := d.DB.WithContext(ctx).Model(&model.FeishuDailyReport{}).Where("app_id = ?", d.AppID)
	if f.ChatID != "" {
		q = q.Where("chat_id = ?", f.ChatID)
	}
	if f.RecipientOpenID != "" {
		q = q.Where("recipient_open_id = ?", f.RecipientOpenID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.ReportDateFrom != "" {
		q = q.Where("report_date >= ? AND report_date <= ?", f.ReportDateFrom, f.ReportDateTo)
	}
	p, err := mentionPage(q.Order("report_date DESC, id DESC"), f, &rows)
	return rows, p, err
}
func (d *FeishuMentionAdminDAO) Report(ctx context.Context, id uint) (*model.FeishuDailyReport, error) {
	var row model.FeishuDailyReport
	err := d.DB.WithContext(ctx).Where("app_id = ? AND id = ?", d.AppID, id).First(&row).Error
	return &row, err
}
func (d *FeishuMentionAdminDAO) ReportJobs(ctx context.Context, ids []uint) ([]model.FeishuDeliveryJob, error) {
	rows := make([]model.FeishuDeliveryJob, 0)
	if len(ids) == 0 {
		return rows, nil
	}
	err := d.DB.WithContext(ctx).Where("app_id = ? AND report_id IN ?", d.AppID, ids).Order("part_no ASC").Find(&rows).Error
	return rows, err
}

type FeishuMentionAdminJobRow struct {
	model.FeishuDeliveryJob
	ChatID string
}

func (d *FeishuMentionAdminDAO) Jobs(ctx context.Context, f FeishuMentionAdminFilter) ([]FeishuMentionAdminJobRow, paginator.Pagination, error) {
	rows := make([]FeishuMentionAdminJobRow, 0)
	q := d.DB.WithContext(ctx).Table("feishu_delivery_jobs j").Joins("LEFT JOIN feishu_message_recipients r ON r.id = j.recipient_record_id").Joins("LEFT JOIN feishu_source_messages m ON m.id = r.source_message_id AND m.app_id = j.app_id").Joins("LEFT JOIN feishu_daily_reports p ON p.id = j.report_id AND p.app_id = j.app_id").Where("j.app_id = ?", d.AppID)
	if f.ChatID != "" {
		q = q.Where("COALESCE(m.chat_id,p.chat_id) = ?", f.ChatID)
	}
	if f.RecipientOpenID != "" {
		q = q.Where("j.recipient_open_id = ?", f.RecipientOpenID)
	}
	if f.Status != "" {
		q = q.Where("j.status = ?", f.Status)
	}
	if f.JobType != "" {
		q = q.Where("j.job_type = ?", f.JobType)
	}
	if f.ReportID != 0 {
		q = q.Where("j.report_id = ?", f.ReportID)
	}
	if f.RecipientRecordID != 0 {
		q = q.Where("j.recipient_record_id = ?", f.RecipientRecordID)
	}
	p, err := mentionPage(q.Select("j.*, COALESCE(m.chat_id,p.chat_id) AS chat_id").Order("j.id DESC"), f, &rows)
	return rows, p, err
}
func (d *FeishuMentionAdminDAO) LockJob(ctx context.Context, id uint) (*model.FeishuDeliveryJob, error) {
	var row model.FeishuDeliveryJob
	err := d.DB.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ? AND id = ?", d.AppID, id).First(&row).Error
	return &row, err
}
func (d *FeishuMentionAdminDAO) GroupEnabled(ctx context.Context, chatID string) (bool, error) {
	var row model.FeishuMonitoredChat
	err := d.DB.WithContext(ctx).Where("app_id = ? AND chat_id = ?", d.AppID, chatID).First(&row).Error
	return row.Enabled, err
}
func (d *FeishuMentionAdminDAO) Requeue(ctx context.Context, row *model.FeishuDeliveryJob, now time.Time) error {
	changes := map[string]interface{}{"status": "pending", "scheduled_at": now, "cycle_attempts": 0, "dispatch_generation": row.DispatchGeneration + 1, "lease_token": nil, "lease_until": nil, "last_error_code": nil, "last_error_message": nil}
	result := d.DB.WithContext(ctx).Model(&model.FeishuDeliveryJob{}).Where("app_id = ? AND id = ? AND status = ?", d.AppID, row.ID, "failed").Updates(changes)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	row.Status = "pending"
	row.ScheduledAt = now
	row.CycleAttempts = 0
	row.DispatchGeneration++
	if row.ReportID != nil {
		if err := d.DB.WithContext(ctx).Model(&model.FeishuDailyReport{}).Where("app_id = ? AND id = ?", d.AppID, *row.ReportID).Update("status", "ready").Error; err != nil {
			return err
		}
	}
	return CreateFeishuMentionOutbox(ctx, d.DB, row)
}
func (d *FeishuMentionAdminDAO) Audit(ctx context.Context, row *model.AuthAudit) error {
	return d.DB.WithContext(ctx).Create(row).Error
}
func (d *FeishuMentionAdminDAO) Backlog(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	err := d.DB.WithContext(ctx).Model(&model.FeishuDeliveryJob{}).Select("status, COUNT(*) AS count").Where("app_id = ?", d.AppID).Group("status").Scan(&rows).Error
	result := map[string]int64{}
	for _, row := range rows {
		result[row.Status] = row.Count
	}
	return result, err
}
func (d *FeishuMentionAdminDAO) Schedule(ctx context.Context) (*model.FeishuDigestSchedule, error) {
	var row model.FeishuDigestSchedule
	err := d.DB.WithContext(ctx).Where("app_id = ?", d.AppID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}
