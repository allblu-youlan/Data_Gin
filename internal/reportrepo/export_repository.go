package reportrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportquery"
	"gin-biz-web-api/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrReportExportRunNotReady = errors.New("report export: run not ready")
	ErrReportExportNotFound    = errors.New("report export: not found")
)

type CreateExportCommand struct {
	Export model.ReportExport
	Outbox model.AsyncJobOutbox
}

func (repository *Repository) CreateOrGetExport(ctx context.Context, actor, runID uint, input reportquery.Input, command *CreateExportCommand) (bool, error) {
	if repository == nil || repository.db == nil || repository.loadPublished == nil || repository.writeAudit == nil || ctx == nil || actor == 0 || runID == 0 || command == nil ||
		uuid.Validate(command.Export.ExportUUID) != nil || command.Export.RunID != runID || command.Export.CreatedBy != actor ||
		command.Export.Status != model.ReportExportStatusPending || !validReportExportOutbox(command.Outbox, command.Export.ExportUUID) {
		return false, fmt.Errorf("report export: invalid create request")
	}
	created := false
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.ReportRun
		if err := buildActorRunQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), actor, runID).First(&run).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrReportRunAccessNotFound
		} else if err != nil {
			return fmt.Errorf("report export: lock run: %w", err)
		}
		if err := repository.authorizeRunExport(ctx, tx, actor, &run); err != nil {
			return err
		}
		columns, err := FrozenExportQueryColumns(run.PresentationSnapshotJSON)
		if err != nil {
			return ErrReportExportRunNotReady
		}
		query, err := reportquery.Normalize(input, columns)
		if err != nil {
			return ErrReportExportRunNotReady
		}
		filtersJSON, sortJSON, err := reportquery.Encode(query)
		if err != nil {
			return fmt.Errorf("report export: encode frozen query: %w", err)
		}
		// Reuse the user's immutable file even after its Oracle source rows were purged.
		var existing model.ReportExport
		if err := tx.Where("run_id = ?", runID).First(&existing).Error; err == nil {
			if existing.CreatedBy != actor || string(existing.FrozenFiltersJSON) != string(filtersJSON) || string(existing.FrozenSortJSON) != string(sortJSON) {
				return ErrReportExportRunNotReady
			}
			command.Export = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("report export: find existing: %w", err)
		}
		requestedAt := command.Outbox.AvailableAt.UTC()
		if run.Status != model.ReportRunStatusSucceeded || run.ResultPurgedAt != nil || run.ResultExpiresAt == nil || !requestedAt.Before(run.ResultExpiresAt.UTC()) {
			return ErrReportExportRunNotReady
		}
		command.Export.FrozenColumnsJSON = run.PresentationSnapshotJSON
		command.Export.FrozenFiltersJSON = model.JSONText(filtersJSON)
		command.Export.FrozenSortJSON = model.JSONText(sortJSON)
		if err := tx.Create(&command.Export).Error; err != nil {
			return fmt.Errorf("report export: create job: %w", err)
		}
		command.Outbox.PayloadJSON = model.JSONText(fmt.Sprintf(`{"export_id":%d}`, command.Export.ID))
		if err := tx.Create(&command.Outbox).Error; err != nil {
			return fmt.Errorf("report export: create outbox: %w", err)
		}
		detail, err := json.Marshal(map[string]interface{}{"runId": runID, "exportId": command.Export.ID})
		if err != nil {
			return fmt.Errorf("report export: encode audit: %w", err)
		}
		if err := repository.writeAudit(ctx, tx, model.ReportAudit{ActorUserID: actor, Action: "REPORT_EXPORT_CREATE", TargetType: "REPORT_EXPORT", TargetID: command.Export.ID, RequestID: uuid.NewString(), DetailJSON: model.JSONText(detail)}); err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

