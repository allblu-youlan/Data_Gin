package data_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
)

type officeOracleMetadataConnection interface {
	ListProcedures(context.Context, reportoracle.ProcedureCatalogQuery) ([]reportoracle.ProcedureSummary, error)
	InspectProcedure(context.Context, reportoracle.ProcedureRef) ([]reportoracle.ProcedureArgument, error)
	ListResultTables(context.Context, reportoracle.ResultTableCatalogQuery) ([]reportoracle.ResultTableSummary, error)
	InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error)
	QuerySelect(context.Context, string, ...interface{}) (*sql.Rows, error)
	Close() error
}

type officeOracleMetadataOpener func(context.Context, reportoracle.Config) (officeOracleMetadataConnection, error)

type OfficeSelectTestInput struct {
	SelectSQL  string                 `json:"selectSql"`
	Parameters []OfficeQueryParameter `json:"parameters"`
	Values     map[string]string      `json:"values"`
}

type OfficeSelectColumn struct {
	Name         string `json:"name"`
	DatabaseType string `json:"databaseType"`
	Nullable     bool   `json:"nullable"`
}

type OfficeProcedureSummary struct {
	Owner         string `json:"owner"`
	PackageName   string `json:"packageName"`
	Name          string `json:"name"`
	Overload      string `json:"overload"`
	ArgumentCount int    `json:"argumentCount"`
}

type OfficeProcedureArgument struct {
	Name      string `json:"name"`
	Position  int    `json:"position"`
	Direction string `json:"direction"`
	DataType  string `json:"dataType"`
	Defaulted bool   `json:"defaulted"`
}

type OfficeResultTableSummary struct {
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	ColumnCount int    `json:"columnCount"`
}

type OfficeResultColumn struct {
	Name     string `json:"name"`
	Comment  string `json:"comment"`
	Position int    `json:"position"`
	DataType string `json:"dataType"`
	Nullable bool   `json:"nullable"`
}

type OfficeOracleMetadataService struct {
	open officeOracleMetadataOpener
}

func NewOfficeOracleMetadataService() *OfficeOracleMetadataService {
	return &OfficeOracleMetadataService{open: func(ctx context.Context, config reportoracle.Config) (officeOracleMetadataConnection, error) {
		return reportoracle.Open(ctx, config)
	}}
}

func (service *OfficeOracleMetadataService) ListProcedures(ctx context.Context, owner, search string, limit int) ([]OfficeProcedureSummary, error) {
	limit = normalizeOfficeMetadataLimit(limit)
	var result []OfficeProcedureSummary
	err := service.withConnection(ctx, func(queryCtx context.Context, connection officeOracleMetadataConnection) error {
		items, err := connection.ListProcedures(queryCtx, reportoracle.ProcedureCatalogQuery{Owner: owner, Search: search, Limit: limit})
		if err != nil {
			return err
		}
		result = make([]OfficeProcedureSummary, 0, len(items))
		for _, item := range items {
			result = append(result, OfficeProcedureSummary{Owner: item.Owner, PackageName: item.Package, Name: item.Name, Overload: item.Overload, ArgumentCount: item.ArgumentCount})
		}
		return nil
	})
	return result, err
}

func (service *OfficeOracleMetadataService) ProcedureSignature(ctx context.Context, ref reportoracle.ProcedureRef) ([]OfficeProcedureArgument, error) {
	var result []OfficeProcedureArgument
	err := service.withConnection(ctx, func(queryCtx context.Context, connection officeOracleMetadataConnection) error {
		items, err := connection.InspectProcedure(queryCtx, ref)
		if err != nil {
			return err
		}
		result = make([]OfficeProcedureArgument, 0, len(items))
		for _, item := range items {
			result = append(result, OfficeProcedureArgument{Name: item.Name, Position: item.Position, Direction: item.Direction, DataType: item.DataType, Defaulted: item.Defaulted})
		}
		return nil
	})
	return result, err
}

