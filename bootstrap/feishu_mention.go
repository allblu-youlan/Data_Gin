package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/connector/feishu"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/providerhttp"
	projectredis "gin-biz-web-api/pkg/redis"

	"github.com/go-redis/redis/v8"
	"github.com/hibiken/asynq"
)

var mentionRuntime struct {
	sync.Mutex
	service   *data_svc.FeishuMentionService
	processor *data_svc.FeishuMentionProcessor
	receiver  *feishu.MentionReceiver
	inspector *asynq.Inspector
	redis     *redis.Client
	cancel    context.CancelFunc
	done      chan struct{}
}

func setupFeishuMention() error {
	cfg, err := appConfig.LoadFeishuMentionConfig()
	if err != nil {
		return err
	}
	s := data_svc.NewFeishuMentionService(database.DB, global.Credentials.FeishuAppID(), cfg, config.GetString("cfg.app.url"))
	mentionRuntime.service = s
	if cfg.Enabled {
		if database.DB == nil {
			return fmt.Errorf("Feishu mention requires the database")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = s.Initialize(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("initialize Feishu mention schema/state; run migrate schema first: %w", err)
		}
	}
	queueRedis := redis.NewClient(&redis.Options{Addr: redisAddrFromConfig(), Username: config.GetString("cfg.queue_job.redis.username"), Password: config.GetString("cfg.queue_job.redis.password"), DB: config.GetInt("cfg.queue_job.redis.db")})
	mentionRuntime.redis = queueRedis
	data_svc.RegisterFeishuMentionQueueStatusProvider(func(ctx context.Context) *bool {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		ok := queueRedis.Ping(ctx).Err() == nil
		return &ok
	})
	var bot data_svc.FeishuMentionCardClient
	var inspector *asynq.Inspector
	if cfg.Enabled && config.GetBool("cfg.queue_job.worker_enabled") {
		redisInstance := projectredis.Instance()
		if redisInstance == nil || redisInstance.Client == nil {
			return fmt.Errorf("Feishu mention requires Redis")
		}
		httpClient, err := providerhttp.NewClient(providerhttp.ClientConfig{Timeout: time.Duration(cfg.APITimeoutSeconds) * time.Second})
		if err != nil {
			return err
		}
		tokens, err := feishu.NewTenantTokenProvider(global.Credentials.FeishuAppID(), global.Credentials.FeishuAppSecret(), httpClient, redisInstance.Client)
		if err != nil {
			return err
		}
		bot, err = feishu.NewBotClient(tokens, httpClient)
		if err != nil {
			return err
		}
		inspector = asynq.NewInspector(asynq.RedisClientOpt{Addr: redisAddrFromConfig(), Username: config.GetString("cfg.queue_job.redis.username"), Password: config.GetString("cfg.queue_job.redis.password"), DB: config.GetInt("cfg.queue_job.redis.db")})
		mentionRuntime.inspector = inspector
	}
	processor := data_svc.NewFeishuMentionProcessor(s, bot, inspector)
	if cfg.Enabled && config.GetBool("cfg.queue_job.worker_enabled") {
		limiter, err := feishu.NewMentionSendLimiter(queueRedis, global.Credentials.FeishuAppID(), cfg.SendRatePerSecond)
		if err != nil {
			return err
		}
		processor.SetLimiter(limiter)
	}
	mentionRuntime.processor = processor
	if !cfg.Enabled || !cfg.ReceiverEnabled {
		data_svc.RegisterFeishuMentionReceiverStatusProvider(func() data_svc.FeishuMentionReceiverStatus {
			connected := false
			return data_svc.FeishuMentionReceiverStatus{Scope: "this_instance", Enabled: false, Connected: &connected}
		})
		return nil
	}
	receiver, err := feishu.NewMentionReceiver(global.Credentials.FeishuAppID(), global.Credentials.FeishuAppSecret(), func(ctx context.Context, e feishu.MentionEvent) error {
		mentions := make([]data_svc.FeishuMention, 0, len(e.Mentions))
		for _, m := range e.Mentions {
			mentions = append(mentions, data_svc.FeishuMention{Key: m.Key, OpenID: m.OpenID, Name: m.Name, MentionedType: m.MentionedType, TenantKey: m.TenantKey})
		}
		return s.Archive(ctx, data_svc.FeishuMentionEvent{AppID: e.AppID, TenantKey: e.TenantKey, MessageID: e.MessageID, ChatID: e.ChatID, ChatType: e.ChatType, SenderType: e.SenderType, SenderOpenID: e.SenderOpenID, SenderName: e.SenderName, MessageType: e.MessageType, Content: e.Content, CreatedAt: e.CreatedAt, Mentions: mentions})
	}, func(ctx context.Context, c feishu.MentionConfirmation) (string, error) {
		already, err := s.Confirm(ctx, data_svc.FeishuMentionConfirmation{AppID: c.AppID, OperatorOpenID: c.OperatorOpenID, MessageID: c.MessageID, Action: c.Action, RecipientRecordID: c.RecipientRecordID, PayloadVersion: c.PayloadVersion})
		if err != nil {
			for _, safe := range []error{data_svc.ErrFeishuMentionDisabled, data_svc.ErrFeishuMentionForbidden, data_svc.ErrFeishuMentionInvalid} {
				if errors.Is(err, safe) {
					return safe.Error(), err
				}
			}
			return "操作失败，请稍后重试", err
		}
		if already {
			return "已记录查看确认，请勿重复操作", nil
		}
		return "已记录查看确认", nil
	})
	if err != nil {
		return err
	}
	mentionRuntime.receiver = receiver
	data_svc.RegisterFeishuMentionReceiverStatusProvider(func() data_svc.FeishuMentionReceiverStatus {
		state := receiver.Status()
		return data_svc.FeishuMentionReceiverStatus{Scope: "this_instance", Enabled: true, Connected: &state.Connected, LastEventAt: state.LastEventAt, LastErrorAt: state.LastErrorAt}
	})
	return nil
}

func startFeishuMentionReceiver() {
	mentionRuntime.Lock()
	defer mentionRuntime.Unlock()
	if mentionRuntime.receiver == nil || mentionRuntime.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	mentionRuntime.cancel, mentionRuntime.done = cancel, done
	receiver := mentionRuntime.receiver
	go func() {
		defer close(done)
		if err := receiver.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			console.Warning("Feishu mention receiver stopped; check credentials and connection status")
		}
	}()
}

