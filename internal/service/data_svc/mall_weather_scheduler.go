package data_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"
)

const (
	mallWeatherSchedulePageSize = 200
	defaultScheduleDBTimeout    = 8 * time.Second
)

type mallWeatherScheduleStore interface {
	ListEnabledMalls(ctx context.Context, afterID uint, limit int) ([]model.Mall, error)
	ListRepairCandidates(ctx context.Context, afterID uint, limit int) ([]model.MallWeatherFetchRun, error)
	ReconcileLatestFreshness(ctx context.Context, now time.Time) (int64, error)
	CreateOutboxes(ctx context.Context, rows []model.AsyncJobOutbox) (int64, error)
}

type gormMallWeatherScheduleStore struct{}

func (gormMallWeatherScheduleStore) ListEnabledMalls(ctx context.Context, afterID uint, limit int) ([]model.Mall, error) {
	return data_dao.NewMallDAO(database.DB).ListEnabledWeatherAfterID(ctx, afterID, limit)
}

func (gormMallWeatherScheduleStore) ListRepairCandidates(ctx context.Context, afterID uint, limit int) ([]model.MallWeatherFetchRun, error) {
	return data_dao.NewMallWeatherDAO(database.DB).ListRepairCandidatesAfterID(ctx, afterID, limit)
}

func (gormMallWeatherScheduleStore) ReconcileLatestFreshness(ctx context.Context, now time.Time) (int64, error) {
	return data_dao.NewMallWeatherDAO(database.DB).ReconcileLatestFreshness(ctx, now)
}

func (gormMallWeatherScheduleStore) CreateOutboxes(ctx context.Context, rows []model.AsyncJobOutbox) (int64, error) {
	return data_dao.NewAsyncJobOutboxDAO(database.DB).CreateBatchIgnoreTaskConflicts(ctx, rows)
}

type MallWeatherSchedulePlanner struct {
	store    mallWeatherScheduleStore
	now      func() time.Time
	location *time.Location

	repairMaxRounds int
	repairSpread    time.Duration
	databaseTimeout time.Duration
	repairEnabled   bool
}

