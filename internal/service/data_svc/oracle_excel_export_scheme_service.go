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

type OracleExcelExportScheme struct {
	ID         uint                    `json:"id"`
	Name       string                  `json:"name"`
	Operation  string                  `json:"operation"`
	Config     OracleExcelExportConfig `json:"config"`
	ConfigJSON string                  `json:"config_json"`
	CreatedAt  int                     `json:"created_at"`
	UpdatedAt  int                     `json:"updated_at"`
}

type oracleExcelExportSchemeStore interface {
	ListSchemes(context.Context, string) ([]model.ExcelMatchScheme, error)
	SaveScheme(context.Context, *model.ExcelMatchScheme) error
	DeleteOracleExportScheme(context.Context, uint) error
}

type OracleExcelExportSchemeService struct {
	store     oracleExcelExportSchemeStore
	normalize func(context.Context, OracleExcelExportConfig) (OracleExcelExportConfig, error)
}

func NewOracleExcelExportSchemeService() *OracleExcelExportSchemeService {
	return &OracleExcelExportSchemeService{store: data_dao.NewExcelMatchJobDAO(), normalize: normalizeOracleExcelExportConfig}
}

func (service *OracleExcelExportSchemeService) List(ctx context.Context) ([]OracleExcelExportScheme, error) {
	rows, err := service.store.ListSchemes(ctx, excelOperationOracleExport)
	if err != nil {
		return nil, fmt.Errorf("list Oracle export configurations: %w", err)
	}
	schemes := make([]OracleExcelExportScheme, 0, len(rows))
	for _, row := range rows {
		scheme, err := oracleExcelExportSchemeFromModel(row)
		if err != nil {
			return nil, err
		}
		schemes = append(schemes, scheme)
	}
	return schemes, nil
}

func (service *OracleExcelExportSchemeService) Save(ctx context.Context, name string, input OracleExcelExportConfig) (*OracleExcelExportScheme, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return nil, errors.New("导出配置名称应为 1 至 100 个字符")
	}
	config, err := service.normalize(ctx, input)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode Oracle export configuration: %w", err)
	}
	row := &model.ExcelMatchScheme{Name: name, Operation: excelOperationOracleExport, ConfigJSON: string(encoded)}
	if err := service.store.SaveScheme(ctx, row); err != nil {
		return nil, fmt.Errorf("save Oracle export configuration: %w", err)
	}
	// SaveScheme updates by operation and name, so read back the canonical ID
	// for both a new configuration and an update of an existing one.
	schemes, err := service.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, scheme := range schemes {
		if scheme.ID == row.ID {
			return &scheme, nil
		}
	}
	return nil, errors.New("saved Oracle export configuration was not found")
}

func (service *OracleExcelExportSchemeService) Delete(ctx context.Context, id uint) error {
	if id == 0 {
		return errors.New("导出配置 ID 不能为空")
	}
	return service.store.DeleteOracleExportScheme(ctx, id)
}

func oracleExcelExportSchemeFromModel(row model.ExcelMatchScheme) (OracleExcelExportScheme, error) {
	var config OracleExcelExportConfig
	if row.Operation != excelOperationOracleExport {
		return OracleExcelExportScheme{}, errors.New("invalid Oracle export configuration type")
	}
	if err := json.Unmarshal([]byte(row.ConfigJSON), &config); err != nil {
		return OracleExcelExportScheme{}, fmt.Errorf("decode Oracle export configuration: %w", err)
	}
	return OracleExcelExportScheme{ID: row.ID, Name: row.Name, Operation: row.Operation, Config: config, ConfigJSON: row.ConfigJSON, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}
