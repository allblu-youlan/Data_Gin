package data_svc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gin-biz-web-api/model"
	pkgConfig "gin-biz-web-api/pkg/config"
)

func TestBojunHenglongPushCancelsHTTPRequest(t *testing.T) {
	setupBojunHenglongContextTestConfig(t, "https://henglong.test/sales")
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	http.DefaultTransport = bojunHenglongTestTransport(func(request *http.Request) (*http.Response, error) {
		cancel()
		select {
		case <-request.Context().Done():
			return nil, request.Context().Err()
		case <-time.After(time.Second):
			return nil, errors.New("HTTP request ignored task cancellation")
		}
	})
	service := &BojunOrderPushService{
		retailOrderDAO: &fakeBojunSyncUpdater{},
		logDAO:         &fakeDeliveryLogCreator{},
		pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
	}
	order := &model.BojunRetailOrder{DocNo: "ORDER-1", StoreCode: "ABCN002A001", BillDate: 20261105, TotalAmtActual: 88}

	result := service.PushNewOrder(ctx, order)
	if result.Success || !errors.Is(result.Error, context.Canceled) {
		t.Fatalf("canceled push result = %+v, want context.Canceled", result)
	}
}

func TestBojunHenglongPushDeadlineStopsResponseBodyRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		select {
		case <-request.Context().Done():
		case <-time.After(time.Second):
			_, _ = io.WriteString(writer, "<responsecode>0</responsecode>")
		}
	}))
	defer server.Close()
	setupBojunHenglongContextTestConfig(t, server.URL)
	service := &BojunOrderPushService{
		retailOrderDAO: &fakeBojunSyncUpdater{},
		logDAO:         &fakeDeliveryLogCreator{},
		pipelineRunDAO: &fakeBojunPipelineRunRecorder{},
	}
	order := &model.BojunRetailOrder{DocNo: "ORDER-1", StoreCode: "ABCN002A001", BillDate: 20261105, TotalAmtActual: 88}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	result := service.PushNewOrder(ctx, order)
	if result.Success || !errors.Is(result.Error, context.DeadlineExceeded) {
		t.Fatalf("push with stalled response body = %+v, want context.DeadlineExceeded", result)
	}
}

func setupBojunHenglongContextTestConfig(t *testing.T, endpoint string) {
	t.Helper()
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("HengLong:\n  SalesAPIURL: "+endpoint+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgConfig.NewConfig("", configDir+string(os.PathSeparator))
}
