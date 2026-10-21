package reportrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gin-biz-web-api/model"
)

var (
	ErrDatasourceNotFound = errors.New("report datasource: not found")
	ErrDatasourceInUse    = errors.New("report datasource: connection is in use")
)

func (repository *Repository) ListReportDatasources(ctx context.Context) ([]model.ReportDatasource, error) {
	if repository == nil || repository.db == nil || ctx == nil {
		return nil, fmt.Errorf("report datasource: repository and context are required")
	}
	if err := repository.ensureEnvironmentDatasource(ctx); err != nil {
		return nil, fmt.Errorf("report datasource: system environment: %w", err)
	}
	var items []model.ReportDatasource
	if err := repository.db.WithContext(ctx).Where("driver = ?", model.ReportDatasourceDriverOracle).Order("id ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("report datasource: list: %w", err)
	}
	return items, nil
}

func (repository *Repository) GetReportDatasource(ctx context.Context, datasourceID uint) (*model.ReportDatasource, error) {
	if repository == nil || repository.db == nil || ctx == nil || datasourceID == 0 {
		return nil, ErrDatasourceNotFound
	}
	var datasource model.ReportDatasource
	err := repository.db.WithContext(ctx).Where("id = ? AND driver = ?", datasourceID, model.ReportDatasourceDriverOracle).First(&datasource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDatasourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report datasource: get: %w", err)
	}
	return &datasource, nil
}

func (repository *Repository) FindEnabledReportDatasourceByCode(ctx context.Context, code string) (*model.ReportDatasource, error) {
	code = strings.TrimSpace(code)
	if repository == nil || repository.db == nil || ctx == nil || code == "" {
		return nil, ErrDatasourceNotFound
	}
	var datasource model.ReportDatasource
	err := repository.db.WithContext(ctx).
		Where("code = ? AND driver = ? AND enabled = ?", code, model.ReportDatasourceDriverOracle, true).
		First(&datasource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDatasourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report datasource: find enabled by code: %w", err)
	}
	return &datasource, nil
}

func (repository *Repository) CreateReportDatasource(ctx context.Context, actor uint, datasource *model.ReportDatasource) error {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || datasource == nil {
		return fmt.Errorf("report datasource: repository, actor and datasource are required")
	}
	datasource.CreatedBy = actor
	datasource.UpdatedBy = actor
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(datasource).Error; err != nil {
			return fmt.Errorf("report datasource: create: %w", err)
		}
		return createDatasourceAudit(ctx, tx, actor, datasource.ID, "REPORT_DATASOURCE_CREATE", datasourceAuditDetailFrom(*datasource))
	})
}

func (repository *Repository) UpdateReportDatasource(ctx context.Context, actor uint, datasource *model.ReportDatasource) error {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || datasource == nil || datasource.ID == 0 {
		return ErrDatasourceNotFound
	}
	datasource.UpdatedBy = actor
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current model.ReportDatasource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND driver = ?", datasource.ID, model.ReportDatasourceDriverOracle).First(&current).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrDatasourceNotFound
		} else if err != nil {
			return fmt.Errorf("report datasource: lock current: %w", err)
		}
		if systemEnvironmentDatasource(current) {
			return ErrDatasourceInUse
		}
		if datasourceConnectionChanged(current, *datasource) {
			if datasourcePhysicalIdentityChanged(current, *datasource) {
				bound, err := datasourceHasPublishedBinding(ctx, tx, datasource.ID)
				if err != nil {
					return err
				}
				if bound {
					return ErrDatasourceInUse
				}
			}
			inUse, err := datasourceHasLiveRuns(ctx, tx, datasource.ID)
			if err != nil {
				return err
			}
			if inUse {
				return ErrDatasourceInUse
			}
		}
		updates := map[string]interface{}{
			"code": datasource.Code, "name": datasource.Name, "host": datasource.Host, "port": datasource.Port,
			"service_name": datasource.ServiceName, "sid": datasource.SID, "username": datasource.Username,
			"session_timezone": datasource.SessionTimezone, "connect_timeout_seconds": datasource.ConnectTimeoutSeconds,
			"query_timeout_seconds": datasource.QueryTimeoutSeconds, "max_open_connections": datasource.MaxOpenConnections,
			"max_idle_connections": datasource.MaxIdleConnections, "prefetch_rows": datasource.PrefetchRows,
			"array_size": datasource.ArraySize, "enabled": datasource.Enabled, "updated_by": actor,
		}
		if datasource.PasswordCiphertext != "" {
			updates["password_ciphertext"] = datasource.PasswordCiphertext
			updates["credential_key_version"] = datasource.CredentialKeyVersion
		}
		result := tx.Model(&model.ReportDatasource{}).Where("id = ? AND driver = ?", datasource.ID, model.ReportDatasourceDriverOracle).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("report datasource: update: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrDatasourceNotFound
		}
		return createDatasourceAudit(ctx, tx, actor, datasource.ID, "REPORT_DATASOURCE_UPDATE", datasourceAuditDetailFrom(*datasource))
	})
}

