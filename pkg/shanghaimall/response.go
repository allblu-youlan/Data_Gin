package shanghaimall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

const maxLoginResponseBytes = 1 << 20

type loginResponseError struct {
	Provider    string
	HTTPStatus  int
	ContentType string
	Class       providerhttp.ErrorClass
	Retryable   bool
	Reason      string
	retryAfter  time.Duration
	cause       error
}

func (err *loginResponseError) Error() string {
	return fmt.Sprintf("%s login failed: stage=login class=%s http_status=%d content_type=%s reason=%s",
		err.Provider, err.Class, err.HTTPStatus, err.ContentType, err.Reason)
}

func (err *loginResponseError) RetryDelay() time.Duration {
	if !err.Retryable {
		return 0
	}
	return err.retryAfter
}

func (err *loginResponseError) Unwrap() error { return err.cause }

func loginTransportError(provider string, err error) error {
	return newLoginResponseError(provider, nil, "transport_failed", providerhttp.ClassifyRetry(0, err), err)
}

func decodeLoginResponse(provider string, response *http.Response, destination any) error {
	if response == nil || response.Body == nil {
		return newLoginResponseError(provider, response, "missing_response", providerhttp.RetryClassification{
			Class: providerhttp.ErrorClassResponse,
		}, nil)
	}
	// Small responses reach EOF, allowing connection reuse on success and failure.
	// Oversized responses are closed by the caller without an unbounded drain.
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxLoginResponseBytes+1))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		classification := providerhttp.ClassifyRetry(response.StatusCode, nil)
		if classification.Class == providerhttp.ErrorClassNone {
			classification.Class = providerhttp.ErrorClassResponse
		}
		return newLoginResponseError(provider, response, "http_failure", classification, readErr)
	}
	if readErr != nil {
		return newLoginResponseError(provider, response, "response_read_failed", providerhttp.ClassifyRetry(0, readErr), readErr)
	}
	if len(body) > maxLoginResponseBytes {
		return newLoginResponseError(provider, response, "response_too_large", providerhttp.RetryClassification{
			Class: providerhttp.ErrorClassResponse,
		}, nil)
	}
	if err := decodeAndDrainJSON(bytes.NewReader(body), destination); err != nil {
		return newLoginResponseError(provider, response, "invalid_json", providerhttp.RetryClassification{
			Class: providerhttp.ErrorClassResponse,
		}, err)
	}
	return nil
}

func newLoginResponseError(provider string, response *http.Response, reason string, classification providerhttp.RetryClassification, cause error) *loginResponseError {
	err := &loginResponseError{
		Provider: provider, ContentType: "unknown", Class: classification.Class,
		Retryable: classification.Retryable, Reason: reason,
	}
	if response != nil {
		err.HTTPStatus = response.StatusCode
		if mediaType, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type")); parseErr == nil && mediaType != "" {
			err.ContentType = mediaType
		}
		if classification.Retryable {
			err.retryAfter = providerhttp.RetryAfter(response.Header, time.Now())
		}
	}
	// Preserve cancellation and truncation identity, never an arbitrary decoder
	// or transport message that could contain credentials or response content.
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF, io.EOF} {
		if errors.Is(cause, sentinel) {
			err.cause = sentinel
			break
		}
	}
	return err
}

func loginFailureResult(target Target, err error) *PushResult {
	var responseError *loginResponseError
	if !errors.As(err, &responseError) {
		return nil
	}
	return &PushResult{Target: target, HTTPStatus: responseError.HTTPStatus, ResponseBody: responseError.Error()}
}

func decodeAndDrainJSON(body io.Reader, destination any) error {
	if err := json.NewDecoder(body).Decode(destination); err != nil {
		return err
	}
	// Consume the remaining response while preserving Decode's tolerance of trailing content.
	_, err := io.Copy(io.Discard, body)
	return err
}
