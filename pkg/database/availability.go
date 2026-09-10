package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

var ErrUnavailable = errors.New("database unavailable")

const (
	databaseRecoveryProbeInterval = time.Second
	databaseRecoveryProbeTimeout  = time.Second
	databaseCapacityCooldown      = 30 * time.Second
)

const (
	databaseStateClosed uint32 = iota
	databaseStateOpen
	databaseStateHalfOpen
)

type availabilityGate struct {
	state           atomic.Uint32
	nextProbeUnixNS atomic.Int64
}

var applicationAvailability = newAvailabilityGate()

func newAvailabilityGate() *availabilityGate {
	return &availabilityGate{}
}

func (gate *availabilityGate) RecordResult(err error) {
	if gate == nil || err == nil {
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
	if ping == nil || !gate.claimRecoveryProbe(time.Now()) {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), databaseRecoveryProbeTimeout)
	defer cancel()
	err := ping(probeCtx)
	if err != nil {
		gate.open(time.Now(), databaseRecoveryCooldown(err))
		return false
	}
	if !gate.state.CompareAndSwap(databaseStateHalfOpen, databaseStateClosed) {
		return false
	}
	gate.nextProbeUnixNS.Store(0)
	return true
}

func (gate *availabilityGate) claimRecoveryProbe(now time.Time) bool {
	if gate.state.Load() != databaseStateOpen || gate.nextProbeUnixNS.Load() > now.UnixNano() {
		return false
	}
	return gate.state.CompareAndSwap(databaseStateOpen, databaseStateHalfOpen)
}

func (gate *availabilityGate) open(now time.Time, cooldown time.Duration) {
	if cooldown <= 0 {
		cooldown = databaseRecoveryProbeInterval
	}
	gate.extendProbeDeadline(now.Add(cooldown).UnixNano())
	gate.state.Store(databaseStateOpen)
}

func (gate *availabilityGate) extendProbeDeadline(deadlineUnixNS int64) {
	for {
		current := gate.nextProbeUnixNS.Load()
		if current >= deadlineUnixNS || gate.nextProbeUnixNS.CompareAndSwap(current, deadlineUnixNS) {
			return
		}
	}
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
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

// PingContext checks the shared application pool and synchronizes the
// availability gate with the result.
func PingContext(ctx context.Context) error {
	sqlDB := SQLDB
	if sqlDB == nil {
		RecordResult(ErrUnavailable)
		return ErrUnavailable
	}
	err := sqlDB.PingContext(ctx)
	RecordResult(err)
	return err
}

// ConnectionStats returns a snapshot of the shared application's pool.
func ConnectionStats() (sql.DBStats, bool) {
	sqlDB := SQLDB
	if sqlDB == nil {
		return sql.DBStats{}, false
	}
	return sqlDB.Stats(), true
}
