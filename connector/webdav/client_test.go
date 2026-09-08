package webdav

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientUploadFileToFixedDirectory(t *testing.T) {
	var gotPath, gotUsername, gotPassword, gotBody string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.EscapedPath()
		gotUsername, gotPassword, _ = request.BasicAuth()
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		gotBody = string(body)
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	localPath := filepath.Join(t.TempDir(), "report.xlsx")
	if err := os.WriteFile(localPath, []byte("excel-content"), 0o600); err != nil {
		t.Fatalf("write test workbook: %v", err)
	}
	client := NewClient(server.Client())
	err := client.UploadFile(t.Context(), Config{
		BaseURL: server.URL + "/dav/", Username: "account@example.com", Password: "app-password", Directory: "/固定 报表/销售",
	}, localPath, "销售日报.xlsx")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if gotPath != "/dav/%E5%9B%BA%E5%AE%9A%20%E6%8A%A5%E8%A1%A8/%E9%94%80%E5%94%AE/%E9%94%80%E5%94%AE%E6%97%A5%E6%8A%A5.xlsx" || gotUsername != "account@example.com" || gotPassword != "app-password" || gotBody != "excel-content" {
		t.Fatalf("request = path %q, username %q, password %q, body %q", gotPath, gotUsername, gotPassword, gotBody)
	}
}

func TestClientUploadFileClassifiesHTTPFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		retryable bool
	}{
		{name: "authentication rejected", status: http.StatusUnauthorized},
		{name: "directory missing", status: http.StatusConflict},
		{name: "rate limited", status: http.StatusTooManyRequests, retryable: true},
		{name: "server unavailable", status: http.StatusServiceUnavailable, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(test.status) }))
			defer server.Close()
			localPath := filepath.Join(t.TempDir(), "report.xlsx")
			if err := os.WriteFile(localPath, []byte("x"), 0o600); err != nil {
				t.Fatalf("write test workbook: %v", err)
			}
			err := NewClient(server.Client()).UploadFile(t.Context(), Config{BaseURL: server.URL, Username: "user", Password: "password"}, localPath, "report.xlsx")
			if err == nil || Retryable(err) != test.retryable {
				t.Fatalf("UploadFile() error = %v, retryable = %t", err, Retryable(err))
			}
		})
	}
}

func TestDestinationURLRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		directory string
		fileName  string
	}{
		{name: "plain HTTP", baseURL: "http://dav.example.com/dav", directory: "/reports", fileName: "report.xlsx"},
		{name: "URL credentials", baseURL: "https://user:password@dav.example.com/dav", directory: "/reports", fileName: "report.xlsx"},
		{name: "parent directory", baseURL: "https://dav.example.com/dav", directory: "/reports/../private", fileName: "report.xlsx"},
		{name: "file path", baseURL: "https://dav.example.com/dav", directory: "/reports", fileName: "nested/report.xlsx"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := destinationURL(test.baseURL, test.directory, test.fileName); err == nil {
				t.Fatal("destinationURL() accepted unsafe configuration")
			}
		})
	}
}

func TestUploadErrorDoesNotExposeResponseBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte("password=secret-private"))
	}))
	defer server.Close()
	localPath := filepath.Join(t.TempDir(), "report.xlsx")
	if err := os.WriteFile(localPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write test workbook: %v", err)
	}
	err := NewClient(server.Client()).UploadFile(t.Context(), Config{BaseURL: server.URL, Username: "user", Password: "password"}, localPath, "report.xlsx")
	if err == nil || strings.Contains(err.Error(), "secret-private") {
		t.Fatalf("UploadFile() error = %v", err)
	}
}
