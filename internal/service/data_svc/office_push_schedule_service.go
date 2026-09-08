package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/model"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const officeScheduleMaximumOffsetDays = 3660

var officeScheduleCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type OfficeScheduleParameterValue struct {
	Mode       string `json:"mode"`
	Value      string `json:"value,omitempty"`
	OffsetDays int    `json:"offsetDays,omitempty"`
}

type OfficePushScheduleInput struct {
	Name                string                                  `json:"name"`
	TargetID            uint                                    `json:"targetId"`
	CronExpr            string                                  `json:"cronExpr"`
	TimeZone            string                                  `json:"timeZone"`
	Parameters          map[string]OfficeScheduleParameterValue `json:"parameters"`
	Enabled             *bool                                   `json:"enabled"`
	ExpectedLockVersion uint64                                  `json:"expectedLockVersion"`
}

type normalizedOfficePushSchedule struct {
	name       string
	targetID   uint
	cronExpr   string
	timeZone   string
	parameters model.JSONText
	enabled    bool
	nextRunAt  time.Time
}

func (service *OfficeMessageService) ListSchedules(ctx context.Context) ([]model.OfficePushSchedule, error) {
	var schedules []model.OfficePushSchedule
	if err := service.db.WithContext(ctx).Order("id DESC").Limit(500).Find(&schedules).Error; err != nil {
		return nil, fmt.Errorf("office message: list push schedules: %w", err)
	}
	return schedules, nil
}

