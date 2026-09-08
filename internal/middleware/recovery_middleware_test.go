package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoveryRequestSummaryOmitsBodyAndRedactsMetadata(t *testing.T) {
	request := httptest.NewRequest(
		"POST",
		"https://example.test/api/office/push-targets?webdavPassword=query-secret&view=full",
		strings.NewReader(`{"webdavPassword":"body-secret","name":"坚果云"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer header-secret")
	request.Header.Set("Cookie", "session=cookie-secret")

	summary := recoveryRequestSummary(request)
	for _, secret := range []string{"query-secret", "body-secret", "header-secret", "cookie-secret"} {
		if strings.Contains(summary, secret) {
			t.Fatalf("recovery request summary leaked %q: %s", secret, summary)
		}
	}
	if !strings.Contains(summary, "view=full") || !strings.Contains(summary, "webdavPassword=%5BREDACTED%5D") {
		t.Fatalf("recovery request summary lost safe metadata or redaction marker: %s", summary)
	}
}
