package data_svc

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"gin-biz-web-api/connector/feishu"
)

type FeishuMentionUserNamesClient interface {
	UserNames(context.Context, []string) (map[string]string, error)
}

type mentionViewNameEntry struct {
	name    string
	expires time.Time
}

type mentionViewNames struct {
	client     FeishuMentionUserNamesClient
	mu         sync.Mutex
	cache      map[string]mentionViewNameEntry
	lookupGate chan struct{}
}

// SetUserNamesClient is called during startup. Name enrichment is independent of
// message intake and uses the existing application token provider.
func (s *FeishuMentionViewService) SetUserNamesClient(client FeishuMentionUserNamesClient) {
	s.names = &mentionViewNames{client: client, cache: make(map[string]mentionViewNameEntry), lookupGate: make(chan struct{}, 1)}
}

func (s *FeishuMentionViewService) fillSenderNames(ctx context.Context, dto *FeishuMentionViewReportDTO) {
	missing := make(map[string]bool)
	for _, message := range dto.Messages {
		if strings.TrimSpace(message.SenderName) == "" && feishu.ValidOAuthOpenID(message.SenderOpenID) {
			missing[message.SenderOpenID] = true
		}
	}
	ids := make([]string, 0, len(missing))
	for id := range missing {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	names := make(map[string]string)
	if s.names != nil && s.names.client != nil && len(ids) > 0 {
		// At most 20 messages are rendered. A slow directory must not prevent
		// reading the report; callers can refresh if enrichment times out.
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		names = s.names.lookup(ctx, ids)
	}
	for i := range dto.Messages {
		message := &dto.Messages[i]
		if strings.TrimSpace(message.SenderName) == "" {
			message.SenderName = names[message.SenderOpenID]
			if message.SenderName == "" {
				dto.SenderNameNotice = "部分发送人姓名暂不可用，请稍后刷新或在原群查看。"
			}
		}
	}
}

func (n *mentionViewNames) lookup(ctx context.Context, ids []string) map[string]string {
	out, missing := n.cached(ids)
	if len(missing) == 0 {
		return out
	}
	// Recheck after waiting so simultaneous opens reuse the first request's
	// results. A caller can cancel while another directory lookup is in progress.
	select {
	case <-ctx.Done():
		return out
	case n.lookupGate <- struct{}{}:
	}
	defer func() { <-n.lookupGate }()
	resolved, pending := n.cached(missing)
	for len(pending) > 0 && ctx.Err() == nil {
		count := min(10, len(pending))
		batch := pending[:count]
		values, err := n.client.UserNames(ctx, batch)
		if err != nil {
			break
		}
		n.mu.Lock()
		if len(n.cache)+len(batch) > 1024 {
			clear(n.cache)
		}
		for _, id := range batch {
			name := strings.TrimSpace(values[id])
			ttl := 30 * time.Minute
			if name == "" {
				ttl = 15 * time.Second
			}
			n.cache[id] = mentionViewNameEntry{name: name, expires: time.Now().Add(ttl)}
			resolved[id] = name
		}
		n.mu.Unlock()
		pending = pending[count:]
	}
	for id, name := range resolved {
		out[id] = name
	}
	return out
}

func (n *mentionViewNames) cached(ids []string) (map[string]string, []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make(map[string]string)
	var missing []string
	for _, id := range ids {
		entry, ok := n.cache[id]
		if ok && entry.expires.After(time.Now()) {
			out[id] = entry.name
		} else {
			missing = append(missing, id)
		}
	}
	return out, missing
}
