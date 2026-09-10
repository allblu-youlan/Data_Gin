package job

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"github.com/hibiken/asynq"
)

const (
	defaultOutboxPollInterval  = time.Second
	defaultOutboxIdlePollMax   = 10 * time.Second
	defaultOutboxLockTimeout   = time.Minute
	defaultOutboxBatchSize     = 100
	defaultOutboxRetryBase     = 5 * time.Second
	defaultOutboxRetryMax      = 5 * time.Minute
	safeInvalidOutboxTaskError = "invalid outbox task"
	safeQueuePublishError      = "queue publish failed"
)

var (
	ErrInvalidOutboxTask          = errors.New("invalid outbox task")
	ErrOutboxPublish              = errors.New("outbox publish failed")
	ErrOutboxState                = errors.New("outbox state update failed")
	ErrOutboxTaskConflictRecovery = errors.New("outbox task conflict recovery failed")
)

type OutboxStore interface {
	ClaimBatch(ctx context.Context, workerID string, taskTypes []string, now time.Time, lockTimeout time.Duration, limit int) ([]model.AsyncJobOutbox, error)
	MarkPublished(ctx context.Context, id uint, publishedAt time.Time) error
	MarkFailed(ctx context.Context, id uint, availableAt time.Time, safeError string) error
}

type TaskPublishOptions struct {
	TaskID                string
	Queue                 string
	MaxRetry              int
	Timeout               time.Duration
	RecoverTaskIDConflict bool
}

type TaskPublisher interface {
	Publish(ctx context.Context, task *asynq.Task, options TaskPublishOptions) error
}

type AsynqTaskPublisher struct {
	client    asynqTaskEnqueuer
	inspector asynqTaskInspector
}

type asynqTaskEnqueuer interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

type asynqTaskInspector interface {
	GetTaskInfo(queue, id string) (*asynq.TaskInfo, error)
	RunTask(queue, id string) error
}

func NewAsynqTaskPublisher(client asynqTaskEnqueuer, inspectors ...asynqTaskInspector) *AsynqTaskPublisher {
	var inspector asynqTaskInspector
	if len(inspectors) > 0 {
		inspector = inspectors[0]
	}
	return &AsynqTaskPublisher{client: client, inspector: inspector}
}

func (publisher *AsynqTaskPublisher) Publish(ctx context.Context, task *asynq.Task, options TaskPublishOptions) error {
	if publisher == nil || publisher.client == nil {
		return fmt.Errorf("outbox publisher: client is required")
	}
	optionsList := []asynq.Option{
		asynq.TaskID(options.TaskID),
		asynq.Queue(options.Queue),
		asynq.MaxRetry(options.MaxRetry),
	}
	if options.Timeout > 0 {
		optionsList = append(optionsList, asynq.Timeout(options.Timeout))
	}
	_, err := publisher.client.EnqueueContext(ctx, task, optionsList...)
	if !errors.Is(err, asynq.ErrTaskIDConflict) || !options.RecoverTaskIDConflict {
		return err
	}
	if publisher.inspector == nil {
		return ErrOutboxTaskConflictRecovery
	}
	info, inspectErr := publisher.inspector.GetTaskInfo(options.Queue, options.TaskID)
	if inspectErr != nil {
		return fmt.Errorf("%w: inspect task", ErrOutboxTaskConflictRecovery)
	}
	if info.Type != task.Type() || !bytes.Equal(info.Payload, task.Payload()) {
		return fmt.Errorf("%w: task identity mismatch", ErrOutboxTaskConflictRecovery)
	}
	switch info.State {
	case asynq.TaskStateArchived, asynq.TaskStateRetry, asynq.TaskStateScheduled:
		if runErr := publisher.inspector.RunTask(options.Queue, options.TaskID); runErr != nil {
			return fmt.Errorf("%w: make existing task runnable", ErrOutboxTaskConflictRecovery)
		}
		return nil
	case asynq.TaskStateActive, asynq.TaskStatePending, asynq.TaskStateAggregating:
		return nil
	default:
		return fmt.Errorf("%w: task is %s", ErrOutboxTaskConflictRecovery, info.State)
	}
}

type OutboxTaskDefinition struct {
	TaskType              string
	Queue                 string
	MaxRetry              int
	Timeout               time.Duration
	RecoverTaskIDConflict bool
	Build                 func([]byte) (*asynq.Task, error)
}

type OutboxTaskRegistry struct {
	definitions map[string]OutboxTaskDefinition
	taskTypes   []string
}

