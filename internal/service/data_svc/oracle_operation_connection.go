package data_svc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"
)

type oracleOperationConnection interface {
	InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error)
	InspectProcedure(context.Context, reportoracle.ProcedureRef) ([]reportoracle.ProcedureArgument, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecuteOperationProcedure(context.Context, *sql.Tx, reportoracle.ProcedureRef, []reportoracle.ProcedureValue) (map[string]string, error)
}

// Default Oracle operations share the existing datasource pool using ID zero,
// which is reserved for the environment connection and never persisted.
func withOracleOperationConnection(ctx context.Context, timeout time.Duration, action func(context.Context, *reportoracle.Adapter) error) error {
	config, _, err := officeOracleConfigFromEnvironment()
	if err != nil {
		return err
	}
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%#v", config))))
	datasource := model.ReportDatasource{
		Host: config.Host, Port: config.Port, ServiceName: config.ServiceName, SID: config.SID,
		Username: config.Username, SessionTimezone: config.Timezone, CredentialKeyVersion: version,
		ConnectTimeoutSeconds: int(config.ConnectTimeout / time.Second),
		MaxOpenConnections:    config.MaxOpenConnections, MaxIdleConnections: config.MaxIdleConnections,
		PrefetchRows: config.PrefetchRows, ArraySize: config.FetchArraySize,
	}
	openCtx, openCancel := context.WithTimeout(ctx, config.ConnectTimeout)
	connection, release, err := acquireSelectOracle(openCtx, datasource, config.Password)
	openCancel()
	if err != nil {
		return err
	}
	defer release()
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return action(queryCtx, connection)
}
