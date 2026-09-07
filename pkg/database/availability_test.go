package database

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestAvailabilityGateOpensOnlyForConnectivityFailures(t *testing.T) {
	gate := newAvailabilityGate()
	for _, err := range []error{errors.New("validation failed"), context.DeadlineExceeded} {
		gate.RecordResult(err)
		if !gate.available.Load() {
			t.Fatalf("non-connectivity error %v opened the gate", err)
		}
	}

	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	if gate.available.Load() {
		t.Fatal("connectivity error did not open the gate")
	}
}

func TestAvailabilityGateRecoversWithSingleProbe(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)

	var calls atomic.Int32
	if !gate.CanServe(context.Background(), func(context.Context) error {
		calls.Add(1)
		return nil
	}) {
		t.Fatal("successful recovery probe did not close the gate")
	}
	if calls.Load() != 1 || !gate.available.Load() {
		t.Fatalf("calls=%d available=%t", calls.Load(), gate.available.Load())
	}
}

func TestAvailabilityGateRejectsConcurrentRequestsAfterFailedProbe(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)

	var calls atomic.Int32
	if gate.CanServe(context.Background(), func(context.Context) error {
		calls.Add(1)
		return mysqlDriver.ErrInvalidConn
	}) {
		t.Fatal("failed recovery probe allowed request")
	}
	if gate.CanServe(context.Background(), func(context.Context) error {
		calls.Add(1)
		return nil
	}) {
		t.Fatal("request bypassed recovery probe cooldown")
	}
	if calls.Load() != 1 {
		t.Fatalf("probe calls=%d, want 1", calls.Load())
	}
}

func TestRequireAvailableUsesApplicationGate(t *testing.T) {
	original := applicationAvailability
	applicationAvailability = newAvailabilityGate()
	t.Cleanup(func() { applicationAvailability = original })

	if err := RequireAvailable(t.Context()); err != nil {
		t.Fatalf("RequireAvailable() error = %v", err)
	}
	applicationAvailability.RecordResult(mysqlDriver.ErrInvalidConn)
	applicationAvailability.nextProbeUnixNS.Store(time.Now().Add(time.Minute).UnixNano())
	if err := RequireAvailable(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RequireAvailable() error = %v, want ErrUnavailable", err)
	}
}
