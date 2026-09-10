package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestAvailabilityGateOpensOnlyForConnectivityFailures(t *testing.T) {
	gate := newAvailabilityGate()
	for _, err := range []error{errors.New("validation failed"), context.DeadlineExceeded} {
		gate.RecordResult(err)
		if gate.state.Load() != databaseStateClosed {
			t.Fatalf("non-connectivity error %v opened the gate", err)
		}
	}

	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	if gate.state.Load() != databaseStateOpen {
		t.Fatal("connectivity error did not open the gate")
	}
}

func TestAvailabilityGateOpensForMySQLCapacityFailures(t *testing.T) {
	for _, number := range []uint16{1040, 1203, 1226} {
		t.Run(fmt.Sprintf("error_%d", number), func(t *testing.T) {
			gate := newAvailabilityGate()
			startedAt := time.Now()
			gate.RecordResult(fmt.Errorf("database query: %w", &mysqlDriver.MySQLError{Number: number}))
			if gate.state.Load() != databaseStateOpen {
				t.Fatalf("MySQL error %d did not open the gate", number)
			}
			minimum := startedAt.Add(databaseCapacityCooldown - time.Second).UnixNano()
			if gate.nextProbeUnixNS.Load() < minimum {
				t.Fatalf("MySQL error %d did not apply capacity cooldown", number)
			}
		})
	}
}

func TestAvailabilityGateIgnoresNonCapacityMySQLError(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(&mysqlDriver.MySQLError{Number: 1062})
	if gate.state.Load() != databaseStateClosed {
		t.Fatal("duplicate-key error opened the gate")
	}
}

func TestAvailabilityGateSuccessCannotReopenGate(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	gate.RecordResult(nil)
	if gate.state.Load() != databaseStateOpen {
		t.Fatal("an unrelated successful query closed the open gate")
	}
}

func TestAvailabilityGateLaterFailureCannotShortenCapacityCooldown(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(&mysqlDriver.MySQLError{Number: 1040})
	capacityDeadline := gate.nextProbeUnixNS.Load()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	if got := gate.nextProbeUnixNS.Load(); got < capacityDeadline {
		t.Fatalf("later failure shortened capacity cooldown: got=%d want-at-least=%d", got, capacityDeadline)
	}
}

func TestAvailabilityGateRecoversWithSingleProbe(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	gate.nextProbeUnixNS.Store(0)

	var calls atomic.Int32
	if !gate.CanServe(context.Background(), func(context.Context) error {
		calls.Add(1)
		return nil
	}) {
		t.Fatal("successful recovery probe did not close the gate")
	}
	if calls.Load() != 1 || gate.state.Load() != databaseStateClosed {
		t.Fatalf("calls=%d state=%d", calls.Load(), gate.state.Load())
	}
}

func TestAvailabilityGateAllowsOnlyOneHalfOpenProbe(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	gate.nextProbeUnixNS.Store(0)

	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	probeResult := make(chan bool, 1)
	var calls atomic.Int32
	go func() {
		probeResult <- gate.CanServe(t.Context(), func(context.Context) error {
			calls.Add(1)
			close(probeStarted)
			<-releaseProbe
			return nil
		})
	}()
	<-probeStarted

	const contenders = 32
	var waitGroup sync.WaitGroup
	waitGroup.Add(contenders)
	for range contenders {
		go func() {
			defer waitGroup.Done()
			if gate.CanServe(t.Context(), func(context.Context) error {
				calls.Add(1)
				return nil
			}) {
				t.Error("concurrent request bypassed the half-open probe")
			}
		}()
	}
	waitGroup.Wait()
	close(releaseProbe)
	if !<-probeResult {
		t.Fatal("successful half-open probe did not close the gate")
	}
	if calls.Load() != 1 {
		t.Fatalf("probe calls=%d, want 1", calls.Load())
	}
}

func TestAvailabilityGateRejectsConcurrentRequestsAfterFailedProbe(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	gate.nextProbeUnixNS.Store(0)

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

func TestAvailabilityGateConcurrentFailureWinsOverProbeSuccess(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	gate.nextProbeUnixNS.Store(0)

	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	result := make(chan bool, 1)
	go func() {
		result <- gate.CanServe(t.Context(), func(context.Context) error {
			close(probeStarted)
			<-releaseProbe
			return nil
		})
	}()
	<-probeStarted
	gate.RecordResult(&mysqlDriver.MySQLError{Number: 1040})
	close(releaseProbe)

	if <-result {
		t.Fatal("stale successful probe overrode a newer connectivity failure")
	}
	if gate.state.Load() != databaseStateOpen {
		t.Fatalf("state=%d, want open", gate.state.Load())
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
