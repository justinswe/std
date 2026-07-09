package retry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type closeTrackingBody struct {
	*strings.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestHTTPRetriesStatusThenSuccess(t *testing.T) {
	t.Parallel()

	firstBody := &closeTrackingBody{Reader: strings.NewReader("retry")}
	attempts := 0
	transport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusServiceUnavailable, firstBody), nil
		}
		return response(http.StatusOK, io.NopCloser(strings.NewReader("ok"))), nil
	}), WithBackoff(ConstantBackoff(0)), WithJitter(NoJitter)).(*retryTransport)
	transport.config.sleep = func(context.Context, time.Duration) error { return nil }

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !firstBody.closed {
		t.Fatal("retryable response body was not closed")
	}
}

func TestHTTPReturnsFinalRetryableResponse(t *testing.T) {
	t.Parallel()

	body := io.NopCloser(strings.NewReader("final"))
	transport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable, body), nil
	}), WithMaxAttempts(1)).(*retryTransport)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "final" {
		t.Fatalf("body = %q, want final", got)
	}
}

func TestHTTPDoesNotRetryUnsafePost(t *testing.T) {
	t.Parallel()

	want := errors.New("connection reset")
	attempts := 0
	transport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, want
	}), WithMaxAttempts(3)).(*retryTransport)

	req, err := http.NewRequest(http.MethodPost, "https://example.com", io.NopCloser(strings.NewReader("payload")))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if resp != nil {
		t.Fatalf("response = %v, want nil", resp)
	}
	if !errors.Is(err, want) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, want)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestHTTPReplaysRequestBody(t *testing.T) {
	t.Parallel()

	var bodies []string
	attempts := 0
	transport := NewTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(body))
		if attempts == 1 {
			return response(http.StatusTooManyRequests, io.NopCloser(strings.NewReader("retry"))), nil
		}
		return response(http.StatusOK, io.NopCloser(strings.NewReader("ok"))), nil
	}), WithBackoff(ConstantBackoff(0)), WithJitter(NoJitter)).(*retryTransport)
	transport.config.sleep = func(context.Context, time.Duration) error { return nil }

	req, err := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Idempotency-Key", "request-1")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	if got, want := strings.Join(bodies, ","), "payload,payload"; got != want {
		t.Fatalf("request bodies = %q, want %q", got, want)
	}
}

func TestHTTPHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	var gotDelay time.Duration
	attempts := 0
	transport := NewTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			resp := response(http.StatusTooManyRequests, io.NopCloser(strings.NewReader("retry")))
			resp.Header.Set("Retry-After", "2")
			return resp, nil
		}
		return response(http.StatusOK, io.NopCloser(strings.NewReader("ok"))), nil
	}), WithBackoff(ConstantBackoff(time.Minute)), WithNotify(func(event Event) {
		gotDelay = event.Delay
	})).(*retryTransport)
	transport.config.sleep = func(context.Context, time.Duration) error { return nil }

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	if gotDelay != 2*time.Second {
		t.Fatalf("retry delay = %v, want 2s", gotDelay)
	}
}

func response(status int, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       body,
	}
}