func datasourcePhysicalIdentityChanged(current, next model.ReportDatasource) bool {
	return current.Host != next.Host || current.Port != next.Port || current.ServiceName != next.ServiceName ||
		current.SID != next.SID || current.Username != next.Username
}

func datasourceHasPublishedBinding(ctx context.Context, tx *gorm.DB, datasourceID uint) (bool, error) {
	var count int64
	if err := tx.WithContext(ctx).Table("report_result_table_bindings AS bindings").
		Joins("JOIN report_versions AS versions ON versions.id = bindings.version_id").
		Where("versions.datasource_id = ?", datasourceID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("report datasource: find published result table bindings: %w", err)
	}
	return count > 0, nil
}

func datasourceConnectionChanged(current, next model.ReportDatasource) bool {
	return current.Host != next.Host || current.Port != next.Port || current.ServiceName != next.ServiceName || current.SID != next.SID ||
		current.Username != next.Username || current.SessionTimezone != next.SessionTimezone ||
		current.ConnectTimeoutSeconds != next.ConnectTimeoutSeconds || current.QueryTimeoutSeconds != next.QueryTimeoutSeconds ||
		current.MaxOpenConnections != next.MaxOpenConnections || current.MaxIdleConnections != next.MaxIdleConnections ||
		current.PrefetchRows != next.PrefetchRows || current.ArraySize != next.ArraySize || next.PasswordCiphertext != ""
}

func datasourceHasLiveRuns(ctx context.Context, tx *gorm.DB, datasourceID uint) (bool, error) {
	var count int64
	err := tx.WithContext(ctx).Table("report_runs AS runs").
		Joins("JOIN report_versions AS versions ON versions.id = runs.version_id AND versions.definition_id = runs.definition_id").
		Where("versions.datasource_id = ?", datasourceID).
		Where("runs.status IN ? OR (runs.status IN ? AND runs.result_purged_at IS NULL)", []string{
			model.ReportRunStatusQueued, model.ReportRunStatusRunning, model.ReportRunStatusCancelRequested,
			model.ReportRunStatusUnknown, model.ReportRunStatusReconciling, model.ReportRunStatusExporting,
			model.ReportRunStatusResultPurging,
		}, []string{
			model.ReportRunStatusSucceeded, model.ReportRunStatusExported,
		}).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("report datasource: find live runs: %w", err)
	}
	return count > 0, nil
}

type datasourceAuditDetail struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Driver  string `json:"driver"`
	Enabled bool   `json:"enabled"`
}

func datasourceAuditDetailFrom(datasource model.ReportDatasource) datasourceAuditDetail {
	return datasourceAuditDetail{
		Code: datasource.Code, Name: datasource.Name, Driver: datasource.Driver, Enabled: datasource.Enabled,
	}
}

func (repository *Repository) RecordReportDatasourceTest(ctx context.Context, actor, datasourceID uint, status, safeError string, testedAt time.Time) error {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || datasourceID == 0 {
		return ErrDatasourceNotFound
	}
	return repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ReportDatasource{}).Where("id = ? AND driver = ?", datasourceID, model.ReportDatasourceDriverOracle).Updates(map[string]interface{}{
			"last_test_status": status, "last_test_error_safe": safeError, "last_tested_at": testedAt, "updated_by": actor,
		})
		if result.Error != nil {
			return fmt.Errorf("report datasource: record test: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrDatasourceNotFound
		}
		return createDatasourceAudit(ctx, tx, actor, datasourceID, "REPORT_DATASOURCE_TEST", map[string]string{"status": status})
	})
}

func createDatasourceAudit(ctx context.Context, tx *gorm.DB, actor, datasourceID uint, action string, detail interface{}) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("report datasource: encode audit: %w", err)
	}
	audit := model.ReportAudit{ActorUserID: actor, Action: action, TargetType: "REPORT_DATASOURCE", TargetID: datasourceID, RequestID: uuid.NewString(), DetailJSON: model.JSONText(encoded)}
	if err := tx.WithContext(ctx).Create(&audit).Error; err != nil {
		return fmt.Errorf("report datasource: create audit: %w", err)
	}
	return nil
}
