package etlmonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbesPreservePrefixAndIndependentFailureRecovery(t *testing.T) {
	var ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/gateway/etl/ping":
			fmt.Fprint(w, "pong")
		case "/gateway/etl/ready":
			if !ready.Load() {
				w.WriteHeader(503)
				fmt.Fprint(w, "output not connected")
				return
			}
			fmt.Fprint(w, "OK")
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	m, err := New(Config{BaseURL: server.URL + "/gateway/etl/", BearerToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if s := m.Snapshot(); s.Probes["ping"].Success != nil || !s.Stale {
		t.Fatalf("initial state must be unknown: %+v", s)
	}
	for range 3 {
		m.collectProbe(context.Background(), "ping")
		m.collectProbe(context.Background(), "ready")
	}
	s := m.Snapshot()
	if !*s.Probes["ping"].Success || !s.Probes["ready"].Alerting || s.Probes["ready"].HTTPStatus != 503 {
		t.Fatalf("independent probes: %+v", s.Probes)
	}
	ready.Store(true)
	m.collectProbe(context.Background(), "ready")
	if !m.Snapshot().Probes["ready"].Alerting {
		t.Fatal("alert recovered without two successes")
	}
	m.collectProbe(context.Background(), "ready")
	if m.Snapshot().Probes["ready"].Alerting || m.Snapshot().Probes["ready"].LastSuccessAt == nil {
		t.Fatal("recovery missing")
	}
}

func TestMetricsRateResetMissingAndStale(t *testing.T) {
	var failed atomic.Bool
	var count atomic.Int64
	count.Store(100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failed.Load() {
			w.WriteHeader(502)
			return
		}
		fmt.Fprintf(w, "# TYPE input_received counter\ninput_received{path=\"root.input\"} %d\n# TYPE go_goroutines gauge\ngo_goroutines 7\n", count.Load())
	}))
	defer server.Close()
	m, _ := New(Config{BaseURL: server.URL})
	m.collectProbe(context.Background(), "metrics")
	s := m.Snapshot()
	if indicator(s, "input_rate") != nil || indicator(s, "output_sent_total") != nil {
		t.Fatal("missing values must not be zero")
	}
	if v := indicator(s, "input_received_total"); v == nil || *v != 100 {
		t.Fatal("input count missing")
	}
	count.Store(160)
	m.collectProbe(context.Background(), "metrics")
	if rate := indicator(m.Snapshot(), "input_rate"); rate == nil || *rate <= 0 {
		t.Fatal("rate not calculated")
	}
	count.Store(3)
	m.collectProbe(context.Background(), "metrics")
	if rate := indicator(m.Snapshot(), "input_rate"); rate != nil {
		t.Fatalf("reset produced misleading rate: %v", *rate)
	}
	failed.Store(true)
	m.collectProbe(context.Background(), "metrics")
	s = m.Snapshot()
	if !s.Stale || len(s.Series) != 2 || s.LastMetricsAt == nil {
		t.Fatalf("failure should retain stale data: %+v", s)
	}
	s.Series[0].Labels["mutation"] = "bad"
	if _, exists := m.Snapshot().Series[0].Labels["mutation"]; exists {
		t.Fatal("snapshot exposes mutable state")
	}
}

func TestMetricsTimingsAndNestedComponentDeduplication(t *testing.T) {
	for _, tc := range []struct {
		name, kind, sum string
		want            float64
	}{
		{"summary", "summary", "100000000", 50},
		{"histogram", "histogram", "0.1", 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "# TYPE input_received counter\ninput_received{path=\"root.input\"} 10\ninput_received{path=\"root.input.broker.inputs.0\"} 10\n" +
				"# TYPE output_latency_ns " + tc.kind + "\noutput_latency_ns_sum{path=\"root.output\"} " + tc.sum + "\noutput_latency_ns_count{path=\"root.output\"} 2\n" +
				"# TYPE go_gc_duration_seconds summary\ngo_gc_duration_seconds{quantile=\"0.99\"} NaN\ngo_gc_duration_seconds_sum 0\ngo_gc_duration_seconds_count 0\n"
			series, err := parseMetrics(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			s := Snapshot{Series: series}
			s.Summary = summarize(series, time.Now())
			if v := indicator(s, "input_received_total"); v == nil || *v != 10 {
				t.Fatalf("nested components double counted: %v", v)
			}
			if v := indicator(s, "output_latency_ms"); v == nil || *v != tc.want {
				t.Fatalf("incorrect timing unit: %v", v)
			}
			if _, err := json.Marshal(s); err != nil {
				t.Fatalf("nonfinite metric broke JSON: %v", err)
			}
		})
	}
}

