package youzan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

const maxTokenResponseBytes = 1 << 20

// TokenConfig is read for each request; credentials and returned tokens are not cached.
type TokenConfig struct {
	URL          string
	ClientID     string
	ClientSecret string
	GrantID      string
}

// TokenClient shares HTTP connections across token requests.
type TokenClient struct {
	httpClient *http.Client
}

func NewTokenClient(httpClient *http.Client) (*TokenClient, error) {
	if httpClient == nil {
		var err error
		httpClient, err = providerhttp.NewClient(providerhttp.ClientConfig{
			Timeout: 30 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("create youzan token HTTP client: %w", err)
		}
	}
	return &TokenClient{httpClient: httpClient}, nil
}

func (client *TokenClient) AccessToken(ctx context.Context, cfg TokenConfig) (string, error) {
	body, err := json.Marshal(map[string]any{
		"authorize_type": "silent", "client_id": cfg.ClientID,
		"client_secret": cfg.ClientSecret, "grant_id": cfg.GrantID, "refresh": false,
	})
	if err != nil {
		return "", newTokenError(nil, "encode_request_failed", providerhttp.RetryClassification{Class: providerhttp.ErrorClassRequest}, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return "", newTokenError(nil, "invalid_request", providerhttp.RetryClassification{Class: providerhttp.ErrorClassRequest}, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return "", newTokenError(nil, "transport_failed", providerhttp.ClassifyRetry(0, err), err)
	}
	defer resp.Body.Close()

	// Small success and failure responses reach EOF for connection reuse.
	// An oversized response is closed without an unbounded drain.
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes+1))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		classification := providerhttp.ClassifyRetry(resp.StatusCode, nil)
		if classification.Class == providerhttp.ErrorClassNone {
			classification.Class = providerhttp.ErrorClassResponse
		}
		return "", newTokenError(resp, "http_failure", classification, readErr)
	}
	if readErr != nil {
		return "", newTokenError(resp, "response_read_failed", providerhttp.ClassifyRetry(0, readErr), readErr)
	}
	invalidResponse := providerhttp.RetryClassification{Class: providerhttp.ErrorClassResponse}
	if len(responseBody) > maxTokenResponseBytes {
		return "", newTokenError(resp, "response_too_large", invalidResponse, nil)
	}
	var response apiResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", newTokenError(resp, "invalid_json", invalidResponse, err)
	}
	code := response.Code
	if response.GatewayError != nil {
		code = response.GatewayError.Code
	}
	var numericCode int64
	if code != "" {
		numericCode, err = code.Int64()
		if err != nil {
			return "", newTokenError(resp, "invalid_json", invalidResponse, err)
		}
	}
	if response.GatewayError != nil || response.Success == nil || !*response.Success || numericCode != http.StatusOK {
		classification := invalidResponse
		if numericCode == 4101 {
			classification = providerhttp.RetryClassification{Class: providerhttp.ErrorClassRateLimited, Retryable: true}
		}
		failure := newTokenError(resp, "api_failure", classification, nil)
		failure.code = numericCode
		return "", failure
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if len(response.Data) > 0 {
		if err := json.Unmarshal(response.Data, &data); err != nil {
			return "", newTokenError(resp, "invalid_json", invalidResponse, err)
		}
	}
	if strings.TrimSpace(data.AccessToken) == "" {
		return "", newTokenError(resp, "missing_token", invalidResponse, nil)
	}
	return data.AccessToken, nil
}

type tokenError struct {
	httpStatus  int
	contentType string
	class       providerhttp.ErrorClass
	reason      string
	code        int64
	retryDelay  time.Duration
	cause       error
}

func (err *tokenError) Error() string {
	return fmt.Sprintf("youzan token failed: stage=token http_status=%d content_type=%s class=%s reason=%s code=%d",
		err.httpStatus, err.contentType, err.class, err.reason, err.code)
}

func (err *tokenError) RetryDelay() time.Duration { return err.retryDelay }
func (err *tokenError) Unwrap() error             { return err.cause }

func newTokenError(resp *http.Response, reason string, classification providerhttp.RetryClassification, cause error) *tokenError {
	err := &tokenError{contentType: "unknown", class: classification.Class, reason: reason}
	if classification.Retryable {
		err.retryDelay = time.Minute
	}
	if resp != nil {
		err.httpStatus = resp.StatusCode
		if mediaType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type")); parseErr == nil && mediaType != "" {
			err.contentType = mediaType
		}
		if classification.Retryable {
			err.retryDelay = max(err.retryDelay, providerhttp.RetryAfter(resp.Header, time.Now()))
		}
	}
	// Never expose raw transport URLs, decoder messages or provider response bodies.
	// Only retain safe cancellation/truncation identities for callers.
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF, io.EOF} {
		if errors.Is(cause, sentinel) {
			err.cause = sentinel
			break
		}
	}
	return err
}
