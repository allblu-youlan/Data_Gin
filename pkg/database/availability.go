package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

var (
	ErrUnavailable          = errors.New("database unavailable")
	errDatabaseProbeTimeout = errors.New("database probe timeout")
)

const (
	databaseRecoveryProbeInterval = time.Second
	databaseRecoveryProbeTimeout  = time.Second
	databaseCapacityCooldown      = 30 * time.Second
	databaseReadinessCacheTTL     = 15 * time.Second
)

const (
	databaseStateClosed uint32 = iota
	databaseStateOpen
	databaseStateHalfOpen
)

type availabilityGate struct {
	state atomic.Uint32

	mu                     sync.Mutex
	nextProbeUnixNS        int64
	failureGeneration      uint64
	readinessProbeInFlight bool
	lastHealthyUnixNS      atomic.Int64
}

var applicationAvailability = newAvailabilityGate()

func newAvailabilityGate() *availabilityGate {
	return &availabilityGate{}
}

func (gate *availabilityGate) RecordResult(err error) {
	if gate == nil {
		return
	}
	if err == nil {
		gate.lastHealthyUnixNS.Store(time.Now().UnixNano())
		return
	}
	if IsConnectivityError(err) {
		gate.open(time.Now(), databaseRecoveryCooldown(err))
	}
}

func (gate *availabilityGate) CanServe(ctx context.Context, ping func(context.Context) error) bool {
	if gate == nil || gate.state.Load() == databaseStateClosed {
		return true
	}
	if ping == nil {
		return false
	}
	probeGeneration, claimed := gate.claimRecoveryProbe(time.Now())
	if !claimed {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeoutCause(ctx, databaseRecoveryProbeTimeout, errDatabaseProbeTimeout)
	defer cancel()
	err := ping(probeCtx)
	if err != nil {
		cooldown, shouldOpen := probeFailureCooldown(ctx, probeCtx, err)
		if !shouldOpen {
			gate.abortRecoveryProbe(probeGeneration)
			return false
		}
		gate.open(time.Now(), cooldown)
		return false
	}
	return gate.closeAfterSuccessfulProbe(probeGeneration, time.Now())
}

func (gate *availabilityGate) claimRecoveryProbe(now time.Time) (uint64, bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.state.Load() != databaseStateOpen || gate.nextProbeUnixNS > now.UnixNano() {
		return 0, false
	}
	gate.state.Store(databaseStateHalfOpen)
	return gate.failureGeneration, true
}

func (gate *availabilityGate) open(now time.Time, cooldown time.Duration) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.openLocked(now, cooldown)
}

func (gate *availabilityGate) openLocked(now time.Time, cooldown time.Duration) {
	if cooldown <= 0 {
		cooldown = databaseRecoveryProbeInterval
	}
	deadlineUnixNS := now.Add(cooldown).UnixNano()
	if gate.nextProbeUnixNS < deadlineUnixNS {
		gate.nextProbeUnixNS = deadlineUnixNS
	}
	gate.failureGeneration++
	gate.state.Store(databaseStateOpen)
}

func (gate *availabilityGate) closeAfterSuccessfulProbe(probeGeneration uint64, now time.Time) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.state.Load() != databaseStateHalfOpen || gate.failureGeneration != probeGeneration {
		return false
	}
	gate.nextProbeUnixNS = 0
	gate.lastHealthyUnixNS.Store(now.UnixNano())
	gate.state.Store(databaseStateClosed)
	return true
}

func (gate *availabilityGate) abortRecoveryProbe(probeGeneration uint64) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.state.Load() == databaseStateHalfOpen && gate.failureGeneration == probeGeneration {
		gate.state.Store(databaseStateOpen)
	}
}

func (gate *availabilityGate) CheckReadiness(ctx context.Context, ping func(context.Context) error) bool {
	if gate == nil || ping == nil {
		return false
	}
	if gate.state.Load() != databaseStateClosed {
		return gate.CanServe(ctx, ping)
	}
	now := time.Now()
	if gate.hasRecentSuccess(now) {
		return true
	}
	probeGeneration, claimed, cached := gate.claimReadinessProbe(now)
	if cached {
		return true
	}
	if !claimed {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeoutCause(ctx, databaseRecoveryProbeTimeout, errDatabaseProbeTimeout)
	defer cancel()
	err := ping(probeCtx)
	probeFinishedAt := time.Now()
	cooldown, shouldOpen := probeFailureCooldown(ctx, probeCtx, err)
	return gate.finishReadinessProbe(probeGeneration, probeFinishedAt, err, cooldown, shouldOpen)
}

func (gate *availabilityGate) claimReadinessProbe(now time.Time) (uint64, bool, bool) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.state.Load() != databaseStateClosed {
		return 0, false, false
	}
	if gate.hasRecentSuccess(now) {
		return 0, false, true
	}
	if gate.readinessProbeInFlight {
		return 0, false, false
	}
	gate.readinessProbeInFlight = true
	return gate.failureGeneration, true, false
}

