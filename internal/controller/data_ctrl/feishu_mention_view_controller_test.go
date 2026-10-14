package data_ctrl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/paginator"

	"github.com/gin-gonic/gin"
)

type mentionViewStub struct {
	parseSession func(string) (string, error)
	beginLogin   func(uint) (string, string, error)
	complete     func(context.Context, string, string, string) (uint, string, error)
	report       func(context.Context, string, uint, int) (*data_svc.FeishuMentionViewReportDTO, error)
}

func (s mentionViewStub) ParseSession(value string) (string, error)  { return s.parseSession(value) }
func (s mentionViewStub) BeginLogin(id uint) (string, string, error) { return s.beginLogin(id) }
func (s mentionViewStub) CompleteLogin(ctx context.Context, code, state, cookie string) (uint, string, error) {
	return s.complete(ctx, code, state, cookie)
}
func (s mentionViewStub) Report(ctx context.Context, openID string, id uint, page int) (*data_svc.FeishuMentionViewReportDTO, error) {
	return s.report(ctx, openID, id, page)
}

func mentionViewTestRouter(controller *FeishuMentionViewController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET(data_svc.FeishuMentionViewBasePath+"/reports/:id", controller.Report)
	router.GET(data_svc.FeishuMentionViewCallbackPath, controller.Callback)
	return router
}

