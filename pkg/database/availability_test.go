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
			if gateProbeDeadline(gate) < minimum {
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
	capacityDeadline := gateProbeDeadline(gate)
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	if got := gateProbeDeadline(gate); got < capacityDeadline {
		t.Fatalf("later failure shortened capacity cooldown: got=%d want-at-least=%d", got, capacityDeadline)
	}
}

func TestAvailabilityGateRecoversWithSingleProbe(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	makeGateProbeDue(gate)

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
	makeGateProbeDue(gate)

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
	makeGateProbeDue(gate)

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
	makeGateProbeDue(gate)

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
	minimum := time.Now().Add(databaseCapacityCooldown - time.Second).UnixNano()
	if deadline := gateProbeDeadline(gate); deadline < minimum {
		t.Fatalf("capacity cooldown lost after concurrent probe: deadline=%d minimum=%d", deadline, minimum)
	}
}

func TestAvailabilityGateCallerCancellationDoesNotExtendRecoveryCooldown(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(mysqlDriver.ErrInvalidConn)
	makeGateProbeDue(gate)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if gate.CanServe(ctx, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}) {
		t.Fatal("canceled recovery probe reported available")
	}
	if gate.state.Load() != databaseStateOpen {
		t.Fatalf("state=%d, want open", gate.state.Load())
	}
	if !gate.CanServe(t.Context(), func(context.Context) error { return nil }) {
		t.Fatal("caller cancellation delayed the next recovery probe")
	}
}

func TestProbeFailureCooldownDistinguishesCallerAndPoolTimeout(t *testing.T) {
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	cancelCaller()
	if cooldown, shouldOpen := probeFailureCooldown(callerCtx, callerCtx, context.Canceled); shouldOpen || cooldown != 0 {
		t.Fatalf("caller cancellation cooldown=%v shouldOpen=%t", cooldown, shouldOpen)
	}

	activeCaller := context.Background()
	probeCtx, cancelProbe := context.WithTimeoutCause(activeCaller, 0, errDatabaseProbeTimeout)
	defer cancelProbe()
	<-probeCtx.Done()
	if cooldown, shouldOpen := probeFailureCooldown(activeCaller, probeCtx, context.DeadlineExceeded); !shouldOpen || cooldown != databaseCapacityCooldown {
		t.Fatalf("pool timeout cooldown=%v shouldOpen=%t", cooldown, shouldOpen)
	}
	if cooldown, shouldOpen := probeFailureCooldown(callerCtx, callerCtx, &mysqlDriver.MySQLError{Number: 1040}); !shouldOpen || cooldown != databaseCapacityCooldown {
		t.Fatalf("capacity error after caller cancellation cooldown=%v shouldOpen=%t", cooldown, shouldOpen)
	}
	joinedCapacityError := errors.Join(&mysqlDriver.MySQLError{Number: 1040}, context.Canceled)
	if cooldown, shouldOpen := probeFailureCooldown(callerCtx, callerCtx, joinedCapacityError); !shouldOpen || cooldown != databaseCapacityCooldown {
		t.Fatalf("joined capacity error cooldown=%v shouldOpen=%t", cooldown, shouldOpen)
	}
	joinedConnectionError := errors.Join(mysqlDriver.ErrInvalidConn, context.Canceled)
	if cooldown, shouldOpen := probeFailureCooldown(callerCtx, callerCtx, joinedConnectionError); !shouldOpen || cooldown != databaseRecoveryProbeInterval {
		t.Fatalf("joined connection error cooldown=%v shouldOpen=%t", cooldown, shouldOpen)
	}
	wantRecoveryCooldown := databaseRecoveryCooldown(errors.New("permission denied"))
	if cooldown, shouldOpen := probeFailureCooldown(activeCaller, probeCtx, errors.New("permission denied")); !shouldOpen || cooldown != wantRecoveryCooldown {
		t.Fatalf("non-context error after probe deadline cooldown=%v shouldOpen=%t", cooldown, shouldOpen)
	}
}

func TestAvailabilityGateReadinessUsesRecentSuccessfulQuery(t *testing.T) {
	gate := newAvailabilityGate()
	gate.RecordResult(nil)
	var calls atomic.Int32
	if !gate.CheckReadiness(t.Context(), func(context.Context) error {
		calls.Add(1)
		return nil
	}) {
		t.Fatal("recent successful query was not accepted for readiness")
	}
	if calls.Load() != 0 {
		t.Fatalf("readiness ping calls=%d, want 0", calls.Load())
	}
}

func TestAvailabilityGateReadinessAllowsOnlyOneStaleProbe(t *testing.T) {
	gate := newAvailabilityGate()
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	result := make(chan bool, 1)
	var calls atomic.Int32
	go func() {
		result <- gate.CheckReadiness(t.Context(), func(context.Context) error {
			calls.Add(1)
			close(probeStarted)
			<-releaseProbe
			return nil
		})
	}()
	<-probeStarted
	if gate.CheckReadiness(t.Context(), func(context.Context) error {
		calls.Add(1)
		return nil
	}) {
		t.Fatal("concurrent readiness check bypassed the in-flight probe")
	}
	close(releaseProbe)
	if !<-result {
		t.Fatal("successful readiness probe reported unavailable")
	}
	if calls.Load() != 1 {
		t.Fatalf("readiness ping calls=%d, want 1", calls.Load())
	}
}

func TestAvailabilityGateConcurrentFailureWinsOverReadinessSuccess(t *testing.T) {
	gate := newAvailabilityGate()
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	result := make(chan bool, 1)
	go func() {
		result <- gate.CheckReadiness(t.Context(), func(context.Context) error {
			close(probeStarted)
			<-releaseProbe
			return nil
		})
	}()
	<-probeStarted
	gate.RecordResult(&mysqlDriver.MySQLError{Number: 1040})
	close(releaseProbe)

	if <-result {
		t.Fatal("stale readiness success overrode a newer capacity failure")
	}
	if gate.state.Load() != databaseStateOpen {
		t.Fatalf("state=%d, want open", gate.state.Load())
	}
	minimum := time.Now().Add(databaseCapacityCooldown - time.Second).UnixNano()
	if deadline := gateProbeDeadline(gate); deadline < minimum {
		t.Fatalf("capacity cooldown lost after readiness race: deadline=%d minimum=%d", deadline, minimum)
	}
}

func TestAvailabilityGateReadinessFailureOpensGate(t *testing.T) {
	gate := newAvailabilityGate()
	if gate.CheckReadiness(t.Context(), func(context.Context) error {
		return &mysqlDriver.MySQLError{Number: 1040}
	}) {
		t.Fatal("failed readiness probe reported available")
	}
	if gate.state.Load() != databaseStateOpen {
		t.Fatalf("state=%d, want open", gate.state.Load())
	}
	minimum := time.Now().Add(databaseCapacityCooldown - time.Second).UnixNano()
	if deadline := gateProbeDeadline(gate); deadline < minimum {
		t.Fatalf("capacity cooldown deadline=%d minimum=%d", deadline, minimum)
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
	applicationAvailability.mu.Lock()
	applicationAvailability.nextProbeUnixNS = time.Now().Add(time.Minute).UnixNano()
	applicationAvailability.mu.Unlock()
	if err := RequireAvailable(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RequireAvailable() error = %v, want ErrUnavailable", err)
	}
}

func makeGateProbeDue(gate *availabilityGate) {
	gate.mu.Lock()
	gate.nextProbeUnixNS = 0
	gate.mu.Unlock()
}

func gateProbeDeadline(gate *availabilityGate) int64 {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.nextProbeUnixNS
}
