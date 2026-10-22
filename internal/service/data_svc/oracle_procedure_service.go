package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gin-biz-web-api/internal/reportoracle"
)

type OracleProcedureParameter struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	DataType  string `json:"dataType"`
	Value     string `json:"value"`
}

type OracleProcedureSuccess struct {
	Parameter string `json:"parameter"`
	Operator  string `json:"operator"`
	Value     string `json:"value"`
}

type OracleProcedureConfig struct {
	Operation     string                     `json:"operation"`
	Owner         string                     `json:"owner"`
	PackageName   string                     `json:"packageName"`
	ProcedureName string                     `json:"procedureName"`
	Parameters    []OracleProcedureParameter `json:"parameters"`
	Success       OracleProcedureSuccess     `json:"success"`
}

func oracleOperationProcedureRef(config OracleProcedureConfig) reportoracle.ProcedureRef {
	return reportoracle.ProcedureRef{Owner: config.Owner, Package: config.PackageName, Name: config.ProcedureName}
}

func NormalizeOracleProcedureConfig(ctx context.Context, input OracleProcedureConfig) (OracleProcedureConfig, error) {
	var normalized OracleProcedureConfig
	err := withOracleOperationConnection(ctx, 30*time.Second, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		arguments, err := connection.InspectProcedure(queryCtx, oracleOperationProcedureRef(input))
		if err != nil {
			return err
		}
		normalized, err = normalizeOracleProcedureWithMetadata(input, arguments)
		return err
	})
	return normalized, err
}

func normalizeOracleProcedureWithMetadata(input OracleProcedureConfig, arguments []reportoracle.ProcedureArgument) (OracleProcedureConfig, error) {
	ref, err := reportoracle.NormalizeProcedureRef(oracleOperationProcedureRef(input))
	if err != nil {
		return input, err
	}
	input.Operation, input.Owner, input.PackageName, input.ProcedureName = ExcelOperationOracleProcedure, ref.Owner, ref.Package, ref.Name
	if len(input.Parameters) > 200 {
		return input, errors.New("存储过程参数不得超过 200 个")
	}
	input.Parameters = append([]OracleProcedureParameter{}, input.Parameters...)
	available := make(map[string]reportoracle.ProcedureArgument, len(arguments))
	for _, argument := range arguments {
		available[strings.ToUpper(argument.Name)] = argument
	}
	seen, outputs := make(map[string]bool), make(map[string]bool)
	for index := range input.Parameters {
		parameter := &input.Parameters[index]
		parameter.Name = strings.ToUpper(strings.TrimSpace(parameter.Name))
		parameter.Direction = reportoracle.NormalizeOperationDirection(parameter.Direction)
		argument, exists := available[parameter.Name]
		if !exists || seen[parameter.Name] {
			return input, fmt.Errorf("存储过程参数不存在或重复: %s", parameter.Name)
		}
		seen[parameter.Name] = true
		actualDirection := reportoracle.NormalizeOperationDirection(argument.Direction)
		if parameter.Direction != actualDirection {
			return input, fmt.Errorf("参数 %s 的方向与 Oracle 不符", parameter.Name)
		}
		kind, err := reportoracle.NormalizeOperationDataType(parameter.DataType)
		if err != nil {
			return input, err
		}
		actualKind, err := reportoracle.NormalizeOperationDataType(argument.DataType)
		if err != nil || kind != actualKind || argument.TypeName != "" || argument.TypeOwner != "" {
			return input, fmt.Errorf("参数 %s 的类型与 Oracle 不符或暂不支持", parameter.Name)
		}
		parameter.DataType = kind
		switch parameter.Direction {
		case "IN", "IN OUT":
			if _, err := reportoracle.OperationValue(kind, parameter.Value); err != nil {
				return input, fmt.Errorf("参数 %s: %w", parameter.Name, err)
			}
		case "OUT":
			parameter.Value = ""
		default:
			return input, fmt.Errorf("参数 %s 的方向无效", parameter.Name)
		}
		outputs[parameter.Name] = parameter.Direction != "IN"
	}
	for _, argument := range arguments {
		if !seen[strings.ToUpper(argument.Name)] && !(reportoracle.NormalizeOperationDirection(argument.Direction) == "IN" && argument.Defaulted) {
			return input, fmt.Errorf("缺少存储过程参数: %s", argument.Name)
		}
	}
	input.Success.Parameter = strings.ToUpper(strings.TrimSpace(input.Success.Parameter))
	input.Success.Operator = strings.ToLower(strings.TrimSpace(input.Success.Operator))
	input.Success.Value = strings.TrimSpace(input.Success.Value)
	if input.Success.Parameter != "" {
		if !outputs[input.Success.Parameter] {
			return input, errors.New("成功标志必须引用 OUT 或 IN OUT 参数")
		}
		if input.Success.Operator == "" {
			input.Success.Operator = "eq"
		}
		if input.Success.Operator != "eq" && input.Success.Operator != "contains" {
			return input, errors.New("成功标志仅支持 eq 或 contains")
		}
		if input.Success.Operator == "contains" && input.Success.Value == "" {
			return input, errors.New("包含成功标志的比较值不能为空")
		}
	} else if input.Success.Operator != "" || input.Success.Value != "" {
		return input, errors.New("请设置成功标志的输出参数")
	}
	return input, nil
}

func checkOracleProcedureSuccess(success OracleProcedureSuccess, outputs map[string]string) error {
	if success.Parameter == "" {
		return nil
	}
	actual, exists := outputs[success.Parameter]
	if !exists {
		return errors.New("存储过程未返回成功标志参数")
	}
	actual = strings.TrimSpace(actual)
	matched := actual == success.Value
	if success.Operator == "contains" {
		matched = strings.Contains(actual, success.Value)
	}
	if !matched {
		return fmt.Errorf("存储过程成功标志不符，%s 返回 %q", success.Parameter, actual)
	}
	return nil
}

func ExecuteOracleProcedure(ctx context.Context, config OracleProcedureConfig) (map[string]string, error) {
	var outputs map[string]string
	err := withOracleOperationConnection(ctx, 30*time.Minute, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
		var err error
		outputs, err = executeOracleProcedureOnConnection(queryCtx, connection, config)
		return err
	})
	return outputs, err
}

func executeOracleProcedureOnConnection(ctx context.Context, connection oracleOperationConnection, config OracleProcedureConfig) (map[string]string, error) {
	arguments, err := connection.InspectProcedure(ctx, oracleOperationProcedureRef(config))
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeOracleProcedureWithMetadata(config, arguments)
	if err != nil {
		return nil, err
	}
	parameters := make([]reportoracle.ProcedureValue, 0, len(normalized.Parameters))
	for _, parameter := range normalized.Parameters {
		parameters = append(parameters, reportoracle.ProcedureValue{Name: parameter.Name, Direction: parameter.Direction, DataType: parameter.DataType, Value: parameter.Value})
	}
	tx, err := connection.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	outputs, err := connection.ExecuteOperationProcedure(ctx, tx, oracleOperationProcedureRef(normalized), parameters)
	if err != nil {
		return nil, err
	}
	if err := checkOracleProcedureSuccess(normalized.Success, outputs); err != nil {
		return outputs, err
	}
	if err := tx.Commit(); err != nil {
		return outputs, fmt.Errorf("commit Oracle procedure: %w", err)
	}
	return outputs, nil
}
