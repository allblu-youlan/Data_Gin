package data_svc

import (
	"context"
	"time"

	"gin-biz-web-api/internal/reportoracle"
)

type oracleExcelImportBatch struct {
	Values       [][]interface{}
	First        oracleExcelImportLocation
	Last         oracleExcelImportLocation
	FileIndex    int
	ReadDuration time.Duration
}

type oracleExcelImportLocatedReader func(context.Context, string, OracleExcelImportConfig, []reportoracle.ResultColumn, func([]interface{}, oracleExcelImportLocation) error) (int, error)

// One queued batch lets the reader overlap Oracle execution while keeping
// memory bounded. The owner must call stop to join the reader on every exit.
func streamOracleExcelImportBatches(ctx context.Context, paths []string, config OracleExcelImportConfig, columns []reportoracle.ResultColumn, batchRows int) (<-chan oracleExcelImportBatch, func() error) {
	return streamOracleExcelImportBatchesWithReader(ctx, paths, config, columns, batchRows, readOracleExcelImportLocated)
}

func streamOracleExcelImportBatchesWithReader(ctx context.Context, paths []string, config OracleExcelImportConfig, columns []reportoracle.ResultColumn, batchRows int, read oracleExcelImportLocatedReader) (<-chan oracleExcelImportBatch, func() error) {
	if batchRows < 1 {
		batchRows = oracleExcelImportBatchRows
	}
	readCtx, cancel := context.WithCancel(ctx)
	batches := make(chan oracleExcelImportBatch, 1)
	done := make(chan struct{})
	var readErr error
	go func() {
		defer close(done)
		defer close(batches)
		defer cancel()
		readErr = func() error {
			values := make([][]interface{}, 0, batchRows)
			var first, last oracleExcelImportLocation
			var fileIndex int
			var readDuration time.Duration
			readingSince := time.Now()
			publish := func() error {
				if len(values) == 0 {
					return nil
				}
				readDuration += time.Since(readingSince)
				batch := oracleExcelImportBatch{Values: values, First: first, Last: last, FileIndex: fileIndex, ReadDuration: readDuration}
				select {
				case <-readCtx.Done():
					return readCtx.Err()
				case batches <- batch:
				}
				// Ownership of this slice passed to the consumer. Allocate the next
				// batch separately, and exclude queue backpressure from read time.
				readingSince = time.Now()
				values = make([][]interface{}, 0, batchRows)
				return nil
			}
			for index, path := range paths {
				if err := readCtx.Err(); err != nil {
					return err
				}
				_, err := read(readCtx, path, config, columns, func(row []interface{}, location oracleExcelImportLocation) error {
					if len(values) == 0 {
						first = location
					}
					last = location
					fileIndex = index + 1
					values = append(values, row)
					if len(values) == batchRows {
						return publish()
					}
					return nil
				})
				if err != nil {
					return err
				}
			}
			return publish()
		}()
	}()
	return batches, func() error {
		cancel()
		<-done // Publishes readErr before it can be observed by any caller.
		return readErr
	}
}
