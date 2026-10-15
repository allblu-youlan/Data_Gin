package data_dao

import (
	"context"
	"time"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

// FeishuMentionAdminObservations exposes only aggregate operational data.
type FeishuMentionAdminObservations struct {
	Outbox       FeishuMentionAdminOutboxStatus   `json:"outbox"`
	Delivery     FeishuMentionAdminDeliveryStatus `json:"delivery"`
	Archive      FeishuMentionAdminArchiveStatus  `json:"archive"`
	Confirmation map[string]int64                 `json:"confirmation"`
	Reports      map[string]int64                 `json:"reports"`
}

type FeishuMentionAdminOutboxStatus struct {
	Unpublished         int64      `json:"unpublished"`
	PublishFailed       int64      `json:"publishFailed"`
	EarliestAvailableAt *time.Time `json:"earliestAvailableAt"`
}

type FeishuMentionAdminDeliveryStatus struct {
	DueWaiting         int64      `json:"dueWaiting"`
	OldestWaitingAt    *time.Time `json:"oldestWaitingAt"`
	EarliestLeaseUntil *time.Time `json:"earliestLeaseUntil"`
	ExpiredLeases      int64      `json:"expiredLeases"`
	CardUpdateFailed   int64      `json:"cardUpdateFailed"`
	DigestPartsFailed  int64      `json:"digestPartsFailed"`
}

type FeishuMentionAdminArchiveStatus struct {
	Messages         int64      `json:"messages"`
	LatestReceivedAt *time.Time `json:"latestReceivedAt"`
	EnabledChats     int64      `json:"enabledChats"`
	WindowMessages   *int64     `json:"windowMessages"`
}

func (d *FeishuMentionAdminDAO) Observations(ctx context.Context, now time.Time, schedule *model.FeishuDigestSchedule) (*FeishuMentionAdminObservations, error) {
	result := &FeishuMentionAdminObservations{}
	// The payload contains only the job ID and dispatch generation. Join on the
	// persisted job to scope all unpublished generations to the current app.
	err := d.DB.WithContext(ctx).Table("async_job_outbox o").
		Joins("JOIN feishu_delivery_jobs j ON j.id = CAST(JSON_UNQUOTE(JSON_EXTRACT(o.payload_json, '$.job_id')) AS UNSIGNED)").
		Where("j.app_id = ? AND o.task_type = ? AND o.published_at IS NULL", d.AppID, "feishu:mention:deliver").
		Select("COUNT(*) AS unpublished, COALESCE(SUM(CASE WHEN o.last_error_safe <> '' THEN 1 ELSE 0 END), 0) AS publish_failed, MIN(o.available_at) AS earliest_available_at").
		Scan(&result.Outbox).Error
	if err != nil {
		return nil, err
	}
	err = d.DB.WithContext(ctx).Model(&model.FeishuDeliveryJob{}).Where("app_id = ?", d.AppID).
		Select(`COALESCE(SUM(CASE WHEN status IN ('pending','retry_wait') AND scheduled_at <= ? THEN 1 ELSE 0 END), 0) AS due_waiting,
		MIN(CASE WHEN status IN ('pending','retry_wait') THEN scheduled_at END) AS oldest_waiting_at,
		MIN(CASE WHEN status = 'sending' THEN lease_until END) AS earliest_lease_until,
		COALESCE(SUM(CASE WHEN status = 'sending' AND lease_until <= ? THEN 1 ELSE 0 END), 0) AS expired_leases,
		COALESCE(SUM(CASE WHEN job_type = 'card_update' AND status = 'failed' THEN 1 ELSE 0 END), 0) AS card_update_failed,
		COALESCE(SUM(CASE WHEN job_type = 'daily_digest' AND status = 'failed' THEN 1 ELSE 0 END), 0) AS digest_parts_failed`, now, now).
		Scan(&result.Delivery).Error
	if err != nil {
		return nil, err
	}
	err = d.DB.WithContext(ctx).Model(&model.FeishuSourceMessage{}).Where("app_id = ?", d.AppID).
		Select("COUNT(*) AS messages, MAX(received_at) AS latest_received_at").Scan(&result.Archive).Error
	if err != nil {
		return nil, err
	}
	err = d.DB.WithContext(ctx).Model(&model.FeishuMonitoredChat{}).Where("app_id = ? AND enabled = ?", d.AppID, true).Count(&result.Archive.EnabledChats).Error
	if err != nil {
		return nil, err
	}
	if schedule != nil {
		var count int64
		err = d.DB.WithContext(ctx).Model(&model.FeishuSourceMessage{}).
			Where("app_id = ? AND received_at >= ? AND received_at < ? AND message_created_at >= ? AND message_created_at < ?", d.AppID, schedule.NextWindowStartAt, schedule.NextCutoffAt, schedule.NextWindowStartAt, schedule.NextCutoffAt).Count(&count).Error
		if err != nil {
			return nil, err
		}
		result.Archive.WindowMessages = &count
	}
	result.Confirmation, err = mentionAdminStatusCounts(d.DB.WithContext(ctx).Table("feishu_message_recipients r").
		Joins("JOIN feishu_source_messages m ON m.id = r.source_message_id").Where("m.app_id = ?", d.AppID).
		Select("r.view_status AS status, COUNT(*) AS count").Group("r.view_status"))
	if err != nil {
		return nil, err
	}
	result.Reports, err = mentionAdminStatusCounts(d.DB.WithContext(ctx).Model(&model.FeishuDailyReport{}).
		Where("app_id = ?", d.AppID).Select("status, COUNT(*) AS count").Group("status"))
	return result, err
}

func mentionAdminStatusCounts(query *gorm.DB) (map[string]int64, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, row := range rows {
		result[row.Status] = row.Count
	}
	return result, nil
}
