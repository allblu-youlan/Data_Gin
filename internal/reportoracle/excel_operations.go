package reportoracle

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/godror/godror"
)

// ProcedureValue describes one explicitly bound procedure argument.
type ProcedureValue struct {
	Name      string
	Direction string
	DataType  string
	Value     string
}

var operationNumberPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func NormalizeOperationDataType(dataType string) (string, error) {
	dataType = strings.ToUpper(strings.Join(strings.Fields(dataType), " "))
	if strings.HasPrefix(dataType, "TIMESTAMP") {
		return "TIMESTAMP", nil
	}
	switch dataType {
	case "VARCHAR2", "NVARCHAR2", "CHAR", "NCHAR", "CLOB", "NCLOB", "LONG", "NUMBER", "FLOAT", "BINARY_FLOAT", "BINARY_DOUBLE", "INTEGER", "DATE":
		return dataType, nil
	default:
		return "", fmt.Errorf("%w: unsupported data type %q", ErrUnsupportedBinding, dataType)
	}
}

// OperationValue preserves decimal precision and binds dates without relying on NLS settings.
func OperationValue(dataType, value string) (interface{}, error) {
	kind, err := NormalizeOperationDataType(dataType)
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, nil
	}
	switch kind {
	case "NUMBER", "FLOAT", "BINARY_FLOAT", "BINARY_DOUBLE", "INTEGER":
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil
		}
		if !operationNumberPattern.MatchString(value) {
			return nil, fmt.Errorf("invalid Oracle number %q", value)
		}
		return godror.Number(value), nil
	case "DATE", "TIMESTAMP":
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil
		}
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return nil, err
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999", "2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02", "2006/1/2 15:04:05", "2006/1/2"} {
			if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("invalid Oracle date %q", value)
	case "CLOB", "NCLOB":
		return godror.Lob{Reader: strings.NewReader(value), IsClob: true}, nil
	default:
		return value, nil
	}
}

type procedureOutput struct {
	text   string
	number godror.Number
	date   time.Time
	kind   string
}

func (output *procedureOutput) destination() interface{} {
	switch output.kind {
	case "NUMBER", "FLOAT", "BINARY_FLOAT", "BINARY_DOUBLE", "INTEGER":
		return &output.number
	case "DATE", "TIMESTAMP":
		return &output.date
	default:
		return &output.text
	}
}

func (output *procedureOutput) value() string {
	switch output.kind {
	case "NUMBER", "FLOAT", "BINARY_FLOAT", "BINARY_DOUBLE", "INTEGER":
		return string(output.number)
	case "DATE", "TIMESTAMP":
		if output.date.IsZero() {
			return ""
		}
		return output.date.Format("2006-01-02 15:04:05.999999999")
	default:
		return output.text
	}
}

func buildOperationProcedureCall(ref ProcedureRef, parameters []ProcedureValue) (string, []interface{}, map[string]*procedureOutput, error) {
	ref, err := NormalizeProcedureRef(ref)
	if err != nil {
		return "", nil, nil, err
	}
	target := ref.Owner + "."
	if ref.Package != "" {
		target += ref.Package + "."
	}
	target += ref.Name
	bindings := make([]string, 0, len(parameters))
	arguments := make([]interface{}, 0, len(parameters))
	outputs := make(map[string]*procedureOutput)
	seen := make(map[string]bool)
	for index, parameter := range parameters {
		name, err := normalizeIdentifier(parameter.Name, "procedure argument")
		if err != nil || seen[name] {
			return "", nil, nil, fmt.Errorf("invalid or duplicate procedure argument %q", parameter.Name)
		}
		seen[name] = true
		kind, err := NormalizeOperationDataType(parameter.DataType)
		if err != nil {
			return "", nil, nil, err
		}
		direction := NormalizeOperationDirection(parameter.Direction)
		var value interface{}
		switch direction {
		case "IN":
			value, err = OperationValue(kind, parameter.Value)
		case "OUT", "IN OUT":
			output := &procedureOutput{kind: kind}
			if direction == "IN OUT" {
				input, valueErr := OperationValue(kind, parameter.Value)
				if valueErr != nil {
					return "", nil, nil, valueErr
				}
				switch typed := input.(type) {
				case godror.Number:
					output.number = typed
				case time.Time:
					output.date = typed
				default:
					output.text = parameter.Value
				}
			}
			value = sql.Out{Dest: output.destination(), In: direction == "IN OUT"}
			outputs[name] = output
		default:
			return "", nil, nil, fmt.Errorf("unsupported procedure direction %q", direction)
		}
		if err != nil {
			return "", nil, nil, fmt.Errorf("procedure argument %s: %w", name, err)
		}
		bind := fmt.Sprintf("arg_%d", index+1)
		bindings = append(bindings, name+" => :"+bind)
		arguments = append(arguments, sql.Named(bind, value))
	}
	return "BEGIN " + target + "(" + strings.Join(bindings, ", ") + "); END;", arguments, outputs, nil
}

func NormalizeOperationDirection(direction string) string {
	direction = strings.ToUpper(strings.Join(strings.Fields(strings.ReplaceAll(direction, "/", " ")), " "))
	if direction == "INOUT" {
		return "IN OUT"
	}
	return direction
}

func (adapter *Adapter) ExecuteOperationProcedure(ctx context.Context, tx *sql.Tx, ref ProcedureRef, parameters []ProcedureValue) (map[string]string, error) {
	if adapter == nil || tx == nil {
		return nil, fmt.Errorf("execute Oracle procedure: transaction is required")
	}
	statement, arguments, outputs, err := buildOperationProcedureCall(ref, parameters)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, statement, arguments...); err != nil {
		return nil, mapExecutionError(ctx, err)
	}
	values := make(map[string]string, len(outputs))
	for name, output := range outputs {
		values[name] = output.value()
	}
	return values, nil
}
