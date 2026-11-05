package shanghaimall

import (
	"net/http"
	"sync"
	"time"

	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/providerhttp"
)

var defaultClientCache struct {
	sync.Mutex
	transport *http.Transport
	timeout   time.Duration
	client    *http.Client
}

func defaultClient() *http.Client {
	timeoutSeconds := envInt("SHANGHAI_MALL_TIMEOUT_SECONDS", config.GetInt("ShanghaiMall.TimeoutSeconds", 10))
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	transport, _ := http.DefaultTransport.(*http.Transport)

	defaultClientCache.Lock()
	if transport != nil && defaultClientCache.transport == transport && defaultClientCache.timeout == timeout {
		client := defaultClientCache.client
		defaultClientCache.Unlock()
		return client
	}

	previous := defaultClientCache.client
	defaultClientCache.client = nil
	defaultClientCache.transport = nil
	client := &http.Client{Timeout: timeout}
	if transport != nil {
		tlsTimeout := transport.TLSHandshakeTimeout
		if tlsTimeout <= 0 {
			tlsTimeout = timeout
		}
		sharedClient, err := providerhttp.NewClient(providerhttp.ClientConfig{
			Timeout:               timeout,
			TLSHandshakeTimeout:   tlsTimeout,
			ResponseHeaderTimeout: timeout,
		})
		if err == nil {
			client = sharedClient
			defaultClientCache.client = client
			defaultClientCache.transport = transport
			defaultClientCache.timeout = timeout
		}
	}
	defaultClientCache.Unlock()

	if previous != nil {
		// Existing requests keep their client and finish with its original timeout.
		previous.CloseIdleConnections()
	}
	return client
}
