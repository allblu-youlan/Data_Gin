package data_svc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/paginator"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrFeishuMentionViewUnauthorized = errors.New("请重新通过飞书登录")
	ErrFeishuMentionViewNotFound     = errors.New("日报不存在或无权查看")
	ErrFeishuMentionViewInvalid      = errors.New("无效的日报请求")
	ErrFeishuMentionViewUnavailable  = errors.New("日报暂时无法访问，请稍后重试")
)

const mentionViewPageSize = 20

type FeishuMentionIdentityClient interface {
	Resolve(ctx context.Context, code, redirectURI string) (openID string, err error)
}

type FeishuMentionViewService struct {
	db          *gorm.DB
	appID       string
	callbackURL string
	cookieKey   []byte
	identity    FeishuMentionIdentityClient
	names       *mentionViewNames
	messages    FeishuMentionMessageDetailsClient
	now         func() time.Time
}

func NewFeishuMentionViewService(db *gorm.DB, appID, appSecret, publicURL string, identity FeishuMentionIdentityClient) (*FeishuMentionViewService, error) {
	base, err := mentionViewPublicBase(publicURL)
	if err != nil || !mentionViewCredential(appID) || !mentionViewCredential(appSecret) {
		return nil, ErrFeishuMentionViewUnavailable
	}
	if identity == nil {
		identity, err = feishu.NewOAuthIdentityClient(appID, appSecret, nil)
		if err != nil {
			return nil, ErrFeishuMentionViewUnavailable
		}
	}
	base.Path = FeishuMentionViewCallbackPath
	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write([]byte("feishu-mention-view:v1:" + appID))
	if db != nil {
		db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)})
	}
	return &FeishuMentionViewService{db: db, appID: appID, callbackURL: base.String(), cookieKey: mac.Sum(nil), identity: identity, now: time.Now}, nil
}

type FeishuMentionViewReportSummary struct {
	ID            uint      `json:"id"`
	ChatName      string    `json:"chatName"`
	RecipientName string    `json:"recipientName"`
	ReportDate    string    `json:"reportDate"`
	WindowStart   time.Time `json:"windowStart"`
	WindowEnd     time.Time `json:"windowEnd"`
	MessageCount  int       `json:"messageCount"`
	GeneratedAt   time.Time `json:"generatedAt"`
	Timezone      string    `json:"timezone"`
	Summary       *string   `json:"summary"`
	ChatURL       *string   `json:"chatUrl,omitempty"`
}

type FeishuMentionViewMessageDTO struct {
	SenderOpenID     string    `json:"senderOpenId"`
	SenderName       string    `json:"senderName"`
	MessageCreatedAt time.Time `json:"messageCreatedAt"`
	ReceivedAt       time.Time `json:"receivedAt"`
	DisplayText      string    `json:"displayText"`
	OriginalURL      *string   `json:"originalUrl"`
}

type FeishuMentionViewReportDTO struct {
	Report           FeishuMentionViewReportSummary `json:"report"`
	Messages         []FeishuMentionViewMessageDTO  `json:"messages"`
	Pagination       paginator.Pagination           `json:"pagination"`
	SenderNameNotice string                         `json:"senderNameNotice,omitempty"`
}

func (s *FeishuMentionViewService) Report(ctx context.Context, openID string, id uint, page int) (*FeishuMentionViewReportDTO, error) {
	if !feishu.ValidOAuthOpenID(openID) {
		return nil, ErrFeishuMentionViewUnauthorized
	}
	if id == 0 || page < 1 {
		return nil, ErrFeishuMentionViewInvalid
	}
	if s == nil || s.db == nil {
		return nil, ErrFeishuMentionViewUnavailable
	}
	var report model.FeishuDailyReport
	// Apply the viewer's identity in SQL before loading the private snapshot.
	err := s.db.WithContext(ctx).Select("id", "content_snapshot").
		Where("app_id = ? AND recipient_open_id = ? AND id = ?", s.appID, openID, id).Take(&report).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrFeishuMentionViewNotFound
	}
	if err != nil {
		return nil, ErrFeishuMentionViewUnavailable
	}
	var snapshot model.FeishuDigestSnapshot
	if json.Unmarshal([]byte(report.ContentSnapshot), &snapshot) != nil || snapshot.AppID != s.appID || snapshot.RecipientOpenID != openID {
		return nil, ErrFeishuMentionViewUnavailable
	}
	dto := mentionViewReportDTO(report.ID, snapshot, page)
	s.fillMessageDetails(ctx, dto, snapshot, page)
	s.fillSenderNames(ctx, dto)
	return dto, nil
}

func mentionViewReportDTO(id uint, snapshot model.FeishuDigestSnapshot, page int) *FeishuMentionViewReportDTO {
	count := len(snapshot.Messages)
	out := &FeishuMentionViewReportDTO{
		Report: FeishuMentionViewReportSummary{
			ID: id, ChatName: snapshot.ChatName, RecipientName: snapshot.RecipientName, ReportDate: snapshot.ReportDate,
			WindowStart: snapshot.WindowStart.In(mentionLocation()), WindowEnd: snapshot.WindowEnd.In(mentionLocation()),
			MessageCount: snapshot.MessageCount, GeneratedAt: snapshot.GeneratedAt.In(mentionLocation()), Timezone: "Asia/Shanghai", Summary: snapshot.Summary,
		},
		Messages:   make([]FeishuMentionViewMessageDTO, 0),
		Pagination: paginator.Pagination{Page: page, PerPage: mentionViewPageSize, TotalCount: count, TotalPage: (count + mentionViewPageSize - 1) / mentionViewPageSize},
	}
	if page > out.Pagination.TotalPage {
		return out
	}
	start := (page - 1) * mentionViewPageSize
	end := min(start+mentionViewPageSize, count)
	for _, message := range snapshot.Messages[start:end] {
		out.Messages = append(out.Messages, FeishuMentionViewMessageDTO{
			SenderOpenID: message.SenderOpenID, SenderName: message.SenderName, MessageCreatedAt: message.MessageCreatedAt.In(mentionLocation()),
			ReceivedAt: message.ReceivedAt.In(mentionLocation()), DisplayText: message.DisplayText, OriginalURL: mentionViewOriginalURL(message.OriginalURL),
		})
	}
	return out
}

func mentionViewOriginalURL(raw *string) *string {
	if raw == nil {
		return nil
	}
	u, err := url.Parse(*raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil
	}
	return raw
}

func mentionViewCredential(value string) bool {
	if value == "" || len(value) > 4096 || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
