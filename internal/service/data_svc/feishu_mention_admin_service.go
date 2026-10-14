package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	appconfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/paginator"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrFeishuMentionAdminInvalid  = errors.New("feishu mention invalid input")
	ErrFeishuMentionAdminNotFound = errors.New("feishu mention not found")
	ErrFeishuMentionAdminConflict = errors.New("feishu mention conflict")
	ErrFeishuMentionAdminPaused   = errors.New("feishu mention paused")
)

type FeishuMentionAdminService struct {
	dao       *data_dao.FeishuMentionAdminDAO
	config    appconfig.FeishuMentionConfig
	now       func() time.Time
	publicURL string
}

func NewFeishuMentionAdminService(db *gorm.DB, appID string, cfg appconfig.FeishuMentionConfig, publicURL ...string) *FeishuMentionAdminService {
	now := func() time.Time { return time.Now().In(mentionLocation()) }
	if db != nil {
		db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent), NowFunc: now})
	}
	s := &FeishuMentionAdminService{dao: &data_dao.FeishuMentionAdminDAO{DB: db, AppID: strings.TrimSpace(appID)}, config: cfg, now: now}
	if len(publicURL) > 0 {
		s.publicURL = strings.TrimSpace(publicURL[0])
	}
	return s
}
func (s *FeishuMentionAdminService) ready() error {
	if s == nil || s.dao == nil || s.dao.DB == nil {
		return errors.New("feishu mention database unavailable")
	}
	return nil
}
func mentionAdminError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrFeishuMentionAdminNotFound
	}
	var duplicate *mysqlDriver.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		return ErrFeishuMentionAdminConflict
	}
	return err
}

// ParseFeishuMentionAdminFilter rejects invalid pagination and fixed enumerations before SQL.
func ParseFeishuMentionAdminFilter(v url.Values, kind string) (data_dao.FeishuMentionAdminFilter, error) {
	f := data_dao.FeishuMentionAdminFilter{Page: 1, PerPage: 20, ChatID: strings.TrimSpace(v.Get("chatId")), Keyword: strings.TrimSpace(v.Get("keyword")), RecipientOpenID: strings.TrimSpace(v.Get("recipientOpenId")), ViewStatus: v.Get("viewStatus"), Status: v.Get("status"), JobType: v.Get("jobType")}
	for key, dst := range map[string]*int{"page": &f.Page, "per_page": &f.PerPage} {
		if values, ok := v[key]; ok {
			if len(values) != 1 {
				return f, ErrFeishuMentionAdminInvalid
			}
			n, e := strconv.Atoi(values[0])
			if e != nil || n < 1 {
				return f, ErrFeishuMentionAdminInvalid
			}
			*dst = n
		}
	}
	if f.PerPage > 100 || f.Page > int(^uint(0)>>1)/f.PerPage {
		return f, ErrFeishuMentionAdminInvalid
	}
	if values, ok := v["enabled"]; ok {
		if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			return f, ErrFeishuMentionAdminInvalid
		}
		b := values[0] == "true"
		f.Enabled = &b
	}
	if f.ViewStatus != "" && !mentionOneOf(f.ViewStatus, "not_required", "pending", "confirmed") {
		return f, ErrFeishuMentionAdminInvalid
	}
	if f.JobType != "" && !mentionOneOf(f.JobType, "initial_reminder", "followup_reminder", "daily_digest", "card_update") {
		return f, ErrFeishuMentionAdminInvalid
	}
	if f.Status != "" {
		valid := mentionOneOf(f.Status, "pending", "sending", "sent", "retry_wait", "failed", "cancelled")
		if kind == "reports" {
			valid = mentionOneOf(f.Status, "ready", "sent", "failed")
		}
		if !valid {
			return f, ErrFeishuMentionAdminInvalid
		}
	}
	for key, dst := range map[string]*uint{"recipientRecordId": &f.RecipientRecordID, "reportId": &f.ReportID} {
		if values, ok := v[key]; ok {
			if len(values) != 1 {
				return f, ErrFeishuMentionAdminInvalid
			}
			n, e := strconv.ParseUint(values[0], 10, 64)
			if e != nil || n == 0 || uint64(uint(n)) != n {
				return f, ErrFeishuMentionAdminInvalid
			}
			*dst = uint(n)
		}
	}
	from, to := v.Get("receivedFrom"), v.Get("receivedTo")
	if from != "" || to != "" {
		start, e1 := time.Parse(time.RFC3339, from)
		end, e2 := time.Parse(time.RFC3339, to)
		if e1 != nil || e2 != nil || !start.Before(end) {
			return f, ErrFeishuMentionAdminInvalid
		}
		start, end = start.In(mentionLocation()), end.In(mentionLocation())
		f.ReceivedFrom, f.ReceivedTo = &start, &end
	}
	f.ReportDateFrom, f.ReportDateTo = v.Get("reportDateFrom"), v.Get("reportDateTo")
	if f.ReportDateFrom != "" || f.ReportDateTo != "" {
		start, e1 := time.Parse("2006-01-02", f.ReportDateFrom)
		end, e2 := time.Parse("2006-01-02", f.ReportDateTo)
		if e1 != nil || e2 != nil || start.After(end) {
			return f, ErrFeishuMentionAdminInvalid
		}
	}
	return f, nil
}
func mentionOneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if a == value {
			return true
		}
	}
	return false
}

