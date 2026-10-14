package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

const (
	OAuthAuthorizeURL        = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	oauthTokenURL            = "https://accounts.feishu.cn/oauth/v3/token"
	oauthUserInfoURL         = "https://open.feishu.cn/open-apis/authen/v1/user_info"
	oauthIdentityTimeout     = 10 * time.Second
	oauthIdentityMaxBodySize = 64 * 1024
)

// OAuthIdentityError deliberately excludes provider messages, tokens and response bodies.
type OAuthIdentityError struct {
	Class      providerhttp.ErrorClass
	HTTPStatus int
}

func (e *OAuthIdentityError) Error() string { return "feishu oauth: identity verification failed" }

type OAuthIdentityClient struct {
	appID, appSecret string
	tokenURL         string
	userInfoURL      string
	http             *http.Client
}

func NewOAuthIdentityClient(appID, appSecret string, httpClient *http.Client) (*OAuthIdentityClient, error) {
	if !validTokenCredential(appID) || !validTokenCredential(appSecret) {
		return nil, errors.New("feishu oauth: invalid client configuration")
	}
	if httpClient == nil {
		var err error
		httpClient, err = providerhttp.NewClient(providerhttp.ClientConfig{Timeout: oauthIdentityTimeout})
		if err != nil {
			return nil, errors.New("feishu oauth: HTTP client unavailable")
		}
	}
	client := *httpClient
	if client.Timeout <= 0 || client.Timeout > oauthIdentityTimeout {
		client.Timeout = oauthIdentityTimeout
	}
	// OAuth endpoints are fixed. Never forward the code, secret or bearer to a redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	return &OAuthIdentityClient{appID: appID, appSecret: appSecret, tokenURL: oauthTokenURL, userInfoURL: oauthUserInfoURL, http: &client}, nil
}

// Resolve exchanges a one-time code and returns only the current user's open ID.
// The access token remains local to this call and is never persisted or returned.
func (c *OAuthIdentityClient) Resolve(ctx context.Context, code, redirectURI string) (string, error) {
	if c == nil || c.http == nil || ctx == nil || !validTokenCredential(code) || !validOAuthRedirectURI(redirectURI) {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassRequest}
	}
	ctx, cancel := context.WithTimeout(ctx, oauthIdentityTimeout)
	defer cancel()
	payload := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {c.appID}, "client_secret": {c.appSecret},
		"code": {code}, "redirect_uri": {redirectURI},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(payload.Encode()))
	if err != nil {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassRequest}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		Code        *int   `json:"code"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := c.do(request, &token); err != nil {
		return "", err
	}
	if token.Code == nil {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassResponse}
	}
	if *token.Code != 0 || token.Error != "" {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassAuth}
	}
	if !validTokenCredential(token.AccessToken) || !strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn <= 0 {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassResponse}
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, c.userInfoURL, nil)
	if err != nil {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassRequest}
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	var user struct {
		Code *int `json:"code"`
		Data struct {
			OpenID string `json:"open_id"`
		} `json:"data"`
	}
	if err := c.do(request, &user); err != nil {
		return "", err
	}
	if user.Code == nil || *user.Code != 0 {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassAuth}
	}
	if !ValidOAuthOpenID(user.Data.OpenID) {
		return "", &OAuthIdentityError{Class: providerhttp.ErrorClassResponse}
	}
	return user.Data.OpenID, nil
}

func (c *OAuthIdentityClient) do(request *http.Request, output any) error {
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return &OAuthIdentityError{Class: providerhttp.ClassifyRetry(0, err).Class}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		class := providerhttp.ClassifyRetry(response.StatusCode, nil).Class
		if response.StatusCode == http.StatusBadRequest {
			class = providerhttp.ErrorClassAuth
		}
		if class == providerhttp.ErrorClassNone {
			class = providerhttp.ErrorClassResponse
		}
		return &OAuthIdentityError{Class: class, HTTPStatus: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, oauthIdentityMaxBodySize+1))
	if err != nil || len(body) > oauthIdentityMaxBodySize {
		return &OAuthIdentityError{Class: providerhttp.ErrorClassResponse, HTTPStatus: response.StatusCode}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(output); err != nil {
		return &OAuthIdentityError{Class: providerhttp.ErrorClassResponse, HTTPStatus: response.StatusCode}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &OAuthIdentityError{Class: providerhttp.ErrorClassResponse, HTTPStatus: response.StatusCode}
	}
	return nil
}

func validOAuthRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && u.User == nil && u.Fragment == "" &&
		(u.Scheme == "https" || u.Scheme == "http" && isLoopbackHost(u.Hostname()))
}

func ValidOAuthOpenID(value string) bool {
	if len(value) < 4 || len(value) > 128 || !(strings.HasPrefix(value, "ou_") || strings.HasPrefix(value, "ou-")) {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func (*OAuthIdentityClient) String() string   { return "feishu.OAuthIdentityClient{redacted}" }
func (*OAuthIdentityClient) GoString() string { return "feishu.OAuthIdentityClient{redacted}" }
