package job

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"github.com/hibiken/asynq"
)

type fakeOutboxStore struct {
	mu              sync.Mutex
	rows            []model.AsyncJobOutbox
	claimErr        error
	publishErr      error
	claimCalls      int
	published       []publishedOutboxRow
	failed          []failedOutboxRow
	firstClaimReady chan struct{}
	claimReadyOnce  sync.Once
}

type publishedOutboxRow struct {
	id          uint
	publishedAt time.Time
}

type failedOutboxRow struct {
	id          uint
	availableAt time.Time
	safeError   string
}

func (store *fakeOutboxStore) ClaimBatch(_ context.Context, _ string, taskTypes []string, _ time.Time, _ time.Duration, _ int) ([]model.AsyncJobOutbox, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimCalls++
	if store.firstClaimReady != nil {
		store.claimReadyOnce.Do(func() { close(store.firstClaimReady) })
	}
	allowed := make(map[string]struct{}, len(taskTypes))
	for _, taskType := range taskTypes {
		allowed[taskType] = struct{}{}
	}
	rows := make([]model.AsyncJobOutbox, 0, len(store.rows))
	for _, row := range store.rows {
		if _, exists := allowed[row.TaskType]; exists {
			rows = append(rows, row)
		}
	}
	return rows, store.claimErr
}

func (store *fakeOutboxStore) MarkPublished(_ context.Context, id uint, publishedAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.publishErr != nil {
		return store.publishErr
	}
	store.published = append(store.published, publishedOutboxRow{id: id, publishedAt: publishedAt})
	return nil
}

func (store *fakeOutboxStore) MarkFailed(_ context.Context, id uint, availableAt time.Time, safeError string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failed = append(store.failed, failedOutboxRow{id: id, availableAt: availableAt, safeError: safeError})
	return nil
}

type fakeMallWeatherPublisher struct {
	calls  []publishCall
	err    error
	errors []error
}

type fakeAsynqTaskEnqueuer struct {
	err error
}

func (client *fakeAsynqTaskEnqueuer) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	return nil, client.err
}

type fakeAsynqTaskInspector struct {
	info     *asynq.TaskInfo
	runCalls int
}

func (inspector *fakeAsynqTaskInspector) GetTaskInfo(string, string) (*asynq.TaskInfo, error) {
	return inspector.info, nil
}

func (inspector *fakeAsynqTaskInspector) RunTask(string, string) error {
	inspector.runCalls++
	return nil
}

type publishCall struct {
	taskType string
	payload  string
	options  TaskPublishOptions
}

func (publisher *fakeMallWeatherPublisher) Publish(_ context.Context, task *asynq.Task, options TaskPublishOptions) error {
	publisher.calls = append(publisher.calls, publishCall{
		taskType: task.Type(),
		payload:  string(task.Payload()),
		options:  options,
	})
	if len(publisher.errors) > 0 {
		err := publisher.errors[0]
		publisher.errors = publisher.errors[1:]
		return err
	}
	return publisher.err
}

func TestOutboxDispatcherDispatchOncePublishesTask(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	store := &fakeOutboxStore{rows: []model.AsyncJobOutbox{weatherOutboxRow(1, 0)}}
	publisher := &fakeMallWeatherPublisher{}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("publish calls = %d, want 1", len(publisher.calls))
	}
	call := publisher.calls[0]
	if call.taskType != TypeMallWeatherFull || call.payload != `{"mall_id":123,"task_window":"full:123:2026071710"}` {
		t.Fatalf("published task = %q %s", call.taskType, call.payload)
	}
	if call.options.TaskID != "weather:full:123:2026071710" || call.options.Queue != MallWeatherQueueName ||
		call.options.MaxRetry != 2 || call.options.Timeout != 5*time.Minute {
		t.Fatalf("publish options = %+v", call.options)
	}
	if len(store.published) != 1 || store.published[0].id != 1 || !store.published[0].publishedAt.Equal(now) {
		t.Fatalf("published rows = %+v", store.published)
	}
	if len(store.failed) != 0 {
		t.Fatalf("failed rows = %+v", store.failed)
	}
}

