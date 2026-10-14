package data_svc

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/pkg/providerhttp"
)

const (
	FeishuMentionViewBasePath     = "/api/v1/feishu-mention-view"
	FeishuMentionViewCallbackPath = FeishuMentionViewBasePath + "/callback"
	FeishuMentionViewStateTTL     = 5 * time.Minute
	FeishuMentionViewSessionTTL   = time.Hour
)

type mentionViewClaims struct {
	Version  int    `json:"v"`
	Kind     string `json:"kind"`
	AppID    string `json:"app"`
	Expires  int64  `json:"exp"`
	Nonce    string `json:"nonce,omitempty"`
	ReportID uint   `json:"report,omitempty"`
	OpenID   string `json:"sub,omitempty"`
}

func (s *FeishuMentionViewService) BeginLogin(reportID uint) (authorizationURL, stateCookie string, err error) {
	if s == nil || reportID == 0 {
		return "", "", ErrFeishuMentionViewInvalid
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", ErrFeishuMentionViewUnavailable
	}
	state := base64.RawURLEncoding.EncodeToString(nonce)
	cookie, err := s.signCookie(mentionViewClaims{Kind: "state", Expires: s.now().Add(FeishuMentionViewStateTTL).Unix(), Nonce: state, ReportID: reportID})
	if err != nil {
		return "", "", err
	}
	query := url.Values{"client_id": {s.appID}, "response_type": {"code"}, "redirect_uri": {s.callbackURL}, "state": {state}}
	return feishu.OAuthAuthorizeURL + "?" + query.Encode(), cookie, nil
}

func (s *FeishuMentionViewService) CompleteLogin(ctx context.Context, code, state, cookie string) (reportID uint, sessionCookie string, err error) {
	claims, err := s.readCookie(cookie, "state")
	if err != nil || state == "" || !hmac.Equal([]byte(state), []byte(claims.Nonce)) || claims.ReportID == 0 || code == "" {
		return 0, "", ErrFeishuMentionViewUnauthorized
	}
	openID, err := s.identity.Resolve(ctx, code, s.callbackURL)
	if err != nil {
		var providerError *feishu.OAuthIdentityError
		if errors.As(err, &providerError) && providerError.Class == providerhttp.ErrorClassAuth {
			return 0, "", ErrFeishuMentionViewUnauthorized
		}
		return 0, "", ErrFeishuMentionViewUnavailable
	}
	if !feishu.ValidOAuthOpenID(openID) {
		return 0, "", ErrFeishuMentionViewUnauthorized
	}
	session, err := s.signCookie(mentionViewClaims{Kind: "session", Expires: s.now().Add(FeishuMentionViewSessionTTL).Unix(), OpenID: openID})
	return claims.ReportID, session, err
}

func (s *FeishuMentionViewService) ParseSession(cookie string) (string, error) {
	claims, err := s.readCookie(cookie, "session")
	if err != nil || !feishu.ValidOAuthOpenID(claims.OpenID) {
		return "", ErrFeishuMentionViewUnauthorized
	}
	return claims.OpenID, nil
}

func (s *FeishuMentionViewService) signCookie(claims mentionViewClaims) (string, error) {
	claims.Version, claims.AppID = 1, s.appID
	encoded, err := json.Marshal(claims)
	if err != nil {
		return "", ErrFeishuMentionViewUnavailable
	}
	body := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, s.cookieKey)
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *FeishuMentionViewService) readCookie(cookie, kind string) (mentionViewClaims, error) {
	var claims mentionViewClaims
	if s == nil || len(cookie) == 0 || len(cookie) > 4096 {
		return claims, ErrFeishuMentionViewUnauthorized
	}
	body, signature, ok := strings.Cut(cookie, ".")
	if !ok {
		return claims, ErrFeishuMentionViewUnauthorized
	}
	supplied, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return claims, ErrFeishuMentionViewUnauthorized
	}
	mac := hmac.New(sha256.New, s.cookieKey)
	_, _ = mac.Write([]byte(body))
	if !hmac.Equal(supplied, mac.Sum(nil)) {
		return claims, ErrFeishuMentionViewUnauthorized
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || json.Unmarshal(raw, &claims) != nil || claims.Version != 1 || claims.Kind != kind || claims.AppID != s.appID || claims.Expires <= s.now().Unix() {
		return mentionViewClaims{}, ErrFeishuMentionViewUnauthorized
	}
	return claims, nil
}
