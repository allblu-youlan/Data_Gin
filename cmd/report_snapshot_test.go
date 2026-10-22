package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"

	"github.com/spf13/cobra"
)

type reportSnapshotTestStore struct {
	checks, installs, closes int
	err, closeErr            error
}

func (s *reportSnapshotTestStore) ValidateJSONSnapshotStore(context.Context) error {
	s.checks++
	return s.err
}
func (s *reportSnapshotTestStore) InitializeJSONSnapshotStore(context.Context) error {
	s.installs++
	return s.err
}
func (s *reportSnapshotTestStore) Close() error { s.closes++; return s.closeErr }

func TestReportSnapshotCommandsReuseSystemOracleWithoutStartingServer(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"check", "init"} {
		t.Run(action, func(t *testing.T) {
			store := &reportSnapshotTestStore{}
			configured := appConfig.ReportInputOracleConfig{Host: "system-oracle", Port: 1521, ServiceName: "SYSTEMDB", Username: "system-report", Password: "test-secret"}
			command := newReportSnapshotCmd(func() (appConfig.ReportInputOracleConfig, error) { return configured, nil }, func(ctx context.Context, config reportoracle.Config) (reportSnapshotStore, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 20*time.Second || config.Host != configured.Host || config.Username != configured.Username || config.Password != configured.Password || config.MaxOpenConnections != 1 {
					t.Fatal("command did not reuse the bounded system Oracle connection")
				}
				return store, nil
			})
			serverStarts := 0
			root := &cobra.Command{Use: "main", PersistentPreRun: func(*cobra.Command, []string) { serverStarts++ }}
			root.AddCommand(command)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs([]string{"report-snapshot", action, "--timeout", "20s"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			wantInstalls := 0
			if action == "init" {
				wantInstalls = 1
			}
			if serverStarts != 0 || store.installs != wantInstalls || store.checks != 1-wantInstalls || store.closes != 1 || !strings.Contains(out.String(), "已就绪") || strings.Contains(out.String(), configured.Password) {
				t.Fatalf("unexpected command lifecycle: server=%d store=%+v output=%s", serverStarts, store, out.String())
			}
		})
	}
}

func TestReportSnapshotCommandRejectsIncompleteConfigurationBeforeOpening(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		config  appConfig.ReportInputOracleConfig
		timeout string
	}{
		{name: "missing connection"},
		{name: "two connect targets", config: appConfig.ReportInputOracleConfig{Host: "host", Username: "user", Password: "secret", ServiceName: "service", SID: "sid"}},
		{name: "invalid timeout", timeout: "0s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			opened := false
			command := newReportSnapshotCmd(func() (appConfig.ReportInputOracleConfig, error) { return test.config, nil }, func(context.Context, reportoracle.Config) (reportSnapshotStore, error) {
				opened = true
				return &reportSnapshotTestStore{}, nil
			})
			args := []string{"init"}
			if test.timeout != "" {
				args = append(args, "--timeout", test.timeout)
			}
			command.SetArgs(args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			if err := command.Execute(); err == nil || opened {
				t.Fatalf("invalid command opened Oracle: error=%v opened=%t", err, opened)
			}
		})
	}
}

func TestReportSnapshotCommandReturnsFailureAndClosesConnection(t *testing.T) {
	t.Parallel()
	failure := errors.New("initialization failed")
	store := &reportSnapshotTestStore{err: failure}
	command := newReportSnapshotCmd(func() (appConfig.ReportInputOracleConfig, error) {
		return appConfig.ReportInputOracleConfig{Host: "host", Username: "user", Password: "secret", SID: "service"}, nil
	}, func(context.Context, reportoracle.Config) (reportSnapshotStore, error) { return store, nil })
	command.SetArgs([]string{"init"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); !errors.Is(err, failure) || store.closes != 1 {
		t.Fatalf("initialization error=%v closes=%d", err, store.closes)
	}
}