func (gate *availabilityGate) finishReadinessProbe(
	probeGeneration uint64,
	now time.Time,
	err error,
	cooldown time.Duration,
	shouldOpen bool,
) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.readinessProbeInFlight = false
	if err == nil && gate.state.Load() == databaseStateClosed && gate.failureGeneration == probeGeneration {
		gate.lastHealthyUnixNS.Store(now.UnixNano())
		return true
	}
	if shouldOpen {
		gate.openLocked(now, cooldown)
	}
	return false
}

func (gate *availabilityGate) hasRecentSuccess(now time.Time) bool {
	lastHealthyUnixNS := gate.lastHealthyUnixNS.Load()
	return lastHealthyUnixNS > 0 && now.UnixNano()-lastHealthyUnixNS <= databaseReadinessCacheTTL.Nanoseconds()
}

func probeFailureCooldown(callerCtx, probeCtx context.Context, err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	if isMySQLCapacityError(err) {
		return databaseCapacityCooldown, true
	}
	if IsConnectivityError(err) {
		return databaseRecoveryCooldown(err), true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if probeCtx != nil && errors.Is(context.Cause(probeCtx), errDatabaseProbeTimeout) {
			return databaseCapacityCooldown, true
		}
		if callerCtx != nil && callerCtx.Err() != nil {
			return 0, false
		}
	}
	return databaseRecoveryCooldown(err), true
}

func databaseRecoveryCooldown(err error) time.Duration {
	if isMySQLCapacityError(err) {
		return databaseCapacityCooldown
	}
	return databaseRecoveryProbeInterval
}

func isMySQLCapacityError(err error) bool {
	var mysqlError *mysqlDriver.MySQLError
	if !errors.As(err, &mysqlError) {
		return false
	}
	switch mysqlError.Number {
	case 1040, 1203, 1226:
		return true
	default:
		return false
	}
}

// IsConnectivityError reports errors for which new database work should fail
// fast until a recovery probe succeeds.
func IsConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnavailable) ||
		errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, mysqlDriver.ErrInvalidConn) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	if isMySQLCapacityError(err) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

// RecordResult records the result of a database operation. Connection-level
// and connection-capacity failures open the gate. Only a recovery probe closes it.
func RecordResult(err error) {
	applicationAvailability.RecordResult(err)
}

// CanServe reports whether the application may start a database-dependent
// request. When the gate is open, at most one request per interval performs a
// short probe; other requests fail immediately.
func CanServe(ctx context.Context) bool {
	return applicationAvailability.CanServe(ctx, PingContext)
}

// RequireAvailable lets non-HTTP workloads share the application availability
// gate without duplicating probe and cooldown logic.
func RequireAvailable(ctx context.Context) error {
	if CanServe(ctx) {
		return nil
	}
	return ErrUnavailable
}

// CheckReadiness verifies that the shared pool was recently healthy. Active
// business traffic refreshes the short cache; otherwise one caller performs a
// bounded ping while concurrent readiness checks fail fast.
func CheckReadiness(ctx context.Context) error {
	if applicationAvailability.CheckReadiness(ctx, rawPingContext) {
		return nil
	}
	return ErrUnavailable
}

// PingContext checks the shared application pool and synchronizes the
// availability gate with the result.
func PingContext(ctx context.Context) error {
	err := rawPingContext(ctx)
	RecordResult(err)
	return err
}

func rawPingContext(ctx context.Context) error {
	if SQLDB == nil {
		return ErrUnavailable
	}
	return SQLDB.PingContext(ctx)
}

// ConnectionStats returns a snapshot of the shared application's pool.
func ConnectionStats() (sql.DBStats, bool) {
	sqlDB := SQLDB
	if sqlDB == nil {
		return sql.DBStats{}, false
	}
	return sqlDB.Stats(), true
}
