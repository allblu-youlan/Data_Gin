//go:build integration

package data_svc

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"github.com/hibiken/asynq"
)

// Queue names isolate this test from other test runs without flushing Redis.
// Registry validation still uses the production delivery queue contract.
type mentionIsolatedPublisher struct {
	job.TaskPublisher
	queue string
}

func (p mentionIsolatedPublisher) Publish(ctx context.Context, task *asynq.Task, options job.TaskPublishOptions) error {
	options.Queue = p.queue
	return p.TaskPublisher.Publish(ctx, task, options)
}

func TestFeishuMentionMySQLRedisOutboxDelivery(t *testing.T) {
	addr := os.Getenv("FEISHU_MENTION_REDIS_ADDR")
	if addr == "" {
		t.Skip("FEISHU_MENTION_REDIS_ADDR is required for Redis integration tests")
	}
	s, now := mentionMySQLFixture(t, true)
	s.cfg.FollowupEnabled = false
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Archive(ctx, mentionTestEvent(s, "chat_a", "redis_outage", "u1")); err != nil {
		t.Fatal(err)
	}
	var delivery model.FeishuDeliveryJob
	if err := s.db.Where("app_id = ?", s.appID).First(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	key := job.FeishuMentionTaskKey(delivery.ID, delivery.DispatchGeneration)
	store := data_dao.NewAsyncJobOutboxDAO(s.db.Where("task_key = ?", key))
	registry, err := job.NewOutboxTaskRegistry(job.FeishuMentionOutboxTaskDefinitions(s.cfg.MaxAttempts, 10*time.Second)...)
	if err != nil {
		t.Fatal(err)
	}
	newDispatcher := func(publisher job.TaskPublisher) *job.OutboxDispatcher {
		t.Helper()
		dispatcher, err := job.NewOutboxDispatcher(store, publisher, registry, job.OutboxDispatcherConfig{
			WorkerID: s.appID, Now: s.now, RetryBase: time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		return dispatcher
	}
	// A closed loopback port exercises a real failed Redis connection.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unavailableAddr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	unavailable := asynq.NewClient(asynq.RedisClientOpt{Addr: unavailableAddr, DialTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { _ = unavailable.Close() })
	if err := newDispatcher(job.NewAsynqTaskPublisher(unavailable)).DispatchOnce(ctx); !errors.Is(err, job.ErrOutboxPublish) {
		t.Fatalf("Redis outage must leave a retryable outbox: %v", err)
	}
	var outbox model.AsyncJobOutbox
	if err := s.db.Where("task_key = ?", key).First(&outbox).Error; err != nil {
		t.Fatal(err)
	}
	if outbox.PublishedAt != nil || outbox.Attempts != 1 || outbox.LastErrorSafe == "" {
		t.Fatalf("outbox lost outage state: %+v", outbox)
	}
	if err := s.db.First(&delivery, delivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "pending" || delivery.Attempts != 0 {
		t.Fatalf("queue outage consumed business attempt: %+v", delivery)
	}
	// Change the injected clock before workers start, avoiding concurrent clock mutation.
	*now = now.Add(2 * time.Second)
	options := asynq.RedisClientOpt{Addr: addr, DB: 15}
	client := asynq.NewClient(options)
	inspector := asynq.NewInspector(options)
	queue := "feishu-mention-integration-" + s.appID
	t.Cleanup(func() {
		_ = inspector.DeleteQueue(queue, true)
		_ = inspector.Close()
		_ = client.Close()
	})
	dispatcher := newDispatcher(mentionIsolatedPublisher{TaskPublisher: job.NewAsynqTaskPublisher(client), queue: queue})
	if err := dispatcher.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := inspector.GetQueueInfo(queue)
	if err != nil || info.Pending != 1 {
		t.Fatalf("repeated dispatch must enqueue once: %+v %v", info, err)
	}
	cardClient := &mentionTestClient{}
	processor := NewFeishuMentionProcessor(s, cardClient, nil)
	completed := make(chan error, 2)
	mux := asynq.NewServeMux()
	mux.HandleFunc(job.TypeFeishuMentionDeliver, func(ctx context.Context, task *asynq.Task) error {
		err := processor.HandleDelivery(ctx, task)
		completed <- err
		return err
	})
	server := asynq.NewServer(options, asynq.Config{
		Concurrency: 2, Queues: map[string]int{queue: 1},
		TaskCheckInterval: 20 * time.Millisecond, ShutdownTimeout: time.Second, LogLevel: asynq.ErrorLevel,
	})
	if err := server.Start(mux); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the real Asynq worker")
	}
	if err := s.db.First(&delivery, delivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&outbox, outbox.ID).Error; err != nil {
		t.Fatal(err)
	}
	cardClient.mu.Lock()
	calls := len(cardClient.calls)
	cardClient.mu.Unlock()
	if delivery.Status != "sent" || delivery.Attempts != 1 || calls != 1 || outbox.PublishedAt == nil {
		t.Fatalf("pipeline did not finish exactly once: status=%s attempts=%d calls=%d published=%v", delivery.Status, delivery.Attempts, calls, outbox.PublishedAt)
	}
}
