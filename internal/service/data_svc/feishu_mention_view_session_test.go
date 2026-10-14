package data_svc

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

type mentionViewIdentityFunc func(context.Context, string, string) (string, error)

func (f mentionViewIdentityFunc) Resolve(ctx context.Context, code, redirectURI string) (string, error) {
	return f(ctx, code, redirectURI)
}

func mentionViewTestService(t *testing.T) (*FeishuMentionViewService, *time.Time, *int) {
	t.Helper()
	calls := 0
	s, err := NewFeishuMentionViewService(nil, "cli_app", "private-shared-app-secret", "https://example.com", mentionViewIdentityFunc(func(_ context.Context, code, redirectURI string) (string, error) {
		calls++
		if code != "one-time-code" || redirectURI != "https://example.com"+FeishuMentionViewCallbackPath {
			t.Fatal("unexpected code or callback")
		}
		return "ou_viewer", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, mentionLocation())
	s.now = func() time.Time { return now }
	return s, &now, &calls
}

func mentionViewTestLogin(t *testing.T, s *FeishuMentionViewService) (string, string) {
	t.Helper()
	authURL, cookie, err := s.BeginLogin(42)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil || u.Host != "accounts.feishu.cn" || u.Query().Get("client_id") != "cli_app" || u.Query().Get("response_type") != "code" || u.Query().Get("redirect_uri") != "https://example.com"+FeishuMentionViewCallbackPath {
		t.Fatalf("invalid authorize URL: %v", err)
	}
	if u.Query().Has("scope") || u.Query().Has("client_secret") || len(u.Query().Get("state")) < 32 {
		t.Fatal("authorization must contain random state and no secrets or excess scopes")
	}
	return u.Query().Get("state"), cookie
}

func TestFeishuMentionViewLoginSessionSharedAcrossInstances(t *testing.T) {
	s, now, _ := mentionViewTestService(t)
	state, cookie := mentionViewTestLogin(t, s)
	reportID, session, err := s.CompleteLogin(t.Context(), "one-time-code", state, cookie)
	if err != nil || reportID != 42 {
		t.Fatalf("login: report=%d err=%v", reportID, err)
	}
	peer, _, _ := mentionViewTestService(t)
	peer.now = func() time.Time { return *now }
	if openID, err := peer.ParseSession(session); err != nil || openID != "ou_viewer" {
		t.Fatalf("shared session=%q err=%v", openID, err)
	}
	body, _, _ := strings.Cut(session, ".")
	decoded, _ := base64.RawURLEncoding.DecodeString(body)
	for _, forbidden := range []string{"one-time-code", "access_token", "private-shared-app-secret", state, "report"} {
		if strings.Contains(string(decoded), forbidden) {
			t.Fatalf("session retained %s", forbidden)
		}
	}
	*now = now.Add(FeishuMentionViewSessionTTL)
	if _, err := peer.ParseSession(session); !errors.Is(err, ErrFeishuMentionViewUnauthorized) {
		t.Fatal("expired session accepted")
	}
}

func TestFeishuMentionViewRejectsInvalidStateBeforeProvider(t *testing.T) {
	for _, kind := range []string{"mismatch", "missing", "tampered", "expired"} {
		t.Run(kind, func(t *testing.T) {
			s, now, calls := mentionViewTestService(t)
			state, cookie := mentionViewTestLogin(t, s)
			switch kind {
			case "mismatch":
				state = "different-state"
			case "missing":
				cookie = ""
			case "tampered":
				cookie = "x" + cookie
			case "expired":
				*now = now.Add(FeishuMentionViewStateTTL)
			}
			if _, _, err := s.CompleteLogin(t.Context(), "one-time-code", state, cookie); !errors.Is(err, ErrFeishuMentionViewUnauthorized) || *calls != 0 {
				t.Fatalf("invalid state reached provider: calls=%d err=%v", *calls, err)
			}
		})
	}
}

func TestFeishuMentionViewCookiesAreBoundToKindAppAndSecret(t *testing.T) {
	s, _, _ := mentionViewTestService(t)
	state, cookie := mentionViewTestLogin(t, s)
	_, session, err := s.CompleteLogin(t.Context(), "one-time-code", state, cookie)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ParseSession(cookie); !errors.Is(err, ErrFeishuMentionViewUnauthorized) {
		t.Fatal("state cookie used as session")
	}
	if _, _, err := s.CompleteLogin(t.Context(), "one-time-code", state, session); !errors.Is(err, ErrFeishuMentionViewUnauthorized) {
		t.Fatal("session cookie used as state")
	}
	for _, config := range [][2]string{{"cli_other", "private-shared-app-secret"}, {"cli_app", "other-secret"}} {
		peer, err := NewFeishuMentionViewService(nil, config[0], config[1], "https://example.com", s.identity)
		if err != nil {
			t.Fatal(err)
		}
		peer.now = s.now
		if _, err := peer.ParseSession(session); !errors.Is(err, ErrFeishuMentionViewUnauthorized) {
			t.Fatal("session accepted by another application or secret")
		}
	}
}

func TestFeishuMentionViewConfigurationAndProviderErrorsAreSafe(t *testing.T) {
	for _, rawURL := range []string{"", "http://public.example", "https://user:secret@example.com", "https://example.com?x=1", "https://example.com/#callback"} {
		if _, err := NewFeishuMentionViewService(nil, "cli_app", "secret", rawURL, nil); !errors.Is(err, ErrFeishuMentionViewUnavailable) {
			t.Fatal("unsafe public URL accepted")
		}
	}
	s, _, _ := mentionViewTestService(t)
	state, cookie := mentionViewTestLogin(t, s)
	s.identity = mentionViewIdentityFunc(func(context.Context, string, string) (string, error) {
		return "", errors.New("provider response includes private-token")
	})
	if _, _, err := s.CompleteLogin(t.Context(), "one-time-code", state, cookie); !errors.Is(err, ErrFeishuMentionViewUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatalf("provider error not safely handled: %v", err)
	}
}