func (service *OfficeMessageService) CreateSchedule(ctx context.Context, actorID uint, input OfficePushScheduleInput) (*model.OfficePushSchedule, error) {
	if actorID == 0 {
		return nil, fmt.Errorf("%w: schedule actor is required", ErrOfficeMessageInvalid)
	}
	var created model.OfficePushSchedule
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		normalized, err := service.normalizeScheduleInput(tx, input, service.now().UTC())
		if err != nil {
			return err
		}
		created = model.OfficePushSchedule{
			Name: normalized.name, TargetID: normalized.targetID, CronExpr: normalized.cronExpr,
			TimeZone: normalized.timeZone, ParametersJSON: normalized.parameters, Enabled: normalized.enabled,
			NextRunAt: normalized.nextRunAt, LockVersion: 1, CreatedBy: actorID, UpdatedBy: actorID,
		}
		if err := tx.Create(&created).Error; err != nil {
			return fmt.Errorf("office message: create push schedule: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (service *OfficeMessageService) UpdateSchedule(ctx context.Context, actorID, scheduleID uint, input OfficePushScheduleInput) (*model.OfficePushSchedule, error) {
	if actorID == 0 || scheduleID == 0 || input.ExpectedLockVersion == 0 {
		return nil, fmt.Errorf("%w: schedule id and lock version are required", ErrOfficeMessageInvalid)
	}
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		normalized, err := service.normalizeScheduleInput(tx, input, service.now().UTC())
		if err != nil {
			return err
		}
		result := tx.Model(&model.OfficePushSchedule{}).
			Where("id = ? AND lock_version = ?", scheduleID, input.ExpectedLockVersion).
			Updates(map[string]interface{}{
				"name": normalized.name, "target_id": normalized.targetID, "cron_expr": normalized.cronExpr,
				"time_zone": normalized.timeZone, "parameters_json": normalized.parameters, "enabled": normalized.enabled,
				"next_run_at": normalized.nextRunAt, "last_error_safe": "", "updated_by": actorID,
				"lock_version": gorm.Expr("lock_version + 1"), "updated_at": service.now().UTC(),
			})
		if result.Error != nil {
			return fmt.Errorf("office message: update push schedule: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrOfficeMessageConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return service.getSchedule(ctx, scheduleID)
}

func (service *OfficeMessageService) DeleteSchedule(ctx context.Context, scheduleID uint, expectedLockVersion uint64) error {
	if scheduleID == 0 || expectedLockVersion == 0 {
		return fmt.Errorf("%w: schedule id and lock version are required", ErrOfficeMessageInvalid)
	}
	result := service.db.WithContext(ctx).Where("id = ? AND lock_version = ?", scheduleID, expectedLockVersion).Delete(&model.OfficePushSchedule{})
	if result.Error != nil {
		return fmt.Errorf("office message: delete push schedule: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrOfficeMessageConflict
	}
	return nil
}

func (service *OfficeMessageService) getSchedule(ctx context.Context, id uint) (*model.OfficePushSchedule, error) {
	var schedule model.OfficePushSchedule
	if err := service.db.WithContext(ctx).Where("id = ?", id).First(&schedule).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOfficeMessageNotFound
	} else if err != nil {
		return nil, fmt.Errorf("office message: get push schedule: %w", err)
	}
	return &schedule, nil
}

func (service *OfficeMessageService) normalizeScheduleInput(tx *gorm.DB, input OfficePushScheduleInput, now time.Time) (normalizedOfficePushSchedule, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.CronExpr = strings.Join(strings.Fields(input.CronExpr), " ")
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	if input.TimeZone == "" {
		input.TimeZone = model.OfficeScheduleTimeZone
	}
	if input.Name == "" || len(input.Name) > 128 || input.TargetID == 0 || input.TimeZone != model.OfficeScheduleTimeZone || now.IsZero() {
		return normalizedOfficePushSchedule{}, fmt.Errorf("%w: push schedule is invalid", ErrOfficeMessageInvalid)
	}
	nextRunAt, err := nextOfficeScheduleTime(input.CronExpr, now)
	if err != nil {
		return normalizedOfficePushSchedule{}, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
	}
	var target model.OfficePushTarget
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.TargetID).First(&target).Error; err != nil {
		return normalizedOfficePushSchedule{}, fmt.Errorf("%w: push target is unavailable", ErrOfficeMessageNotFound)
	}
	var message model.OfficeMessage
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", target.MessageID).First(&message).Error; err != nil {
		return normalizedOfficePushSchedule{}, fmt.Errorf("%w: message is unavailable", ErrOfficeMessageNotFound)
	}
	if err := service.preparePersistedOfficePushTarget(&target, message); err != nil {
		return normalizedOfficePushSchedule{}, err
	}
	parameters, err := normalizeOfficeScheduleParameters(message, input.Parameters)
	if err != nil {
		return normalizedOfficePushSchedule{}, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	return normalizedOfficePushSchedule{
		name: input.Name, targetID: input.TargetID, cronExpr: input.CronExpr, timeZone: input.TimeZone,
		parameters: parameters, enabled: enabled, nextRunAt: nextRunAt,
	}, nil
}

func nextOfficeScheduleTime(expression string, after time.Time) (time.Time, error) {
	expression = strings.Join(strings.Fields(expression), " ")
	if expression == "" || after.IsZero() {
		return time.Time{}, fmt.Errorf("office message schedule: cron expression is required")
	}
	parsed, err := officeScheduleCronParser.Parse(expression)
	if err != nil {
		return time.Time{}, fmt.Errorf("office message schedule: cron expression must contain five fields")
	}
	next := parsed.Next(after.In(officeShanghaiLocation))
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("office message schedule: cron expression has no next execution")
	}
	return next.UTC(), nil
}

func normalizeOfficeScheduleParameters(message model.OfficeMessage, input map[string]OfficeScheduleParameterValue) (model.JSONText, error) {
	if input == nil {
		input = map[string]OfficeScheduleParameterValue{}
	}
	if message.SourceType != model.OfficeMessageSourceOracleQuery {
		if len(input) != 0 {
			return "", fmt.Errorf("office message schedule: this source does not accept parameters")
		}
		return model.JSONText("{}"), nil
	}
	schema, _, err := normalizeOfficeQueryParameters(message.SelectSQL, message.ParameterSchemaJSON)
	if err != nil {
		return "", err
	}
	byCode := make(map[string]OfficeScheduleParameterValue, len(input))
	for code, configuration := range input {
		code = strings.ToLower(strings.TrimSpace(code))
		if !officeParameterPattern.MatchString(code) {
			return "", fmt.Errorf("office message schedule: parameter code is invalid")
		}
		if _, exists := byCode[code]; exists {
			return "", fmt.Errorf("office message schedule: parameter is duplicated")
		}
		configuration.Mode = strings.ToUpper(strings.TrimSpace(configuration.Mode))
		configuration.Value = strings.TrimSpace(configuration.Value)
		if configuration.OffsetDays < -officeScheduleMaximumOffsetDays || configuration.OffsetDays > officeScheduleMaximumOffsetDays {
			return "", fmt.Errorf("office message schedule: parameter date offset is invalid")
		}
		byCode[code] = configuration
	}
	known := make(map[string]struct{}, len(schema))
	validationValues := make(map[string]string, len(schema))
	for _, parameter := range schema {
		known[parameter.Code] = struct{}{}
		configuration, exists := byCode[parameter.Code]
		if !exists {
			if parameter.Required {
				return "", fmt.Errorf("office message schedule: parameter %q is required", parameter.Code)
			}
			continue
		}
		switch configuration.Mode {
		case model.OfficeScheduleParameterLiteral:
			if configuration.OffsetDays != 0 {
				return "", fmt.Errorf("office message schedule: literal parameter offset is invalid")
			}
			validationValues[parameter.Code] = configuration.Value
		case model.OfficeScheduleParameterScheduledDate:
			if parameter.ValueType != "date" || configuration.Value != "" {
				return "", fmt.Errorf("office message schedule: scheduled date parameter is invalid")
			}
			layout, _ := officeDateLayout(parameter.Format)
			validationValues[parameter.Code] = time.Date(2026, 9, 1, 8, 0, 0, 0, officeShanghaiLocation).AddDate(0, 0, configuration.OffsetDays).Format(layout)
		default:
			return "", fmt.Errorf("office message schedule: parameter mode is invalid")
		}
	}
	for code := range byCode {
		if _, exists := known[code]; !exists {
			return "", fmt.Errorf("office message schedule: parameter %q is not configured", code)
		}
	}
	if _, err := normalizeOfficeRunParameters(message, validationValues); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(byCode)
	if err != nil {
		return "", fmt.Errorf("office message schedule: encode parameters: %w", err)
	}
	return model.JSONText(encoded), nil
}

func renderOfficeScheduleParameters(message model.OfficeMessage, raw model.JSONText, scheduledFor time.Time) (map[string]string, error) {
	if scheduledFor.IsZero() {
		return nil, fmt.Errorf("office message schedule: scheduled time is required")
	}
	configuration := make(map[string]OfficeScheduleParameterValue)
	if err := decodeOfficeJSON(raw, &configuration); err != nil {
		return nil, fmt.Errorf("office message schedule: decode parameters: %w", err)
	}
	if _, err := normalizeOfficeScheduleParameters(message, configuration); err != nil {
		return nil, err
	}
	if message.SourceType != model.OfficeMessageSourceOracleQuery {
		return map[string]string{}, nil
	}
	schema, _, err := normalizeOfficeQueryParameters(message.SelectSQL, message.ParameterSchemaJSON)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(configuration))
	for _, parameter := range schema {
		configured, exists := configuration[parameter.Code]
		if !exists {
			continue
		}
		if configured.Mode == model.OfficeScheduleParameterLiteral {
			values[parameter.Code] = configured.Value
			continue
		}
		layout, _ := officeDateLayout(parameter.Format)
		values[parameter.Code] = scheduledFor.In(officeShanghaiLocation).AddDate(0, 0, configured.OffsetDays).Format(layout)
	}
	if _, err := normalizeOfficeRunParameters(message, values); err != nil {
		return nil, err
	}
	return values, nil
}