func TestFeishuMentionViewStartsOAuthWithoutConsoleAccount(t *testing.T) {
	controller := &FeishuMentionViewController{secure: true, service: mentionViewStub{
		parseSession: func(value string) (string, error) {
			if value != "" {
				t.Fatalf("unexpected session: %q", value)
			}
			return "", data_svc.ErrFeishuMentionViewUnauthorized
		},
		beginLogin: func(id uint) (string, string, error) {
			if id != 27 {
				t.Fatalf("report id = %d", id)
			}
			return "https://accounts.feishu.cn/open-apis/authen/v1/authorize?state=random", "signed-state", nil
		},
	}}
	response := httptest.NewRecorder()
	mentionViewTestRouter(controller).ServeHTTP(response, httptest.NewRequest(http.MethodGet, mentionViewReportPath(27), nil))
	if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "https://accounts.feishu.cn/") {
		t.Fatalf("status=%d location=%s", response.Code, response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != mentionViewStateCookie || cookies[0].Value != "signed-state" || cookies[0].Path != data_svc.FeishuMentionViewBasePath || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 300 {
		t.Fatalf("unsafe state cookie: %#v", cookies)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("private redirects must not be cached or send referrers")
	}
}

func TestFeishuMentionViewCallbackClearsStateAndUsesLocalReportRedirect(t *testing.T) {
	controller := &FeishuMentionViewController{secure: true, service: mentionViewStub{
		complete: func(_ context.Context, code, state, cookie string) (uint, string, error) {
			if code != "auth-code" || state != "nonce" || cookie != "signed-state" {
				t.Fatal("callback did not pass browser-bound state to service")
			}
			return 27, "signed-session", nil
		},
	}}
	request := httptest.NewRequest(http.MethodGet, data_svc.FeishuMentionViewCallbackPath+"?code=auth-code&state=nonce&next=https://attacker.example", nil)
	request.AddCookie(&http.Cookie{Name: mentionViewStateCookie, Value: "signed-state"})
	response := httptest.NewRecorder()
	mentionViewTestRouter(controller).ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != mentionViewReportPath(27) {
		t.Fatalf("status=%d location=%s", response.Code, response.Header().Get("Location"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 || cookies[0].Name != mentionViewStateCookie || cookies[0].MaxAge != -1 || cookies[1].Name != mentionViewSessionCookie || !cookies[1].HttpOnly || !cookies[1].Secure || cookies[1].MaxAge != 3600 {
		t.Fatalf("unsafe callback cookies: %#v", cookies)
	}
}

func TestFeishuMentionViewRendersFullTextSafelyInShanghaiTime(t *testing.T) {
	summary, original, unsafe := "摘要 <b>不能执行</b>", "https://applink.feishu.cn/client/chat/open?openChatId=oc_A&position=42", "javascript:alert(1)"
	chatURL := "https://applink.feishu.cn/client/chat/open?openChatId=oc_A"
	instant := time.Date(2026, 10, 13, 18, 1, 0, 0, time.UTC)
	text := "第一行\n<script>alert('message')</script>\n" + strings.Repeat("完整原文", 2000)
	controller := &FeishuMentionViewController{service: mentionViewStub{
		parseSession: func(value string) (string, error) {
			if value != "signed-session" {
				t.Fatal("session cookie missing")
			}
			return "ou_viewer", nil
		},
		report: func(_ context.Context, openID string, id uint, page int) (*data_svc.FeishuMentionViewReportDTO, error) {
			if openID != "ou_viewer" || id != 27 || page != 2 {
				t.Fatalf("identity must come from session: %q %d %d", openID, id, page)
			}
			return &data_svc.FeishuMentionViewReportDTO{
				Report:     data_svc.FeishuMentionViewReportSummary{ID: 27, ChatName: "采购群 <img>", RecipientName: "收件人", ReportDate: "2026-10-14", WindowStart: instant.Add(-24 * time.Hour), WindowEnd: instant, GeneratedAt: instant, MessageCount: 41, Summary: &summary, ChatURL: &chatURL},
				Messages:   []data_svc.FeishuMentionViewMessageDTO{{SenderOpenID: "ou_private_sender", SenderName: "王五", MessageCreatedAt: instant, DisplayText: text, OriginalURL: &original}, {SenderOpenID: "ou_private_second", MessageCreatedAt: instant, DisplayText: "第二条", OriginalURL: &unsafe}},
				Pagination: paginator.Pagination{Page: 2, PerPage: 20, TotalPage: 3, TotalCount: 41},
			}, nil
		},
	}}
	request := httptest.NewRequest(http.MethodGet, mentionViewReportPath(27)+"?page=2&open_id=ou_attacker", nil)
	request.AddCookie(&http.Cookie{Name: mentionViewSessionCookie, Value: "signed-session"})
	response := httptest.NewRecorder()
	mentionViewTestRouter(controller).ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
	for _, want := range []string{"采购群 &lt;img&gt;", "王五", "2026-10-14 02:01", "2026-10-14T02:01:00&#43;08:00", strings.Repeat("完整原文", 2000), "&lt;script&gt;", "&lt;b&gt;不能执行&lt;/b&gt;", "/reports/27?page=1", "/reports/27?page=3", "定位到原消息", "打开原群", "暂未取得这条消息的定位信息", "姓名暂不可用", "position=42"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing rendered content %q", want[:min(len(want), 70)])
		}
	}
	for _, forbidden := range []string{"<script>", "<img>", "javascript:", "ou_private", "ou_attacker", "office_mention", "重新排队"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("unsafe or internal content %q rendered", forbidden)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatal("missing private HTML response protections")
	}
}

func TestFeishuMentionViewErrorsNeverExposeProviderOrReportContent(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{data_svc.ErrFeishuMentionViewNotFound, 404}, {data_svc.ErrFeishuMentionViewUnauthorized, 401}, {data_svc.ErrFeishuMentionViewInvalid, 400}, {errors.New("private-token private-message"), 503}} {
		controller := &FeishuMentionViewController{service: mentionViewStub{
			parseSession: func(string) (string, error) { return "ou_viewer", nil },
			report: func(context.Context, string, uint, int) (*data_svc.FeishuMentionViewReportDTO, error) {
				return nil, tc.err
			},
		}}
		response := httptest.NewRecorder()
		mentionViewTestRouter(controller).ServeHTTP(response, httptest.NewRequest(http.MethodGet, mentionViewReportPath(27), nil))
		if response.Code != tc.code || strings.Contains(response.Body.String(), "private-") {
			t.Fatalf("status=%d body=%s", response.Code, response.Body)
		}
	}
}

func TestFeishuMentionViewRejectsMalformedNavigationBeforeOAuth(t *testing.T) {
	controller := &FeishuMentionViewController{service: mentionViewStub{}}
	for _, path := range []string{"/reports/0", "/reports/no", "/reports/27?page=0", "/reports/27?page=-1", "/reports/27?page=abc"} {
		response := httptest.NewRecorder()
		mentionViewTestRouter(controller).ServeHTTP(response, httptest.NewRequest(http.MethodGet, data_svc.FeishuMentionViewBasePath+path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("path=%s status=%d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	mentionViewTestRouter(controller).ServeHTTP(response, httptest.NewRequest(http.MethodGet, data_svc.FeishuMentionViewCallbackPath+"?error=access_denied", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("denied consent status=%d", response.Code)
	}
}
