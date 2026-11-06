// Package etlmonitor polls Bento's HTTP monitoring contract independently of business requests.
package etlmonitor

import (
	"context"
	"errors"
	"fmt"
	"gin-biz-web-api/pkg/providerhttp"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	BaseURL         string
	BearerToken     string
	Username        string
	Password        string
	HealthInterval  time.Duration
	MetricsInterval time.Duration
	HealthTimeout   time.Duration
	MetricsTimeout  time.Duration
	HTTPClient      *http.Client
}

type Probe struct {
	State                string     `json:"state"`
	Success              *bool      `json:"success"`
	HTTPStatus           int        `json:"http_status"`
	Error                string     `json:"error"`
	CheckedAt            *time.Time `json:"checked_at"`
	LastSuccessAt        *time.Time `json:"last_success_at"`
	DurationMS           float64    `json:"duration_ms"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	ConsecutiveSuccesses int        `json:"consecutive_successes"`
	Alerting             bool       `json:"alerting"`
}

type Indicator struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Value       *float64 `json:"value"`
	Unit        string   `json:"unit"`
	Description string   `json:"description"`
}

type Series struct {
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Labels        map[string]string `json:"labels"`
	Value         *float64          `json:"value"`
	RatePerSecond *float64          `json:"rate_per_second"`
}

type HistoryPoint struct {
	At         time.Time `json:"at"`
	InputRate  *float64  `json:"input_rate"`
	OutputRate *float64  `json:"output_rate"`
	ErrorRate  *float64  `json:"error_rate"`
	CPUPercent *float64  `json:"cpu_percent"`
	RSSBytes   *float64  `json:"rss_bytes"`
}

type Snapshot struct {
	Configured             bool             `json:"configured"`
	ConfigError            string           `json:"config_error"`
	Target                 string           `json:"target"`
	CheckedAt              *time.Time       `json:"checked_at"`
	LastMetricsAt          *time.Time       `json:"last_metrics_at"`
	Stale                  bool             `json:"stale"`
	HealthIntervalSeconds  float64          `json:"health_interval_seconds"`
	MetricsIntervalSeconds float64          `json:"metrics_interval_seconds"`
	Probes                 map[string]Probe `json:"probes"`
	Summary                []Indicator      `json:"summary"`
	Series                 []Series         `json:"series"`
	History                []HistoryPoint   `json:"history"`
}

type Monitor struct {
	cfg     Config
	client  *http.Client
	mu      sync.RWMutex
	runOnce sync.Once
	state   Snapshot
}

func New(cfg Config) (*Monitor, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return nil, errors.New("ETL monitor base URL must be HTTP(S), without credentials, query or fragment")
		}
	}
	if cfg.BearerToken != "" && (cfg.Username != "" || cfg.Password != "") {
		return nil, errors.New("ETL monitor accepts either bearer token or basic authentication")
	}
	for _, d := range []time.Duration{cfg.HealthInterval, cfg.MetricsInterval, cfg.HealthTimeout, cfg.MetricsTimeout} {
		if d < 0 {
			return nil, errors.New("ETL monitor durations must not be negative")
		}
	}
	if cfg.HealthInterval == 0 {
		cfg.HealthInterval = 15 * time.Second
	}
	if cfg.MetricsInterval == 0 {
		cfg.MetricsInterval = 30 * time.Second
	}
	if cfg.HealthTimeout == 0 {
		cfg.HealthTimeout = 3 * time.Second
	}
	if cfg.MetricsTimeout == 0 {
		cfg.MetricsTimeout = 5 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		// The shared provider client does not expose a dial timeout. Keep a dedicated
		// pool so stalled ETL connections cannot exhaust business HTTP resources.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = (&net.Dialer{Timeout: time.Second, KeepAlive: 30 * time.Second}).DialContext
		transport.MaxIdleConns = 6
		transport.MaxIdleConnsPerHost = 3
		transport.MaxConnsPerHost = 3
		transport.TLSHandshakeTimeout = time.Second
		client = &http.Client{Transport: transport}
	} else {
		copyClient := *client
		client = &copyClient
	}
	// Redirects can conceal a gateway login page or send credentials elsewhere.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	m := &Monitor{cfg: cfg, client: client, state: Snapshot{
		Configured: cfg.BaseURL != "", Target: cfg.BaseURL, Stale: true,
		HealthIntervalSeconds: cfg.HealthInterval.Seconds(), MetricsIntervalSeconds: cfg.MetricsInterval.Seconds(),
		Probes: map[string]Probe{"ping": {State: "unknown"}, "ready": {State: "unknown"}, "metrics": {State: "unknown"}},
		Series: []Series{}, History: []HistoryPoint{}, Summary: summarize(nil, time.Now()),
	}}
	return m, nil
}

// Run blocks until cancellation. Each endpoint has one worker, so requests never overlap.
func (m *Monitor) Run(ctx context.Context) {
	m.runOnce.Do(func() {
		if !m.state.Configured {
			return
		}
		defer m.client.CloseIdleConnections()
		var wg sync.WaitGroup
		for _, name := range []string{"ping", "ready", "metrics"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				interval := m.cfg.HealthInterval
				if name == "metrics" {
					interval = m.cfg.MetricsInterval
				}
				for ctx.Err() == nil {
					m.collectProbe(ctx, name)
					timer := time.NewTimer(interval)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}()
		}
		wg.Wait()
	})
}

// Snapshot returns an isolated, non-blocking view of the latest completed probes.
func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.state
	s.Probes = maps.Clone(s.Probes)
	for k, p := range s.Probes {
		p.Success = copyPtr(p.Success)
		p.CheckedAt = copyPtr(p.CheckedAt)
		p.LastSuccessAt = copyPtr(p.LastSuccessAt)
		s.Probes[k] = p
	}
	s.CheckedAt = copyPtr(s.CheckedAt)
	s.LastMetricsAt = copyPtr(s.LastMetricsAt)
	s.Series = make([]Series, len(m.state.Series))
	for i, v := range m.state.Series {
		v.Labels = maps.Clone(v.Labels)
		v.Value = copyPtr(v.Value)
		v.RatePerSecond = copyPtr(v.RatePerSecond)
		s.Series[i] = v
	}
	s.Summary = append([]Indicator{}, s.Summary...)
	for i := range s.Summary {
		s.Summary[i].Value = copyPtr(s.Summary[i].Value)
	}
	s.History = append([]HistoryPoint{}, s.History...)
	for i := range s.History {
		p := &s.History[i]
		p.InputRate = copyPtr(p.InputRate)
		p.OutputRate = copyPtr(p.OutputRate)
		p.ErrorRate = copyPtr(p.ErrorRate)
		p.CPUPercent = copyPtr(p.CPUPercent)
		p.RSSBytes = copyPtr(p.RSSBytes)
	}
	s.Stale = s.LastMetricsAt == nil || time.Since(*s.LastMetricsAt) > 2*m.cfg.MetricsInterval+m.cfg.MetricsTimeout || s.Probes["metrics"].Success == nil || !*s.Probes["metrics"].Success
	return s
}

func (m *Monitor) collectProbe(ctx context.Context, name string) {
	timeout := m.cfg.HealthTimeout
	if name == "metrics" {
		timeout = m.cfg.MetricsTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, m.cfg.BaseURL+"/"+name, nil)
	status := 0
	var series []Series
	if err == nil {
		if m.cfg.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+m.cfg.BearerToken)
		} else if m.cfg.Username != "" {
			req.SetBasicAuth(m.cfg.Username, m.cfg.Password)
		}
		if name == "metrics" {
			req.Header.Set("Accept", "text/plain; version=0.0.4")
		}
		var resp *http.Response
		resp, err = m.client.Do(req)
		if err == nil {
			status = resp.StatusCode
			defer resp.Body.Close()
			if status != http.StatusOK {
				err = fmt.Errorf("HTTP %d", status)
			} else if name == "metrics" {
				var body []byte
				body, err = io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
				if len(body) > 4<<20 {
					err = errors.New("metrics response exceeds 4 MiB")
				}
				if err == nil {
					series, err = parseMetrics(strings.NewReader(string(body)))
				}
			} else {
				_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	end := time.Now()
	m.mu.Lock()
	previous := m.state.Probes[name]
	p := previous
	success := err == nil
	p.Success = &success
	p.HTTPStatus = status
	p.CheckedAt = &end
	p.DurationMS = float64(end.Sub(start)) / float64(time.Millisecond)
	p.Error = ""
	if success {
		p.LastSuccessAt = &end
		p.ConsecutiveFailures = 0
		p.ConsecutiveSuccesses++
		if p.ConsecutiveSuccesses >= 2 {
			p.Alerting = false
		}
		p.State = "healthy"
		if p.Alerting {
			p.State = "recovering"
		}
	} else {
		p.ConsecutiveSuccesses = 0
		p.ConsecutiveFailures++
		if p.ConsecutiveFailures >= 3 {
			p.Alerting = true
		}
		p.State = "failing"
		if p.Alerting {
			p.State = "unhealthy"
		}
		if status == 0 {
			p.Error = "request failed: " + string(providerhttp.ClassifyRetry(0, err).Class)
		} else if status != 200 {
			p.Error = fmt.Sprintf("HTTP %d", status)
		} else {
			p.Error = "invalid or unreadable Prometheus response"
		}
	}
	m.state.Probes[name] = p
	m.state.CheckedAt = &end
	if success && name == "metrics" {
		applyRates(series, m.state.Series, m.state.LastMetricsAt, end)
		m.state.Series = series
		m.state.LastMetricsAt = &end
		m.state.Summary = summarize(series, end)
		values := map[string]*float64{}
		for _, v := range m.state.Summary {
			values[v.Key] = v.Value
		}
		m.state.History = append(m.state.History, HistoryPoint{At: end, InputRate: values["input_rate"], OutputRate: values["output_rate"], ErrorRate: values["error_rate"], CPUPercent: values["cpu_percent"], RSSBytes: values["rss_bytes"]})
		if len(m.state.History) > 60 {
			m.state.History = append([]HistoryPoint{}, m.state.History[len(m.state.History)-60:]...)
		}
	}
	m.mu.Unlock()
	if p.Alerting != previous.Alerting {
		slog.InfoContext(ctx, "ETL monitor probe state changed", "probe", name, "alerting", p.Alerting, "http_status", status, "state", p.State)
	}
}

func copyPtr[T any](v *T) *T {
	if v == nil {
		return nil
	}
	copyValue := *v
	return &copyValue
}
