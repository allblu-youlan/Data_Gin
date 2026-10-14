package data_ctrl

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/providerhttp"
	projectredis "gin-biz-web-api/pkg/redis"

	"github.com/gin-gonic/gin"
)

const (
	mentionViewStateCookie   = "feishu_mention_view_state"
	mentionViewSessionCookie = "feishu_mention_view_session"
)

//go:embed feishu_mention_view.html
var mentionViewHTML string

var mentionViewTemplate = template.Must(template.New("mention-view").Parse(mentionViewHTML))
var mentionViewShanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

type mentionViewService interface {
	BeginLogin(uint) (string, string, error)
	CompleteLogin(context.Context, string, string, string) (uint, string, error)
	ParseSession(string) (string, error)
	Report(context.Context, string, uint, int) (*data_svc.FeishuMentionViewReportDTO, error)
}

type FeishuMentionViewController struct {
	service     mentionViewService
	configError error
	secure      bool
}

func NewFeishuMentionViewController() *FeishuMentionViewController {
	publicURL := config.GetString("cfg.app.url")
	service, err := data_svc.NewFeishuMentionViewService(database.DB, global.Credentials.FeishuAppID(), global.Credentials.FeishuAppSecret(), publicURL, nil)
	if err == nil {
		configureMentionViewNames(service)
	}
	parsed, _ := url.Parse(publicURL)
	return &FeishuMentionViewController{service: service, configError: err, secure: parsed != nil && parsed.Scheme == "https"}
}

func configureMentionViewNames(service *data_svc.FeishuMentionViewService) {
	rds := projectredis.Instance()
	if rds == nil || rds.Client == nil {
		return
	}
	httpClient, err := providerhttp.NewClient(providerhttp.ClientConfig{Timeout: 2 * time.Second})
	if err != nil {
		return
	}
	tokens, err := feishu.NewTenantTokenProvider(global.Credentials.FeishuAppID(), global.Credentials.FeishuAppSecret(), httpClient, rds.Client)
	if err != nil {
		return
	}
	bot, err := feishu.NewBotClient(tokens, httpClient)
	if err == nil {
		service.SetUserNamesClient(bot)
	}
}

