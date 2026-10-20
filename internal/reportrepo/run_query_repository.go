package reportrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gin-biz-web-api/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrReportRunAccessNotFound   = errors.New("report run query: not found")
	ErrReportRunStateConflict    = errors.New("report run query: state conflict")
	ErrReportResultUnavailable   = errors.New("report run query: result unavailable")
	ErrReportSelectResultExpired = errors.New("REPORT_RESULT_EXPIRED")
)

type RunResultContract struct {
	Run        model.ReportRun
	Version    model.ReportVersion
	Datasource model.ReportDatasource
}

func (repository *Repository) AcquireResultReadLease(ctx context.Context, actor, runID uint, now time.Time) (*RunResultContract, string, error) {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || runID == 0 || now.IsZero() {
		return nil, "", fmt.Errorf("report result read lease: invalid request")
	}
	now = now.UTC().Truncate(time.Millisecond)
	token := uuid.NewString()
	var contract *RunResultContract
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.ReportRun
		if err := buildActorRunQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), actor, runID).First(&run).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrReportRunAccessNotFound
		} else if err != nil {
			return fmt.Errorf("report result read lease: lock run: %w", err)
		}
		if run.RetainsQuerySnapshot() && run.ResultExpiresAt != nil && !now.Before(*run.ResultExpiresAt) {
			return ErrReportSelectResultExpired
		}
		if run.Status != model.ReportRunStatusSucceeded || run.ResultPurgedAt != nil || run.ResultExpiresAt == nil || !now.Before(run.ResultExpiresAt.UTC()) {
			return ErrReportResultUnavailable
		}
		loaded, err := repository.loadResultContract(ctx, tx, run, now)
		if err != nil {
			return err
		}
		if err := tx.Where("run_id = ? AND expires_at <= ?", runID, now).Delete(&model.ReportResultReadLease{}).Error; err != nil {
			return fmt.Errorf("report result read lease: delete expired: %w", err)
		}
		leaseTTL := time.Duration(loaded.Datasource.QueryTimeoutSeconds)*time.Second + 30*time.Second
		if leaseTTL < 2*time.Minute {
			leaseTTL = 2 * time.Minute
		}
		lease := model.ReportResultReadLease{RunID: runID, LeaseToken: token, ExpiresAt: now.Add(leaseTTL)}
		if err := tx.Create(&lease).Error; err != nil {
			return fmt.Errorf("report result read lease: create: %w", err)
		}
		contract = loaded
		return nil
	})
	return contract, token, err
}

func (repository *Repository) ReleaseResultReadLease(ctx context.Context, token string) error {
	if repository == nil || repository.db == nil || ctx == nil || uuid.Validate(token) != nil {
		return fmt.Errorf("report result read lease: invalid release")
	}
	if err := repository.db.WithContext(ctx).Where("lease_token = ?", token).Delete(&model.ReportResultReadLease{}).Error; err != nil {
		return fmt.Errorf("report result read lease: release: %w", err)
	}
	return nil
}

func (repository *Repository) FindRunForActor(ctx context.Context, actor, runID uint) (*model.ReportRun, error) {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || runID == 0 {
		return nil, fmt.Errorf("report run query: invalid request")
	}
	var run model.ReportRun
	err := buildActorRunQuery(repository.db.WithContext(ctx), actor, runID).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrReportRunAccessNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("report run query: find run: %w", err)
	}
	return &run, nil
}

func buildActorRunQuery(db *gorm.DB, actor, runID uint) *gorm.DB {
	return db.Where("id = ? AND requested_by = ?", runID, actor)
}

func (repository *Repository) RequestRunCancellation(ctx context.Context, actor, runID uint, now time.Time) (*model.ReportRun, error) {
	if repository == nil || repository.db == nil || ctx == nil || actor == 0 || runID == 0 || now.IsZero() {
		return nil, fmt.Errorf("report run cancellation: invalid request")
	}
	now = now.UTC().Truncate(time.Millisecond)
	var run model.ReportRun
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := buildActorRunQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), actor, runID).First(&run).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrReportRunAccessNotFound
		} else if err != nil {
			return fmt.Errorf("report run cancellation: lock run: %w", err)
		}
		updates := map[string]interface{}{}
		switch run.Status {
		case model.ReportRunStatusQueued:
			updates = terminalRunUpdates(model.ReportRunStatusCancelled, "CANCELLED", "报表运行已取消", now)
			updates["cancel_requested"] = true
			run.Status, run.CancelRequested, run.FinishedAt = model.ReportRunStatusCancelled, true, &now
		case model.ReportRunStatusRunning:
			if run.CancelRequested {
				return nil
			}
			updates = map[string]interface{}{"cancel_requested": true, "updated_at": now}
			run.CancelRequested = true
		case model.ReportRunStatusCancelled:
			return nil
		default:
			return ErrReportRunStateConflict
		}
		if result := buildActorRunQuery(tx.Model(&model.ReportRun{}), actor, runID).Updates(updates); result.Error != nil {
			return fmt.Errorf("report run cancellation: update run: %w", result.Error)
		} else if result.RowsAffected != 1 {
			return ErrReportRunAccessNotFound
		}
		detail, err := json.Marshal(map[string]interface{}{"runId": runID, "status": run.Status})
		if err != nil {
			return fmt.Errorf("report run cancellation: encode audit: %w", err)
		}
		if err := repository.writeAudit(ctx, tx, model.ReportAudit{
			ActorUserID: actor, Action: "REPORT_RUN_CANCEL_REQUEST", TargetType: "REPORT_RUN", TargetID: runID,
			RequestID: uuid.NewString(), DetailJSON: model.JSONText(detail),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (repository *Repository) loadResultContract(ctx context.Context, db *gorm.DB, run model.ReportRun, now time.Time) (*RunResultContract, error) {
	contract := RunResultContract{Run: run}
	if err := db.Where("id = ? AND definition_id = ?", contract.Run.VersionID, contract.Run.DefinitionID).First(&contract.Version).Error; err != nil {
		return nil, fmt.Errorf("report result query: load version: %w", err)
	}
	if contract.Version.ContractHash != contract.Run.ContractHash ||
		contract.Version.ProcedureSignatureHash != contract.Run.ProcedureSignatureHash ||
		contract.Version.ResultSchemaHash != contract.Run.ResultSchemaHash || contract.Version.DatasourceID == 0 {
		return nil, ErrReportResultUnavailable
	}
	if contract.Version.ExecutionMode == model.ReportExecutionModeSelectSnapshot {
		if _, err := repository.loadPublished(ctx, db, run.RequestedBy, run.DefinitionID, ReportActionQuery, false); err != nil {
			return nil, ErrReportActionDenied
		}
		if err := validateSelectAuthorization(ctx, db, run, contract.Version); err != nil {
			return nil, err
		}
	}
	if err := db.Where("id = ? AND driver = ?", contract.Version.DatasourceID, model.ReportDatasourceDriverOracle).First(&contract.Datasource).Error; err != nil {
		return nil, fmt.Errorf("report result query: load datasource: %w", err)
	}
	return &contract, nil
}