func TestOutboxDispatcherSkipsDatabaseClaimWhileUnavailable(t *testing.T) {
	store := &fakeOutboxStore{}
	dispatcher := newTestOutboxDispatcher(t, store, &fakeMallWeatherPublisher{}, time.Now())
	dispatcher.databaseAvailable = func(context.Context) bool { return false }

	err := dispatcher.DispatchOnce(t.Context())
	if !errors.Is(err, database.ErrUnavailable) {
		t.Fatalf("DispatchOnce() error = %v, want ErrUnavailable", err)
	}
	if store.claimCalls != 0 {
		t.Fatalf("claim calls = %d, want 0", store.claimCalls)
	}
}

func TestOutboxDispatcherTreatsTaskIDConflictAsPublished(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	store := &fakeOutboxStore{rows: []model.AsyncJobOutbox{weatherOutboxRow(2, 0)}}
	publisher := &fakeMallWeatherPublisher{err: asynq.ErrTaskIDConflict}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(store.published) != 1 || store.published[0].id != 2 {
		t.Fatalf("published rows = %+v", store.published)
	}
	if len(store.failed) != 0 {
		t.Fatalf("failed rows = %+v", store.failed)
	}
}

func TestAsynqTaskPublisherMakesRecoverableReportTaskRunnable(t *testing.T) {
	for _, state := range []asynq.TaskState{asynq.TaskStateArchived, asynq.TaskStateRetry, asynq.TaskStateScheduled} {
		t.Run(state.String(), func(t *testing.T) {
			task := asynq.NewTask(TypeReportRun, []byte(`{"run_id":27}`))
			inspector := &fakeAsynqTaskInspector{info: &asynq.TaskInfo{
				ID: "report:run:run-uuid", Queue: ReportQueueName, Type: TypeReportRun,
				Payload: task.Payload(), State: state,
			}}
			publisher := NewAsynqTaskPublisher(&fakeAsynqTaskEnqueuer{err: asynq.ErrTaskIDConflict}, inspector)
			err := publisher.Publish(t.Context(), task, TaskPublishOptions{
				TaskID: "report:run:run-uuid", Queue: ReportQueueName, MaxRetry: ReportRunMaxRetry,
				RecoverTaskIDConflict: true,
			})
			if err != nil || inspector.runCalls != 1 {
				t.Fatalf("Publish() error=%v run calls=%d", err, inspector.runCalls)
			}
		})
	}
}

func TestOutboxDispatcherReportsPublishedRowsAfterStateUpdate(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	row := weatherOutboxRow(5, 0)
	row.AvailableAt = now.Add(-3 * time.Second)
	store := &fakeOutboxStore{rows: []model.AsyncJobOutbox{row}}
	publisher := &fakeMallWeatherPublisher{}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)

	var observedRows []model.AsyncJobOutbox
	var observedTimes []time.Time
	dispatcher.onPublished = func(row model.AsyncJobOutbox, publishedAt time.Time) {
		observedRows = append(observedRows, row)
		observedTimes = append(observedTimes, publishedAt)
	}

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(store.published) != 1 {
		t.Fatalf("published rows = %+v", store.published)
	}
	if len(observedRows) != 1 || observedRows[0].ID != 5 || !observedRows[0].AvailableAt.Equal(row.AvailableAt) ||
		len(observedTimes) != 1 || !observedTimes[0].Equal(now) {
		t.Fatalf("observed rows=%+v times=%+v", observedRows, observedTimes)
	}
}

func TestOutboxDispatcherSkipsPublishedHookWhenStateUpdateFails(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	store := &fakeOutboxStore{
		rows:       []model.AsyncJobOutbox{weatherOutboxRow(6, 0)},
		publishErr: errors.New("database unavailable"),
	}
	publisher := &fakeMallWeatherPublisher{}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)

	observed := 0
	dispatcher.onPublished = func(model.AsyncJobOutbox, time.Time) {
		observed++
	}

	err := dispatcher.DispatchOnce(context.Background())
	if !errors.Is(err, ErrOutboxState) {
		t.Fatalf("DispatchOnce() error = %v, want ErrOutboxState", err)
	}
	if observed != 0 {
		t.Fatalf("published hook called %d times, want 0", observed)
	}
}

