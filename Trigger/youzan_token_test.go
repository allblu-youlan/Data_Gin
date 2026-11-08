package Trigger

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gin-biz-web-api/pkg/config"
)

func TestGetYouzanAccessTokenRejectsGatewayResponsesSafely(t *testing.T) {
	for _, test := range []struct {
		name, body string
	}{
		{name: "HTML", body: "<html>private-client-secret private-token</html>"},
		{name: "token in error response", body: `{"success":true,"code":200,"data":{"access_token":"private-token"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			setupYouzanTokenConfig(t, server.URL)
			token, err := GetYouzanAccessToken()
			if token != "" || err == nil {
				t.Fatalf("gateway response accepted: token length=%d error=%v", len(token), err)
			}
			for _, field := range []string{"stage=token", "http_status=503", "class=provider_unavailable"} {
				if !strings.Contains(err.Error(), field) {
					t.Errorf("missing %s: %v", field, err)
				}
			}
			for _, secret := range []string{"private-client-secret", "private-token", "<html>"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error exposed response or credentials")
				}
			}
		})
	}
}

func setupYouzanTokenConfig(t *testing.T, endpoint string) {
	t.Helper()
	directory := t.TempDir()
	contents := fmt.Sprintf("cfg:\n  youzan:\n    token_url: %q\n    client_id: test-client\n    client_secret: private-client-secret\n    grant_id: test-grant\n", endpoint)
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", directory+string(os.PathSeparator))
}
