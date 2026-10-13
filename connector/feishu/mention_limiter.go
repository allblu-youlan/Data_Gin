package feishu

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// MentionSendLimiter shares the application's outbound quota between workers.
// A permit is acquired before the business attempt starts; no local fallback
// is used when Redis is unavailable, which would multiply the send rate.
type MentionSendLimiter struct {
	client     *redis.Client
	key        string
	intervalMS int64
}

func NewMentionSendLimiter(client *redis.Client, appID string, perSecond int) (*MentionSendLimiter, error) {
	if client == nil || appID == "" || perSecond <= 0 {
		return nil, fmt.Errorf("feishu mention: invalid rate limit configuration")
	}
	interval := (1000 + int64(perSecond) - 1) / int64(perSecond)
	return &MentionSendLimiter{client: client, key: fmt.Sprintf("feishu:mention:send-rate:%x", sha256.Sum256([]byte(appID))), intervalMS: interval}, nil
}

var mentionRatePermit = redis.NewScript(`
local ttl = redis.call('PTTL', KEYS[1])
if ttl > 0 then return ttl end
redis.call('SET', KEYS[1], '1', 'PX', ARGV[1])
return 0
`)

func (l *MentionSendLimiter) Wait(ctx context.Context) error {
	for {
		delay, err := mentionRatePermit.Run(ctx, l.client, []string{l.key}, l.intervalMS).Int64()
		if err != nil {
			return fmt.Errorf("feishu mention: shared send quota unavailable")
		}
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