func NewMallWeatherSchedulePlanner() (*MallWeatherSchedulePlanner, error) {
	if database.DB == nil {
		return nil, fmt.Errorf("mall weather scheduler: database is unavailable")
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	planner, err := newMallWeatherSchedulePlanner(gormMallWeatherScheduleStore{}, time.Now, location)
	if err != nil {
		return nil, err
	}
	planner.repairMaxRounds = config.GetInt("cfg.mall_weather.repair_max_rounds")
	planner.repairSpread = time.Duration(config.GetInt("cfg.mall_weather.repair_spread_seconds")) * time.Second
	planner.databaseTimeout = time.Duration(config.GetInt("cfg.mall_weather.schedule_db_timeout_seconds")) * time.Second
	planner.repairEnabled = config.GetBool("cfg.mall_weather.repair_enabled")
	if planner.repairMaxRounds < 1 || planner.repairMaxRounds > 10 || planner.repairSpread < 0 || planner.repairSpread > time.Hour ||
		planner.databaseTimeout < time.Second || planner.databaseTimeout > time.Minute {
		return nil, fmt.Errorf("mall weather scheduler: invalid repair configuration")
	}
	return planner, nil
}

func newMallWeatherSchedulePlanner(store mallWeatherScheduleStore, now func() time.Time, location *time.Location) (*MallWeatherSchedulePlanner, error) {
	if store == nil || now == nil || location == nil {
		return nil, fmt.Errorf("mall weather scheduler: invalid configuration")
	}
	return &MallWeatherSchedulePlanner{
		store: store, now: now, location: location,
		repairMaxRounds: 3, repairSpread: 15 * time.Minute, databaseTimeout: defaultScheduleDBTimeout, repairEnabled: true,
	}, nil
}

func (planner *MallWeatherSchedulePlanner) Plan(ctx context.Context, payload job.MallWeatherSchedulePayload) error {
	if planner == nil || ctx == nil {
		return fmt.Errorf("mall weather scheduler: invalid planner")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mall weather scheduler: encode schedule payload: %w", err)
	}
	payload, err = job.DecodeMallWeatherSchedulePayload(encoded)
	if err != nil {
		return err
	}
	scheduledAt := planner.now().UTC()
	if payload.TaskType == job.TypeMallWeatherRepair {
		if !planner.repairEnabled {
			return nil
		}
		repairCtx, cancel := planner.databaseContext(ctx)
		defer cancel()
		return planner.planRepairs(repairCtx, scheduledAt)
	}
	var afterID uint
	for {
		queryCtx, cancel := planner.databaseContext(ctx)
		malls, err := planner.store.ListEnabledMalls(queryCtx, afterID, mallWeatherSchedulePageSize)
		cancel()
		if err != nil {
			return fmt.Errorf("mall weather scheduler: list enabled malls: %w", err)
		}
		rows := make([]model.AsyncJobOutbox, 0, len(malls))
		for index := range malls {
			mall := &malls[index]
			if mall.DetailProfile != payload.DetailProfile || mall.WeatherProvider != "caiyun" {
				continue
			}
			row, err := planner.outboxForMall(payload.TaskType, mall.ID, scheduledAt)
			if err != nil {
				return err
			}
			rows = append(rows, row)
		}
		writeCtx, cancel := planner.databaseContext(ctx)
		_, err = planner.store.CreateOutboxes(writeCtx, rows)
		cancel()
		if err != nil {
			return fmt.Errorf("mall weather scheduler: store outboxes: %w", err)
		}
		if len(malls) < mallWeatherSchedulePageSize {
			return nil
		}
		afterID = malls[len(malls)-1].ID
		if afterID == 0 {
			return fmt.Errorf("mall weather scheduler: invalid mall page cursor")
		}
	}
}

func (planner *MallWeatherSchedulePlanner) planRepairs(ctx context.Context, scheduledAt time.Time) error {
	if planner.repairMaxRounds < 1 || planner.repairSpread < 0 || planner.databaseTimeout <= 0 || scheduledAt.IsZero() {
		return fmt.Errorf("mall weather scheduler: invalid repair plan")
	}
	_, err := planner.store.ReconcileLatestFreshness(ctx, scheduledAt)
	if err != nil {
		return fmt.Errorf("mall weather scheduler: reconcile freshness: %w", err)
	}
	var afterID uint
	for {
		candidates, err := planner.store.ListRepairCandidates(ctx, afterID, mallWeatherSchedulePageSize)
		if err != nil {
			return fmt.Errorf("mall weather scheduler: list repair candidates: %w", err)
		}
		rows := make([]model.AsyncJobOutbox, 0, len(candidates))
		for index := range candidates {
			row, schedule, err := planner.repairOutbox(&candidates[index], scheduledAt)
			if err != nil {
				return err
			}
			if schedule {
				rows = append(rows, row)
			}
		}
		_, err = planner.store.CreateOutboxes(ctx, rows)
		if err != nil {
			return fmt.Errorf("mall weather scheduler: store repair outboxes: %w", err)
		}
		if len(candidates) < mallWeatherSchedulePageSize {
			return nil
		}
		afterID = candidates[len(candidates)-1].ID
		if afterID == 0 {
			return fmt.Errorf("mall weather scheduler: invalid repair cursor")
		}
	}
}

func (planner *MallWeatherSchedulePlanner) databaseContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, planner.databaseTimeout)
}