func TestRunCancelsInflightRequestsAndMetricsParsingFailure(t *testing.T) {
	entered := make(chan struct{}, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }))
	defer server.Close()
	m, _ := New(Config{BaseURL: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("collector never started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
	for _, body := range []string{"", "<html>login</html>", "broken x"} {
		if _, err := parseMetrics(strings.NewReader(body)); err == nil {
			t.Fatalf("accepted invalid payload: %q", body)
		}
	}
}

func TestNewRejectsCredentialsAndAmbiguousURLs(t *testing.T) {
	for _, target := range []string{"ftp://example.com", "http://user:pass@example.com", "http://example.com?token=secret", "http://example.com#part"} {
		if _, err := New(Config{BaseURL: target}); err == nil {
			t.Fatalf("accepted invalid base URL %s", target)
		}
	}
	m, err := New(Config{})
	if err != nil || m.Snapshot().Configured {
		t.Fatal("unconfigured monitor should be available without probes")
	}
}

func TestCounterRatesUseElapsedTimeAndRestartInvalidatesAllRates(t *testing.T) {
	before := time.Unix(2000, 0)
	after := before.Add(30 * time.Second)
	previous, err := parseMetrics(strings.NewReader("# TYPE input_received counter\ninput_received 100\n# TYPE process_cpu_seconds_total counter\nprocess_cpu_seconds_total 40\n# TYPE process_start_time_seconds gauge\nprocess_start_time_seconds 1000\n"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := parseMetrics(strings.NewReader("# TYPE input_received counter\ninput_received 160\n# TYPE process_cpu_seconds_total counter\nprocess_cpu_seconds_total 46\n# TYPE process_start_time_seconds gauge\nprocess_start_time_seconds 1000\n"))
	if err != nil {
		t.Fatal(err)
	}
	applyRates(current, previous, &before, after)
	s := Snapshot{Summary: summarize(current, after)}
	if rate := indicator(s, "input_rate"); rate == nil || *rate != 2 {
		t.Fatalf("want 2 messages/s, got %v", rate)
	}
	if cpu := indicator(s, "cpu_percent"); cpu == nil || *cpu != 20 {
		t.Fatalf("want 20%% CPU, got %v", cpu)
	}
	if uptime := indicator(s, "uptime_seconds"); uptime == nil || *uptime != 1030 {
		t.Fatalf("want 1030s uptime, got %v", uptime)
	}
	for i := range current {
		current[i].RatePerSecond = nil
		if current[i].Name == "process_start_time_seconds" {
			current[i].Value = finite(2000)
		}
	}
	applyRates(current, previous, &before, after)
	if rate := indicator(Snapshot{Summary: summarize(current, after)}, "input_rate"); rate != nil {
		t.Fatal("restart should reset rate baseline even when new counter is higher")
	}
}

func TestHTTPRedirectIsNotAHealthyProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			fmt.Fprint(w, "login")
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer server.Close()
	m, _ := New(Config{BaseURL: server.URL})
	m.collectProbe(context.Background(), "ping")
	p := m.Snapshot().Probes["ping"]
	if p.Success == nil || *p.Success || p.HTTPStatus != 302 {
		t.Fatalf("redirect masqueraded as healthy: %+v", p)
	}
}

func indicator(s Snapshot, key string) *float64 {
	for _, v := range s.Summary {
		if v.Key == key {
			return v.Value
		}
	}
	return nil
}
