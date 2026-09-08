package data_svc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/database"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const officePushSchedulePlannerBatchSize = 100

type OfficePushSchedulePlanner struct {
	service *OfficeMessageService
	now     func() time.Time
	limit   int
}

func NewOfficePushSchedulePlanner() (*OfficePushSchedulePlanner, error) {
	if database.DB == nil {
		return nil, fmt.Errorf("office message schedule planner: database is unavailable")
	}
	service := NewOfficeMessageService()
	return &OfficePushSchedulePlanner{service: service, now: func() time.Time { return time.Now().UTC() }, limit: officePushSchedulePlannerBatchSize}, nil
}

func (planner *OfficePushSchedulePlanner) Plan(ctx context.Context) error {
	if planner == nil || planner.service == nil || planner.service.db == nil || planner.now == nil {
		return fmt.Errorf("office message schedule planner: planner is not configured")
	}
	if ctx == nil {
		return fmt.Errorf("office message schedule planner: context is required")
	}
	limit := planner.limit
	if limit <= 0 || limit > officePushSchedulePlannerBatchSize {
		limit = officePushSchedulePlannerBatchSize
	}
	now := planner.now().UTC()
	for planned := 0; planned < limit; planned++ {
		found, err := planner.planOne(ctx, now)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
	return nil
}

func (planner *OfficePushSchedulePlanner) planOne(ctx context.Context, now time.Time) (bool, error) {
	found := false
	err := planner.service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var schedule model.OfficePushSchedule
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("enabled = ? AND next_run_at <= ?", true, now).
			Order("next_run_at ASC, id ASC").
			First(&schedule).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("office message schedule planner: claim due schedule: %w", err)
		}
		found = true
		return planner.planLocked(ctx, tx, &schedule, now)
	})
	return found, err
}

func (planner *OfficePushSchedulePlanner) planLocked(ctx context.Context, tx *gorm.DB, schedule *model.OfficePushSchedule, now time.Time) error {
	scheduledFor := schedule.NextRunAt.UTC()
	nextRunAt, err := nextOfficeScheduleTime(schedule.CronExpr, now)
	if err != nil {
		return planner.disableInvalidSchedule(tx, schedule, scheduledFor, "定时表达式已失效")
	}

	var target model.OfficePushTarget
	if err := tx.Where("id = ? AND enabled = ?", schedule.TargetID, true).First(&target).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return planner.advanceInvalidSchedule(tx, schedule, scheduledFor, nextRunAt, "推送配置已停用或不存在")
	} else if err != nil {
		return fmt.Errorf("office message schedule planner: read push target: %w", err)
	}
	var message model.OfficeMessage
	if err := tx.Where("id = ? AND enabled = ?", target.MessageID, true).First(&message).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return planner.advanceInvalidSchedule(tx, schedule, scheduledFor, nextRunAt, "消息已停用或不存在")
	} else if err != nil {
		return fmt.Errorf("office message schedule planner: read message: %w", err)
	}
	if err := planner.service.preparePersistedOfficePushTarget(&target, message); err != nil {
		return planner.advanceInvalidSchedule(tx, schedule, scheduledFor, nextRunAt, "推送配置凭据未配置或已失效")
	}
	parameterValues, err := renderOfficeScheduleParameters(message, schedule.ParametersJSON, scheduledFor)
	if err != nil {
		return planner.advanceInvalidSchedule(tx, schedule, scheduledFor, nextRunAt, "定时参数配置已失效")
	}
	parameters, err := normalizeOfficeRunParameters(message, parameterValues)
	if err != nil {
		return planner.advanceInvalidSchedule(tx, schedule, scheduledFor, nextRunAt, "定时参数配置已失效")
	}
	snapshot, err := newOfficePushSnapshot(target, message)
	if err != nil {
		return planner.advanceInvalidSchedule(tx, schedule, scheduledFor, nextRunAt, "消息执行快照生成失败")
	}
	runUUID := officeScheduledRunUUID(schedule.ID, scheduledFor)
	var existing model.OfficePushRun
	if err := tx.Where("run_uuid = ?", runUUID).First(&existing).Error; err == nil {
		if existing.ScheduleID == nil || *existing.ScheduleID != schedule.ID || existing.ScheduledFor == nil || !existing.ScheduledFor.Equal(scheduledFor) {
			return fmt.Errorf("office message schedule planner: deterministic run identity conflict")
		}
		return planner.advanceSchedule(tx, schedule, scheduledFor, nextRunAt, "")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("office message schedule planner: read scheduled run: %w", err)
	}
	scheduleID := schedule.ID
	run := model.OfficePushRun{
		RunUUID: runUUID, TargetID: target.ID, MessageID: message.ID,
		Status: model.OfficePushRunStatusQueued, TriggerType: model.OfficePushTriggerSchedule,
		ScheduleID: &scheduleID, ScheduledFor: &scheduledFor, RequestedBy: schedule.UpdatedBy,
		ParametersJSON: parameters, SnapshotJSON: snapshot,
	}
	if err := tx.Create(&run).Error; err != nil {
		return fmt.Errorf("office message schedule planner: create scheduled run: %w", err)
	}
	outbox, err := newOfficePushOutbox(run.ID, run.RunUUID, now)
	if err != nil {
		return err
	}
	if err := data_dao.NewAsyncJobOutboxDAO(tx).Create(ctx, &outbox); err != nil {
		return fmt.Errorf("office message schedule planner: create push outbox: %w", err)
	}
	return planner.advanceSchedule(tx, schedule, scheduledFor, nextRunAt, "")
}

func (planner *OfficePushSchedulePlanner) advanceInvalidSchedule(tx *gorm.DB, schedule *model.OfficePushSchedule, scheduledFor, nextRunAt time.Time, safeError string) error {
	return planner.advanceSchedule(tx, schedule, scheduledFor, nextRunAt, safeError)
}

func (planner *OfficePushSchedulePlanner) disableInvalidSchedule(tx *gorm.DB, schedule *model.OfficePushSchedule, scheduledFor time.Time, safeError string) error {
	result := tx.Model(&model.OfficePushSchedule{}).Where("id = ?", schedule.ID).Updates(map[string]interface{}{
		"enabled": false, "last_scheduled_at": scheduledFor, "last_error_safe": safeError,
		"lock_version": gorm.Expr("lock_version + 1"), "updated_at": planner.now().UTC(),
	})
	if result.Error != nil {
		return fmt.Errorf("office message schedule planner: disable invalid schedule: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("office message schedule planner: invalid schedule changed during planning")
	}
	return nil
}

func (planner *OfficePushSchedulePlanner) advanceSchedule(tx *gorm.DB, schedule *model.OfficePushSchedule, scheduledFor, nextRunAt time.Time, safeError string) error {
	result := tx.Model(&model.OfficePushSchedule{}).Where("id = ?", schedule.ID).Updates(map[string]interface{}{
		"next_run_at": nextRunAt, "last_scheduled_at": scheduledFor, "last_error_safe": safeError,
		"lock_version": gorm.Expr("lock_version + 1"), "updated_at": planner.now().UTC(),
	})
	if result.Error != nil {
		return fmt.Errorf("office message schedule planner: advance schedule: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("office message schedule planner: schedule changed during planning")
	}
	return nil
}

func officeScheduledRunUUID(scheduleID uint, scheduledFor time.Time) string {
	identity := "office-push-schedule/" + strconv.FormatUint(uint64(scheduleID), 10) + "/" + scheduledFor.UTC().Format(time.RFC3339Nano)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(identity)).String()
}
