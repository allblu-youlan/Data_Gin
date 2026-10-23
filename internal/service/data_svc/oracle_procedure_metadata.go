package data_svc

import (
	"context"
	"time"

	"gin-biz-web-api/internal/reportoracle"
)

func ListOracleOperationProcedures(ctx context.Context, owner, search string, limit int) ([]OfficeProcedureSummary, error) {
	var result []OfficeProcedureSummary
	err := withOracleOperationConnection(ctx, 30*time.Second, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
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

func GetOracleOperationProcedureSignature(ctx context.Context, ref reportoracle.ProcedureRef) ([]OfficeProcedureArgument, error) {
	var result []OfficeProcedureArgument
	err := withOracleOperationConnection(ctx, 30*time.Second, func(queryCtx context.Context, connection *reportoracle.Adapter) error {
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
