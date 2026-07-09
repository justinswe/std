package retry

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"
)

func TestDoValueRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	attempts := 0
	var events []Event
	got, err := DoValue(context.Background(), func(context.Context) (string, error) {
		attempts++
		if attempts < 3 {
			return "", Retryable(fmt.Errorf("attempt %d", attempts))
		}
		return "ok", nil
	}, WithBackoff(ConstantBackoff(0)), WithJitter(NoJitter), WithMaxAttempts(3), WithNotify(func(event Event) {
		events = append(events, event)
	}))
	if err != nil {
		t.Fatalf("DoValue() error = %v", err)
	}
	if got != "ok" {
		t.Fatalf("DoValue() = %q, want ok", got)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	for i, event := range events {
		if event.Attempt != i+1 || event.MaxAttempts != 3 || event.Err == nil {
			t.Fatalf("event[%d] = %#v", i, event)
		}
	}
}

func TestDoStopsOnPermanentError(t *testing.T) {
	t.Parallel()

	want := stderrors.New("not retryable")
	attempts := 0
	err := Do(context.Background(), func(context.Context) error {
		attempts++
		return Permanent(want)
	}, WithRetryIf(func(error) bool { return true }), WithMaxAttempts(3))
	if !stderrors.Is(err, want) {
		t.Fatalf("Do() error = %v, want %v", err, want)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryMarkers(t *testing.T) {
	t.Parallel()

	inner := stderrors.New("inner")
	retryable := fmt.Errorf("wrapped: %w", Retryable(inner))
	if !IsRetryable(retryable) {
		t.Fatal("IsRetryable() = false, want true")
	}
	if !stderrors.Is(retryable, inner) {
		t.Fatal("retryable marker does not unwrap to inner error")
	}

	permanent := fmt.Errorf("wrapped: %w", Permanent(inner))
	if !IsPermanent(permanent) {
		t.Fatal("IsPermanent() = false, want true")
	}
	if !stderrors.Is(permanent, inner) {
		t.Fatal("permanent marker does not unwrap to inner error")
	}

	nested := Retryable(Permanent(inner))
	if !IsPermanent(nested) {
		t.Fatal("IsPermanent() nested in retryable marker = false, want true")
	}
}

func TestDoReturnsContextErrorFromSleep(t *testing.T) {
	t.Parallel()

	c := defaultConfig()
	c.backoff = ConstantBackoff(time.Second)
	c.jitter = NoJitter
	c.maxAttempts = 2
	c.retryIf = func(error) bool { return true }
	c.sleep = func(context.Context, time.Duration) error {
		return context.Canceled
	}

	err := doValueNoResult(context.Background(), func(context.Context) error {
		return stderrors.New("retry me")
	}, c)
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("doValue() error = %v, want context.Canceled", err)
	}
}

func doValueNoResult(ctx context.Context, fn Operation, c config) error {
	_, err := doValue(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	}, c)
	return err
}

func TestBackoffDelays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backoff Backoff
		attempt int
		want    time.Duration
	}{
		{name: "constant", backoff: ConstantBackoff(2 * time.Second), attempt: 3, want: 2 * time.Second},
		{name: "linear", backoff: LinearBackoff(time.Second, 5*time.Second), attempt: 3, want: 3 * time.Second},
		{name: "linear capped", backoff: LinearBackoff(time.Second, 2*time.Second), attempt: 3, want: 2 * time.Second},
		{name: "exponential", backoff: ExponentialBackoff(time.Second, 10*time.Second), attempt: 4, want: 8 * time.Second},
		{name: "exponential capped", backoff: ExponentialBackoff(time.Second, 4*time.Second), attempt: 4, want: 4 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.backoff.Delay(tt.attempt); got != tt.want {
				t.Fatalf("Delay(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestJitterDelays(t *testing.T) {
	t.Parallel()

	c := defaultConfig()
	c.randInt64N = func(n int64) int64 {
		return n - 1
	}

	c.jitter = FullJitter
	if got, want := c.jitterDelay(10*time.Nanosecond), 10*time.Nanosecond; got != want {
		t.Fatalf("full jitter = %v, want %v", got, want)
	}

	c.jitter = EqualJitter
	if got, want := c.jitterDelay(10*time.Nanosecond), 10*time.Nanosecond; got != want {
		t.Fatalf("equal jitter = %v, want %v", got, want)
	}

	c.jitter = NoJitter
	if got, want := c.jitterDelay(10*time.Nanosecond), 10*time.Nanosecond; got != want {
		t.Fatalf("no jitter = %v, want %v", got, want)
	}
}
