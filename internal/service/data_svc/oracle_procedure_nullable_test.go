package data_svc

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"gin-biz-web-api/internal/reportoracle"
)

type recordingOracleProcedureConnection struct {
	oracleOperationsFakeConnection
	parameters []reportoracle.ProcedureValue
}

func (connection *recordingOracleProcedureConnection) ExecuteOperationProcedure(_ context.Context, _ *sql.Tx, _ reportoracle.ProcedureRef, parameters []reportoracle.ProcedureValue) (map[string]string, error) {
	connection.parameters = parameters
	return connection.outputs, nil
}

func TestOracleProcedureEmptyParametersCanBeSavedAndExecuted(t *testing.T) {
	for _, test := range []struct {
		name       string
		arguments  []reportoracle.ProcedureArgument
		parameters []OracleProcedureParameter
	}{
		{
			name:       "reported output-only procedure",
			arguments:  []reportoracle.ProcedureArgument{{Name: "R_MESSAGE", Direction: "OUT", DataType: "VARCHAR2"}},
			parameters: []OracleProcedureParameter{{Name: "R_MESSAGE", Direction: "OUT", DataType: "VARCHAR2"}},
		},
		{
			name: "missing inputs bind null and optional inputs use defaults",
			arguments: []reportoracle.ProcedureArgument{
				{Name: "P_CODE", Direction: "IN", DataType: "VARCHAR2"},
				{Name: "P_DATE", Direction: "IN", DataType: "DATE"},
				{Name: "P_COUNT", Direction: "IN/OUT", DataType: "NUMBER"},
				{Name: "P_DEFAULT", Direction: "IN", DataType: "NUMBER", Defaulted: true},
				{Name: "R_MESSAGE", Direction: "OUT", DataType: "VARCHAR2"},
			},
			parameters: []OracleProcedureParameter{
				{Name: "P_CODE", Direction: "IN", DataType: "VARCHAR2"},
				{Name: "P_DATE", Direction: "IN", DataType: "DATE"},
				{Name: "P_COUNT", Direction: "IN OUT", DataType: "NUMBER"},
				{Name: "R_MESSAGE", Direction: "OUT", DataType: "VARCHAR2"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"operation":"oracle_procedure","owner":"YL_DBS","packageName":"","parameters":[],"procedureName":"ADS_SALES_PRODUCT_REPORT_UPDATE_V3","success":{"operator":"eq","parameter":"","value":""}}`)
			service := &ExcelOperationSchemeService{store: &operationSchemeMemoryStore{}, normalize: func(_ context.Context, _ string, raw json.RawMessage) (json.RawMessage, error) {
				var input OracleProcedureConfig
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, err
				}
				normalized, err := normalizeOracleProcedureWithMetadata(input, test.arguments)
				if err != nil {
					return nil, err
				}
				return json.Marshal(normalized)
			}}
			saved, err := service.Save(t.Context(), "商品周报数据存储过程执行", ExcelOperationOracleProcedure, raw)
			if err != nil {
				t.Fatal(err)
			}
			var config OracleProcedureConfig
			if err := json.Unmarshal(saved.Config, &config); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(config.Parameters, test.parameters) {
				t.Fatalf("saved parameters = %+v, want %+v", config.Parameters, test.parameters)
			}
			state := &oracleOperationsTestState{}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			connection := &recordingOracleProcedureConnection{oracleOperationsFakeConnection: oracleOperationsFakeConnection{db: db, arguments: test.arguments, outputs: map[string]string{"R_MESSAGE": ""}}}
			if _, err := executeOracleProcedureOnConnection(t.Context(), connection, config); err != nil {
				t.Fatal(err)
			}
			if state.commits != 1 || state.rollbacks != 0 || len(connection.parameters) != len(test.parameters) {
				t.Fatalf("transaction = %+v, bound parameters = %+v", state, connection.parameters)
			}
			for index, parameter := range connection.parameters {
				want := test.parameters[index]
				if parameter.Name != want.Name || parameter.Direction != want.Direction || parameter.DataType != want.DataType || parameter.Value != "" {
					t.Fatalf("unexpected binding: %+v", parameter)
				}
			}
		})
	}
}
