package data_svc

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestOfficePushClaimableRespectsActiveLease(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Second)
	tests := []struct {
		name string
		run  model.OfficePushRun
		want bool
	}{
		{name: "queued", run: model.OfficePushRun{Status: model.OfficePushRunStatusQueued}, want: true},
		{name: "active running", run: model.OfficePushRun{Status: model.OfficePushRunStatusRunning, LeaseToken: "active", LeaseExpiresAt: &future}},
		{name: "expired running", run: model.OfficePushRun{Status: model.OfficePushRunStatusRunning, LeaseToken: "expired", LeaseExpiresAt: &past}, want: true},
		{name: "failed", run: model.OfficePushRun{Status: model.OfficePushRunStatusFailed}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := officePushClaimable(test.run, now); got != test.want {
				t.Fatalf("officePushClaimable() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOfficePushOwnedUpdateUsesLeaseFence(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	processor := &OfficePushProcessor{db: db}
	query := processor.ownedRun(t.Context(), 31, "11111111-1111-4111-8111-111111111111").Update("status", model.OfficePushRunStatusSucceeded)
	statement := query.Statement.SQL.String()
	for _, fragment := range []string{"id = ?", "status = ?", "lease_token = ?"} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("owned update %q does not contain %q", statement, fragment)
		}
	}
}

func TestOfficeProcedureLockMonitorCancelsWhenLeaseIsLost(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	nowCalls := 0
	processor := &OfficePushProcessor{db: db, heartbeat: time.Millisecond, stateLimit: time.Second, now: func() time.Time {
		nowCalls++
		return time.Now().UTC()
	}}
	executionCtx, cancelExecution := context.WithCancel(t.Context())
	done := processor.startProcedureLockMonitor(executionCtx, cancelExecution, "OWNER.RESULT_TABLE", "11111111-1111-4111-8111-111111111111")
	select {
	case <-executionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("procedure lock monitor did not cancel execution after zero-row renewal")
	}
	<-done
	if nowCalls == 0 {
		t.Fatal("procedure lock monitor did not read the current time after its tick")
	}
}

func TestOfficeProcedureLockRenewalUsesLeaseFence(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	processor := &OfficePushProcessor{db: db}
	query := processor.ownedProcedureLock(t.Context(), "OWNER.RESULT_TABLE", "11111111-1111-4111-8111-111111111111", time.Now().UTC()).Update("updated_at", time.Now().UTC())
	statement := query.Statement.SQL.String()
	for _, fragment := range []string{"lock_key = ?", "lease_token = ?", "lease_expires_at > ?"} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("procedure lock renewal %q does not contain %q", statement, fragment)
		}
	}
}

func TestOfficeRunRenewalRejectsExpiredLease(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	processor := &OfficePushProcessor{db: db}
	query := processor.ownedActiveRun(t.Context(), 31, "11111111-1111-4111-8111-111111111111", time.Now().UTC()).Update("updated_at", time.Now().UTC())
	statement := query.Statement.SQL.String()
	for _, fragment := range []string{"id = ?", "status = ?", "lease_token = ?", "lease_expires_at > ?"} {
		if !strings.Contains(statement, fragment) {
			t.Fatalf("active run renewal %q does not contain %q", statement, fragment)
		}
	}
}

func TestOfficeRunMonitorReadsCurrentTimeAfterTick(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	nowCalls := 0
	processor := &OfficePushProcessor{db: db, heartbeat: time.Millisecond, stateLimit: time.Second, now: func() time.Time {
		nowCalls++
		return time.Now().UTC()
	}}
	executionCtx, cancelExecution := context.WithCancel(t.Context())
	done := processor.startMonitor(executionCtx, cancelExecution, 31, "11111111-1111-4111-8111-111111111111")
	select {
	case <-executionCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("run monitor did not cancel execution after zero-row renewal")
	}
	<-done
	if nowCalls == 0 {
		t.Fatal("run monitor did not read the current time after its tick")
	}
}

func TestOfficePushProcessGeneratesOneLeaseToken(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open dry-run database: %v", err)
	}
	processor := newOfficePushProcessor(db, func() (officeBot, error) { return nil, nil }, func(context.Context, reportoracle.Config) (officeOracleConnection, error) { return nil, nil })
	tokenCalls := 0
	processor.newToken = func() string {
		tokenCalls++
		return "invalid-token"
	}
	err = processor.Process(t.Context(), 31, true)
	if !errors.Is(err, ErrOfficePushProcessNonRetryable) {
		t.Fatalf("Process() error = %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("newToken() calls = %d, want 1", tokenCalls)
	}
}

func TestOfficePushBotMatchesConfiguredAndLegacyTargets(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		configured   string
		wantMatching bool
	}{
		{name: "same bot", target: "cli_office", configured: "cli_office", wantMatching: true},
		{name: "legacy snapshot", configured: "cli_office", wantMatching: true},
		{name: "changed bot", target: "cli_old", configured: "cli_new"},
		{name: "configured bot missing", target: "cli_office"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := officePushBotMatches(test.target, test.configured); got != test.wantMatching {
				t.Fatalf("officePushBotMatches(%q, %q) = %t, want %t", test.target, test.configured, got, test.wantMatching)
			}
		})
	}
}

func TestOfficePushRetryableUsesWebDAVClassification(t *testing.T) {
	for _, test := range []struct {
		name      string
		retryable bool
	}{
		{name: "authentication"},
		{name: "rate limited", retryable: true},
		{name: "server unavailable", retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := &officeRetryableTestError{retryable: test.retryable}
			if got := officePushRetryable(err); got != test.retryable {
				t.Fatalf("officePushRetryable() = %t, want %t", got, test.retryable)
			}
		})
	}
}

type officeRetryableTestError struct{ retryable bool }

func (err *officeRetryableTestError) Error() string   { return "classified upload error" }
func (err *officeRetryableTestError) Retryable() bool { return err.retryable }

func TestRenderOfficeWorkbookFileNameUsesShanghaiDate(t *testing.T) {
	name, err := renderOfficeWorkbookFileName(
		"销售日报_{{date:yyyyMMdd}}.xlsx",
		"销售日报",
		time.Date(2026, 8, 31, 16, 30, 0, 0, time.UTC),
	)
	if err != nil || name != "销售日报_20260901.xlsx" {
		t.Fatalf("renderOfficeWorkbookFileName() = %q, %v", name, err)
	}
	name, err = renderOfficeWorkbookFileName("销售日报_{{date:yyyy-MM-dd}}.xlsx", "销售日报", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || name != "销售日报_2026-09-01.xlsx" {
		t.Fatalf("renderOfficeWorkbookFileName() = %q, %v", name, err)
	}
}

func TestRenderOfficeWorkbookFileNameSupportsLegacyFallback(t *testing.T) {
	name, err := renderOfficeWorkbookFileName("", "线下/销售", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || name != "线下-销售.xlsx" {
		t.Fatalf("renderOfficeWorkbookFileName() = %q, %v", name, err)
	}
}

func TestNormalizeOfficeWorkbookFileNameTemplateRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{
		"../secret.xlsx",
		"sales.csv",
		"sales_{{unknown}}.xlsx",
		"sales_{{date:yyyy/MM/dd}}.xlsx",
		"sales\treport.xlsx",
		strings.Repeat("销", 84) + ".xlsx",
	} {
		if _, err := normalizeOfficeWorkbookFileNameTemplate(value, "销售日报"); err == nil {
			t.Fatalf("normalizeOfficeWorkbookFileNameTemplate(%q) accepted invalid value", value)
		}
	}
}
