package data_svc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
)

func TestOracleExcelImportPipelinePreservesWorkbookOrderAndLocations(t *testing.T) {
	first := oracleImportWorkbook(t, "first.xlsx", oracleImportSheetFixture{name: "订单", rows: [][]interface{}{{"订单号", "金额"}, {"A", "10"}, {"B", "20"}}})
	second := oracleImportWorkbook(t, "second.xlsx", oracleImportSheetFixture{name: "明细", rows: [][]interface{}{{"金额", "订单号"}, {"30", "C"}, {"40", "D"}}})
	empty := oracleImportWorkbook(t, "empty.xlsx", oracleImportSheetFixture{name: "空表", rows: [][]interface{}{{"订单号", "金额"}}})
	config := oracleImportFilesConfig()
	config.FixedIdentifier.Value = "42"
	batches, stop := streamOracleExcelImportBatches(t.Context(), []string{first, second, empty}, config, oracleImportFilesColumns(), 3)
	t.Cleanup(func() { _ = stop() })
	var got []oracleExcelImportBatch
	for batch := range batches {
		got = append(got, batch)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Values) != 3 || len(got[1].Values) != 1 || got[0].FileIndex != 2 || got[1].FileIndex != 2 {
		t.Fatalf("unexpected batches: %+v", got)
	}
	var codes []interface{}
	for _, batch := range got {
		for _, row := range batch.Values {
			codes = append(codes, row[0])
		}
	}
	if !reflect.DeepEqual(codes, []interface{}{"A", "B", "C", "D"}) || got[0].First.file != "first.xlsx" || got[0].First.sheet != "订单" || got[0].First.row != 2 || got[0].Last.file != "second.xlsx" || got[0].Last.row != 2 || got[1].Last.row != 3 {
		t.Fatalf("row order or source locations changed: codes=%v batches=%+v", codes, got)
	}
	if got[0].ReadDuration <= 0 || got[1].ReadDuration < got[0].ReadDuration {
		t.Fatalf("read duration must be cumulative: %+v", got)
	}
}

func TestOracleExcelImportPipelineReadsAheadWithBoundedBackpressure(t *testing.T) {
	reading := make(chan int, 8)
	reader := func(ctx context.Context, _ string, _ OracleExcelImportConfig, _ []reportoracle.ResultColumn, write func([]interface{}, oracleExcelImportLocation) error) (int, error) {
		for row := 1; row <= 6; row++ {
			reading <- row
			if err := write([]interface{}{row}, oracleExcelImportLocation{file: "orders.xlsx", sheet: "订单", row: row}); err != nil {
				return 0, err
			}
		}
		return 6, nil
	}
	batches, stop := streamOracleExcelImportBatchesWithReader(t.Context(), []string{"orders.xlsx"}, OracleExcelImportConfig{}, nil, 1, reader)
	t.Cleanup(func() { _ = stop() })
	for want := 1; want <= 2; want++ {
		select {
		case got := <-reading:
			if got != want {
				t.Fatalf("reading row=%d want=%d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("reader did not fill its bounded queue")
		}
	}
	select {
	case row := <-reading:
		t.Fatalf("reader bypassed its one-batch queue: row=%d", row)
	case <-time.After(20 * time.Millisecond):
	}
	first := <-batches
	// While the consumer still owns its first batch, the reader can prepare the
	// next batch. It cannot run through the rest of the workbook unchecked.
	select {
	case row := <-reading:
		if row != 3 || first.Values[0][0] != 1 {
			t.Fatalf("reader did not overlap consumer work: row=%d first=%+v", row, first)
		}
	case <-time.After(time.Second):
		t.Fatal("reading could not overlap the consumer's first write")
	}
	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopping a backpressured producer returned %v", err)
	}
	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("repeated stop changed the reader result: %v", err)
	}
}

func TestOracleExcelImportPipelinePublishesReaderErrorBeforeClosing(t *testing.T) {
	want := errors.New("workbook read failed")
	reader := func(context.Context, string, OracleExcelImportConfig, []reportoracle.ResultColumn, func([]interface{}, oracleExcelImportLocation) error) (int, error) {
		return 0, want
	}
	batches, stop := streamOracleExcelImportBatchesWithReader(t.Context(), []string{"bad.xlsx"}, OracleExcelImportConfig{}, nil, 2, reader)
	t.Cleanup(func() { _ = stop() })
	if batch, ok := <-batches; ok {
		t.Fatalf("reader error published a partial batch: %+v", batch)
	}
	if err := stop(); !errors.Is(err, want) {
		t.Fatalf("closed stream lost its reader error: %v", err)
	}
}

func TestOracleExcelImportPipelineCancellationJoinsActiveReader(t *testing.T) {
	started, exited := make(chan struct{}), make(chan struct{})
	reader := func(ctx context.Context, _ string, _ OracleExcelImportConfig, _ []reportoracle.ResultColumn, _ func([]interface{}, oracleExcelImportLocation) error) (int, error) {
		close(started)
		<-ctx.Done()
		close(exited)
		return 0, ctx.Err()
	}
	batches, stop := streamOracleExcelImportBatchesWithReader(t.Context(), []string{"orders.xlsx"}, OracleExcelImportConfig{}, nil, 1, reader)
	t.Cleanup(func() { _ = stop() })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("reader did not start")
	}
	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result=%v", err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("stop returned while the reader was still running")
	}
	if _, ok := <-batches; ok {
		t.Fatal("canceled reader left the output stream open")
	}
}

func TestOracleExcelImportPipelineReadDurationExcludesQueueWait(t *testing.T) {
	blocked := make(chan struct{})
	reader := func(_ context.Context, _ string, _ OracleExcelImportConfig, _ []reportoracle.ResultColumn, write func([]interface{}, oracleExcelImportLocation) error) (int, error) {
		for row := 1; row <= 3; row++ {
			if row == 2 {
				close(blocked)
			}
			if err := write([]interface{}{row}, oracleExcelImportLocation{row: row}); err != nil {
				return 0, err
			}
		}
		return 3, nil
	}
	batches, stop := streamOracleExcelImportBatchesWithReader(t.Context(), []string{"orders.xlsx"}, OracleExcelImportConfig{}, nil, 1, reader)
	t.Cleanup(func() { _ = stop() })
	<-blocked
	waitStarted := time.Now()
	<-time.After(60 * time.Millisecond)
	waited := time.Since(waitStarted)
	var last oracleExcelImportBatch
	for batch := range batches {
		last = batch
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if last.ReadDuration >= waited/2 {
		t.Fatalf("queue wait was charged as reading: read=%s queue_wait=%s", last.ReadDuration, waited)
	}
}
