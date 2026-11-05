package shanghaimall

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/pkg/config"
)

func setupMallClientTest(t *testing.T) *http.Transport {
	t.Helper()
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("ShanghaiMall:\n  TimeoutSeconds: 17\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", configDir+string(os.PathSeparator))
	t.Setenv("SHANGHAI_MALL_TIMEOUT_SECONDS", "3")
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	http.DefaultTransport = transport
	t.Cleanup(func() {
		defaultClient().CloseIdleConnections()
		transport.CloseIdleConnections()
		http.DefaultTransport = original
	})
	return transport
}

func readMallClientTestResponse(ctx context.Context, client *http.Client, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, response.Body)
	return err
}

func TestMallDefaultClientReusesBurstConnections(t *testing.T) {
	setupMallClientTest(t)
	var connections atomic.Int32
	var arrived, release [3]chan struct{}
	for index := range arrived {
		arrived[index] = make(chan struct{}, 8)
		release[index] = make(chan struct{})
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		wave, _ := strconv.Atoi(request.URL.Query().Get("wave"))
		arrived[wave] <- struct{}{}
		select {
		case <-release[wave]:
			_, _ = io.WriteString(writer, "{}")
		case <-request.Context().Done():
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for wave := range 3 {
		var workers sync.WaitGroup
		for range 8 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				if err := readMallClientTestResponse(ctx, defaultClient(), fmt.Sprintf("%s?wave=%d", server.URL, wave)); err != nil {
					t.Error(err)
				}
			}()
		}
		for range 8 {
			select {
			case <-arrived[wave]:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		close(release[wave])
		workers.Wait()
		if connections.Load() != 8 {
			t.Errorf("wave %d opened %d total connections, want 8", wave+1, connections.Load())
		}
	}
}

func TestMallDefaultClientReusesConfigurationAndRefreshesTimeout(t *testing.T) {
	setupMallClientTest(t)
	initial := defaultClient()
	if initial != defaultClient() || initial.Timeout != 3*time.Second {
		t.Fatal("same timeout did not reuse its client")
	}
	for _, test := range []struct {
		value string
		want  time.Duration
	}{{"7", 7 * time.Second}, {"0", 10 * time.Second}, {"", 17 * time.Second}} {
		t.Setenv("SHANGHAI_MALL_TIMEOUT_SECONDS", test.value)
		client := defaultClient()
		if client == initial || client != defaultClient() || client.Timeout != test.want {
			t.Fatalf("timeout %q produced client timeout %v, want %v and stable reuse", test.value, client.Timeout, test.want)
		}
		if initial.Timeout != 3*time.Second {
			t.Fatal("configuration refresh mutated an existing client")
		}
	}
}

func TestMallDefaultClientRefreshClosesIdleConnectionsAndPreservesActiveRequests(t *testing.T) {
	for _, changed := range []string{"timeout", "transport"} {
		t.Run(changed, func(t *testing.T) {
			transport := setupMallClientTest(t)
			started, release := make(chan struct{}), make(chan struct{})
			closed := make(chan struct{}, 4)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/active" {
					close(started)
					select {
					case <-release:
					case <-request.Context().Done():
						return
					}
				}
				_, _ = io.WriteString(writer, "{}")
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					closed <- struct{}{}
				}
			}
			server.Start()
			defer server.Close()
			var releaseOnce sync.Once
			releaseActive := func() { releaseOnce.Do(func() { close(release) }) }
			defer releaseActive()
			initial := defaultClient()
			activeResult := make(chan error, 1)
			go func() { activeResult <- readMallClientTestResponse(t.Context(), initial, server.URL+"/active") }()
			select {
			case <-started:
			case err := <-activeResult:
				t.Fatalf("active request ended before reaching the server: %v", err)
			case <-time.After(time.Second):
				t.Fatal("active request did not reach the server")
			}
			if err := readMallClientTestResponse(t.Context(), initial, server.URL+"/idle"); err != nil {
				t.Fatal(err)
			}
			if changed == "timeout" {
				t.Setenv("SHANGHAI_MALL_TIMEOUT_SECONDS", "4")
			} else {
				http.DefaultTransport = transport.Clone()
			}
			if defaultClient() == initial {
				t.Fatal("configuration change retained its old client")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("configuration change left the old idle connection open")
			}
			releaseActive()
			if err := <-activeResult; err != nil {
				t.Fatalf("configuration change interrupted an active request: %v", err)
			}
		})
	}
}

func TestMallDefaultClientPreservesCustomTransportAndInjectedClient(t *testing.T) {
	setupMallClientTest(t)
	_ = defaultClient()
	var defaultCalls, injectedCalls int
	responseTransport := func(calls *int) http.RoundTripper {
		return kerryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			*calls = *calls + 1
			return &http.Response{
				StatusCode: http.StatusOK, Request: request,
				Body: io.NopCloser(strings.NewReader(`{"error":false}`)), Header: make(http.Header),
			}, nil
		})
	}
	http.DefaultTransport = responseTransport(&defaultCalls)
	if err := readMallClientTestResponse(t.Context(), defaultClient(), "https://default.test"); err != nil {
		t.Fatal(err)
	}
	result, err := postKerrySales(t.Context(), kerryConfig{
		SalesURL: "https://injected.test", Client: &http.Client{Transport: responseTransport(&injectedCalls)},
	}, map[string]interface{}{"docKey": "ORDER-1"})
	if err != nil || result == nil || !result.Success || defaultCalls != 1 || injectedCalls != 1 {
		t.Fatalf("custom/injected transport calls=%d/%d result=%+v error=%v", defaultCalls, injectedCalls, result, err)
	}
}

func TestMallDefaultClientConcurrentGetReusesOneClient(t *testing.T) {
	setupMallClientTest(t)
	start := make(chan struct{})
	clients := make(chan *http.Client, 64)
	var workers sync.WaitGroup
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			clients <- defaultClient()
		}()
	}
	close(start)
	workers.Wait()
	close(clients)
	var first *http.Client
	for client := range clients {
		if first == nil {
			first = client
		}
		if client != first {
			t.Fatal("concurrent callers received separate default clients")
		}
	}
}