func (c *FeishuMentionViewController) Report(ctx *gin.Context) {
	mentionViewHeaders(ctx)
	if c.configError != nil || c.service == nil {
		renderMentionViewError(ctx, data_svc.ErrFeishuMentionViewUnavailable)
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	page, pageErr := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if err != nil || id == 0 || uint64(uint(id)) != id || pageErr != nil || page < 1 {
		renderMentionViewError(ctx, data_svc.ErrFeishuMentionViewInvalid)
		return
	}
	cookie, _ := ctx.Cookie(mentionViewSessionCookie)
	openID, err := c.service.ParseSession(cookie)
	if err != nil {
		authorizationURL, state, loginErr := c.service.BeginLogin(uint(id))
		if loginErr != nil {
			renderMentionViewError(ctx, loginErr)
			return
		}
		c.setCookie(ctx, mentionViewStateCookie, state, data_svc.FeishuMentionViewStateTTL)
		ctx.Redirect(http.StatusFound, authorizationURL)
		return
	}
	report, err := c.service.Report(ctx.Request.Context(), openID, uint(id), page)
	if err != nil {
		renderMentionViewError(ctx, err)
		return
	}
	renderMentionView(ctx, http.StatusOK, mentionViewPageFromReport(report))
}

func (c *FeishuMentionViewController) Callback(ctx *gin.Context) {
	mentionViewHeaders(ctx)
	if c.configError != nil || c.service == nil {
		renderMentionViewError(ctx, data_svc.ErrFeishuMentionViewUnavailable)
		return
	}
	stateCookie, _ := ctx.Cookie(mentionViewStateCookie)
	c.setCookie(ctx, mentionViewStateCookie, "", -time.Second)
	if ctx.Query("error") != "" {
		renderMentionViewError(ctx, data_svc.ErrFeishuMentionViewUnauthorized)
		return
	}
	reportID, session, err := c.service.CompleteLogin(ctx.Request.Context(), ctx.Query("code"), ctx.Query("state"), stateCookie)
	if err != nil {
		renderMentionViewError(ctx, err)
		return
	}
	c.setCookie(ctx, mentionViewSessionCookie, session, data_svc.FeishuMentionViewSessionTTL)
	ctx.Redirect(http.StatusSeeOther, mentionViewReportPath(reportID))
}

func (c *FeishuMentionViewController) setCookie(ctx *gin.Context, name, value string, ttl time.Duration) {
	ctx.SetSameSite(http.SameSiteLaxMode)
	ctx.SetCookie(name, value, int(ttl/time.Second), data_svc.FeishuMentionViewBasePath, "", c.secure, true)
}

func mentionViewHeaders(ctx *gin.Context) {
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Referrer-Policy", "no-referrer")
	ctx.Header("X-Content-Type-Options", "nosniff")
	ctx.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
}

type mentionViewPage struct {
	ChatName, RecipientName, ReportDate, Window, GeneratedAt, Summary string
	MessageCount, Page, TotalPages                                    int
	Messages                                                          []mentionViewMessage
	PreviousURL, NextURL                                              string
	ErrorTitle, ErrorText                                             string
	SenderNameNotice                                                  string
}

type mentionViewMessage struct {
	Sender, Time, DateTime, Text, OriginalURL string
}

func mentionViewPageFromReport(dto *data_svc.FeishuMentionViewReportDTO) mentionViewPage {
	report := dto.Report
	page := mentionViewPage{
		ChatName: report.ChatName, RecipientName: report.RecipientName, ReportDate: report.ReportDate,
		Window:      mentionViewTime(report.WindowStart) + " ～ " + mentionViewTime(report.WindowEnd),
		GeneratedAt: mentionViewTime(report.GeneratedAt), MessageCount: report.MessageCount,
		Page: dto.Pagination.Page, TotalPages: dto.Pagination.TotalPage,
		Messages:         make([]mentionViewMessage, 0, len(dto.Messages)),
		SenderNameNotice: dto.SenderNameNotice,
	}
	if report.Summary != nil {
		page.Summary = *report.Summary
	}
	for _, message := range dto.Messages {
		sender := strings.TrimSpace(message.SenderName)
		if sender == "" {
			sender = "姓名暂不可用"
		}
		item := mentionViewMessage{Sender: sender, Time: mentionViewTime(message.MessageCreatedAt), DateTime: message.MessageCreatedAt.In(mentionViewShanghai).Format(time.RFC3339), Text: message.DisplayText}
		if message.OriginalURL != nil {
			parsed, err := url.Parse(*message.OriginalURL)
			if err == nil && parsed.Host != "" && parsed.User == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") {
				item.OriginalURL = parsed.String()
			}
		}
		page.Messages = append(page.Messages, item)
	}
	if page.Page > 1 {
		page.PreviousURL = mentionViewReportPath(report.ID) + "?page=" + strconv.Itoa(page.Page-1)
	}
	if page.Page < page.TotalPages {
		page.NextURL = mentionViewReportPath(report.ID) + "?page=" + strconv.Itoa(page.Page+1)
	}
	return page
}

func mentionViewTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.In(mentionViewShanghai).Format("2006-01-02 15:04")
}

func mentionViewReportPath(id uint) string {
	return data_svc.FeishuMentionViewBasePath + "/reports/" + strconv.FormatUint(uint64(id), 10)
}

func renderMentionViewError(ctx *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	page := mentionViewPage{ErrorTitle: "暂时无法打开日报", ErrorText: "服务暂时不可用，请稍后从飞书卡片重新打开。"}
	switch {
	case errors.Is(err, data_svc.ErrFeishuMentionViewUnauthorized):
		status, page.ErrorTitle, page.ErrorText = http.StatusUnauthorized, "登录未完成", "请从飞书卡片重新打开，并使用接收这份日报的飞书账号。"
	case errors.Is(err, data_svc.ErrFeishuMentionViewNotFound):
		status, page.ErrorTitle, page.ErrorText = http.StatusNotFound, "无法查看这份日报", "日报不存在，或当前飞书账号不是这份日报的收件人。"
	case errors.Is(err, data_svc.ErrFeishuMentionViewInvalid):
		status, page.ErrorTitle, page.ErrorText = http.StatusBadRequest, "日报链接无效", "请从飞书卡片重新打开日报详情。"
	}
	renderMentionView(ctx, status, page)
}

func renderMentionView(ctx *gin.Context, status int, page mentionViewPage) {
	var output bytes.Buffer
	if err := mentionViewTemplate.Execute(&output, page); err != nil {
		ctx.String(http.StatusInternalServerError, "日报暂时无法显示，请稍后重试。")
		return
	}
	ctx.Data(status, "text/html; charset=utf-8", output.Bytes())
}
