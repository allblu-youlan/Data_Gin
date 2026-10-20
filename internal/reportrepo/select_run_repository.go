package reportrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

func prepareSelectRunSlot(ctx context.Context, tx *gorm.DB, definitionID, actor uint, fingerprint string, now time.Time) (reportRunSlotPreparation, error) {
	var runs []model.ReportRun
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("definition_id = ? AND requested_by = ? AND execution_fingerprint = ?", definitionID, actor, fingerprint).Where("status IN ?", []string{model.ReportRunStatusQueued, model.ReportRunStatusRunning, model.ReportRunStatusSucceeded}).Order("id DESC").Find(&runs).Error
	if e != nil {
		return reportRunSlotPreparation{}, e
	}
	for i := range runs {
		r := &runs[i]
		if r.Status == model.ReportRunStatusSucceeded && (r.ResultPurgedAt != nil || r.ResultExpiresAt == nil || !now.Before(*r.ResultExpiresAt)) {
			continue
		}
		return reportRunSlotPreparation{ExistingRun: r}, nil
	}
	return reportRunSlotPreparation{}, nil
}

// MySQL advisory locks use dedicated connections, enforcing a shared source budget
// across worker replicas and releasing automatically when a worker disconnects.
func (r *Repository) AcquireSelectDatasourceSlot(ctx context.Context, ds uint) (func(), error) {
	db, e := r.db.DB()
	if e != nil {
		return nil, e
	}
	conn, e := db.Conn(ctx)
	if e != nil {
		return nil, e
	}
	for slot := 0; slot < 2; slot++ {
		name := fmt.Sprintf("report-select:%d:%d", ds, slot)
		var locked sql.NullInt64
		if e = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&locked); e != nil {
			conn.Close()
			return nil, e
		}
		if locked.Valid && locked.Int64 == 1 {
			return func() {
				release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				var unlocked sql.NullInt64
				_ = conn.QueryRowContext(release, "SELECT RELEASE_LOCK(?)", name).Scan(&unlocked)
				_ = conn.Close()
			}, nil
		}
	}
	conn.Close()
	return nil, ErrReportRunBusy
}
func (r *Repository) DeferSelectRun(ctx context.Context, id uint, token string) error {
	result := r.db.WithContext(ctx).Model(&model.ReportRun{}).Where("id = ? AND lease_token = ? AND status = ? AND oracle_started_at IS NULL", id, token, model.ReportRunStatusRunning).Updates(map[string]any{"status": model.ReportRunStatusQueued, "lease_token": "", "lease_expires_at": nil, "heartbeat_at": nil, "worker_id": "", "attempt": gorm.Expr("GREATEST(attempt - 1, 0)")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrReportRunLeaseLost
	}
	return nil
}

// EffectiveSelectAuthorization uses only local catalog/config state. It must also
// validate field dependencies before issuing a new run from an older version.
func (r *Repository) EffectiveSelectAuthorization(ctx context.Context, ds uint, c reportdataset.Compiled, actor uint) (reportdataset.Compiled, string, error) {
	return effectiveSelectAuthorization(ctx, r.db, ds, c, actor)
}
func effectiveSelectAuthorization(ctx context.Context, db *gorm.DB, ds uint, c reportdataset.Compiled, actor uint) (reportdataset.Compiled, string, error) {
	scopes := map[string]any{}
	actorID := strconv.FormatUint(uint64(actor), 10)
	for id, frozen := range c.Tables {
		localID, e := strconv.ParseUint(id, 10, 64)
		if e != nil {
			return c, "", ErrReportActionDenied
		}
		var table model.ReportBojunTable
		if e = db.WithContext(ctx).Where("id = ? AND datasource_id = ? AND source_active = ?", uint(localID), ds, true).First(&table).Error; e != nil {
			return c, "", ErrReportActionDenied
		}
		var row model.ReportBojunTableConfig
		if e = db.WithContext(ctx).Where("table_id = ?", table.ID).First(&row).Error; e != nil {
			return c, "", ErrReportActionDenied
		}
		var config reportdataset.TableConfig
		if json.Unmarshal([]byte(row.ConfigJSON), &config) != nil || !config.Enabled {
			return c, "", ErrReportActionDenied
		}
		scope := config.DataScope
		if scope.Mode != frozen.Config.DataScope.Mode || scope.ColumnID != frozen.Config.DataScope.ColumnID {
			return c, "", ErrReportActionDenied
		}
		if scope.Mode != "GLOBAL_APPROVED" && scope.Mode != "SCOPE_COLUMN" {
			return c, "", ErrReportActionDenied
		}
		scopes[id] = struct {
			Enabled                bool
			ScopeMode, ScopeColumn string
			Values                 []string
			Selectable, Detail     *[]string
		}{config.Enabled, scope.Mode, scope.ColumnID, scope.UserValues[actorID], config.SelectableColumns, config.DetailColumns}
		frozen.Config.SelectableColumns = config.SelectableColumns
		frozen.Config.DetailColumns = config.DetailColumns
		frozen.Config.DataScope.UserValues = map[string][]string{actorID: scope.UserValues[actorID]}
		c.Tables[id] = frozen
	}
	if _, e := reportdataset.Compile(c.Query, c.Tables, c.Policy); e != nil {
		return c, "", ErrReportActionDenied
	}
	return c, reportdataset.Hash(scopes), nil
}
func validateSelectAuthorization(ctx context.Context, db *gorm.DB, run model.ReportRun, version model.ReportVersion) error {
	if version.ExecutionMode != model.ReportExecutionModeSelectSnapshot {
		return nil
	}
	var contract struct {
		Dataset reportdataset.Compiled `json:"dataset"`
	}
	if json.Unmarshal([]byte(version.CompiledSpecJSON), &contract) != nil || reportdataset.Hash(json.RawMessage(version.CompiledSpecJSON)) != run.ContractHash {
		return ErrReportRunStateConflict
	}
	_, hash, e := effectiveSelectAuthorization(ctx, db, version.DatasourceID, contract.Dataset, run.RequestedBy)
	if e != nil {
		return e
	}
	if hash != run.DataScopeHash {
		return ErrReportActionDenied
	}
	return nil
}