func stopFeishuMentionReceiver(ctx context.Context) {
	mentionRuntime.Lock()
	cancel, done, receiver := mentionRuntime.cancel, mentionRuntime.done, mentionRuntime.receiver
	mentionRuntime.cancel = nil
	mentionRuntime.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if receiver != nil {
		_ = receiver.Close(ctx)
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func closeFeishuMentionResources() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopFeishuMentionReceiver(ctx)
	if mentionRuntime.inspector != nil {
		_ = mentionRuntime.inspector.Close()
	}
	if mentionRuntime.redis != nil {
		_ = mentionRuntime.redis.Close()
	}
}

func registerFeishuMentionScheduledTasks(scheduler scheduledTaskRegistrar) {
	if scheduler == nil || !config.GetBool("cfg.feishu_mention.enabled") {
		return
	}
	task, err := job.NewFeishuMentionPlanTask()
	if err != nil {
		console.Warning("Failed to create Feishu mention planner")
		return
	}
	if _, err = scheduler.Register(job.FeishuMentionPlanCron, task, asynq.Unique(job.FeishuMentionPlanUniquePeriod)); err != nil {
		console.Warning("Failed to register Feishu mention planner")
	}
	if global.QueueJobClient != nil {
		if _, err := global.QueueJobClient.Enqueue(task, asynq.Unique(job.FeishuMentionPlanUniquePeriod)); err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
			console.Warning("Initial Feishu mention plan deferred until next scheduled minute")
		}
	}
}
