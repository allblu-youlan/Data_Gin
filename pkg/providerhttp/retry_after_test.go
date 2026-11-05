package providerhttp

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, time.November, 5, 15, 40, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "seconds", value: "90", want: 90 * time.Second},
		{name: "whitespace", value: " 17 ", want: 17 * time.Second},
		{name: "leading plus compatibility", value: "+17", want: 17 * time.Second},
		{name: "HTTP date", value: "Thu, 05 Nov 2026 15:42:00 GMT", want: 2 * time.Minute},
		{name: "past date", value: "Thu, 05 Nov 2026 15:39:00 GMT"},
		{name: "current date", value: "Thu, 05 Nov 2026 15:40:00 GMT"},
		{name: "missing value"},
		{name: "zero", value: "0"},
		{name: "negative", value: "-1"},
		{name: "fractional", value: "1.5"},
		{name: "invalid", value: "invalid-header-secret"},
		{name: "largest whole-second duration", value: "9223372036", want: 9223372036 * time.Second},
		{name: "duration overflow", value: "9223372037"},
		{name: "duration wraparound", value: "18446744074"},
		{name: "integer overflow", value: "9223372036854775808"},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{"Retry-After": []string{test.value}}
			if got := RetryAfter(header, now); got != test.want {
				t.Fatalf("RetryAfter(%q)=%v want %v", test.value, got, test.want)
			}
		})
	}
	if got := RetryAfter(nil, now); got != 0 {
		t.Fatalf("RetryAfter(nil)=%v want zero", got)
	}
}
