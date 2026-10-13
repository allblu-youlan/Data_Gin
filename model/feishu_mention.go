package model

import (
	"context"
	"fmt"
	"gorm.io/gorm/schema"
	"reflect"
	"time"
)

// FeishuMonitoredChat is an explicit inbound group allowlist for one bot application.
type FeishuMonitoredChat struct {
	BaseModel
	AppID           string `gorm:"size:64;not null;uniqueIndex:uk_feishu_chat,priority:1"`
	ChatID          string `gorm:"size:128;not null;uniqueIndex:uk_feishu_chat,priority:2"`
	ChatName        string `gorm:"size:255;not null"`
	IsExternal      bool   `gorm:"not null;default:false"`
	Enabled         bool   `gorm:"not null"`
	FollowupMinutes uint   `gorm:"not null;default:30"`
	WeatherTimestamps
}

func (FeishuMonitoredChat) TableName() string { return "feishu_monitored_chats" }

type FeishuSourceMessage struct {
	BaseModel
	AppID            string    `gorm:"size:64;not null;uniqueIndex:uk_feishu_source,priority:1;index:idx_feishu_source_chat_time,priority:1;index:idx_feishu_source_received,priority:1"`
	EventTenantKey   string    `gorm:"size:128"`
	MessageID        string    `gorm:"size:128;not null;uniqueIndex:uk_feishu_source,priority:2"`
	ChatID           string    `gorm:"size:128;not null;index:idx_feishu_source_chat_time,priority:2"`
	SenderOpenID     string    `gorm:"size:128;not null"`
	SenderName       string    `gorm:"size:255"`
	MessageType      string    `gorm:"size:32;not null"`
	MessageCreatedAt time.Time `gorm:"type:datetime(3);not null"`
	ReceivedAt       time.Time `gorm:"type:datetime(3);not null;index:idx_feishu_source_chat_time,priority:3;index:idx_feishu_source_received,priority:2"`
	RawContent       JSONText  `gorm:"type:json;not null"`
	DisplayText      string    `gorm:"type:longtext;not null"`
	MentionsSnapshot JSONText  `gorm:"type:json;not null"`
	OriginalURL      string    `gorm:"type:text"`
	WeatherTimestamps
}

func (FeishuSourceMessage) TableName() string { return "feishu_source_messages" }

type FeishuMessageRecipient struct {
	BaseModel
	SourceMessageID    uint       `gorm:"not null;uniqueIndex:uk_feishu_recipient,priority:1"`
	RecipientOpenID    string     `gorm:"size:128;not null;uniqueIndex:uk_feishu_recipient,priority:2;index:idx_feishu_recipient_status,priority:1;index:idx_feishu_recipient_report,priority:1"`
	RecipientName      string     `gorm:"size:255"`
	RecipientTenantKey string     `gorm:"size:128"`
	ViewStatus         string     `gorm:"size:16;not null;default:not_required;index:idx_feishu_recipient_status,priority:2"`
	ConfirmedAt        *time.Time `gorm:"type:datetime(3)"`
	InitialSentAt      *time.Time `gorm:"type:datetime(3)"`
	FollowupSentAt     *time.Time `gorm:"type:datetime(3)"`
	FirstReportID      *uint      `gorm:"index:idx_feishu_recipient_report,priority:2"`
	WeatherTimestamps
}

func (FeishuMessageRecipient) TableName() string { return "feishu_message_recipients" }

type FeishuDailyReport struct {
	BaseModel
	AppID           string     `gorm:"size:64;not null;uniqueIndex:uk_feishu_report,priority:1;index:idx_feishu_report_status,priority:1"`
	ChatID          string     `gorm:"size:128;not null;uniqueIndex:uk_feishu_report,priority:2"`
	RecipientOpenID string     `gorm:"size:128;not null;uniqueIndex:uk_feishu_report,priority:3"`
	ReportDate      string     `gorm:"type:date;serializer:feishu_date;not null;uniqueIndex:uk_feishu_report,priority:4;index:idx_feishu_report_status,priority:3"`
	WindowStart     time.Time  `gorm:"type:datetime(3);not null"`
	WindowEnd       time.Time  `gorm:"type:datetime(3);not null"`
	GeneratedAt     time.Time  `gorm:"type:datetime(3);not null"`
	Status          string     `gorm:"size:16;not null;default:ready;index:idx_feishu_report_status,priority:2"`
	PartCount       uint       `gorm:"not null;default:1"`
	ContentSnapshot JSONText   `gorm:"type:json;not null"`
	SentAt          *time.Time `gorm:"type:datetime(3)"`
	WeatherTimestamps
}

func (FeishuDailyReport) TableName() string { return "feishu_daily_reports" }

