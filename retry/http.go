package retry

import (
	"context"
	stderrors "errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/justinswe/std/errors"
)

const retryDrainLimit = 512 << 10

var defaultRetryStatuses = map[int]struct{}{
	http.StatusRequestTimeout:      {},
	http.StatusTooEarly:            {},
	http.StatusTooManyRequests:     {},
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
}

// HTTPClient sends requests with retry behavior.
type HTTPClient struct {
	client *http.Client
}

// NewHTTPClient returns an HTTP client using http.DefaultClient as its base.
func NewHTTPClient(opts ...Option) *HTTPClient {
	return WrapHTTPClient(http.DefaultClient, opts...)
}

// WrapHTTPClient returns a retrying HTTP client that copies base's settings.
func WrapHTTPClient(base *http.Client, opts ...Option) *HTTPClient {
	if base == nil {
		base = http.DefaultClient
	}

	client := *base
	client.Transport = NewTransport(client.Transport, opts...)
	return &HTTPClient{client: &client}
}

// Do sends req with retry behavior.
func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, apperrors.New("retry: nil HTTP client")
	}
	return c.client.Do(req)
}

type retryTransport struct {
	base   http.RoundTripper
	config config
}

// NewTransport returns an HTTP RoundTripper with retry behavior.
func NewTransport(base http.RoundTripper, opts ...Option) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &retryTransport{
		base:   base,
		config: newHTTPConfig(opts...),
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, apperrors.New("retry: nil HTTP request")
	}
	ctx := req.Context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	start := t.config.now()
	for attempt := 1; ; attempt++ {
		attemptReq, err := requestForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}

		resp, err := t.base.RoundTrip(attemptReq)
		if err := ctx.Err(); err != nil {
			closeResponse(resp)
			return nil, err
		}

		shouldRetry := t.shouldRetry(req, resp, err)
		if !shouldRetry || attempt >= t.config.maxAttempts {
			return resp, err
		}

		delay := t.retryDelay(resp, attempt)
		if t.config.maxElapsed > 0 && t.config.now().Sub(start)+delay > t.config.maxElapsed {
			return resp, err
		}

		closeResponse(resp)
		if t.config.notify != nil {
			t.config.notify(Event{
				Attempt:     attempt,
				MaxAttempts: t.config.maxAttempts,
				Delay:       delay,
				Err:         err,
				Response:    resp,
			})
		}
		if err := t.config.sleep(ctx, delay); err != nil {
			return nil, retryContextError(ctx, err)
		}
	}
}

func (t *retryTransport) shouldRetry(req *http.Request, resp *http.Response, err error) bool {
	if !canRetryRequest(req) {
		return false
	}
	if !isRetryableMethod(req) {
		return false
	}
	if err != nil {
		return t.config.shouldRetry(err)
	}
	if resp == nil {
		return false
	}
	_, ok := defaultRetryStatuses[resp.StatusCode]
	return ok
}

func (t *retryTransport) retryDelay(resp *http.Response, attempt int) time.Duration {
	if delay, ok := retryAfterDelay(resp, t.config.now); ok {
		return delay
	}
	return t.config.delay(attempt)
}

func requestForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 1 {
		return req, nil
	}

	next := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		return next, nil
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, apperrors.Wrap(err, "retry: get HTTP request body")
	}
	next.Body = body
	return next, nil
}

func canRetryRequest(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func isRetryableMethod(req *http.Request) bool {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete:
		return true
	case http.MethodPost, http.MethodPatch:
		return req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != ""
	default:
		return false
	}
}

func retryAfterDelay(resp *http.Response, now func() time.Time) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > int64(time.Duration(1<<63-1)/time.Second) {
			return time.Duration(1<<63 - 1), true
		}
		return time.Duration(seconds) * time.Second, true
	}

	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := when.Sub(now())
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func closeResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, resp.Body, retryDrainLimit)
	_ = resp.Body.Close()
}

func retryHTTPError(err error) bool {
	if err == nil || IsPermanent(err) {
		return false
	}
	return !stderrors.Is(err, context.Canceled) && !stderrors.Is(err, context.DeadlineExceeded)
}
