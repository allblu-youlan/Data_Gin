package providerhttp

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryAfter parses a delay in seconds or an HTTP date. Missing, invalid, expired,
// and overflowing second values return zero.
func RetryAfter(header http.Header, now time.Time) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 && seconds <= math.MaxInt64/int64(time.Second) {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}
