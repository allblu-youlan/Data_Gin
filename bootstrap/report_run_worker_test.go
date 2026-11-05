package bootstrap

import (
	"context"
	"errors"
	"testing"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/job"

	"github.com/hibiken/asynq"
)

func TestReportRunHandlerDecodesStrictPayload(t *testing.T) {
	processor := &fakeReportRunProcessor{}
	task := asynq.NewTask(job.TypeReportRun, []byte(`{"run_id":31}`))
	if err := newReportRunHandler(processor, nil)(t.Context(), task); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if processor.runID != 31 {
		t.Fatalf("run id = %d", processor.runID)
	}
	if processor.failureRetryLimit != job.ReportRunFailureMaxRetry {
		t.Fatalf("failure retry limit = %d", processor.failureRetryLimit)
	}
	if !processor.queueRetryAllowed {
		t.Fatal("queue retry should remain available without Asynq retry metadata")
	}
	bad := asynq.NewTask(job.TypeReportRun, []byte(`{"run_id":31,"secret":"x"}`))
	if err := newReportRunHandler(processor, nil)(t.Context(), bad); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("invalid payload error = %v", err)
	}
}

func TestReportRunHandlerSkipsPermanentFailures(t *testing.T) {
	processor := &fakeReportRunProcessor{err: data_svc.ErrReportRunProcessNonRetryable}
	err := newReportRunHandler(processor, nil)(t.Context(), asynq.NewTask(job.TypeReportRun, []byte(`{"run_id":31}`)))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("handler error = %v", err)
	}
}

func TestReportRunHandlerEnqueuesTargetedSnapshotCleanup(t *testing.T) {
	processor := &fakeReportRunProcessor{err: targetedCleanupError{runID: 24}}
	enqueuer := &fakeReportCleanupTaskEnqueuer{}
	err := newReportRunHandler(processor, enqueuer)(t.Context(), asynq.NewTask(job.TypeReportRun, []byte(`{"run_id":31}`)))
	if err == nil || enqueuer.task == nil {
		t.Fatalf("handler error=%v task=%v", err, enqueuer.task)
	}
	payload, decodeErr := job.DecodeReportResultCleanupTaskPayload(enqueuer.task.Payload())
	if decodeErr != nil || payload.RunID != 24 || enqueuer.task.Type() != job.TypeReportResultCleanup {
		t.Fatalf("payload=%+v type=%q error=%v", payload, enqueuer.task.Type(), decodeErr)
	}
}

func TestReportRunHandlerEnqueuesReadyExportCleanup(t *testing.T) {
	processor := &fakeReportRunProcessor{err: targetedCleanupError{exportID: 41}}
	enqueuer := &fakeReportCleanupTaskEnqueuer{}
	err := newReportRunHandler(processor, enqueuer)(t.Context(), asynq.NewTask(job.TypeReportRun, []byte(`{"run_id":31}`)))
	if err == nil || enqueuer.task == nil {
		t.Fatalf("handler error=%v task=%v", err, enqueuer.task)
	}
	payload, decodeErr := job.DecodeReportResultCleanupTaskPayload(enqueuer.task.Payload())
	if decodeErr != nil || payload.ExportID != 41 || payload.RunID != 0 {
		t.Fatalf("payload=%+v error=%v", payload, decodeErr)
	}
}

func TestReportWorkersUseDedicatedQueues(t *testing.T) {
	configured := map[string]int{
		"default":                  3,
		job.ReportQueueName:        99,
		job.ReportExportQueueName:  88,
		job.ReportCleanupQueueName: 77,
	}
	specs := queueJobServerSpecs(configured, 10, true, 2, 4, 5, 1, false, 20)
	if len(specs) != 4 {
		t.Fatalf("server specs = %#v", specs)
	}
	if specs[0].name != "default" || specs[0].concurrency != 10 || len(specs[0].queues) != 1 || specs[0].queues["default"] != 3 {
		t.Fatalf("default server = %#v", specs[0])
	}
	if specs[1].name != "report run" || specs[1].concurrency != 4 || len(specs[1].queues) != 1 || specs[1].queues[job.ReportQueueName] != 2 {
		t.Fatalf("report run server = %#v", specs[1])
	}
	if specs[2].name != "report export" || specs[2].concurrency != 5 || len(specs[2].queues) != 1 || specs[2].queues[job.ReportExportQueueName] != 2 {
		t.Fatalf("report export server = %#v", specs[2])
	}
	if specs[3].name != "report cleanup" || specs[3].concurrency != 1 || len(specs[3].queues) != 1 || specs[3].queues[job.ReportCleanupQueueName] != 2 {
		t.Fatalf("report cleanup server = %#v", specs[3])
	}
	seen := make(map[string]string)
	for _, spec := range specs {
		for queue := range spec.queues {
			if owner, exists := seen[queue]; exists {
				t.Fatalf("queue %q is consumed by %q and %q", queue, owner, spec.name)
			}
			seen[queue] = spec.name
		}
	}
}

func TestQueueJobServerSpecsDoNotInventDefaultQueue(t *testing.T) {
	specs := queueJobServerSpecs(map[string]int{job.ReportQueueName: 2}, 10, true, 2, 4, 2, 1, false, 20)
	if len(specs) != 3 || specs[0].name != "report run" || specs[1].name != "report export" || specs[2].name != "report cleanup" {
		t.Fatalf("server specs = %#v", specs)
	}
}

type fakeReportRunProcessor struct {
	runID             uint
	failureRetryLimit int
	queueRetryAllowed bool
	err               error
}

type targetedCleanupError struct {
	runID    uint
	exportID uint
}

func (err targetedCleanupError) Error() string         { return "waiting for cleanup" }
func (err targetedCleanupError) CleanupRunID() uint    { return err.runID }
func (err targetedCleanupError) CleanupExportID() uint { return err.exportID }

type fakeReportCleanupTaskEnqueuer struct{ task *asynq.Task }

func (enqueuer *fakeReportCleanupTaskEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	enqueuer.task = task
	return nil, nil
}

func (processor *fakeReportRunProcessor) Process(_ context.Context, runID uint, failureRetryLimit int, queueRetryAllowed bool) error {
	processor.runID = runID
	processor.failureRetryLimit = failureRetryLimit
	processor.queueRetryAllowed = queueRetryAllowed
	return processor.err
}