func NewOutboxTaskRegistry(definitions ...OutboxTaskDefinition) (*OutboxTaskRegistry, error) {
	registry := &OutboxTaskRegistry{definitions: make(map[string]OutboxTaskDefinition, len(definitions))}
	for _, definition := range definitions {
		if definition.TaskType == "" || definition.Queue == "" || definition.MaxRetry < 0 || definition.Build == nil {
			return nil, fmt.Errorf("outbox task registry: invalid task definition")
		}
		if _, exists := registry.definitions[definition.TaskType]; exists {
			return nil, fmt.Errorf("outbox task registry: duplicate task type %q", definition.TaskType)
		}
		registry.definitions[definition.TaskType] = definition
		registry.taskTypes = append(registry.taskTypes, definition.TaskType)
	}
	if len(registry.taskTypes) == 0 {
		return nil, fmt.Errorf("outbox task registry: at least one task definition is required")
	}
	return registry, nil
}

func (registry *OutboxTaskRegistry) TaskTypes() []string {
	if registry == nil {
		return nil
	}
	return append([]string(nil), registry.taskTypes...)
}

func (registry *OutboxTaskRegistry) Resolve(row model.AsyncJobOutbox) (*asynq.Task, TaskPublishOptions, error) {
	if registry == nil || row.TaskKey == "" {
		return nil, TaskPublishOptions{}, ErrInvalidOutboxTask
	}
	definition, exists := registry.definitions[row.TaskType]
	if !exists || row.QueueName != definition.Queue {
		return nil, TaskPublishOptions{}, ErrInvalidOutboxTask
	}
	task, err := definition.Build([]byte(row.PayloadJSON))
	if err != nil || task == nil || task.Type() != definition.TaskType {
		return nil, TaskPublishOptions{}, ErrInvalidOutboxTask
	}
	options := TaskPublishOptions{
		TaskID: row.TaskKey, Queue: definition.Queue, MaxRetry: definition.MaxRetry, Timeout: definition.Timeout,
		RecoverTaskIDConflict: definition.RecoverTaskIDConflict,
	}
	return task, options, nil
}

func MallWeatherOutboxTaskDefinitions(maxRetry int, fetchTimeout time.Duration) []OutboxTaskDefinition {
	definitions := make([]OutboxTaskDefinition, 0, len(MallWeatherTaskTypes()))
	for _, taskType := range MallWeatherTaskTypes() {
		currentTaskType := taskType
		queue, _ := ExpectedMallWeatherQueue(currentTaskType)
		timeout := time.Duration(0)
		if IsMallWeatherFetchTaskType(currentTaskType) {
			timeout = fetchTimeout
		}
		definitions = append(definitions, OutboxTaskDefinition{
			TaskType: currentTaskType, Queue: queue, MaxRetry: maxRetry, Timeout: timeout,
			Build: func(payload []byte) (*asynq.Task, error) { return NewMallWeatherTask(currentTaskType, payload) },
		})
	}
	return definitions
}

type OutboxDispatcherConfig struct {
	WorkerID          string
	PollInterval      time.Duration
	IdlePollMax       time.Duration
	LockTimeout       time.Duration
	BatchSize         int
	RetryBase         time.Duration
	RetryMax          time.Duration
	Now               func() time.Time
	OnPublished       func(model.AsyncJobOutbox, time.Time)
	OnCycleError      func(error)
	DatabaseAvailable func(context.Context) bool
}

type OutboxDispatcher struct {
	store             OutboxStore
	publisher         TaskPublisher
	registry          *OutboxTaskRegistry
	taskTypes         []string
	workerID          string
	pollInterval      time.Duration
	idlePollMax       time.Duration
	lockTimeout       time.Duration
	batchSize         int
	retryBase         time.Duration
	retryMax          time.Duration
	now               func() time.Time
	onPublished       func(model.AsyncJobOutbox, time.Time)
	onCycleError      func(error)
	databaseAvailable func(context.Context) bool
	wait              func(context.Context, time.Duration) error
}

