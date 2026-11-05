package reportoracle

import (
	"context"
	"fmt"
)

type ErrorLogEntry struct {
	InterfaceDirect string
	KeyInfoNo       string
	ProInfo         string
	ErrorInfo       string
	Email           string
}

// WriteErrorLog uses the existing autonomous procedure for IDs and commit.
func (adapter *Adapter) WriteErrorLog(ctx context.Context, entry ErrorLogEntry) error {
	if adapter == nil || adapter.db == nil {
		return fmt.Errorf("write Oracle error log: adapter is closed")
	}
	_, err := adapter.db.ExecContext(ctx,
		"BEGIN YL_DBS.YL_ERROR_LOG_WRITE(:1, :2, :3, :4, :5); END;",
		entry.InterfaceDirect, entry.KeyInfoNo, entry.ProInfo, entry.ErrorInfo, entry.Email,
	)
	if err != nil {
		return fmt.Errorf("write Oracle error log: %w", err)
	}
	return nil
}
