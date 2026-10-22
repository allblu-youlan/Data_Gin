package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
)

const (
	ExcelOperationOracleImport    = "oracle_import"
	ExcelOperationOracleProcedure = "oracle_procedure"
)

type ExcelOperationScheme struct {
	ID         uint            `json:"id"`
	Name       string          `json:"name"`
	Operation  string          `json:"operation"`
	Config     json.RawMessage `json:"config"`
	ConfigJSON string          `json:"config_json"`
	CreatedAt  int             `json:"created_at"`
	UpdatedAt  int             `json:"updated_at"`
}

type excelOperationSchemeStore interface {
	ListSchemes(context.Context, string) ([]model.ExcelMatchScheme, error)
	SaveScheme(context.Context, *model.ExcelMatchScheme) error
	GetOperationScheme(context.Context, string, uint) (*model.ExcelMatchScheme, error)
	DeleteOperationScheme(context.Context, string, uint) error
}

type ExcelOperationSchemeService struct {
	store     excelOperationSchemeStore
	normalize func(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

func NewExcelOperationSchemeService() *ExcelOperationSchemeService {
	return &ExcelOperationSchemeService{store: data_dao.NewExcelMatchJobDAO(), normalize: normalizeExcelOperationConfig}
}

func validExcelOperation(operation string) bool {
	return operation == ExcelOperationOracleImport || operation == ExcelOperationOracleProcedure
}

func (service *ExcelOperationSchemeService) List(ctx context.Context, operation string) ([]ExcelOperationScheme, error) {
	if !validExcelOperation(operation) {
		return nil, errors.New("无效的 Oracle 方法类型")
	}
	rows, err := service.store.ListSchemes(ctx, operation)
	if err != nil {
		return nil, fmt.Errorf("list Oracle operation schemes: %w", err)
	}
	result := make([]ExcelOperationScheme, 0, len(rows))
	for _, row := range rows {
		item, err := excelOperationSchemeFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (service *ExcelOperationSchemeService) Save(ctx context.Context, name, operation string, rawConfig json.RawMessage) (*ExcelOperationScheme, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 || !validExcelOperation(operation) {
		return nil, errors.New("方法名称应为 1 至 100 个字符，且方法类型有效")
	}
	config, err := service.normalize(ctx, operation, rawConfig)
	if err != nil {
		return nil, err
	}
	row := &model.ExcelMatchScheme{Name: name, Operation: operation, ConfigJSON: string(config)}
	if err := service.store.SaveScheme(ctx, row); err != nil {
		return nil, fmt.Errorf("save Oracle operation scheme: %w", err)
	}
	return service.Get(ctx, operation, row.ID)
}

func (service *ExcelOperationSchemeService) Get(ctx context.Context, operation string, id uint) (*ExcelOperationScheme, error) {
	if !validExcelOperation(operation) || id == 0 {
		return nil, errors.New("方法类型或 ID 无效")
	}
	row, err := service.store.GetOperationScheme(ctx, operation, id)
	if err != nil {
		return nil, fmt.Errorf("get Oracle operation scheme: %w", err)
	}
	item, err := excelOperationSchemeFromModel(*row)
	return &item, err
}

func (service *ExcelOperationSchemeService) Delete(ctx context.Context, operation string, id uint) error {
	if !validExcelOperation(operation) || id == 0 {
		return errors.New("方法类型或 ID 无效")
	}
	return service.store.DeleteOperationScheme(ctx, operation, id)
}

func excelOperationSchemeFromModel(row model.ExcelMatchScheme) (ExcelOperationScheme, error) {
	if !validExcelOperation(row.Operation) || !json.Valid([]byte(row.ConfigJSON)) {
		return ExcelOperationScheme{}, errors.New("无效的 Oracle 方法配置")
	}
	return ExcelOperationScheme{ID: row.ID, Name: row.Name, Operation: row.Operation, Config: json.RawMessage(row.ConfigJSON), ConfigJSON: row.ConfigJSON, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func normalizeExcelOperationConfig(ctx context.Context, operation string, raw json.RawMessage) (json.RawMessage, error) {
	var config interface{}
	switch operation {
	case ExcelOperationOracleImport:
		var input OracleExcelImportConfig
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, fmt.Errorf("decode Oracle import config: %w", err)
		}
		normalized, err := NormalizeOracleExcelImportConfig(ctx, input)
		if err != nil {
			return nil, err
		}
		config = normalized
	case ExcelOperationOracleProcedure:
		var input OracleProcedureConfig
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, fmt.Errorf("decode Oracle procedure config: %w", err)
		}
		normalized, err := NormalizeOracleProcedureConfig(ctx, input)
		if err != nil {
			return nil, err
		}
		config = normalized
	default:
		return nil, errors.New("无效的 Oracle 方法类型")
	}
	encoded, err := json.Marshal(config)
	return json.RawMessage(encoded), err
}
