package data_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
)

func TestMallWeatherSchedulePlannerCreatesProfileOutboxes(t *testing.T) {
	store := &fakeMallWeatherScheduleStore{malls: []model.Mall{
		{BaseModel: model.BaseModel{ID: 7}, DetailProfile: "full", WeatherProvider: "caiyun"},
		{BaseModel: model.BaseModel{ID: 8}, DetailProfile: "standard", WeatherProvider: "caiyun"},
	}}
	scheduledAt := time.Date(2026, 7, 22, 3, 7, 30, 0, time.UTC)
	planner, err := newMallWeatherSchedulePlanner(store, func() time.Time { return scheduledAt }, time.FixedZone("CST", 8*60*60))
	if err != nil {
		t.Fatalf("newMallWeatherSchedulePlanner() error=%v", err)
	}
	if err := planner.Plan(context.Background(), job.MallWeatherSchedulePayload{
		TaskType: job.TypeMallWeatherFull, DetailProfile: "full",
	}); err != nil {
		t.Fatalf("Plan() error=%v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("outboxes=%+v", store.rows)
	}
	row := store.rows[0]
	if row.TaskKey != "mall:weather:full:7:2026072211" || row.TaskType != job.TypeMallWeatherFull ||
		string(row.PayloadJSON) != `{"mall_id":7,"task_window":"full:7:2026072211"}` || !row.AvailableAt.Equal(scheduledAt) {
		t.Fatalf("outbox=%+v", row)
	}
}

func TestMallWeatherSchedulePlannerUsesStableBusinessWindows(t *testing.T) {
	planner, err := newMallWeatherSchedulePlanner(&fakeMallWeatherScheduleStore{}, time.Now, time.UTC)
	if err != nil {
		t.Fatalf("newMallWeatherSchedulePlanner() error=%v", err)
	}
	scheduledAt := time.Date(2026, 7, 22, 3, 9, 59, 0, time.UTC)
	fast, err := planner.outboxForMall(job.TypeMallWeatherFast, 7, scheduledAt)
	if err != nil {
		t.Fatalf("outboxForMall(fast) error=%v", err)
	}
	if fast.TaskKey != "mall:weather:fast:7:202607220309" {
		t.Fatalf("fast task key=%s", fast.TaskKey)
	}
	life, err := planner.outboxForMall(job.TypeMallWeatherLifeIndex, 7, scheduledAt)
	if err != nil {
		t.Fatalf("outboxForMall(life) error=%v", err)
	}
	if life.TaskKey != "mall:weather:life:7:2026072203" {
		t.Fatalf("life task key=%s", life.TaskKey)
	}
}

func TestMallWeatherSchedulePlannerCreatesBoundedRepairOutboxes(t *testing.T) {
	store := &fakeMallWeatherScheduleStore{repairCandidates: []model.MallWeatherFetchRun{
		{
			BaseModel: model.BaseModel{ID: 31}, MallID: 7, TaskKind: "full", TaskWindow: "full:7:2026072211",
			EndpointKind: "v26_weather", Status: "failed",
		},
		{
			BaseModel: model.BaseModel{ID: 42}, MallID: 8, TaskKind: "repair", TaskWindow: "repair:41:1",
			EndpointKind: "v3_life_index", Status: "partial_success",
		},
		{
			BaseModel: model.BaseModel{ID: 53}, MallID: 9, TaskKind: "repair", TaskWindow: "repair:52:3",
			EndpointKind: "v26_weather", Status: "failed",
		},
	}}
	scheduledAt := time.Date(2026, 7, 22, 3, 15, 0, 0, time.UTC)
	planner, err := newMallWeatherSchedulePlanner(store, func() time.Time { return scheduledAt }, time.UTC)
	if err != nil {
		t.Fatalf("newMallWeatherSchedulePlanner() error=%v", err)
	}
	planner.repairSpread = 0
	if err := planner.Plan(context.Background(), job.MallWeatherSchedulePayload{TaskType: job.TypeMallWeatherRepair}); err != nil {
		t.Fatalf("Plan(repair) error=%v", err)
	}
	if !store.reconciledAt.Equal(scheduledAt) || len(store.rows) != 2 {
		t.Fatalf("reconciled=%s rows=%+v", store.reconciledAt, store.rows)
	}
	if store.rows[0].TaskKey != "mall:weather:repair:31:1" ||
		string(store.rows[0].PayloadJSON) != `{"mall_id":7,"task_window":"repair:31:1","endpoint_kind":"v26_weather"}` ||
		!store.rows[0].AvailableAt.Equal(scheduledAt) {
		t.Fatalf("first repair=%+v", store.rows[0])
	}
	if store.rows[1].TaskKey != "mall:weather:repair:41:2" ||
		string(store.rows[1].PayloadJSON) != `{"mall_id":8,"task_window":"repair:41:2","endpoint_kind":"v26_weather"}` {
		t.Fatalf("second repair=%+v", store.rows[1])
	}
}

func TestMallWeatherSchedulePlannerSkipsDisabledRepairWithoutDatabaseWork(t *testing.T) {
	store := &fakeMallWeatherScheduleStore{
		repairListErr: errors.New("repair query must not run"),
		reconcileErr:  errors.New("freshness reconciliation must not run"),
	}
	planner, err := newMallWeatherSchedulePlanner(store, time.Now, time.UTC)
	if err != nil {
		t.Fatalf("newMallWeatherSchedulePlanner() error=%v", err)
	}
	planner.repairEnabled = false
	if err := planner.Plan(t.Context(), job.MallWeatherSchedulePayload{TaskType: job.TypeMallWeatherRepair}); err != nil {
		t.Fatalf("Plan(disabled repair) error=%v", err)
	}
	if !store.reconciledAt.IsZero() || len(store.rows) != 0 {
		t.Fatalf("disabled repair touched store: reconciled=%s rows=%+v", store.reconciledAt, store.rows)
	}
}

func TestMallWeatherSchedulePlannerTimesOutRepairCandidateQuery(t *testing.T) {
	store := &fakeMallWeatherScheduleStore{
		repairList: func(ctx context.Context, _ uint, _ int) ([]model.MallWeatherFetchRun, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("repair candidate query context has no deadline")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	planner, err := newMallWeatherSchedulePlanner(store, time.Now, time.UTC)
	if err != nil {
		t.Fatalf("newMallWeatherSchedulePlanner() error=%v", err)
	}
	planner.databaseTimeout = 10 * time.Millisecond

	err = planner.Plan(t.Context(), job.MallWeatherSchedulePayload{TaskType: job.TypeMallWeatherRepair})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Plan(repair) error=%v, want context deadline exceeded", err)
	}
}

func TestMallWeatherSchedulePlannerSharesOneRepairDeadline(t *testing.T) {
	store := &fakeMallWeatherScheduleStore{}
	planner, err := newMallWeatherSchedulePlanner(store, time.Now, time.UTC)
	if err != nil {
		t.Fatalf("newMallWeatherSchedulePlanner() error=%v", err)
	}
	planner.databaseTimeout = 2 * time.Second
	if err := planner.Plan(t.Context(), job.MallWeatherSchedulePayload{TaskType: job.TypeMallWeatherRepair}); err != nil {
		t.Fatalf("Plan(repair) error=%v", err)
	}
	if len(store.deadlines) != 3 {
		t.Fatalf("repair database calls=%d want=3", len(store.deadlines))
	}
	for _, deadline := range store.deadlines[1:] {
		if !deadline.Equal(store.deadlines[0]) {
			t.Fatalf("repair deadline renewed: first=%s next=%s", store.deadlines[0], deadline)
		}
	}
}

func TestNextWeatherRepairIdentityValidatesHistory(t *testing.T) {
	tests := []struct {
		name         string
		run          model.MallWeatherFetchRun
		wantSource   uint
		wantRound    int
		wantSchedule bool
		wantFailure  bool
	}{
		{name: "first repair", run: model.MallWeatherFetchRun{BaseModel: model.BaseModel{ID: 11}, TaskKind: "full"}, wantSource: 11, wantRound: 1, wantSchedule: true},
		{name: "next repair", run: model.MallWeatherFetchRun{BaseModel: model.BaseModel{ID: 12}, TaskKind: "repair", TaskWindow: "repair:11:1"}, wantSource: 11, wantRound: 2, wantSchedule: true},
		{name: "round budget exhausted", run: model.MallWeatherFetchRun{BaseModel: model.BaseModel{ID: 13}, TaskKind: "repair", TaskWindow: "repair:11:3"}, wantSource: 11, wantRound: 3},
		{name: "malformed history", run: model.MallWeatherFetchRun{BaseModel: model.BaseModel{ID: 14}, TaskKind: "repair", TaskWindow: "repair:bad:1"}, wantFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, round, schedule, err := nextWeatherRepairIdentity(&test.run, 3)
			if test.wantFailure {
				if err == nil {
					t.Fatal("nextWeatherRepairIdentity() accepted malformed history")
				}
				return
			}
			if err != nil || source != test.wantSource || round != test.wantRound || schedule != test.wantSchedule {
				t.Fatalf("identity=(%d,%d,%t,%v)", source, round, schedule, err)
			}
		})
	}
}

func TestDeterministicRepairDelaySpreadsByRunID(t *testing.T) {
	if got := deterministicRepairDelay(901, 15*time.Minute); got != time.Second {
		t.Fatalf("deterministicRepairDelay()=%s", got)
	}
	if got := deterministicRepairDelay(901, 0); got != 0 {
		t.Fatalf("zero spread delay=%s", got)
	}
}

type fakeMallWeatherScheduleStore struct {
	malls            []model.Mall
	repairCandidates []model.MallWeatherFetchRun
	repairList       func(context.Context, uint, int) ([]model.MallWeatherFetchRun, error)
	rows             []model.AsyncJobOutbox
	reconciledAt     time.Time
	deadlines        []time.Time

	listErr       error
	repairListErr error
	reconcileErr  error
	createErr     error
}

func (store *fakeMallWeatherScheduleStore) ListRepairCandidates(ctx context.Context, afterID uint, limit int) ([]model.MallWeatherFetchRun, error) {
	store.recordDeadline(ctx)
	if store.repairList != nil {
		return store.repairList(ctx, afterID, limit)
	}
	if store.repairListErr != nil {
		return nil, store.repairListErr
	}
	rows := make([]model.MallWeatherFetchRun, 0, limit)
	for _, run := range store.repairCandidates {
		if run.ID > afterID && len(rows) < limit {
			rows = append(rows, run)
		}
	}
	return rows, nil
}

func (store *fakeMallWeatherScheduleStore) ReconcileLatestFreshness(ctx context.Context, now time.Time) (int64, error) {
	store.recordDeadline(ctx)
	if store.reconcileErr != nil {
		return 0, store.reconcileErr
	}
	store.reconciledAt = now
	return 0, nil
}

func (store *fakeMallWeatherScheduleStore) ListEnabledMalls(_ context.Context, afterID uint, limit int) ([]model.Mall, error) {
	if store.listErr != nil {
		return nil, store.listErr
	}
	rows := make([]model.Mall, 0, limit)
	for _, mall := range store.malls {
		if mall.ID > afterID && len(rows) < limit {
			rows = append(rows, mall)
		}
	}
	return rows, nil
}

func (store *fakeMallWeatherScheduleStore) CreateOutboxes(ctx context.Context, rows []model.AsyncJobOutbox) (int64, error) {
	store.recordDeadline(ctx)
	if store.createErr != nil {
		return 0, store.createErr
	}
	store.rows = append(store.rows, rows...)
	return int64(len(rows)), nil
}

func (store *fakeMallWeatherScheduleStore) recordDeadline(ctx context.Context) {
	if deadline, ok := ctx.Deadline(); ok {
		store.deadlines = append(store.deadlines, deadline)
	}
}
