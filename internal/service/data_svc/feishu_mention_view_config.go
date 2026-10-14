package data_svc

import (
	"net/url"
	"strings"
)

type FeishuMentionDetailAccessDTO struct {
	AppID       string `json:"appId"`
	PublicURL   string `json:"publicUrl"`
	CallbackURL string `json:"callbackUrl"`
	Valid       bool   `json:"valid"`
	Hint        string `json:"hint"`
}

// Keep the card link, OAuth redirect_uri and administrator's copyable value
// derived from the same configured origin; never trust a forwarded Host header.
func mentionViewPublicBase(raw string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		(base.Path != "" && base.Path != "/") ||
		(base.Scheme != "https" && !(base.Scheme == "http" && mentionViewLoopback(base.Hostname()))) {
		return nil, ErrFeishuMentionViewUnavailable
	}
	base.Path, base.RawPath = "", ""
	return base, nil
}

func mentionViewLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func mentionDetailAccess(appID, raw string) FeishuMentionDetailAccessDTO {
	out := FeishuMentionDetailAccessDTO{AppID: appID, Hint: "请将 APP_URL / App.Url 配置为可访问的 HTTPS 站点根地址，不含 /api、查询参数或页面路径。"}
	base, err := mentionViewPublicBase(raw)
	if err != nil {
		return out
	}
	out.PublicURL = base.String()
	out.CallbackURL = base.String() + FeishuMentionViewCallbackPath
	out.Valid = mentionViewCredential(appID) && !mentionViewLoopback(base.Hostname())
	out.Hint = "请在当前飞书应用的安全设置 → 重定向 URL 中添加完整回调地址，不要填到事件与回调的请求地址；事件与回调可继续使用长连接。出现 20029 时核对应用 ID、协议、域名、端口和路径完全一致；此处不能验证飞书后台是否已保存。"
	if mentionViewLoopback(base.Hostname()) {
		out.Hint = "当前 APP_URL 为本机地址，飞书收件人无法访问。请配置可访问的 HTTPS 域名，并将完整回调地址添加到飞书应用的重定向 URL。"
	}
	if !mentionViewCredential(appID) {
		out.Hint = "请先配置飞书应用 ID，再到同一应用的安全设置 → 重定向 URL 中添加完整回调地址。"
	}
	return out
}
