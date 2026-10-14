package data_svc

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gin-biz-web-api/model"
)

func TestFeishuMentionViewConfigMatchesOAuthAndCard(t *testing.T) {
	for _, raw := range []string{"https://example.test:8443", "https://example.test:8443/"} {
		t.Run(raw, func(t *testing.T) {
			access := mentionDetailAccess("cli_test", raw)
			if !access.Valid || access.PublicURL != "https://example.test:8443" || access.CallbackURL != "https://example.test:8443"+FeishuMentionViewCallbackPath {
				t.Fatalf("invalid public configuration: %+v", access)
			}
			s, err := NewFeishuMentionViewService(nil, "cli_test", "test-secret", raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			authorization, _, err := s.BeginLogin(42)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(authorization)
			if err != nil || u.Query().Get("redirect_uri") != access.CallbackURL || u.Query().Get("client_id") != access.AppID {
				t.Fatalf("administrator configuration differs from OAuth: %s", authorization)
			}
			cardJSON, err := compactMentionDigestCard(model.FeishuDigestSnapshot{}, 42, raw)
			if err != nil {
				t.Fatal(err)
			}
			var card struct {
				Elements []struct{ Actions []struct{ URL string } }
			}
			if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
				t.Fatal(err)
			}
			link := card.Elements[len(card.Elements)-1].Actions[0].URL
			u, err = url.Parse(link)
			if err != nil || u.Query().Get("url") != access.PublicURL+FeishuMentionViewBasePath+"/reports/42" {
				t.Fatalf("card origin differs from OAuth: %s", link)
			}
		})
	}
}

func TestFeishuMentionViewConfigRejectsAmbiguousOrigin(t *testing.T) {
	for _, raw := range []string{"https://example.test/api", "https://example.test/?secret=hidden", "https://hidden:password@example.test", "https://example.test/#hidden", "http://example.test"} {
		t.Run(raw, func(t *testing.T) {
			access := mentionDetailAccess("cli_test", raw)
			encoded, _ := json.Marshal(access)
			if access.Valid || access.PublicURL != "" || access.CallbackURL != "" || strings.Contains(string(encoded), "hidden") {
				t.Fatal("invalid origin was exposed as usable configuration")
			}
			if _, err := NewFeishuMentionViewService(nil, "cli_test", "test-secret", raw, nil); err == nil {
				t.Fatal("OAuth accepted ambiguous origin")
			}
			if _, err := compactMentionDigestCard(model.FeishuDigestSnapshot{}, 42, raw); err == nil {
				t.Fatal("card accepted ambiguous origin")
			}
		})
	}
	access := mentionDetailAccess("cli_test", "http://localhost:8501")
	if access.Valid || access.CallbackURL == "" {
		t.Fatal("localhost must be marked inaccessible to recipients")
	}
	if _, err := NewFeishuMentionViewService(nil, "cli_test", "test-secret", access.PublicURL, nil); err != nil {
		t.Fatalf("local OAuth development is unavailable: %v", err)
	}
	if access := mentionDetailAccess("", "https://example.test"); access.Valid || !strings.Contains(access.Hint, "应用 ID") {
		t.Fatal("missing app ID was not explained")
	}
}
