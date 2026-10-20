package data_svc

import (
	"context"
	"errors"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/internal/reportrepo"
)

type reportResultCleanupOracleFactory interface {
	Open(context.Context, reportrepo.ResultCleanupRuntime, string) (reportExportOracleSession, error)
}

type oracleReportResultCleanupFactory struct{}

func (oracleReportResultCleanupFactory) Open(ctx context.Context, runtime reportrepo.ResultCleanupRuntime, password string) (reportExportOracleSession, error) {
	queryCtx, cancel := reportOracleQueryContext(ctx, runtime.Datasource)
	defer cancel()
	adapter, err := reportoracle.Open(queryCtx, oracleConfigFromDatasource(runtime.Datasource, password))
	if err != nil {
		return nil, err
	}
	closeOnError := func(openErr error) (reportExportOracleSession, error) {
		return nil, errors.Join(openErr, adapter.Close())
	}
	queryTimeout := time.Duration(runtime.Datasource.QueryTimeoutSeconds) * time.Second
	if queryTimeout <= 0 {
		queryTimeout = defaultReportPublicationInspectionTimeout
	}
	if runtime.Version.UsesJSONSnapshot() {
		configuredColumns := make([]string, 0, len(runtime.Columns))
		for _, column := range runtime.Columns {
			configuredColumns = append(configuredColumns, column.DatabaseColumn)
		}
		if err := adapter.ValidateJSONSnapshotStore(queryCtx); err != nil {
			return closeOnError(err)
		}
		return &oracleReportExportSession{
			adapter: adapter, jsonSnapshot: true, columns: configuredColumns,
			runUUID: runtime.Run.RunUUID, queryTimeout: queryTimeout,
		}, nil
	}
	table := reportoracle.ResultTableRef{Owner: runtime.Version.ResultTableOwner, Name: runtime.Version.ResultTableName}
	if err := adapter.ValidateResultSnapshotTable(queryCtx, table); err != nil {
		return closeOnError(err)
	}
	purgePlan, err := reportoracle.BuildTablePurgePlan(table)
	if err != nil {
		return closeOnError(err)
	}
	return &oracleReportExportSession{
		adapter: adapter, purgePlan: purgePlan, queryTimeout: queryTimeout,
	}, nil
}