func (service *OfficeOracleMetadataService) ListResultTables(ctx context.Context, owner, search string, limit int) ([]OfficeResultTableSummary, error) {
	limit = normalizeOfficeMetadataLimit(limit)
	var result []OfficeResultTableSummary
	err := service.withConnection(ctx, func(queryCtx context.Context, connection officeOracleMetadataConnection) error {
		items, err := connection.ListResultTables(queryCtx, reportoracle.ResultTableCatalogQuery{Owner: owner, Search: search, Limit: limit})
		if err != nil {
			return err
		}
		result = make([]OfficeResultTableSummary, 0, len(items))
		for _, item := range items {
			result = append(result, OfficeResultTableSummary{Owner: item.Owner, Name: item.Name, ColumnCount: item.ColumnCount})
		}
		return nil
	})
	return result, err
}

func (service *OfficeOracleMetadataService) ResultTableSchema(ctx context.Context, ref reportoracle.ResultTableRef) ([]OfficeResultColumn, error) {
	var result []OfficeResultColumn
	err := service.withConnection(ctx, func(queryCtx context.Context, connection officeOracleMetadataConnection) error {
		items, err := connection.InspectResultTable(queryCtx, ref)
		if err != nil {
			return err
		}
		result = make([]OfficeResultColumn, 0, len(items))
		for _, item := range items {
			result = append(result, OfficeResultColumn{Name: item.Name, Comment: item.Comment, Position: item.Position, DataType: item.DataType, Nullable: item.Nullable})
		}
		return nil
	})
	return result, err
}

func normalizeOfficeMetadataLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 50
	}
	return limit
}

func (service *OfficeOracleMetadataService) TestSelect(ctx context.Context, input OfficeSelectTestInput) ([]OfficeSelectColumn, error) {
	parameterRaw, err := jsonMarshalOffice(input.Parameters)
	if err != nil {
		return nil, err
	}
	schema, _, err := normalizeOfficeQueryParameters(input.SelectSQL, parameterRaw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
	}
	_, arguments, err := normalizeOfficeParameterValues(schema, input.Values)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOfficeMessageInvalid, err)
	}
	var result []OfficeSelectColumn
	err = service.withConnection(ctx, func(queryCtx context.Context, connection officeOracleMetadataConnection) error {
		rows, err := connection.QuerySelect(queryCtx, input.SelectSQL, arguments...)
		if err != nil {
			return err
		}
		defer rows.Close()
		columnTypes, err := rows.ColumnTypes()
		if err != nil {
			return fmt.Errorf("office message metadata: read SELECT columns: %w", err)
		}
		result = make([]OfficeSelectColumn, 0, len(columnTypes))
		seen := make(map[string]struct{}, len(columnTypes))
		for _, column := range columnTypes {
			name := strings.ToUpper(strings.TrimSpace(column.Name()))
			if name == "" {
				return fmt.Errorf("office message metadata: SELECT returned an unnamed column")
			}
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("office message metadata: SELECT returned duplicate columns")
			}
			seen[name] = struct{}{}
			nullable, ok := column.Nullable()
			result = append(result, OfficeSelectColumn{Name: name, DatabaseType: column.DatabaseTypeName(), Nullable: ok && nullable})
		}
		if len(result) == 0 || len(result) > officeMessageMaxColumns {
			return fmt.Errorf("office message metadata: SELECT column count is invalid")
		}
		return rows.Err()
	})
	return result, err
}

func (service *OfficeOracleMetadataService) withConnection(ctx context.Context, action func(context.Context, officeOracleMetadataConnection) error) error {
	if service == nil || service.open == nil || ctx == nil || action == nil {
		return fmt.Errorf("office message metadata: invalid request")
	}
	config, queryTimeout, err := officeOracleConfigFromEnvironment()
	if err != nil {
		return err
	}
	openCtx, cancel := context.WithTimeout(ctx, config.ConnectTimeout)
	connection, err := service.open(openCtx, config)
	cancel()
	if err != nil {
		return err
	}
	defer connection.Close()
	queryCtx, queryCancel := context.WithTimeout(ctx, maxOfficeMetadataTimeout(queryTimeout))
	defer queryCancel()
	return action(queryCtx, connection)
}

func maxOfficeMetadataTimeout(configured time.Duration) time.Duration {
	const maximum = 30 * time.Second
	if configured <= 0 || configured > maximum {
		return maximum
	}
	return configured
}

func jsonMarshalOffice(value interface{}) (model.JSONText, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("office message metadata: encode request: %w", err)
	}
	return model.JSONText(encoded), nil
}
