package reportrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"gin-biz-web-api/internal/reportdataset"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

func (r *Repository) StartBojunSync(ctx context.Context, ds uint, scope string, tableID uint) (*model.ReportBojunSyncRun, error) {
	run := &model.ReportBojunSyncRun{DatasourceID: ds, Scope: scope, TableID: tableID, Status: "QUEUED"}
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.ReportBojunCatalog{DatasourceID: ds}).Error; e != nil {
			return e
		}
		var cat model.ReportBojunCatalog
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("datasource_id = ?", ds).First(&cat).Error; e != nil {
			return e
		}
		var pending int64
		if e := tx.Model(&model.ReportBojunSyncRun{}).Where("datasource_id = ? AND status IN ? AND created_at > ?", ds, []string{"QUEUED", "RUNNING"}, time.Now().Add(-40*time.Minute)).Count(&pending).Error; e != nil {
			return e
		}
		if pending > 0 {
			return ErrDraftVersionConflict
		}
		run.Generation = cat.CurrentGeneration + 1
		if e := tx.Create(run).Error; e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]uint{"run_id": run.ID})
		return tx.Create(&model.AsyncJobOutbox{TaskKey: fmt.Sprintf("report:bojun_sync:%d", run.ID), TaskType: job.TypeReportBojunSync, QueueName: job.ReportQueueName, PayloadJSON: model.JSONText(payload), AvailableAt: time.Now().UTC()}).Error
	})
	return run, e
}
func (r *Repository) GetBojunSync(ctx context.Context, ds, id uint) (*model.ReportBojunSyncRun, error) {
	var v model.ReportBojunSyncRun
	e := r.db.WithContext(ctx).Where("id = ? AND datasource_id = ?", id, ds).First(&v).Error
	return &v, e
}
func (r *Repository) StageBojunTables(ctx context.Context, runID uint, items []reportoracle.BojunTableMetadata) error {
	staged := make([]model.ReportBojunSyncStaging, 0, len(items))
	for _, v := range items {
		b, _ := json.Marshal(v)
		staged = append(staged, model.ReportBojunSyncStaging{SyncRunID: runID, SourceKey: v.ID, PayloadJSON: model.JSONText(b)})
	}
	if len(staged) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sync_run_id"}, {Name: "source_key"}}, DoUpdates: clause.AssignmentColumns([]string{"payload_json"})}).Create(&staged).Error
}
func (r *Repository) MarkBojunSync(ctx context.Context, id uint, status, message string) error {
	m := map[string]any{"status": status, "error_safe": message}
	now := time.Now().UTC()
	if status == "RUNNING" {
		m["started_at"] = now
	} else {
		m["finished_at"] = now
	}
	return r.db.WithContext(ctx).Model(&model.ReportBojunSyncRun{}).Where("id = ? AND status <> 'SUCCEEDED'", id).Updates(m).Error
}
func (r *Repository) FinishBojunSync(ctx context.Context, run model.ReportBojunSyncRun, n reportoracle.BojunNamespace) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cat model.ReportBojunCatalog
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("datasource_id = ?", run.DatasourceID).First(&cat).Error; e != nil {
			return e
		}
		if cat.CurrentGeneration >= run.Generation {
			return nil
		}
		var stage []model.ReportBojunSyncStaging
		if e := tx.Where("sync_run_id = ?", run.ID).Find(&stage).Error; e != nil {
			return e
		}
		now := time.Now().UTC()
		for _, s := range stage {
			var source reportoracle.BojunTableMetadata
			if e := json.Unmarshal([]byte(s.PayloadJSON), &source); e != nil {
				return e
			}
			t := model.ReportBojunTable{DatasourceID: run.DatasourceID, BojunTableID: source.ID, LogicalName: source.Name, Description: source.Description, RealTableID: source.RealTableID, SourceFilter: source.Filter, SourcePK: source.PK, SourceAK: source.AK, SourceDK: source.DK, ResolvedOwner: n.Owner, DBLink: n.DBLink, SourceActive: true, Generation: run.Generation, LastSyncedAt: &now, ValidationStatus: "PENDING"}
			if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "datasource_id"}, {Name: "bojun_table_id"}}, DoUpdates: clause.AssignmentColumns([]string{"logical_name", "description", "real_table_id", "source_filter", "source_pk", "source_ak", "source_dk", "resolved_owner", "db_link", "source_active", "generation", "last_synced_at", "validation_status"})}).Create(&t).Error; e != nil {
				return e
			}
		}
		if e := tx.Model(&model.ReportBojunTable{}).Where("datasource_id = ? AND generation <> ?", run.DatasourceID, run.Generation).Update("source_active", false).Error; e != nil {
			return e
		}
		if e := tx.Model(&cat).Updates(map[string]any{"current_generation": run.Generation, "last_success_at": now}).Error; e != nil {
			return e
		}
		if e := tx.Model(&model.ReportBojunSyncRun{}).Where("id = ?", run.ID).Updates(map[string]any{"status": "SUCCEEDED", "table_count": len(stage), "finished_at": now}).Error; e != nil {
			return e
		}
		return tx.Where("sync_run_id = ?", run.ID).Delete(&model.ReportBojunSyncStaging{}).Error
	})
}
func (r *Repository) ListBojunTables(ctx context.Context, ds, after uint, limit int, search, status string) ([]model.ReportBojunTable, uint64, error) {
	var cat model.ReportBojunCatalog
	if e := r.db.WithContext(ctx).Where("datasource_id = ?", ds).First(&cat).Error; e != nil && e != gorm.ErrRecordNotFound {
		return nil, 0, e
	}
	q := r.db.WithContext(ctx).Where("datasource_id = ? AND source_active = ? AND id > ?", ds, true, after)
	if search != "" {
		q = q.Where("(logical_name LIKE ? OR description LIKE ?)", "%"+search+"%", "%"+search+"%")
	}
	if status != "" {
		q = q.Where("validation_status = ?", status)
	}
	var list []model.ReportBojunTable
	e := q.Order("id").Limit(limit + 1).Find(&list).Error
	return list, cat.CurrentGeneration, e
}
func (r *Repository) BojunSourceTables(ctx context.Context, ds uint) (map[string]reportoracle.BojunTableMetadata, error) {
	var list []model.ReportBojunTable
	if e := r.db.WithContext(ctx).Where("datasource_id = ? AND source_active = ?", ds, true).Find(&list).Error; e != nil {
		return nil, e
	}
	out := map[string]reportoracle.BojunTableMetadata{}
	for _, t := range list {
		out[t.BojunTableID] = reportoracle.BojunTableMetadata{ID: t.BojunTableID, Name: t.LogicalName, Description: t.Description, RealTableID: t.RealTableID, Filter: t.SourceFilter, PK: t.SourcePK, AK: t.SourceAK, DK: t.SourceDK}
	}
	return out, nil
}
func (r *Repository) GetBojunTable(ctx context.Context, ds, id uint) (*model.ReportBojunTable, error) {
	var t model.ReportBojunTable
	e := r.db.WithContext(ctx).Where("id = ? AND datasource_id = ? AND source_active = ?", id, ds, true).First(&t).Error
	return &t, e
}
func (r *Repository) SaveBojunMetadata(ctx context.Context, record *model.ReportBojunTable, t reportdataset.Table) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the table so a concurrent generation switch cannot publish stale fields.
		var current model.ReportBojunTable
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, record.ID).Error; e != nil {
			return e
		}
		if current.Generation != record.Generation {
			return ErrDraftVersionConflict
		}
		dbToID := map[string]string{}
		for i := range t.Columns {
			c := &t.Columns[i]
			v := model.ReportBojunColumn{TableID: record.ID, BojunColumnID: c.BojunColumnID, MetadataJSON: "{}"}
			if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&v).Error; e != nil {
				return e
			}
			if e := tx.Where("table_id = ? AND bojun_column_id = ?", record.ID, c.BojunColumnID).First(&v).Error; e != nil {
				return e
			}
			c.ID = strconv.FormatUint(uint64(v.ID), 10)
			if c.IsPhysical {
				dbToID[c.DBName] = c.ID
			}
			b, _ := json.Marshal(c)
			if e := tx.Model(&v).Update("metadata_json", model.JSONText(b)).Error; e != nil {
				return e
			}
		}
		remap := func(ids []string) []string {
			out := []string{}
			for _, id := range ids {
				if mapped := dbToID[id]; mapped != "" {
					out = append(out, mapped)
				}
			}
			return out
		}
		t.ID = strconv.FormatUint(uint64(record.ID), 10)
		t.PrimaryKeys = remap(t.PrimaryKeys)
		t.DisplayFields = remap(t.DisplayFields)
		t.SearchFields = remap(t.SearchFields)
		for i := range t.UniqueKeys {
			originalSize := len(t.UniqueKeys[i])
			t.UniqueKeys[i] = remap(t.UniqueKeys[i])
			if len(t.UniqueKeys[i]) != originalSize {
				t.UniqueKeys[i] = nil
			}
		}
		if len(t.PrimaryKeys) == 0 {
			t.ValidationStatus = "UNSUPPORTED"
			if t.ValidationReason == "" {
				t.ValidationReason = "唯一键未完整映射到可用字段"
			}
		}
		b, _ := json.Marshal(t)
		return tx.Model(record).Updates(map[string]any{"metadata_json": model.JSONText(b), "resolved_table": t.PhysicalTable, "validation_status": t.ValidationStatus, "validation_reason": t.ValidationReason}).Error
	})
}
func (r *Repository) GetBojunConfig(ctx context.Context, id uint) (model.ReportBojunTableConfig, error) {
	var c model.ReportBojunTableConfig
	e := r.db.WithContext(ctx).Where("table_id = ?", id).First(&c).Error
	if e == gorm.ErrRecordNotFound {
		return c, nil
	}
	return c, e
}
func (r *Repository) SaveBojunConfig(ctx context.Context, actor, id uint, expected uint64, config reportdataset.TableConfig) error {
	b, _ := json.Marshal(config)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.ReportBojunTableConfig{TableID: id, ConfigJSON: "{}", LockVersion: 1}).Error; e != nil {
			return e
		}
		q := tx.Model(&model.ReportBojunTableConfig{}).Where("table_id = ?", id)
		if expected == 0 {
			expected = 1
		}
		res := q.Where("lock_version = ?", expected).Updates(map[string]any{"config_json": model.JSONText(b), "updated_by": actor, "lock_version": gorm.Expr("lock_version + 1")})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrDraftVersionConflict
		}
		return nil
	})
}
func (r *Repository) GetBojunSyncByID(ctx context.Context, id uint) (*model.ReportBojunSyncRun, error) {
	var run model.ReportBojunSyncRun
	e := r.db.WithContext(ctx).First(&run, id).Error
	return &run, e
}