func TestOutboxDispatcherStoresSafeFailureAndBackoff(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	store := &fakeOutboxStore{rows: []model.AsyncJobOutbox{weatherOutboxRow(3, 2)}}
	publisher := &fakeMallWeatherPublisher{err: errors.New("redis://user:top-secret@queue.invalid")}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)

	err := dispatcher.DispatchOnce(context.Background())
	if !errors.Is(err, ErrOutboxPublish) {
		t.Fatalf("DispatchOnce() error = %v, want ErrOutboxPublish", err)
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("DispatchOnce() exposed publisher error: %v", err)
	}
	if len(store.failed) != 1 {
		t.Fatalf("failed rows = %+v", store.failed)
	}
	failure := store.failed[0]
	if failure.id != 3 || failure.safeError != safeQueuePublishError {
		t.Fatalf("failure = %+v", failure)
	}
	if want := now.Add(4 * time.Second); !failure.availableAt.Equal(want) {
		t.Fatalf("available at = %v, want %v", failure.availableAt, want)
	}
	if len(store.published) != 0 {
		t.Fatalf("published rows = %+v", store.published)
	}
}

func TestOutboxDispatcherRejectsUnsafePayloadBeforePublish(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	row := weatherOutboxRow(4, 0)
	row.PayloadJSON = `{"mall_id":123,"task_window":"full:123:2026071710","app_secret":"do-not-queue"}`
	store := &fakeOutboxStore{rows: []model.AsyncJobOutbox{row}}
	publisher := &fakeMallWeatherPublisher{}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)

	err := dispatcher.DispatchOnce(context.Background())
	if !errors.Is(err, ErrInvalidOutboxTask) {
		t.Fatalf("DispatchOnce() error = %v, want ErrInvalidOutboxTask", err)
	}
	if len(publisher.calls) != 0 {
		t.Fatalf("publish calls = %d, want 0", len(publisher.calls))
	}
	if len(store.failed) != 1 || store.failed[0].safeError != safeInvalidOutboxTaskError {
		t.Fatalf("failed rows = %+v", store.failed)
	}
}

func TestOutboxDispatcherLeavesUnregisteredTasksForAnotherDispatcher(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	store := &fakeOutboxStore{rows: []model.AsyncJobOutbox{{
		BaseModel: model.BaseModel{ID: 8}, TaskKey: "report:run:uuid", TaskType: TypeReportRun,
		QueueName: ReportQueueName, PayloadJSON: model.JSONText(`{"run_id":31}`),
	}}}
	publisher := &fakeMallWeatherPublisher{}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)
	if err := dispatcher.DispatchOnce(t.Context()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(publisher.calls) != 0 || len(store.failed) != 0 || len(store.published) != 0 {
		t.Fatalf("unregistered row was touched: calls=%#v failed=%#v published=%#v", publisher.calls, store.failed, store.published)
	}
}

