package data_dao

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestBojunOracleReleaseLeaseUsesOneOwnedUpdate(t *testing.T) {
	for _, test := range []struct {
		name, failure string
		affected      int64
		wantError     error
	}{
		{name: "unused BEGIN failure cannot prevent release", failure: "begin", affected: 1},
		{name: "lost UPDATE response is not replayed", failure: "update", affected: 1, wantError: mysqlDriver.ErrInvalidConn},
		{name: "another owner keeps its lease", affected: 0, wantError: ErrBojunOracleSyncLeaseLost},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &claimConnectionTestState{failure: test.failure, autocommit: true, affected: test.affected}
			db := newClaimConnectionTestDB(t, state)
			now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
			err := NewBojunOracleSyncStateDAO(db).ReleaseLease(t.Context(), " etl01 ", " lease-owner-1 ", now)
			if !errors.Is(err, test.wantError) || state.begins != 0 || state.commits != 0 || state.writes != 1 || state.reads != 0 {
				t.Fatalf("release must use one autocommit UPDATE: error=%v state=%+v", err, state)
			}
			if !strings.Contains(state.updateQuery, "UPDATE `bojun_oracle_sync_states`") ||
				!strings.Contains(state.updateQuery, "WHERE source_code = ? AND lease_token = ?") || len(state.updateArguments) != 5 {
				t.Fatalf("release lost its ownership predicate: SQL=%s args=%v", state.updateQuery, state.updateArguments)
			}
			arguments := state.updateArguments
			if arguments[0].Value != nil || arguments[1].Value != "" || arguments[2].Value != now.Unix() ||
				arguments[3].Value != "etl01" || arguments[4].Value != "lease-owner-1" {
				t.Fatalf("release changed its values or ownership: %v", arguments)
			}
			if db.Config.SkipDefaultTransaction {
				t.Fatal("release disabled transactions on the shared database")
			}
		})
	}
}

func TestBojunOracleReleaseLeasePreservesContext(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "deadline during UPDATE"
		if canceled {
			name = "canceled before UPDATE"
		}
		t.Run(name, func(t *testing.T) {
			state := &claimConnectionTestState{failure: "timeout", autocommit: true, affected: 1}
			db := newClaimConnectionTestDB(t, state)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			wantError, wantWrites := context.DeadlineExceeded, 1
			if canceled {
				cancel()
				wantError, wantWrites = context.Canceled, 0
			}
			err := NewBojunOracleSyncStateDAO(db).ReleaseLease(ctx, "etl01", "lease-owner-1", time.Now())
			if !errors.Is(err, wantError) || state.begins != 0 || state.commits != 0 || state.writes != wantWrites {
				t.Fatalf("release changed caller cancellation or replayed work: error=%v state=%+v", err, state)
			}
		})
	}
}
