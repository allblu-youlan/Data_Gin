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
)

type availabilityGate struct {
	available       atomic.Bool
	nextProbeUnixNS atomic.Int64
}

var applicationAvailability = newAvailabilityGate()

func newAvailabilityGate() *availabilityGate {
	gate := &availabilityGate{}
	gate.available.Store(true)
	return gate
}

func (gate *availabilityGate) RecordResult(err error) {
	if gate == nil {
		return
	}
	if err == nil {
		gate.available.Store(true)
		gate.nextProbeUnixNS.Store(0)
		return
	}
	if IsConnectivityError(err) {
		gate.available.Store(false)
	}
}

func (gate *availabilityGate) CanServe(ctx context.Context, ping func(context.Context) error) bool {
	if gate == nil || gate.available.Load() {
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
	gate.RecordResult(err)
	return err == nil
}

func (gate *availabilityGate) claimRecoveryProbe(now time.Time) bool {
	for {
		nextProbe := gate.nextProbeUnixNS.Load()
		if nextProbe > now.UnixNano() {
			return false
		}
		if gate.nextProbeUnixNS.CompareAndSwap(nextProbe, now.Add(databaseRecoveryProbeInterval).UnixNano()) {
			return true
		}
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
	var networkError net.Error
	return errors.As(err, &networkError)
}

// RecordResult records the result of a database operation. Successful queries
// close the gate; connection-level failures open it.
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
