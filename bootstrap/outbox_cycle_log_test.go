package bootstrap

import (
	"errors"
	"fmt"
	"testing"

	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestOutboxCycleLogDistinguishesDatabaseGuardFromClaimFailure(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	for _, test := range []struct {
		name string
		err  error
		warn bool
	}{
		{name: "database guard", err: database.ErrUnavailable, warn: true},
		{name: "wrapped database guard", err: fmt.Errorf("outbox dispatcher: %w", database.ErrUnavailable), warn: true},
		{name: "real claim failure", err: fmt.Errorf("outbox dispatcher: claim batch: %w", mysqlDriver.ErrInvalidConn)},
		{name: "matching text is not a guard", err: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			logOutboxCycleError(test.err)
			entries := logs.TakeAll()
			if len(entries) != 1 {
				t.Fatalf("log count=%d", len(entries))
			}
			wantLevel, wantMessage := zap.ErrorLevel, "Outbox dispatch cycle failed"
			if test.warn {
				wantLevel, wantMessage = zap.WarnLevel, "Outbox dispatch paused by database availability guard"
			}
			if entries[0].Level != wantLevel || entries[0].Message != wantMessage || entries[0].ContextMap()["error"] != test.err.Error() {
				t.Fatalf("wrong classification or lost cause: %+v fields=%v", entries[0], entries[0].ContextMap())
			}
		})
	}
}