func TestOutboxDispatcherRunStopsOnCancellation(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	firstClaimReady := make(chan struct{})
	store := &fakeOutboxStore{firstClaimReady: firstClaimReady}
	publisher := &fakeMallWeatherPublisher{}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, now)
	dispatcher.pollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()
	<-firstClaimReady
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestOutboxDispatcherRunBacksOffAfterCycleFailureAndResetsAfterRecovery(t *testing.T) {
	store := &recoveringOutboxStore{
		claimErrors: []error{errors.New("database unavailable"), nil},
		claimCalls:  make(chan int, 3),
	}
	publisher := &fakeMallWeatherPublisher{}
	dispatcher := newTestOutboxDispatcher(t, store, publisher, time.Now())
	dispatcher.pollInterval = time.Hour
	dispatcher.retryBase = 5 * time.Millisecond
	dispatcher.retryMax = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()
	t.Cleanup(cancel)

	for want := 1; want <= 2; want++ {
		select {
		case got := <-store.claimCalls:
			if got != want {
				t.Fatalf("claim call = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("claim call %d did not run after retry backoff", want)
		}
	}

	select {
	case call := <-store.claimCalls:
		t.Fatalf("claim call %d ran before the recovered poll interval", call)
	case <-time.After(25 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after recovery cancellation")
	}
}

func TestOutboxDispatcherRunBacksOffWhileIdleAndResetsAfterWork(t *testing.T) {
	store := &scriptedOutboxStore{rows: [][]model.AsyncJobOutbox{
		nil,
		nil,
		{weatherOutboxRow(10, 0)},
		nil,
	}}
	dispatcher := newTestOutboxDispatcher(t, store, &fakeMallWeatherPublisher{}, time.Now())
	dispatcher.pollInterval = time.Second
	dispatcher.idlePollMax = 8 * time.Second

	var delays []time.Duration
	dispatcher.wait = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		if len(delays) == 4 {
			return context.Canceled
		}
		return nil
	}

	err := dispatcher.Run(t.Context())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, time.Second, time.Second}
	if len(delays) != len(want) {
		t.Fatalf("delays = %v, want %v", delays, want)
	}
	for index := range want {
		if delays[index] != want[index] {
			t.Fatalf("delays[%d] = %v, want %v", index, delays[index], want[index])
		}
	}
}

func TestOutboxBackoffIsBounded(t *testing.T) {
	tests := []struct {
		name     string
		attempts int
		want     time.Duration
	}{
		{"negative", -1, time.Second},
		{"first", 0, time.Second},
		{"third", 2, 4 * time.Second},
		{"capped", 20, 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outboxBackoff(time.Second, 10*time.Second, tt.attempts); got != tt.want {
				t.Fatalf("outboxBackoff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newTestOutboxDispatcher(t *testing.T, store OutboxStore, publisher TaskPublisher, now time.Time) *OutboxDispatcher {
	t.Helper()
	registry, err := NewOutboxTaskRegistry(MallWeatherOutboxTaskDefinitions(2, 5*time.Minute)...)
	if err != nil {
		t.Fatalf("NewOutboxTaskRegistry() error = %v", err)
	}
	dispatcher, err := NewOutboxDispatcher(store, publisher, registry, OutboxDispatcherConfig{
		WorkerID:     "test-dispatcher",
		PollInterval: time.Second,
		LockTimeout:  time.Minute,
		BatchSize:    10,
		RetryBase:    time.Second,
		RetryMax:     10 * time.Second,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewOutboxDispatcher() error = %v", err)
	}
	return dispatcher
}

func weatherOutboxRow(id uint, attempts int) model.AsyncJobOutbox {
	return model.AsyncJobOutbox{
		BaseModel:   model.BaseModel{ID: id},
		TaskKey:     "weather:full:123:2026071710",
		TaskType:    TypeMallWeatherFull,
		PayloadJSON: model.JSONText(`{"mall_id":123,"task_window":"full:123:2026071710"}`),
		QueueName:   MallWeatherQueueName,
		Attempts:    attempts,
	}
}

type recoveringOutboxStore struct {
	mu          sync.Mutex
	claimErrors []error
	claimCalls  chan int
	calls       int
}

type scriptedOutboxStore struct {
	rows [][]model.AsyncJobOutbox
}

func (store *scriptedOutboxStore) ClaimBatch(context.Context, string, []string, time.Time, time.Duration, int) ([]model.AsyncJobOutbox, error) {
	if len(store.rows) == 0 {
		return nil, nil
	}
	rows := store.rows[0]
	store.rows = store.rows[1:]
	return rows, nil
}

func (*scriptedOutboxStore) MarkPublished(context.Context, uint, time.Time) error {
	return nil
}

func (*scriptedOutboxStore) MarkFailed(context.Context, uint, time.Time, string) error {
	return nil
}

func (store *recoveringOutboxStore) ClaimBatch(_ context.Context, _ string, _ []string, _ time.Time, _ time.Duration, _ int) ([]model.AsyncJobOutbox, error) {
	store.mu.Lock()
	store.calls++
	call := store.calls
	var err error
	if len(store.claimErrors) > 0 {
		err = store.claimErrors[0]
		store.claimErrors = store.claimErrors[1:]
	}
	store.mu.Unlock()
	store.claimCalls <- call
	return nil, err
}

func (*recoveringOutboxStore) MarkPublished(context.Context, uint, time.Time) error {
	return nil
}

func (*recoveringOutboxStore) MarkFailed(context.Context, uint, time.Time, string) error {
	return nil
}
