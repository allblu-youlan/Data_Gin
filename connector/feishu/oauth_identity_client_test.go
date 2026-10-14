package feishu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

type oauthIdentityTransport func(*http.Request) (*http.Response, error)

func (f oauthIdentityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func oauthIdentityResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestOAuthIdentityUsesOfficialCodeFlowWithoutRetainingToken(t *testing.T) {
	const token = "private-user-access-token"
	calls := 0
	httpClient := &http.Client{Transport: oauthIdentityTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > oauthIdentityTimeout {
			t.Fatal("identity request must have a bounded deadline")
		}
		if request.URL.RawQuery != "" {
			t.Fatal("credentials must not be placed in a URL")
		}
		switch calls {
		case 1:
			if request.URL.String() != oauthTokenURL || request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Fatal("incorrect token request")
			}
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("client_id") != "cli_app" || request.Form.Get("client_secret") != "private-app-secret" || request.Form.Get("code") != "one-time-code" || request.Form.Get("redirect_uri") != "https://example.com/callback" || request.Form.Get("grant_type") != "authorization_code" {
				t.Fatal("incorrect token parameters")
			}
			if request.Form.Has("scope") || request.Form.Has("refresh_token") {
				t.Fatal("identity lookup must not request offline access")
			}
			return oauthIdentityResponse(200, `{"code":0,"access_token":"`+token+`","token_type":"Bearer","expires_in":1800}`), nil
		case 2:
			if request.URL.String() != oauthUserInfoURL || request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer "+token {
				t.Fatal("incorrect user identity request")
			}
			return oauthIdentityResponse(200, `{"code":0,"data":{"open_id":"ou_viewer","name":"张三","email":"private@example.com"}}`), nil
		default:
			t.Fatal("unexpected identity request")
			return nil, errors.New("unexpected request")
		}
	})}
	client, err := NewOAuthIdentityClient("cli_app", "private-app-secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	openID, err := client.Resolve(t.Context(), "one-time-code", "https://example.com/callback")
	if err != nil || openID != "ou_viewer" || calls != 2 {
		t.Fatalf("identity=%q calls=%d err=%v", openID, calls, err)
	}
	if httpClient.Timeout != 0 || client.http.Timeout != oauthIdentityTimeout {
		t.Fatal("bounded timeout must not mutate caller's client")
	}
	if value := fmt.Sprintf("%+v %#v", client, client); strings.Contains(value, token) || strings.Contains(value, "private-app-secret") {
		t.Fatal("client formatting leaked credentials")
	}
}

func TestOAuthIdentityRejectsUnsafeProviderResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"provider error", 503, `{"error_description":"private-user-access-token"}`},
		{"code denied", 200, `{"code":20003,"error":"invalid_grant","error_description":"private-user-access-token"}`},
		{"missing token", 200, `{"code":0,"expires_in":7200,"token_type":"Bearer"}`},
		{"bad JSON", 200, `{"access_token":"private-user-access-token"`},
		{"trailing JSON", 200, `{"code":0,"access_token":"private-user-access-token","expires_in":7200,"token_type":"Bearer"}{}`},
		{"oversized body", 200, strings.Repeat("x", oauthIdentityMaxBodySize+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewOAuthIdentityClient("cli_app", "private-app-secret", &http.Client{Transport: oauthIdentityTransport(func(*http.Request) (*http.Response, error) {
				return oauthIdentityResponse(tc.status, tc.body), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			openID, err := client.Resolve(t.Context(), "one-time-code", "https://example.com/callback")
			if err == nil || openID != "" || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe response accepted: identity=%q error=%v", openID, err)
			}
		})
	}
}

func TestOAuthIdentityRejectsRedirectAndMissingIdentity(t *testing.T) {
	for _, redirect := range []bool{true, false} {
		calls := 0
		client, err := NewOAuthIdentityClient("cli_app", "secret", &http.Client{Transport: oauthIdentityTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if redirect {
				response := oauthIdentityResponse(http.StatusTemporaryRedirect, "private redirect body")
				response.Header.Set("Location", "https://unexpected.example/token")
				return response, nil
			}
			if calls == 1 {
				return oauthIdentityResponse(200, `{"code":0,"access_token":"private-user-token","expires_in":3600,"token_type":"Bearer"}`), nil
			}
			return oauthIdentityResponse(200, `{"code":0,"data":{"user_id":"internal-id","name":"private name"}}`), nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		if openID, err := client.Resolve(t.Context(), "code", "https://example.com/callback"); err == nil || openID != "" {
			t.Fatal("invalid identity accepted")
		}
		if redirect && calls != 1 {
			t.Fatal("OAuth redirect followed")
		}
	}
}

func TestOAuthIdentityCancellationAndInvalidRedirect(t *testing.T) {
	client, err := NewOAuthIdentityClient("cli_app", "secret", &http.Client{Timeout: time.Hour, Transport: oauthIdentityTransport(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Resolve(ctx, "code", "https://example.com/callback")
	var identityError *OAuthIdentityError
	if !errors.As(err, &identityError) || identityError.Class != providerhttp.ErrorClassCanceled {
		t.Fatalf("cancellation=%v", err)
	}
	for _, redirect := range []string{"https://user:password@example.com/callback", "http://public.example/callback", "javascript:alert(1)"} {
		if _, err := client.Resolve(t.Context(), "code", redirect); err == nil {
			t.Fatal("unsafe callback accepted")
		}
	}
}