func NewOutboxDispatcher(store OutboxStore, publisher TaskPublisher, registry *OutboxTaskRegistry, cfg OutboxDispatcherConfig) (*OutboxDispatcher, error) {
	if store == nil {
		return nil, fmt.Errorf("outbox dispatcher: store is required")
	}
	if publisher == nil {
		return nil, fmt.Errorf("outbox dispatcher: publisher is required")
	}
	if registry == nil || len(registry.TaskTypes()) == 0 {
		return nil, fmt.Errorf("outbox dispatcher: task registry is required")
	}
	if cfg.WorkerID == "" {
		return nil, fmt.Errorf("outbox dispatcher: worker id is required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultOutboxPollInterval
	}
	if cfg.IdlePollMax <= 0 {
		cfg.IdlePollMax = defaultOutboxIdlePollMax
		if cfg.IdlePollMax < cfg.PollInterval {
			cfg.IdlePollMax = cfg.PollInterval
		}
	}
	if cfg.IdlePollMax < cfg.PollInterval {
		return nil, fmt.Errorf("outbox dispatcher: idle poll max must not be less than poll interval")
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = defaultOutboxLockTimeout
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultOutboxBatchSize
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = defaultOutboxRetryBase
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = defaultOutboxRetryMax
	}
	if cfg.RetryMax < cfg.RetryBase {
		return nil, fmt.Errorf("outbox dispatcher: retry max must not be less than retry base")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.DatabaseAvailable == nil {
		cfg.DatabaseAvailable = func(context.Context) bool { return true }
	}

	return &OutboxDispatcher{
		store:             store,
		publisher:         publisher,
		registry:          registry,
		taskTypes:         registry.TaskTypes(),
		workerID:          cfg.WorkerID,
		pollInterval:      cfg.PollInterval,
		idlePollMax:       cfg.IdlePollMax,
		lockTimeout:       cfg.LockTimeout,
		batchSize:         cfg.BatchSize,
		retryBase:         cfg.RetryBase,
		retryMax:          cfg.RetryMax,
		now:               cfg.Now,
		onPublished:       cfg.OnPublished,
		onCycleError:      cfg.OnCycleError,
		databaseAvailable: cfg.DatabaseAvailable,
		wait:              waitForOutboxPoll,
	}, nil
}

// Run dispatches immediately, then waits between cycles until ctx is cancelled.
// Empty cycles use a bounded idle backoff, while a cycle that finds work resets
// to the normal poll interval. Consecutive failures use the separate retry
// backoff so a database outage is not polled every second.
func (dispatcher *OutboxDispatcher) Run(ctx context.Context) error {
	consecutiveFailures := 0
	consecutiveIdleCycles := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		delay := dispatcher.pollInterval
		hadWork, err := dispatcher.dispatchOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			if dispatcher.onCycleError != nil {
				dispatcher.onCycleError(err)
			}
			delay = outboxBackoff(dispatcher.retryBase, dispatcher.retryMax, consecutiveFailures)
			consecutiveFailures++
			consecutiveIdleCycles = 0
		} else {
			consecutiveFailures = 0
			if hadWork {
				consecutiveIdleCycles = 0
			} else {
				delay = outboxBackoff(dispatcher.pollInterval, dispatcher.idlePollMax, consecutiveIdleCycles)
				consecutiveIdleCycles++
			}
		}

		if err := dispatcher.wait(ctx, delay); err != nil {
			return err
		}
	}
}

func (dispatcher *OutboxDispatcher) DispatchOnce(ctx context.Context) error {
	_, err := dispatcher.dispatchOnce(ctx)
	return err
}

func (dispatcher *OutboxDispatcher) dispatchOnce(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !dispatcher.databaseAvailable(ctx) {
		return false, fmt.Errorf("outbox dispatcher: %w", database.ErrUnavailable)
	}
	now := dispatcher.now().UTC()
	rows, err := dispatcher.store.ClaimBatch(ctx, dispatcher.workerID, dispatcher.taskTypes, now, dispatcher.lockTimeout, dispatcher.batchSize)
	if err != nil {
		return false, fmt.Errorf("outbox dispatcher: claim batch: %w", err)
	}

	var dispatchErrors []error
	for i := range rows {
		if err := ctx.Err(); err != nil {
			dispatchErrors = append(dispatchErrors, err)
			break
		}
		if err := dispatcher.dispatchRow(ctx, rows[i]); err != nil {
			dispatchErrors = append(dispatchErrors, err)
		}
	}
	return len(rows) > 0, errors.Join(dispatchErrors...)
}

func waitForOutboxPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (dispatcher *OutboxDispatcher) dispatchRow(ctx context.Context, row model.AsyncJobOutbox) error {
	task, publishOptions, err := dispatcher.registry.Resolve(row)
	if err != nil {
		return dispatcher.failRow(ctx, row, safeInvalidOutboxTaskError, ErrInvalidOutboxTask)
	}
	err = dispatcher.publisher.Publish(ctx, task, publishOptions)
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return dispatcher.failRow(ctx, row, safeQueuePublishError, ErrOutboxPublish)
	}

	publishedAt := dispatcher.now().UTC()
	if err := dispatcher.store.MarkPublished(ctx, row.ID, publishedAt); err != nil {
		return fmt.Errorf("%w: mark row %d published", ErrOutboxState, row.ID)
	}
	if dispatcher.onPublished != nil {
		dispatcher.onPublished(row, publishedAt)
	}
	return nil
}

func (dispatcher *OutboxDispatcher) failRow(ctx context.Context, row model.AsyncJobOutbox, safeError string, cause error) error {
	delay := outboxBackoff(dispatcher.retryBase, dispatcher.retryMax, row.Attempts)
	if err := dispatcher.store.MarkFailed(ctx, row.ID, dispatcher.now().UTC().Add(delay), safeError); err != nil {
		return errors.Join(
			fmt.Errorf("%w: row %d", cause, row.ID),
			fmt.Errorf("%w: mark row %d failed", ErrOutboxState, row.ID),
		)
	}
	return fmt.Errorf("%w: row %d", cause, row.ID)
}

func outboxBackoff(base, maximum time.Duration, attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	delay := base
	for i := 0; i < attempts && delay < maximum; i++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