func (planner *MallWeatherSchedulePlanner) repairOutbox(run *model.MallWeatherFetchRun, scheduledAt time.Time) (model.AsyncJobOutbox, bool, error) {
	if run == nil || run.ID == 0 || run.MallID == 0 || scheduledAt.IsZero() ||
		(run.EndpointKind != caiyun.EndpointWeatherV26 && run.EndpointKind != caiyun.EndpointLifeIndexV3) {
		return model.AsyncJobOutbox{}, false, fmt.Errorf("mall weather scheduler: invalid repair candidate")
	}
	sourceRunID, repairRound, schedule, err := nextWeatherRepairIdentity(run, planner.repairMaxRounds)
	if err != nil || !schedule {
		return model.AsyncJobOutbox{}, schedule, err
	}
	window := fmt.Sprintf("repair:%d:%d", sourceRunID, repairRound)
	payloadJSON, err := json.Marshal(job.MallTaskPayload{
		MallID: run.MallID, TaskWindow: window, EndpointKind: caiyun.EndpointWeatherV26,
	})
	if err != nil {
		return model.AsyncJobOutbox{}, false, fmt.Errorf("mall weather scheduler: encode repair payload: %w", err)
	}
	availableAt := scheduledAt.UTC().Add(deterministicRepairDelay(run.ID, planner.repairSpread))
	return model.AsyncJobOutbox{
		TaskKey: "mall:weather:" + window, TaskType: job.TypeMallWeatherRepair,
		PayloadJSON: model.JSONText(payloadJSON), QueueName: job.MallWeatherQueueName, AvailableAt: availableAt,
	}, true, nil
}

func nextWeatherRepairIdentity(run *model.MallWeatherFetchRun, maxRounds int) (uint, int, bool, error) {
	if run == nil || run.ID == 0 || maxRounds < 1 {
		return 0, 0, false, fmt.Errorf("mall weather scheduler: invalid repair identity")
	}
	if run.TaskKind != "repair" {
		return run.ID, 1, true, nil
	}
	parts := strings.Split(run.TaskWindow, ":")
	if len(parts) != 3 || parts[0] != "repair" {
		return 0, 0, false, fmt.Errorf("mall weather scheduler: invalid prior repair window")
	}
	source, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || source == 0 || uint64(uint(source)) != source {
		return 0, 0, false, fmt.Errorf("mall weather scheduler: invalid repair source run")
	}
	round, err := strconv.Atoi(parts[2])
	if err != nil || round < 1 || round > maxRounds {
		return 0, 0, false, fmt.Errorf("mall weather scheduler: invalid repair round")
	}
	if round == maxRounds {
		return uint(source), round, false, nil
	}
	return uint(source), round + 1, true, nil
}

func deterministicRepairDelay(candidateRunID uint, spread time.Duration) time.Duration {
	seconds := uint64(spread / time.Second)
	if candidateRunID == 0 || seconds == 0 {
		return 0
	}
	return time.Duration(uint64(candidateRunID)%seconds) * time.Second
}

func (planner *MallWeatherSchedulePlanner) outboxForMall(taskType string, mallID uint, scheduledAt time.Time) (model.AsyncJobOutbox, error) {
	if mallID == 0 || scheduledAt.IsZero() {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather scheduler: invalid task identity")
	}
	local := scheduledAt.In(planner.location)
	var window string
	switch taskType {
	case job.TypeMallWeatherFast:
		window = fmt.Sprintf("fast:%d:%s", mallID, local.Format("200601021504"))
	case job.TypeMallWeatherFull:
		window = fmt.Sprintf("full:%d:%s", mallID, local.Format("2006010215"))
	case job.TypeMallWeatherLifeIndex:
		window = fmt.Sprintf("life:%d:%s", mallID, local.Format("2006010215"))
	default:
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather scheduler: unsupported task type")
	}
	payloadJSON, err := json.Marshal(job.MallTaskPayload{MallID: mallID, TaskWindow: window})
	if err != nil {
		return model.AsyncJobOutbox{}, fmt.Errorf("mall weather scheduler: encode task payload: %w", err)
	}
	return model.AsyncJobOutbox{
		TaskKey: "mall:weather:" + window, TaskType: taskType, PayloadJSON: model.JSONText(payloadJSON),
		QueueName: job.MallWeatherQueueName, AvailableAt: scheduledAt.UTC(),
	}, nil
}
