package reportoracle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gin-biz-web-api/etc"
)

// InitializeJSONSnapshotStore applies the existing platform schema on a dedicated
// administrative connection. It never changes or clears existing objects.
func (adapter *Adapter) InitializeJSONSnapshotStore(ctx context.Context) error {
	if err := adapter.ValidateJSONSnapshotStore(ctx); err == nil {
		return nil
	} else if !errors.Is(err, ErrJSONSnapshotStoreNotReady) {
		return err
	}
	statements := strings.Split(etc.OracleReportSnapshotSQL, ";")
	tables := []string{JSONSnapshotManifestTable, JSONSnapshotRowsTable}
	for index, table := range tables {
		if index >= len(statements) || !strings.HasPrefix(strings.TrimSpace(statements[index]), "CREATE TABLE "+table+" (") {
			return fmt.Errorf("initialize JSON snapshot store: bundled schema is invalid")
		}
		var exists int
		if err := adapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_tables WHERE table_name = :1", table).Scan(&exists); err != nil {
			return fmt.Errorf("initialize JSON snapshot store: inspect %s: %w", table, err)
		}
		if exists != 0 {
			continue
		}
		// These statements come only from the bundled, versioned schema above.
		if _, err := adapter.db.ExecContext(ctx, strings.TrimSpace(statements[index])); err != nil {
			var oracleError interface{ Code() int }
			if errors.As(err, &oracleError) && oracleError.Code() == 955 {
				continue // Another initializer created the object; validate below.
			}
			return fmt.Errorf("initialize JSON snapshot store: create %s; requires CREATE TABLE and tablespace quota, or have the DBA apply etc/oracle_report_snapshot.sql: %w", table, err)
		}
	}
	if err := adapter.ValidateJSONSnapshotStore(ctx); err != nil {
		return fmt.Errorf("initialize JSON snapshot store: existing table schema requires administrator inspection; no existing objects were altered: %w", err)
	}
	return nil
}
