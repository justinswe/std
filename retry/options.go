package retry

import (
	"context"
	"math/rand/v2"
	"net/http"
	"time"
)

const (
	defaultMaxAttempts = 3
)

// Jitter controls how retry delays are randomized.
type Jitter int

const (
	// NoJitter leaves retry delays unchanged.
	NoJitter Jitter = iota
	// FullJitter randomizes each delay between zero and the calculated delay.
	FullJitter
	// EqualJitter keeps half the calculated delay and randomizes the other half.
	EqualJitter
)

// Event describes a retry that is about to sleep before the next attempt.
type Event struct {
	Attempt     int
	MaxAttempts int
	Delay       time.Duration
	Err         error
	Response    *http.Response
}

// Option configures retry behavior.
type Option interface {
	applyRetry(*config)
}

type optionFunc func(*config)

func (f optionFunc) applyRetry(c *config) {
	f(c)
}

type config struct {
	backoff     Backoff
	jitter      Jitter
	maxAttempts int
	maxElapsed  time.Duration
	retryIf     func(error) bool
	notify      func(Event)
	randInt64N  func(int64) int64
	sleep       func(context.Context, time.Duration) error
	now         func() time.Time
}

func newConfig(opts ...Option) config {
	c := defaultConfig()
	applyOptions(&c, opts...)
	return c
}

func newHTTPConfig(opts ...Option) config {
	c := defaultConfig()
	c.retryIf = retryHTTPError
	applyOptions(&c, opts...)
	return c
}

func defaultConfig() config {
	return config{
		backoff:     ExponentialBackoff(100*time.Millisecond, 2*time.Second),
		jitter:      FullJitter,
		maxAttempts: defaultMaxAttempts,
		retryIf:     IsRetryable,
		randInt64N:  rand.Int64N,
		sleep:       sleep,
		now:         time.Now,
	}
}

func applyOptions(c *config, opts ...Option) {
	for _, opt := range opts {
		if opt == nil {
			panic("retry: nil option")
		}
		opt.applyRetry(c)
	}
}

// WithBackoff sets the backoff used between retry attempts.
func WithBackoff(backoff Backoff) Option {
	if backoff == nil {
		panic("retry: nil backoff")
	}
	return optionFunc(func(c *config) {
		c.backoff = backoff
	})
}

// WithJitter sets the jitter strategy applied to calculated backoff delays.
func WithJitter(jitter Jitter) Option {
	switch jitter {
	case NoJitter, FullJitter, EqualJitter:
	default:
		panic("retry: invalid jitter")
	}
	return optionFunc(func(c *config) {
		c.jitter = jitter
	})
}

// WithMaxAttempts sets the total number of attempts, including the first try.
func WithMaxAttempts(maxAttempts int) Option {
	if maxAttempts < 1 {
		panic("retry: max attempts must be at least 1")
	}
	return optionFunc(func(c *config) {
		c.maxAttempts = maxAttempts
	})
}

// WithMaxElapsed sets the maximum elapsed time spent retrying.
// A zero duration means no elapsed-time limit.
func WithMaxElapsed(maxElapsed time.Duration) Option {
	if maxElapsed < 0 {
		panic("retry: max elapsed must be non-negative")
	}
	return optionFunc(func(c *config) {
		c.maxElapsed = maxElapsed
	})
}

// WithRetryIf sets the predicate used to decide whether an error can be retried.
func WithRetryIf(retryIf func(error) bool) Option {
	if retryIf == nil {
		panic("retry: nil retry predicate")
	}
	return optionFunc(func(c *config) {
		c.retryIf = retryIf
	})
}

// WithNotify sets a hook called before each retry sleep.
func WithNotify(notify func(Event)) Option {
	if notify == nil {
		panic("retry: nil notify hook")
	}
	return optionFunc(func(c *config) {
		c.notify = notify
	})
}

func (c config) shouldRetry(err error) bool {
	if err == nil || IsPermanent(err) {
		return false
	}
	return c.retryIf(err)
}

func (c config) delay(attempt int) time.Duration {
	delay := c.backoff.Delay(attempt)
	if delay < 0 {
		delay = 0
	}
	return c.jitterDelay(delay)
}

func (c config) jitterDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}

	switch c.jitter {
	case NoJitter:
		return delay
	case FullJitter:
		return c.randomDuration(delay)
	case EqualJitter:
		half := delay / 2
		return half + c.randomDuration(delay-half)
	default:
		return delay
	}
}

func (c config) randomDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	if max == time.Duration(1<<63-1) {
		return time.Duration(c.randInt64N(int64(max)))
	}
	return time.Duration(c.randInt64N(int64(max) + 1))
}

func sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryContextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
