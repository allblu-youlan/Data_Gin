package data_svc

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func TestOracleExcelImportProgressCountsAndCommitsAllFiles(t *testing.T) {
	rows := [][]interface{}{{"订单号", "金额"}, {"", ""}}
	for index := 0; index < 201; index++ {
		rows = append(rows, []interface{}{fmt.Sprintf("ORDER_%d", index), "10"})
	}
	first := oracleImportWorkbook(t, "first.xlsx", oracleImportSheetFixture{name: "订单", rows: rows})
	second := oracleImportWorkbook(t, "second.xlsx", oracleImportSheetFixture{name: "店铺A", rows: [][]interface{}{{"订单号", "金额"}, {"A", "20"}}}, oracleImportSheetFixture{name: "店铺B", rows: [][]interface{}{{"订单号", "金额"}, {"B", "30"}}})
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("second_batch_fails_%t", fail), func(t *testing.T) {
			state := &oracleOperationsTestState{}
			if fail {
				state.writeError, state.writeErrorAt = errors.New("Oracle batch rejected"), 2
			}
			db := sql.OpenDB(oracleOperationsTestConnector{state: state})
			defer db.Close()
			var events []OracleExcelImportProgress
			count, err := executeOracleExcelImportFilesWithProgressOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportFilesColumns()}, []string{first, second}, oracleImportFilesConfig(), oracleImportTestIdentifier, func(progress OracleExcelImportProgress) {
				events = append(events, progress)
				if progress.Stage == "completed" && state.commits != 1 {
					t.Fatal("success was published before Oracle committed")
				}
				if progress.Stage == "failed" && state.rollbacks != 1 {
					t.Fatal("failure was published before Oracle rolled back")
				}
			})
			if (err != nil) != fail || len(events) < 4 {
				t.Fatalf("count=%d events=%+v error=%v", count, events, err)
			}
			if events[0].Stage != "counting" || events[0].TotalKnown {
				t.Fatalf("statistics must start without a fabricated total: %+v", events[0])
			}
			intermediate, committing := false, false
			for _, progress := range events {
				if progress.Stage == "importing" {
					if !progress.TotalKnown || progress.TotalRows != 203 || progress.Identifier != "42" {
						t.Fatalf("wrong import total or identifier: %+v", progress)
					}
					intermediate = intermediate || progress.ProcessedRows == 200
				}
				committing = committing || progress.Stage == "committing"
			}
			last := events[len(events)-1]
			if !intermediate || last.FileIndex != 2 || last.FileName != "second.xlsx" || last.SheetName != "店铺B" {
				t.Fatalf("progress lost intermediate count or source location: %+v", events)
			}
			if fail {
				if count != 0 || last.Stage != "failed" || last.ProcessedRows != 200 || committing || state.commits != 0 {
					t.Fatalf("failed batch reported successful rows: count=%d last=%+v", count, last)
				}
			} else if count != 203 || !committing || last.Stage != "completed" || last.ProcessedRows != 203 || last.TotalRows != 203 {
				t.Fatalf("final progress did not match committed rows: count=%d last=%+v", count, last)
			}
		})
	}
}

func TestOracleExcelImportProgressCompletesEmptySheet(t *testing.T) {
	path := oracleImportWorkbook(t, "empty.xlsx", oracleImportSheetFixture{name: "Sheet1", rows: [][]interface{}{{"订单号", "金额"}, {"", ""}}})
	state := &oracleOperationsTestState{}
	db := sql.OpenDB(oracleOperationsTestConnector{state: state})
	defer db.Close()
	var last OracleExcelImportProgress
	count, err := executeOracleExcelImportFilesWithProgressOnConnection(t.Context(), oracleOperationsFakeConnection{db: db, columns: oracleImportFilesColumns()}, []string{path}, oracleImportFilesConfig(), oracleImportTestIdentifier, func(progress OracleExcelImportProgress) { last = progress })
	if err != nil || count != 0 || state.commits != 1 || last.Stage != "completed" || !last.TotalKnown || last.TotalRows != 0 || last.ProcessedRows != 0 {
		t.Fatalf("empty import progress=%+v count=%d error=%v", last, count, err)
	}
}