func (repository *Repository) authorizeRunExport(ctx context.Context, tx *gorm.DB, actor uint, run *model.ReportRun) error {
	if run != nil && run.RetainsQuerySnapshot() {
		if !frozenRunAllowsAction(run.PermissionSnapshotJSON, actor, ReportActionExport) {
			return ErrReportExportRunNotReady
		}
		if _, err := repository.loadPublished(ctx, tx, actor, run.DefinitionID, ReportActionExport, false); err != nil {
			return ErrReportActionDenied
		}
		var version model.ReportVersion
		if err := tx.WithContext(ctx).First(&version, run.VersionID).Error; err != nil {
			return err
		}
		return validateSelectAuthorization(ctx, tx, *run, version)
	}

	if run == nil || frozenRunAllowsAction(run.PermissionSnapshotJSON, actor, ReportActionExport) {
		if run == nil {
			return ErrReportExportRunNotReady
		}
		return nil
	}
	if !frozenLegacyRunAllowsLiveAuthorization(run.PermissionSnapshotJSON, actor) {
		return ErrReportExportRunNotReady
	}
	if _, err := repository.loadPublished(ctx, tx, actor, run.DefinitionID, ReportActionExport, false); errors.Is(err, ErrReportActionDenied) || errors.Is(err, ErrPublishedReportNotFound) {
		return ErrReportExportRunNotReady
	} else if err != nil {
		return fmt.Errorf("report export: authorize legacy run: %w", err)
	}
	return nil
}

type frozenExportQueryColumn struct {
	FieldID           string          `json:"fieldId"`
	LogicalCode       string          `json:"logicalCode"`
	DatabaseColumn    string          `json:"databaseColumn"`
	ValueType         string          `json:"valueType"`
	SourceOracleType  string          `json:"sourceOracleType"`
	Precision         *int            `json:"precision,omitempty"`
	Scale             *int            `json:"scale,omitempty"`
	Nullable          bool            `json:"nullable"`
	PreviewHeader     string          `json:"previewHeader"`
	ExcelHeader       string          `json:"excelHeader"`
	DisplayOrder      int             `json:"displayOrder"`
	ExportOrder       int             `json:"exportOrder"`
	PreviewVisible    bool            `json:"previewVisible"`
	ExportVisible     bool            `json:"exportVisible"`
	Filterable        bool            `json:"filterable"`
	Sortable          bool            `json:"sortable"`
	ExportAllowed     bool            `json:"exportAllowed"`
	AllowedOperators  json.RawMessage `json:"allowedOperators,omitempty"`
	Format            json.RawMessage `json:"format,omitempty"`
	DictionaryVersion json.RawMessage `json:"dictionaryVersion,omitempty"`
	MaskingPolicy     json.RawMessage `json:"maskingPolicy,omitempty"`
	ExcelWidth        float64         `json:"excelWidth"`
	NullDisplay       string          `json:"nullDisplay"`
}

func FrozenExportQueryColumns(raw model.JSONText) ([]reportquery.Column, error) {
	columns, err := decodeFrozenExportQueryColumns(raw)
	if err != nil {
		return nil, err
	}
	result := make([]reportquery.Column, 0, len(columns))
	for _, column := range columns {
		var operators []string
		if len(bytes.TrimSpace(column.AllowedOperators)) > 0 && json.Unmarshal(column.AllowedOperators, &operators) != nil {
			return nil, fmt.Errorf("report export: invalid frozen operators")
		}
		masked := len(bytes.TrimSpace(column.MaskingPolicy)) > 0 && !bytes.Equal(bytes.TrimSpace(column.MaskingPolicy), []byte("{}")) && !bytes.Equal(bytes.TrimSpace(column.MaskingPolicy), []byte("null"))
		result = append(result, reportquery.Column{FieldID: column.FieldID, LogicalCode: column.LogicalCode, DatabaseColumn: column.DatabaseColumn, ValueType: column.ValueType, SourceOracleType: column.SourceOracleType, Nullable: column.Nullable, Filterable: column.PreviewVisible && column.Filterable && !masked, Sortable: column.PreviewVisible && column.Sortable && !masked, AllowedOperators: operators})
	}
	return result, nil
}

func decodeFrozenExportQueryColumns(raw model.JSONText) ([]frozenExportQueryColumn, error) {
	var columns []frozenExportQueryColumn
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&columns); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("report export: frozen columns contain trailing data")
	}
	return columns, nil
}

func validReportExportOutbox(outbox model.AsyncJobOutbox, exportUUID string) bool {
	return outbox.ID == 0 && outbox.TaskKey == "report:export:"+exportUUID && outbox.TaskType == "report:export" &&
		outbox.QueueName == "report_export" && !outbox.AvailableAt.IsZero() && outbox.PublishedAt == nil && string(outbox.PayloadJSON) == `{"export_id":0}`
}

func NewReportExportOutbox(exportUUID string, now time.Time) model.AsyncJobOutbox {
	return model.AsyncJobOutbox{TaskKey: "report:export:" + exportUUID, TaskType: "report:export", PayloadJSON: model.JSONText(`{"export_id":0}`), QueueName: "report_export", AvailableAt: now.UTC()}
}
