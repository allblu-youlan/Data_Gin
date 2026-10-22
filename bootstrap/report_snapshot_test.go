package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"
)

type fakeBootstrapSnapshotStore struct {
	initialized, closed bool
	initErr, closeErr   error
}

func (s *fakeBootstrapSnapshotStore) InitializeJSONSnapshotStore(context.Context) error {
	s.initialized = true
	return s.initErr
}

func (s *fakeBootstrapSnapshotStore) Close() error {
	s.closed = true
	return s.closeErr
}

func TestInitializeSystemReportSnapshotsSkipsDisabledOrUnconfiguredSystemOracle(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		loads := 0
		ready, err := initializeSystemReportSnapshots(t.Context(), enabled, func() (appConfig.ReportInputOracleConfig, error) {
			loads++
			return appConfig.ReportInputOracleConfig{}, nil
		}, func(context.Context, reportoracle.Config) (reportSnapshotInitializer, error) {
			t.Fatal("disabled or unconfigured report storage opened Oracle")
			return nil, nil
		})
		if ready || err != nil || (!enabled && loads != 0) || (enabled && loads != 1) {
			t.Fatalf("enabled=%t ready=%t error=%v loads=%d", enabled, ready, err, loads)
		}
	}
}

func TestInitializeSystemReportSnapshotsUsesDedicatedSystemConnectionAndPreservesFailures(t *testing.T) {
	failure := errors.New("Oracle initialization failed")
	for _, tc := range []struct {
		name                       string
		openErr, initErr, closeErr error
	}{
		{name: "ready"},
		{name: "source unavailable", openErr: failure},
		{name: "create privilege missing", initErr: failure},
		{name: "close failed", closeErr: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configured := appConfig.ReportInputOracleConfig{Host: "system-oracle", Port: 1521, ServiceName: "SYSTEMDB", Username: "report_user", Password: "test-password", MaxOpenConnections: 20}
			store := &fakeBootstrapSnapshotStore{initErr: tc.initErr, closeErr: tc.closeErr}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			ready, err := initializeSystemReportSnapshots(ctx, true, func() (appConfig.ReportInputOracleConfig, error) {
				return configured, nil
			}, func(gotCtx context.Context, config reportoracle.Config) (reportSnapshotInitializer, error) {
				if gotCtx != ctx || config.Host != configured.Host || config.Username != configured.Username || config.Password != configured.Password || config.ServiceName != configured.ServiceName || config.MaxOpenConnections != 1 || config.MaxIdleConnections != 1 {
					t.Fatal("initialization did not reuse the bounded system Oracle connection")
				}
				return store, tc.openErr
			})
			wantFailure := tc.openErr != nil || tc.initErr != nil || tc.closeErr != nil
			if errors.Is(err, failure) != wantFailure || ready != (tc.openErr == nil && tc.initErr == nil) || store.closed != (tc.openErr == nil) || store.initialized != (tc.openErr == nil) {
				t.Fatalf("ready=%t error=%v initialized=%t closed=%t", ready, err, store.initialized, store.closed)
			}
		})
	}
}
