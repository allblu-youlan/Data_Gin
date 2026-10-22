package data_svc

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"gin-biz-web-api/internal/reportoracle"

	"github.com/godror/godror"
)

type oracleExcelExportConnection interface {
	InspectResultTable(context.Context, reportoracle.ResultTableRef) ([]reportoracle.ResultColumn, error)
	QuerySelect(context.Context, string, ...interface{}) (*sql.Rows, error)
}

// Shared by normal export tasks and the serial Excel workflow. Cursors are
// opened and closed one at a time; a single-connection Oracle pool also works.
func executeOracleExcelExport(ctx context.Context, connection oracleExcelExportConnection, config OracleExcelExportConfig, workDir string, rowsPerFile int, progress func(int) error) (string, int, error) {
	sheets, legacy, err := oracleExcelExportSheets(config)
	if err != nil {
		return "", 0, err
	}
	spoolRoot := filepath.Join(workDir, "sheet-data")
	if !legacy {
		defer os.RemoveAll(spoolRoot)
	}
	snapshots := make([]oracleExcelSheetSnapshot, 0, len(sheets))
	total := 0
	for index, sheet := range sheets {
		output, count, snapshot, err := func() (string, int, oracleExcelSheetSnapshot, error) {
			schema, err := connection.InspectResultTable(ctx, reportoracle.ResultTableRef{Owner: sheet.Owner, Name: sheet.TableName})
			if err != nil {
				return "", 0, oracleExcelSheetSnapshot{}, err
			}
			normalized, statement, arguments, err := buildOracleExcelExportQuery(sheet.tableConfig(), schema)
			if err != nil {
				return "", 0, oracleExcelSheetSnapshot{}, err
			}
			rows, err := connection.QuerySelect(ctx, statement, append(arguments, godror.NumberAsString())...)
			if err != nil {
				return "", 0, oracleExcelSheetSnapshot{}, err
			}
			defer rows.Close()
			if legacy {
				output, count, err := renderOracleExcelExport(ctx, rows, normalized, schema, workDir, rowsPerFile, progress)
				return output, count, oracleExcelSheetSnapshot{}, err
			}
			sheet.Owner, sheet.TableName, sheet.Columns, sheet.Filters = normalized.Owner, normalized.TableName, normalized.Columns, normalized.Filters
			var sheetProgress func(int) error
			if progress != nil {
				sheetProgress = func(count int) error { return progress(total + count) }
			}
			snapshot, err := spoolOracleExcelSheet(ctx, rows, sheet, schema, filepath.Join(spoolRoot, fmt.Sprintf("sheet_%02d", index+1)), rowsPerFile, sheetProgress)
			return "", snapshot.Rows, snapshot, err
		}()
		if err != nil {
			return "", total, fmt.Errorf("Sheet %s: %w", sheet.SheetName, err)
		}
		if legacy {
			return output, count, nil
		}
		total += count
		snapshots = append(snapshots, snapshot)
		if progress != nil {
			if err := progress(total); err != nil {
				return "", total, err
			}
		}
	}
	return renderOracleExcelSnapshots(ctx, snapshots, workDir, rowsPerFile, progress)
}