type FeishuDeliveryJob struct {
	BaseModel
	BusinessKey           string     `gorm:"size:255;not null;uniqueIndex:uk_feishu_job_business"`
	SendUUID              string     `gorm:"type:char(36);not null;uniqueIndex:uk_feishu_job_uuid"`
	AppID                 string     `gorm:"size:64;not null;index"`
	RecipientOpenID       string     `gorm:"size:128;not null"`
	JobType               string     `gorm:"size:32;not null"`
	RecipientRecordID     *uint      `gorm:"index:idx_feishu_job_recipient"`
	ReportID              *uint      `gorm:"uniqueIndex:uk_feishu_job_report_part,priority:1"`
	PartNo                *uint      `gorm:"uniqueIndex:uk_feishu_job_report_part,priority:2"`
	TargetFeishuMessageID string     `gorm:"size:128"`
	Status                string     `gorm:"size:16;not null;default:pending;index:idx_feishu_job_due,priority:1;index:idx_feishu_job_lease,priority:1"`
	ScheduledAt           time.Time  `gorm:"type:datetime(3);not null;index:idx_feishu_job_due,priority:2"`
	Attempts              uint       `gorm:"not null;default:0"`
	CycleAttempts         uint       `gorm:"not null;default:0"`
	CancelReason          string     `gorm:"size:64"`
	DispatchGeneration    uint       `gorm:"not null;default:1"`
	LeaseToken            string     `gorm:"size:64"`
	LeaseUntil            *time.Time `gorm:"type:datetime(3);index:idx_feishu_job_lease,priority:2"`
	PayloadSnapshot       JSONText   `gorm:"type:json;not null"`
	FeishuMessageID       string     `gorm:"size:128"`
	LastErrorCode         string     `gorm:"size:64"`
	LastErrorMessage      string     `gorm:"type:text"`
	SentAt                *time.Time `gorm:"type:datetime(3)"`
	WeatherTimestamps
}

func (FeishuDeliveryJob) TableName() string { return "feishu_delivery_jobs" }

type FeishuDigestSchedule struct {
	BaseModel
	AppID             string     `gorm:"size:64;not null;uniqueIndex:uk_feishu_digest_app"`
	Enabled           bool       `gorm:"not null;index:idx_feishu_digest_due,priority:1"`
	NextWindowStartAt time.Time  `gorm:"type:datetime(3);not null"`
	NextCutoffAt      time.Time  `gorm:"type:datetime(3);not null;index:idx_feishu_digest_due,priority:2"`
	LastPlannedAt     *time.Time `gorm:"type:datetime(3)"`
	WeatherTimestamps
}

func (FeishuDigestSchedule) TableName() string { return "feishu_digest_schedules" }

// These snapshots are immutable; delivery state always comes from the jobs table.
type FeishuDigestSnapshot struct {
	SchemaVersion   int                   `json:"schemaVersion"`
	AppID           string                `json:"appId"`
	ChatID          string                `json:"chatId"`
	ChatName        string                `json:"chatName"`
	RecipientOpenID string                `json:"recipientOpenId"`
	RecipientName   string                `json:"recipientName"`
	ReportDate      string                `json:"reportDate"`
	Timezone        string                `json:"timezone"`
	WindowStart     time.Time             `json:"windowStart"`
	WindowEnd       time.Time             `json:"windowEnd"`
	GeneratedAt     time.Time             `json:"generatedAt"`
	MessageCount    int                   `json:"messageCount"`
	Messages        []FeishuDigestMessage `json:"messages"`
	Summary         *string               `json:"summary"`
	Parts           []FeishuDigestPart    `json:"parts"`
}
type FeishuDigestMessage struct {
	RecipientRecordID uint      `json:"recipientRecordId"`
	SourceMessageID   uint      `json:"sourceMessageId"`
	MessageID         string    `json:"messageId"`
	SenderOpenID      string    `json:"senderOpenId"`
	SenderName        string    `json:"senderName"`
	MessageCreatedAt  time.Time `json:"messageCreatedAt"`
	ReceivedAt        time.Time `json:"receivedAt"`
	DisplayText       string    `json:"displayText"`
	OriginalURL       *string   `json:"originalUrl"`
}
type FeishuDigestPart struct {
	PartNo    uint                   `json:"partNo"`
	Title     string                 `json:"title"`
	Fragments []FeishuDigestFragment `json:"fragments"`
}
type FeishuDigestFragment struct {
	RecipientRecordID uint   `json:"recipientRecordId"`
	FragmentNo        int    `json:"fragmentNo"`
	FragmentCount     int    `json:"fragmentCount"`
	Text              string `json:"text"`
}
type FeishuDeliverySnapshot struct {
	CardJSON        string `json:"cardJSON"`
	ChatID          string `json:"chatId"`
	FollowupMinutes uint   `json:"followupMinutes"`
}

// Keep DATE fields in their API YYYY-MM-DD form even when the MySQL driver has
// parseTime enabled (which returns time.Time for a DATE column).
type feishuDateSerializer struct{}

func init() { schema.RegisterSerializer("feishu_date", feishuDateSerializer{}) }
func (feishuDateSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, value interface{}) error {
	var date string
	switch typed := value.(type) {
	case time.Time:
		date = typed.Format("2006-01-02")
	case []byte:
		date = string(typed)
	case string:
		date = typed
	default:
		return fmt.Errorf("feishu report: unsupported date value %T", value)
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return err
	}
	field.ReflectValueOf(ctx, dst).SetString(date)
	return nil
}
func (feishuDateSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, value interface{}) (interface{}, error) {
	date, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("feishu report: date must be a string")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, err
	}
	return date, nil
}
