package data_svc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gin-biz-web-api/internal/reportoracle"
)

const oracleExcelMaxSheets = 20

type OracleExcelExportSheet struct {
	SheetName string                    `json:"sheetName"`
	Owner     string                    `json:"owner"`
	TableName string                    `json:"tableName"`
	Columns   []OracleExcelExportColumn `json:"columns"`
	Filters   []OracleExcelExportFilter `json:"filters"`
}

func (sheet OracleExcelExportSheet) tableConfig() OracleExcelExportConfig {
	return OracleExcelExportConfig{Operation: excelOperationOracleExport, Owner: sheet.Owner, TableName: sheet.TableName, Columns: sheet.Columns, Filters: sheet.Filters}
}

func oracleExcelExportSheets(input OracleExcelExportConfig) ([]OracleExcelExportSheet, bool, error) {
	if input.Sheets == nil {
		return []OracleExcelExportSheet{{SheetName: "数据", Owner: input.Owner, TableName: input.TableName, Columns: input.Columns, Filters: input.Filters}}, true, nil
	}
	if len(input.Sheets) == 0 || len(input.Sheets) > oracleExcelMaxSheets {
		return nil, false, fmt.Errorf("导出须包含 1 至 %d 个 Sheet", oracleExcelMaxSheets)
	}
	sheets := append([]OracleExcelExportSheet(nil), input.Sheets...)
	seen := make(map[string]bool, len(sheets))
	for index := range sheets {
		sheet := &sheets[index]
		name := strings.TrimSpace(sheet.SheetName)
		if name == "" {
			base := sanitizeMallWeatherExportSheetName(sheet.TableName)
			name = base
			for suffix := 2; seen[strings.ToLower(name)]; suffix++ {
				tail := "_" + strconv.Itoa(suffix)
				name = truncateMallWeatherExportRunes(base, 31-len(tail)) + tail
			}
		}
		if utf8.RuneCountInString(name) > 31 || strings.ContainsAny(name, `[]:*?/\`) || strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, false, fmt.Errorf("Sheet 名称 %q 无效，须不超过 31 字符且不含 []:*?/\\", name)
		}
		if seen[strings.ToLower(name)] {
			return nil, false, fmt.Errorf("Sheet 名称重复: %s", name)
		}
		seen[strings.ToLower(name)] = true
		sheet.SheetName = name
	}
	return sheets, false, nil
}

func normalizeOracleExcelExportWithMetadata(ctx context.Context, input OracleExcelExportConfig, inspect func(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error)) (OracleExcelExportConfig, error) {
	sheets, legacy, err := oracleExcelExportSheets(input)
	if err != nil {
		return input, err
	}
	for index := range sheets {
		sheet := &sheets[index]
		schema, err := inspect(ctx, reportoracle.ResultTableRef{Owner: sheet.Owner, Name: sheet.TableName})
		if err != nil {
			return input, fmt.Errorf("Sheet %s: %w", sheet.SheetName, err)
		}
		normalized, _, _, err := buildOracleExcelExportQuery(sheet.tableConfig(), schema)
		if err != nil {
			return input, fmt.Errorf("Sheet %s: %w", sheet.SheetName, err)
		}
		if legacy {
			return normalized, nil
		}
		sheet.Owner, sheet.TableName, sheet.Columns, sheet.Filters = normalized.Owner, normalized.TableName, normalized.Columns, normalized.Filters
	}
	return OracleExcelExportConfig{Operation: excelOperationOracleExport, Sheets: sheets}, nil
}

func oracleExcelExportSourceName(config OracleExcelExportConfig) string {
	if len(config.Sheets) == 0 {
		return config.Owner + "." + config.TableName
	}
	first := config.Sheets[0]
	name := first.Owner + "." + first.TableName
	if len(config.Sheets) > 1 {
		name += fmt.Sprintf(" 等 %d 张表", len(config.Sheets))
	}
	return truncateMallWeatherExportRunes(name, 255)
}
