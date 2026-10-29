package data_svc

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strings"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
	"gin-biz-web-api/internal/reportsecret"
	"github.com/godror/godror"
	"gorm.io/gorm"
)

type tableEmailOracle interface {
	ListResultTables(context.Context, reportoracle.ResultTableCatalogQuery) ([]reportoracle.ResultTableSummary, error)
	InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error)
	QuerySelect(context.Context, string, ...interface{}) (*sql.Rows, error)
	Close() error
}

type tableEmailOracleOpener func(context.Context, uint) (tableEmailOracle, error)

func newTableEmailOracleOpener(db *gorm.DB) tableEmailOracleOpener {
	return func(ctx context.Context, datasourceID uint) (tableEmailOracle, error) {
		var cfg reportoracle.Config
		if datasourceID == 0 {
			var err error
			cfg, _, err = officeOracleConfigFromEnvironment()
			if err != nil {
				return nil, fmt.Errorf("table email: default oracle configuration: %w", err)
			}
		} else {
			source, err := reportrepo.New(db).GetReportDatasource(ctx, datasourceID)
			if err != nil {
				return nil, fmt.Errorf("table email: get datasource: %w", err)
			}
			if !source.Enabled || source.Driver != "ORACLE" {
				return nil, tableEmailInvalid("Oracle 数据源未启用")
			}
			password, err := (reportsecret.EnvironmentKeyring{}).Decrypt(source.CredentialKeyVersion, source.PasswordCiphertext)
			if err != nil {
				return nil, fmt.Errorf("table email: decrypt datasource: %w", err)
			}
			cfg = oracleConfigFromDatasource(*source, password)
		}
		return reportoracle.Open(ctx, cfg)
	}
}

func tableEmailColumns(ctx context.Context, connection tableEmailOracle, input TableEmailRuleInput) ([]OfficeResultColumn, error) {
	ref, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: input.Owner, Name: input.TableName})
	if err != nil {
		return nil, tableEmailInvalid("Oracle 表名无效")
	}
	columns, err := connection.InspectResultTable(ctx, ref)
	if err != nil {
		return nil, err
	}
	result := make([]OfficeResultColumn, 0, len(columns))
	for _, column := range columns {
		result = append(result, OfficeResultColumn{Name: column.Name, Comment: column.Comment, Position: column.Position, DataType: column.DataType, Nullable: column.Nullable})
	}
	return result, nil
}

// A unique, non-null integer key makes each scan position unambiguous. Values
// must increase on insertion; the UI states this requirement explicitly.
func validateTableEmailOracleCursor(ctx context.Context, connection tableEmailOracle, input TableEmailRuleInput) error {
	rows, err := connection.QuerySelect(ctx, `SELECT COUNT(*) FROM ALL_TAB_COLUMNS c
 WHERE c.OWNER = :owner AND c.TABLE_NAME = :table_name AND c.COLUMN_NAME = :column_name
 AND c.DATA_TYPE = 'NUMBER' AND NVL(c.DATA_SCALE, 0) = 0 AND c.NULLABLE = 'N'
 AND EXISTS (SELECT 1 FROM ALL_CONSTRAINTS k JOIN ALL_CONS_COLUMNS kc
 ON kc.OWNER = k.OWNER AND kc.CONSTRAINT_NAME = k.CONSTRAINT_NAME
 WHERE k.OWNER = c.OWNER AND k.TABLE_NAME = c.TABLE_NAME AND k.CONSTRAINT_TYPE IN ('P', 'U')
 AND k.STATUS = 'ENABLED' AND k.VALIDATED = 'VALIDATED' AND kc.COLUMN_NAME = c.COLUMN_NAME
 AND (SELECT COUNT(*) FROM ALL_CONS_COLUMNS cc WHERE cc.OWNER = k.OWNER AND cc.CONSTRAINT_NAME = k.CONSTRAINT_NAME) = 1)`,
		sql.Named("owner", input.Owner), sql.Named("table_name", input.TableName), sql.Named("column_name", input.CursorColumn))
	if err != nil {
		return err
	}
	defer rows.Close()
	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != 1 {
		return tableEmailInvalid("增量字段需要非空整数 NUMBER 列，并具有单列主键或唯一约束")
	}
	return nil
}

func normalizeTableEmailCursor(value string) (string, error) {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || n.Sign() < 0 || len(n.String()) > 38 {
		return "", tableEmailInvalid("增量字段值必须为非负整数")
	}
	return n.String(), nil
}

func tableEmailQualifiedTable(input TableEmailRuleInput) (string, string, error) {
	ref, err := reportoracle.NormalizeResultTableRef(reportoracle.ResultTableRef{Owner: input.Owner, Name: input.TableName})
	column := strings.ToUpper(input.CursorColumn)
	if err != nil || !tableEmailIdentifierPattern.MatchString(column) {
		return "", "", tableEmailInvalid("Oracle 表名或字段名无效")
	}
	return `"` + ref.Owner + `"."` + ref.Name + `"`, `"` + column + `"`, nil
}

func tableEmailIncrementalSQL(input TableEmailRuleInput, fields []string) (string, error) {
	table, cursor, err := tableEmailQualifiedTable(input)
	if err != nil {
		return "", err
	}
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		if !tableEmailIdentifierPattern.MatchString(field) {
			return "", tableEmailInvalid("Oracle 字段名无效")
		}
		columns = append(columns, `"`+strings.ToUpper(field)+`"`)
	}
	if len(columns) == 0 {
		return "", tableEmailInvalid("未选择字段")
	}
	return `SELECT * FROM (SELECT ` + strings.Join(columns, ", ") + ` FROM ` + table + ` WHERE ` + cursor + ` > :after_id ORDER BY ` + cursor + ` ASC) WHERE ROWNUM <= :batch_size`, nil
}

func tableEmailHighWater(ctx context.Context, connection tableEmailOracle, input TableEmailRuleInput) (string, error) {
	table, column, err := tableEmailQualifiedTable(input)
	if err != nil {
		return "", err
	}
	rows, err := connection.QuerySelect(ctx, `SELECT NVL(MAX(`+column+`), 0) FROM `+table, godror.NumberAsString())
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var value string
	if !rows.Next() {
		return "", fmt.Errorf("table email: no high watermark: %w", rows.Err())
	}
	if err := rows.Scan(&value); err != nil {
		return "", err
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return normalizeTableEmailCursor(value)
}

func tableEmailReadRows(ctx context.Context, connection tableEmailOracle, input TableEmailRuleInput, after string) ([]map[string]any, error) {
	fields := []string{input.CursorColumn}
	seen := map[string]bool{input.CursorColumn: true}
	for _, template := range []string{input.RecipientsTemplate, input.SubjectTemplate, input.BodyTemplate} {
		referenced, err := tableEmailFields(template)
		if err != nil {
			return nil, err
		}
		for _, field := range referenced {
			if !seen[field] {
				fields = append(fields, field)
				seen[field] = true
			}
		}
	}
	statement, err := tableEmailIncrementalSQL(input, fields)
	if err != nil {
		return nil, err
	}
	if after, err = normalizeTableEmailCursor(after); err != nil {
		return nil, err
	}
	rows, err := connection.QuerySelect(ctx, statement, sql.Named("after_id", godror.Number(after)), sql.Named("batch_size", 100), godror.NumberAsString())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(fields))
		pointers := make([]any, len(fields))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		record := make(map[string]any, len(fields))
		for i, field := range fields {
			if bytes, ok := values[i].([]byte); ok {
				values[i] = string(bytes)
			}
			record[field] = values[i]
		}
		result = append(result, record)
	}
	return result, rows.Err()
}
