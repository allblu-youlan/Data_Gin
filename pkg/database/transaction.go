package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// TransactionWithBeginRetry is for the shared application MySQL pool: it
// records the final result in that pool's availability gate. It retries one
// failed BEGIN, never the callback or an uncertain COMMIT. Existing transactions
// retain GORM's savepoint semantics and are never retried.
func TransactionWithBeginRetry(db *gorm.DB, callback func(*gorm.DB) error, options ...*sql.TxOptions) (err error) {
	defer func() { RecordResult(err) }()
	if db == nil || db.Statement == nil || callback == nil {
		return gorm.ErrInvalidDB
	}
	if transaction, nested := db.Statement.ConnPool.(gorm.TxCommitter); nested && transaction != nil {
		return db.Transaction(callback, options...)
	}
	ctx := db.Statement.Context
	for attempt := 0; ; attempt++ {
		phase := "begin"
		err = db.Transaction(func(tx *gorm.DB) error {
			phase = "body"
			if callbackErr := callback(tx); callbackErr != nil {
				return callbackErr
			}
			phase = "commit"
			return nil
		}, options...)
		if err == nil {
			return nil
		}
		if phase != "begin" || attempt != 0 || !errors.Is(err, mysqlDriver.ErrInvalidConn) {
			return fmt.Errorf("mysql transaction phase=%s: %w", phase, err)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("mysql transaction phase=begin: %w", errors.Join(err, ctx.Err()))
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return fmt.Errorf("mysql transaction phase=begin: %w", errors.Join(err, ctx.Err()))
		}
	}
}
