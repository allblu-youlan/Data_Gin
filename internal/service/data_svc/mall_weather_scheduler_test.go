package data_svc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"

	"gorm.io/gorm"
)

func TestNewMallWeatherSchedulePlannerHonorsConfiguredZeroSpread(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", directory+string(os.PathSeparator))
	config.Instance().Set("cfg.mall_weather.schedule_spread_seconds", 0)
	previousDB := database.DB
	database.DB = &gorm.DB{}
	t.Cleanup(func() { database.DB = previousDB })
	planner, err := NewMallWeatherSchedulePlanner()
	if err != nil {
		t.Fatal(err)
	}
	scheduledAt := time.Date(2026, 7, 22, 3, 0, 0, 0, time.UTC)
	row, err := planner.outboxForMall(job.TypeMallWeatherFull, 7, scheduledAt)
	if err != nil {
		t.Fatal(err)
	}
	if !row.AvailableAt.Equal(scheduledAt) {
		t.Fatalf("zero configured spread still delays task by %s", row.AvailableAt.Sub(scheduledAt))
	}
}

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
	planner.scheduleSpread = 0
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

func TestMallWeatherSchedulePlannerSpreadsConsecutiveMallsAcrossWindow(t *testing.T) {
	for _, taskType := range []string{job.TypeMallWeatherFast, job.TypeMallWeatherFull} {
		t.Run(taskType, func(t *testing.T) {
			store := &fakeMallWeatherScheduleStore{}
			for mallID := uint(1); mallID <= 60; mallID++ {
				store.malls = append(store.malls, model.Mall{
					BaseModel: model.BaseModel{ID: mallID}, DetailProfile: "full", WeatherProvider: "caiyun",
				})
			}
			scheduledAt := time.Date(2026, 7, 22, 3, 59, 59, 0, time.UTC)
			planner, err := newMallWeatherSchedulePlanner(store, func() time.Time { return scheduledAt }, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			if err := planner.Plan(t.Context(), job.MallWeatherSchedulePayload{TaskType: taskType, DetailProfile: "full"}); err != nil {
				t.Fatal(err)
			}
			if len(store.rows) != 60 {
				t.Fatalf("scheduled malls=%d want=60", len(store.rows))
			}
			minutes := make(map[time.Duration]bool)
			for _, row := range store.rows {
				delay := row.AvailableAt.Sub(scheduledAt)
				if delay < 0 || delay >= 5*time.Minute {
					t.Fatalf("task=%s delay=%s outside spread window", row.TaskKey, delay)
				}
				minutes[delay/time.Minute] = true
			}
			if len(minutes) < 4 {
				t.Fatalf("consecutive malls cluster into %d minute buckets, want at least 4", len(minutes))
			}
		})
	}
}

func TestMallWeatherScheduleDelayPreservesTaskIdentityAndRetryOffset(t *testing.T) {
	planner, err := newMallWeatherSchedulePlanner(&fakeMallWeatherScheduleStore{}, time.Now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	scheduledAt := time.Date(2026, 7, 22, 3, 59, 55, 0, time.UTC)
	for _, test := range []struct {
		name     string
		taskType string
		wantKey  string
	}{
		{name: "fast", taskType: job.TypeMallWeatherFast, wantKey: "mall:weather:fast:7:202607220359"},
		{name: "full", taskType: job.TypeMallWeatherFull, wantKey: "mall:weather:full:7:2026072203"},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, err := planner.outboxForMall(test.taskType, 7, scheduledAt)
			if err != nil {
				t.Fatal(err)
			}
			retriedAt := scheduledAt.Add(time.Second)
			retry, err := planner.outboxForMall(test.taskType, 7, retriedAt)
			if err != nil {
				t.Fatal(err)
			}
			delay := first.AvailableAt.Sub(scheduledAt)
			if delay <= 5*time.Second || !first.AvailableAt.After(time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC)) {
				t.Fatalf("task should be deferred across the hour boundary: available=%s", first.AvailableAt)
			}
			if retry.AvailableAt.Sub(retriedAt) != delay {
				t.Fatalf("retry changed mall delay: first=%s retry=%s", delay, retry.AvailableAt.Sub(retriedAt))
			}
			if first.TaskKey != test.wantKey || retry.TaskKey != test.wantKey || retry.PayloadJSON != first.PayloadJSON {
				t.Fatalf("spread changed logical task identity: first=%+v retry=%+v", first, retry)
			}
		})
	}
}

func TestMallWeatherScheduleSpreadKeepsSamplingIntervalsAcrossDays(t *testing.T) {
	planner, err := newMallWeatherSchedulePlanner(&fakeMallWeatherScheduleStore{}, time.Now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	scheduledAt := time.Date(2026, 7, 22, 23, 59, 55, 0, time.UTC)
	fast, err := planner.outboxForMall(job.TypeMallWeatherFast, 7, scheduledAt)
	if err != nil {
		t.Fatal(err)
	}
	next, err := planner.outboxForMall(job.TypeMallWeatherFast, 7, scheduledAt.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	full, err := planner.outboxForMall(job.TypeMallWeatherFull, 7, scheduledAt)
	if err != nil {
		t.Fatal(err)
	}
	if fast.AvailableAt.Day() != 23 || fast.TaskKey != "mall:weather:fast:7:202607222359" ||
		next.TaskKey != "mall:weather:fast:7:202607230009" {
		t.Fatalf("cross-day task identities changed: first=%+v next=%+v", fast, next)
	}
	if next.AvailableAt.Sub(fast.AvailableAt) != 10*time.Minute {
		t.Fatalf("spread changed sampling interval: first=%s next=%s", fast.AvailableAt, next.AvailableAt)
	}
	if !full.AvailableAt.Equal(fast.AvailableAt) {
		t.Fatalf("same mall has different fast/full offset: fast=%s full=%s", fast.AvailableAt, full.AvailableAt)
	}
}

func TestMallWeatherScheduleSpreadCanBeConfiguredOrDisabled(t *testing.T) {
	for _, test := range []struct {
		name   string
		spread time.Duration
	}{
		{name: "disabled", spread: 0},
		{name: "short window", spread: 90 * time.Second},
		{name: "long window", spread: 15 * time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			planner, err := newMallWeatherSchedulePlanner(&fakeMallWeatherScheduleStore{}, time.Now, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			planner.scheduleSpread = test.spread
			scheduledAt := time.Date(2026, 7, 22, 3, 0, 0, 0, time.UTC)
			row, err := planner.outboxForMall(job.TypeMallWeatherFull, 7, scheduledAt)
			if err != nil {
				t.Fatal(err)
			}
			delay := row.AvailableAt.Sub(scheduledAt)
			if test.spread == 0 {
				if delay != 0 {
					t.Fatalf("disabled spread delay=%s", delay)
				}
			} else if delay <= 0 || delay >= test.spread {
				t.Fatalf("delay=%s outside configured window %s", delay, test.spread)
			}
		})
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