type FeishuMentionChatInput struct {
	ChatID          *string `json:"chatId"`
	ChatName        *string `json:"chatName"`
	IsExternal      *bool   `json:"isExternal"`
	Enabled         *bool   `json:"enabled"`
	FollowupMinutes *int    `json:"followupMinutes"`
}
type FeishuMentionChatDTO struct {
	ID              uint      `json:"id"`
	AppID           string    `json:"appId"`
	ChatID          string    `json:"chatId"`
	ChatName        string    `json:"chatName"`
	IsExternal      bool      `json:"isExternal"`
	Enabled         bool      `json:"enabled"`
	FollowupMinutes uint      `json:"followupMinutes"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func mentionChatDTO(r model.FeishuMonitoredChat) FeishuMentionChatDTO {
	return FeishuMentionChatDTO{r.ID, r.AppID, r.ChatID, r.ChatName, r.IsExternal, r.Enabled, r.FollowupMinutes, r.CreatedAt.In(mentionLocation()), r.UpdatedAt.In(mentionLocation())}
}
func (s *FeishuMentionAdminService) Chats(ctx context.Context, f data_dao.FeishuMentionAdminFilter) ([]FeishuMentionChatDTO, paginator.Pagination, error) {
	if e := s.ready(); e != nil {
		return nil, paginator.Pagination{}, e
	}
	rows, p, e := s.dao.Chats(ctx, f)
	out := make([]FeishuMentionChatDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, mentionChatDTO(r))
	}
	return out, p, e
}
func (s *FeishuMentionAdminService) SaveChat(ctx context.Context, actor, id uint, input FeishuMentionChatInput) (*FeishuMentionChatDTO, error) {
	if input.ChatName == nil || input.IsExternal == nil || input.Enabled == nil {
		return nil, ErrFeishuMentionAdminInvalid
	}
	name := strings.TrimSpace(*input.ChatName)
	if name == "" || utf8.RuneCountInString(name) > 255 {
		return nil, ErrFeishuMentionAdminInvalid
	}
	minutes := s.config.DefaultFollowupMinutes
	if input.FollowupMinutes != nil {
		minutes = *input.FollowupMinutes
	}
	if minutes <= 0 || uint64(minutes) > uint64(^uint32(0)) {
		return nil, ErrFeishuMentionAdminInvalid
	}
	if id != 0 && (input.ChatID != nil || input.FollowupMinutes == nil) {
		return nil, ErrFeishuMentionAdminInvalid
	}
	chatID := ""
	if id == 0 {
		if input.ChatID == nil {
			return nil, ErrFeishuMentionAdminInvalid
		}
		chatID = strings.TrimSpace(*input.ChatID)
		if chatID == "" || utf8.RuneCountInString(chatID) > 128 {
			return nil, ErrFeishuMentionAdminInvalid
		}
	}
	if e := s.ready(); e != nil {
		return nil, e
	}
	if s.dao.AppID == "" {
		return nil, ErrFeishuMentionAdminInvalid
	}
	var result FeishuMentionChatDTO
	err := s.dao.Transaction(ctx, func(d *data_dao.FeishuMentionAdminDAO) error {
		row := &model.FeishuMonitoredChat{AppID: d.AppID, ChatID: chatID}
		var before interface{}
		action := "FEISHU_CHAT_CREATE"
		if id != 0 {
			var err error
			row, err = d.Chat(ctx, id, true)
			if err != nil {
				return err
			}
			before = mentionChatDTO(*row)
			action = "FEISHU_CHAT_UPDATE"
		}
		row.ChatName, row.IsExternal, row.Enabled, row.FollowupMinutes = name, *input.IsExternal, *input.Enabled, uint(minutes)
		if err := d.SaveChat(ctx, row, id == 0); err != nil {
			return err
		}
		if !row.Enabled {
			if err := d.CancelChatReminders(ctx, row.ChatID); err != nil {
				return err
			}
		}
		saved, err := d.Chat(ctx, row.ID, false)
		if err != nil {
			return err
		}
		result = mentionChatDTO(*saved)
		return mentionAudit(ctx, d, actor, action, "feishu_chat", row.ID, before, result)
	})
	if err != nil {
		return nil, mentionAdminError(err)
	}
	return &result, nil
}
func mentionAudit(ctx context.Context, d *data_dao.FeishuMentionAdminDAO, actor uint, action, target string, id uint, before, after interface{}) error {
	b, err := json.Marshal(before)
	if err != nil {
		return err
	}
	a, err := json.Marshal(after)
	if err != nil {
		return err
	}
	return d.Audit(ctx, &model.AuthAudit{ActorUserID: actor, Action: action, TargetType: target, TargetID: id, BeforeJSON: model.JSONText(b), AfterJSON: model.JSONText(a), Reason: "", RequestID: ""})
}

type FeishuMentionJobDTO struct {
	ID                uint       `json:"id"`
	JobID             uint       `json:"jobId"`
	JobType           string     `json:"jobType"`
	ChatID            string     `json:"chatId"`
	RecipientOpenID   string     `json:"recipientOpenId"`
	RecipientRecordID *uint      `json:"recipientRecordId"`
	ReportID          *uint      `json:"reportId"`
	PartNo            *uint      `json:"partNo"`
	Status            string     `json:"status"`
	ScheduledAt       time.Time  `json:"scheduledAt"`
	Attempts          uint       `json:"attempts"`
	CycleAttempts     uint       `json:"cycleAttempts"`
	SentAt            *time.Time `json:"sentAt"`
	LastErrorCode     string     `json:"lastErrorCode"`
	LastErrorMessage  string     `json:"lastErrorMessage"`
	CancelReason      string     `json:"cancelReason"`
	Retryable         bool       `json:"retryable"`
}

func mentionJobDTO(j model.FeishuDeliveryJob, chatID string) FeishuMentionJobDTO {
	return FeishuMentionJobDTO{j.ID, j.ID, j.JobType, chatID, j.RecipientOpenID, j.RecipientRecordID, j.ReportID, j.PartNo, j.Status, j.ScheduledAt.In(mentionLocation()), j.Attempts, j.CycleAttempts, mentionAdminTime(j.SentAt), j.LastErrorCode, j.LastErrorMessage, j.CancelReason, false}
}

type FeishuMentionRecipientDTO struct {
	ID                  uint                 `json:"id"`
	SourceMessageID     uint                 `json:"sourceMessageId"`
	MessageID           string               `json:"messageId"`
	ChatID              string               `json:"chatId"`
	ChatName            string               `json:"chatName"`
	RecipientOpenID     string               `json:"recipientOpenId"`
	RecipientName       string               `json:"recipientName"`
	SenderName          string               `json:"senderName"`
	MessageCreatedAt    time.Time            `json:"messageCreatedAt"`
	ReceivedAt          time.Time            `json:"receivedAt"`
	ContentPreview      string               `json:"contentPreview"`
	ViewStatus          string               `json:"viewStatus"`
	ConfirmedAt         *time.Time           `json:"confirmedAt"`
	OriginalReadStatus  string               `json:"originalReadStatus"`
	ConfirmationEnabled bool                 `json:"confirmationEnabled"`
	InitialReminder     *FeishuMentionJobDTO `json:"initialReminder"`
	FollowupReminder    *FeishuMentionJobDTO `json:"followupReminder"`
	DisplayText         *string              `json:"displayText,omitempty"`
	MessageType         string               `json:"messageType,omitempty"`
	SenderOpenID        string               `json:"senderOpenId,omitempty"`
	OriginalURL         *string              `json:"originalUrl"`
	FirstReportID       *uint                `json:"firstReportId"`
}

func (s *FeishuMentionAdminService) recipientDTO(r data_dao.FeishuMentionAdminRecipientRow, jobs []model.FeishuDeliveryJob, detail bool) FeishuMentionRecipientDTO {
	text := []rune(r.DisplayText)
	preview := r.DisplayText
	if len(text) > 160 {
		preview = string(text[:160]) + "…"
	}
	out := FeishuMentionRecipientDTO{ID: r.ID, SourceMessageID: r.SourceMessageID, MessageID: r.MessageID, ChatID: r.ChatID, ChatName: r.ChatName, RecipientOpenID: r.RecipientOpenID, RecipientName: r.RecipientName, SenderName: r.SenderName, MessageCreatedAt: r.MessageCreatedAt.In(mentionLocation()), ReceivedAt: r.ReceivedAt.In(mentionLocation()), ContentPreview: preview, ViewStatus: r.ViewStatus, ConfirmedAt: mentionAdminTime(r.ConfirmedAt), OriginalReadStatus: "unavailable", ConfirmationEnabled: s.config.Enabled && s.config.PersonalReminderEnabled, FirstReportID: r.FirstReportID}
	for _, j := range jobs {
		if j.RecipientRecordID == nil || *j.RecipientRecordID != r.ID {
			continue
		}
		dto := mentionJobDTO(j, r.ChatID)
		if j.JobType == "initial_reminder" {
			out.InitialReminder = &dto
		}
		if j.JobType == "followup_reminder" {
			out.FollowupReminder = &dto
		}
	}
	if detail {
		out.DisplayText = &r.DisplayText
		out.MessageType = r.MessageType
		out.SenderOpenID = r.SenderOpenID
		if r.OriginalURL != nil && strings.TrimSpace(*r.OriginalURL) != "" {
			out.OriginalURL = r.OriginalURL
		}
	}
	return out
}
func (s *FeishuMentionAdminService) Recipients(ctx context.Context, f data_dao.FeishuMentionAdminFilter) ([]FeishuMentionRecipientDTO, paginator.Pagination, error) {
	if e := s.ready(); e != nil {
		return nil, paginator.Pagination{}, e
	}
	rows, p, e := s.dao.Recipients(ctx, f)
	if e != nil {
		return nil, p, e
	}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	jobs, e := s.dao.RecipientJobs(ctx, ids)
	if e != nil {
		return nil, p, e
	}
	out := make([]FeishuMentionRecipientDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.recipientDTO(r, jobs, false))
	}
	return out, p, nil
}
func (s *FeishuMentionAdminService) Recipient(ctx context.Context, id uint) (*FeishuMentionRecipientDTO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	r, e := s.dao.Recipient(ctx, id, false)
	if e != nil {
		return nil, mentionAdminError(e)
	}
	jobs, e := s.dao.RecipientJobs(ctx, []uint{id})
	if e != nil {
		return nil, e
	}
	dto := s.recipientDTO(*r, jobs, true)
	return &dto, nil
}

type FeishuMentionReportDTO struct {
	ID              uint                  `json:"id"`
	AppID           string                `json:"appId"`
	ChatID          string                `json:"chatId"`
	ChatName        string                `json:"chatName"`
	RecipientOpenID string                `json:"recipientOpenId"`
	RecipientName   string                `json:"recipientName"`
	ReportDate      string                `json:"reportDate"`
	ReportKind      string                `json:"reportKind"`
	WindowStart     time.Time             `json:"windowStart"`
	WindowEnd       time.Time             `json:"windowEnd"`
	MessageCount    int                   `json:"messageCount"`
	Status          string                `json:"status"`
	PartCount       uint                  `json:"partCount"`
	SentPartCount   uint                  `json:"sentPartCount"`
	GeneratedAt     time.Time             `json:"generatedAt"`
	SentAt          *time.Time            `json:"sentAt"`
	Timezone        string                `json:"timezone,omitempty"`
	Summary         *string               `json:"summary"`
	Parts           []FeishuMentionJobDTO `json:"parts,omitempty"`
}

func mentionReportDTO(r model.FeishuDailyReport, jobs []model.FeishuDeliveryJob, detail bool) (FeishuMentionReportDTO, error) {
	var snap model.FeishuDigestSnapshot
	if e := json.Unmarshal([]byte(r.ContentSnapshot), &snap); e != nil {
		return FeishuMentionReportDTO{}, e
	}
	out := FeishuMentionReportDTO{ID: r.ID, AppID: r.AppID, ChatID: r.ChatID, ChatName: snap.ChatName, RecipientOpenID: r.RecipientOpenID, RecipientName: snap.RecipientName, ReportDate: r.ReportDate, WindowStart: r.WindowStart.In(mentionLocation()), WindowEnd: r.WindowEnd.In(mentionLocation()), MessageCount: snap.MessageCount, Status: r.Status, PartCount: r.PartCount, GeneratedAt: r.GeneratedAt.In(mentionLocation()), SentAt: mentionAdminTime(r.SentAt)}
	out.ReportKind = r.ReportKind
	if out.ReportKind == "" {
		out.ReportKind = "daily"
	}
	for _, j := range jobs {
		if j.ReportID == nil || *j.ReportID != r.ID || j.PartNo == nil || *j.PartNo > r.PartCount {
			continue
		}
		if j.Status == "sent" {
			out.SentPartCount++
		}
		if detail {
			out.Parts = append(out.Parts, mentionJobDTO(j, r.ChatID))
		}
	}
	if detail {
		out.Timezone = snap.Timezone
		out.Summary = snap.Summary
	}
	return out, nil
}
func (s *FeishuMentionAdminService) Reports(ctx context.Context, f data_dao.FeishuMentionAdminFilter) ([]FeishuMentionReportDTO, paginator.Pagination, error) {
	if e := s.ready(); e != nil {
		return nil, paginator.Pagination{}, e
	}
	rows, p, e := s.dao.Reports(ctx, f)
	if e != nil {
		return nil, p, e
	}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	jobs, e := s.dao.ReportJobs(ctx, ids)
	if e != nil {
		return nil, p, e
	}
	out := make([]FeishuMentionReportDTO, 0, len(rows))
	for _, r := range rows {
		dto, e := mentionReportDTO(r, jobs, false)
		if e != nil {
			return nil, p, e
		}
		out = append(out, dto)
	}
	return out, p, nil
}
func (s *FeishuMentionAdminService) Report(ctx context.Context, id uint) (*FeishuMentionReportDTO, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	r, e := s.dao.Report(ctx, id)
	if e != nil {
		return nil, mentionAdminError(e)
	}
	jobs, e := s.dao.ReportJobs(ctx, []uint{id})
	if e != nil {
		return nil, e
	}
	dto, e := mentionReportDTO(*r, jobs, true)
	for i := range dto.Parts {
		for _, job := range jobs {
			if job.ID == dto.Parts[i].ID {
				dto.Parts[i].Retryable = s.config.Enabled && job.Status == "failed" && s.validateRetry(ctx, s.dao, &job) == nil
			}
		}
	}
	return &dto, e
}
func (s *FeishuMentionAdminService) ReportMessages(ctx context.Context, id uint, f data_dao.FeishuMentionAdminFilter) ([]model.FeishuDigestMessage, paginator.Pagination, error) {
	if e := s.ready(); e != nil {
		return nil, paginator.Pagination{}, e
	}
	r, e := s.dao.Report(ctx, id)
	if e != nil {
		return nil, paginator.Pagination{}, mentionAdminError(e)
	}
	var snap model.FeishuDigestSnapshot
	if e = json.Unmarshal([]byte(r.ContentSnapshot), &snap); e != nil {
		return nil, paginator.Pagination{}, e
	}
	count := len(snap.Messages)
	p := paginator.Pagination{Page: f.Page, PerPage: f.PerPage, TotalCount: count, TotalPage: (count + f.PerPage - 1) / f.PerPage}
	out := make([]model.FeishuDigestMessage, 0)
	start := (f.Page - 1) * f.PerPage
	if start < count {
		end := start + f.PerPage
		if end > count {
			end = count
		}
		out = append(out, snap.Messages[start:end]...)
		for i := range out {
			out[i].MessageCreatedAt = out[i].MessageCreatedAt.In(mentionLocation())
			out[i].ReceivedAt = out[i].ReceivedAt.In(mentionLocation())
		}
	}
	return out, p, nil
}

type FeishuMentionReportPartDTO struct {
	model.FeishuDigestPart
	Content json.RawMessage     `json:"content"`
	Job     FeishuMentionJobDTO `json:"job"`
}

func (s *FeishuMentionAdminService) ReportPart(ctx context.Context, id, partNo uint) (*FeishuMentionReportPartDTO, error) {
	if partNo == 0 {
		return nil, ErrFeishuMentionAdminInvalid
	}
	if e := s.ready(); e != nil {
		return nil, e
	}
	r, e := s.dao.Report(ctx, id)
	if e != nil {
		return nil, mentionAdminError(e)
	}
	if partNo > r.PartCount {
		return nil, ErrFeishuMentionAdminNotFound
	}
	var snap model.FeishuDigestSnapshot
	if e = json.Unmarshal([]byte(r.ContentSnapshot), &snap); e != nil {
		return nil, e
	}
	jobs, e := s.dao.ReportJobs(ctx, []uint{id})
	if e != nil {
		return nil, e
	}
	for _, part := range snap.Parts {
		if part.PartNo != partNo {
			continue
		}
		for _, j := range jobs {
			if j.PartNo == nil || *j.PartNo != partNo {
				continue
			}
			var payload model.FeishuDeliverySnapshot
			if e = json.Unmarshal([]byte(j.PayloadSnapshot), &payload); e != nil {
				return nil, e
			}
			if !json.Valid([]byte(payload.CardJSON)) {
				return nil, errors.New("invalid fixed card snapshot")
			}
			dto := mentionJobDTO(j, r.ChatID)
			dto.Retryable = s.config.Enabled && j.Status == "failed" && s.validateRetry(ctx, s.dao, &j) == nil
			return &FeishuMentionReportPartDTO{part, json.RawMessage(payload.CardJSON), dto}, nil
		}
	}
	return nil, ErrFeishuMentionAdminNotFound
}
func (s *FeishuMentionAdminService) Jobs(ctx context.Context, f data_dao.FeishuMentionAdminFilter) ([]FeishuMentionJobDTO, paginator.Pagination, error) {
	if e := s.ready(); e != nil {
		return nil, paginator.Pagination{}, e
	}
	rows, p, e := s.dao.Jobs(ctx, f)
	out := make([]FeishuMentionJobDTO, 0, len(rows))
	for _, r := range rows {
		dto := mentionJobDTO(r.FeishuDeliveryJob, r.ChatID)
		dto.Retryable = s.config.Enabled && r.Status == "failed" && s.validateRetry(ctx, s.dao, &r.FeishuDeliveryJob) == nil
		out = append(out, dto)
	}
	return out, p, e
}

type FeishuMentionRetryDTO struct {
	JobID              uint   `json:"jobId"`
	Status             string `json:"status"`
	DispatchGeneration uint   `json:"dispatchGeneration"`
}

func (s *FeishuMentionAdminService) Retry(ctx context.Context, actor, id uint) (*FeishuMentionRetryDTO, error) {
	if !s.config.Enabled {
		return nil, ErrFeishuMentionAdminPaused
	}
	if e := s.ready(); e != nil {
		return nil, e
	}
	var result FeishuMentionRetryDTO
	err := s.dao.Transaction(ctx, func(d *data_dao.FeishuMentionAdminDAO) error {
		j, e := d.LockJob(ctx, id)
		if e != nil {
			return e
		}
		if j.Status != "failed" {
			return ErrFeishuMentionAdminConflict
		}
		if e = s.validateRetry(ctx, d, j); e != nil {
			return e
		}
		// Only state and safe error fields enter audit; immutable message/card snapshots never do.
		before := map[string]interface{}{"status": j.Status, "attempts": j.Attempts, "cycleAttempts": j.CycleAttempts, "dispatchGeneration": j.DispatchGeneration, "lastErrorCode": j.LastErrorCode, "lastErrorMessage": j.LastErrorMessage}
		if e = d.Requeue(ctx, j, s.now()); e != nil {
			return e
		}
		result = FeishuMentionRetryDTO{j.ID, j.Status, j.DispatchGeneration}
		return mentionAudit(ctx, d, actor, "FEISHU_DELIVERY_RETRY", "feishu_delivery_job", id, before, result)
	})
	if err != nil {
		return nil, mentionAdminError(err)
	}
	return &result, nil
}
func (s *FeishuMentionAdminService) validateRetry(ctx context.Context, d *data_dao.FeishuMentionAdminDAO, j *model.FeishuDeliveryJob) error {
	if j.JobType == "daily_digest" {
		if !s.config.DigestEnabled {
			return ErrFeishuMentionAdminPaused
		}
		if j.ReportID == nil || j.PartNo == nil || *j.PartNo == 0 {
			return ErrFeishuMentionAdminConflict
		}
		schedule, e := d.Schedule(ctx)
		if e != nil {
			return e
		}
		if schedule != nil && !schedule.Enabled {
			return ErrFeishuMentionAdminPaused
		}
		report, e := d.Report(ctx, *j.ReportID)
		if e != nil {
			return e
		}
		if report.RecipientOpenID != j.RecipientOpenID {
			return ErrFeishuMentionAdminConflict
		}
		parts, e := d.ReportJobs(ctx, []uint{*j.ReportID})
		if e != nil {
			return e
		}
		var sent uint
		for _, part := range parts {
			if part.PartNo != nil && *part.PartNo < *j.PartNo {
				if part.Status != "sent" {
					return ErrFeishuMentionAdminConflict
				}
				sent++
			}
		}
		if sent != *j.PartNo-1 {
			return ErrFeishuMentionAdminConflict
		}
		return nil
	}
	if !s.config.PersonalReminderEnabled {
		return ErrFeishuMentionAdminPaused
	}
	if j.RecipientRecordID == nil {
		return ErrFeishuMentionAdminConflict
	}
	r, e := d.Recipient(ctx, *j.RecipientRecordID, false)
	if e != nil {
		return e
	}
	if r.RecipientOpenID != j.RecipientOpenID {
		return ErrFeishuMentionAdminConflict
	}
	if j.JobType == "card_update" {
		if r.ViewStatus != "confirmed" || j.TargetFeishuMessageID == "" {
			return ErrFeishuMentionAdminConflict
		}
		jobs, e := d.RecipientJobs(ctx, []uint{r.ID})
		if e != nil {
			return e
		}
		for _, card := range jobs {
			if mentionCardUpdateable(card, j.TargetFeishuMessageID, s.now()) {
				return nil
			}
		}
		return ErrFeishuMentionAdminConflict
	}
	if !mentionOneOf(j.JobType, "initial_reminder", "followup_reminder") || r.ViewStatus != "pending" {
		return ErrFeishuMentionAdminConflict
	}
	if j.JobType == "followup_reminder" && !s.config.FollowupEnabled {
		return ErrFeishuMentionAdminPaused
	}
	enabled, e := d.GroupEnabled(ctx, r.ChatID)
	if e != nil {
		return e
	}
	if !enabled {
		return ErrFeishuMentionAdminConflict
	}
	return nil
}

type FeishuMentionReceiverStatus struct {
	Scope       string     `json:"scope"`
	Enabled     bool       `json:"enabled"`
	Connected   *bool      `json:"connected"`
	LastEventAt *time.Time `json:"lastEventAt"`
	LastErrorAt *time.Time `json:"lastErrorAt"`
}

var mentionStatusObservers struct {
	sync.RWMutex
	receiver func() FeishuMentionReceiverStatus
	queue    func(context.Context) *bool
}

func RegisterFeishuMentionReceiverStatusProvider(fn func() FeishuMentionReceiverStatus) {
	mentionStatusObservers.Lock()
	defer mentionStatusObservers.Unlock()
	mentionStatusObservers.receiver = fn
}
func RegisterFeishuMentionQueueStatusProvider(fn func(context.Context) *bool) {
	mentionStatusObservers.Lock()
	defer mentionStatusObservers.Unlock()
	mentionStatusObservers.queue = fn
}
func (s *FeishuMentionAdminService) Status(ctx context.Context) (map[string]interface{}, error) {
	receiver := FeishuMentionReceiverStatus{Scope: "this_instance", Enabled: s.config.Enabled && s.config.ReceiverEnabled}
	var queue *bool
	mentionStatusObservers.RLock()
	receiverFn, queueFn := mentionStatusObservers.receiver, mentionStatusObservers.queue
	mentionStatusObservers.RUnlock()
	if receiverFn != nil {
		receiver = receiverFn()
		receiver.Scope = "this_instance"
	}
	receiver.LastEventAt = mentionAdminTime(receiver.LastEventAt)
	receiver.LastErrorAt = mentionAdminTime(receiver.LastErrorAt)
	if queueFn != nil {
		queue = queueFn(ctx)
	}
	pauseReason := ""
	if !s.config.Enabled {
		pauseReason = "模块已关闭"
	} else if !s.config.DigestEnabled {
		pauseReason = "日报已暂停，恢复后按原统计日补建"
	}
	status := map[string]interface{}{"appId": s.dao.AppID, "enabled": s.config.Enabled, "timezone": s.config.Timezone, "personalReminderEnabled": s.config.PersonalReminderEnabled, "followupEnabled": s.config.FollowupEnabled, "digestEnabled": s.config.DigestEnabled, "digestTime": nil, "receiver": receiver, "backlog": nil, "observations": nil, "pauseReason": pauseReason, "digest": map[string]interface{}{"scheduleEnabled": nil, "nextWindowStartAt": nil, "nextCutoffAt": nil, "lastPlannedAt": nil}, "dependencies": map[string]interface{}{"database": nil, "queue": queue}}
	status["detailAccess"] = mentionDetailAccess(s.dao.AppID, s.publicURL)
	if e := s.ready(); e != nil {
		return status, nil
	}
	db, e := s.dao.DB.DB()
	if e != nil {
		return status, nil
	}
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	healthy := db.PingContext(healthCtx) == nil
	status["dependencies"] = map[string]interface{}{"database": healthy, "queue": queue}
	if !healthy {
		return status, nil
	}
	settings, e := loadMentionDigestSettings(ctx, s.dao.DB, s.dao.AppID, s.config.DigestTime)
	if e != nil {
		return nil, e
	}
	status["digestTime"] = settings.DigestTime
	backlog, e := s.dao.Backlog(ctx)
	if e != nil {
		return nil, e
	}
	status["backlog"] = backlog
	schedule, e := s.dao.Schedule(ctx)
	if e != nil {
		return nil, e
	}
	if schedule != nil {
		status["digest"] = map[string]interface{}{"scheduleEnabled": schedule.Enabled, "nextWindowStartAt": schedule.NextWindowStartAt.In(mentionLocation()), "nextCutoffAt": schedule.NextCutoffAt.In(mentionLocation()), "lastPlannedAt": mentionAdminTime(schedule.LastPlannedAt)}
		if pauseReason == "" && !schedule.Enabled {
			status["pauseReason"] = "日报调度已暂停，恢复后按原统计日补建"
		}
	}
	observations, e := s.dao.Observations(ctx, s.now(), schedule)
	if e != nil {
		return nil, e
	}
	observations.Outbox.EarliestAvailableAt = mentionAdminTime(observations.Outbox.EarliestAvailableAt)
	observations.Delivery.OldestWaitingAt = mentionAdminTime(observations.Delivery.OldestWaitingAt)
	observations.Delivery.EarliestLeaseUntil = mentionAdminTime(observations.Delivery.EarliestLeaseUntil)
	observations.Archive.LatestReceivedAt = mentionAdminTime(observations.Archive.LatestReceivedAt)
	status["observations"] = observations
	return status, nil
}

func FeishuMentionAdminSafeError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrFeishuMentionAdminInvalid):
		return 400, "请求参数无效，请检查必填字段、日期、状态及提醒间隔"
	case errors.Is(err, ErrFeishuMentionAdminNotFound):
		return 404, "当前应用内记录不存在"
	case errors.Is(err, ErrFeishuMentionAdminPaused):
		return 409, "当前功能已暂停，暂不能发送或重试"
	case errors.Is(err, ErrFeishuMentionAdminConflict):
		return 409, "记录已存在或当前发送状态不允许此操作，请刷新后重试"
	default:
		return 500, "操作失败，请稍后重试"
	}
}

func mentionCardUpdateable(card model.FeishuDeliveryJob, targetMessageID string, now time.Time) bool {
	return card.Status == "sent" && card.FeishuMessageID == targetMessageID && card.SentAt != nil && card.SentAt.Add(14*24*time.Hour).After(now)
}

func mentionAdminTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	local := value.In(mentionLocation())
	return &local
}
