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
func executeOracleExcelExport(ctx context.Context, connection oracleExcelExportConnection, config OracleExcelExportConfig, workDir string, rowsPerSheet int, progress func(int) error) (string, int, error) {
	sheets, _, err := oracleExcelExportSheets(config)
	if err != nil {
		return "", 0, err
	}
	spoolRoot := filepath.Join(workDir, "sheet-data")
	defer os.RemoveAll(spoolRoot)
	snapshots := make([]oracleExcelSheetSnapshot, 0, len(sheets))
	total := 0
	for index, sheet := range sheets {
		snapshot, err := func() (oracleExcelSheetSnapshot, error) {
			schema, err := connection.InspectResultTable(ctx, reportoracle.ResultTableRef{Owner: sheet.Owner, Name: sheet.TableName})
			if err != nil {
				return oracleExcelSheetSnapshot{}, err
			}
			normalized, statement, arguments, err := buildOracleExcelExportQuery(sheet.tableConfig(), schema)
			if err != nil {
				return oracleExcelSheetSnapshot{}, err
			}
			rows, err := connection.QuerySelect(ctx, statement, append(arguments, godror.NumberAsString())...)
			if err != nil {
				return oracleExcelSheetSnapshot{}, err
			}
			defer rows.Close()
			sheet.Owner, sheet.TableName, sheet.Columns, sheet.Filters = normalized.Owner, normalized.TableName, normalized.Columns, normalized.Filters
			var sheetProgress func(int) error
			if progress != nil {
				sheetProgress = func(count int) error { return progress(total + count) }
			}
			return spoolOracleExcelSheet(ctx, rows, sheet, schema, filepath.Join(spoolRoot, fmt.Sprintf("sheet_%02d", index+1)), sheetProgress)
		}()
		if err != nil {
			return "", total, fmt.Errorf("Sheet %s: %w", sheet.SheetName, err)
		}
		total += snapshot.Rows
		snapshots = append(snapshots, snapshot)
		if progress != nil {
			if err := progress(total); err != nil {
				return "", total, err
			}
		}
	}
	return renderOracleExcelSnapshots(ctx, snapshots, workDir, rowsPerSheet, progress)
}
