package retry

import (
	"math"
	"time"
)

// Backoff calculates the delay before retrying a failed attempt.
type Backoff interface {
	Delay(attempt int) time.Duration
}

// BackoffFunc adapts a function into a Backoff.
type BackoffFunc func(attempt int) time.Duration

// Delay returns f(attempt).
func (f BackoffFunc) Delay(attempt int) time.Duration {
	return f(attempt)
}

type constantBackoff struct {
	delay time.Duration
}

// ConstantBackoff returns the same delay for every retry.
func ConstantBackoff(delay time.Duration) Backoff {
	if delay < 0 {
		panic("retry: constant backoff delay must be non-negative")
	}
	return constantBackoff{delay: delay}
}

func (b constantBackoff) Delay(int) time.Duration {
	return b.delay
}

type linearBackoff struct {
	base time.Duration
	max  time.Duration
}

// LinearBackoff returns base multiplied by the failed attempt number.
// If max is greater than zero, delays are capped at max.
func LinearBackoff(base, max time.Duration) Backoff {
	validateBackoffBounds("linear", base, max)
	return linearBackoff{base: base, max: max}
}

func (b linearBackoff) Delay(attempt int) time.Duration {
	if attempt <= 0 || b.base == 0 {
		return 0
	}
	delay := multiplyDuration(b.base, int64(attempt))
	return capDuration(delay, b.max)
}

type exponentialBackoff struct {
	base time.Duration
	max  time.Duration
}

// ExponentialBackoff returns base * 2^(attempt-1).
// If max is greater than zero, delays are capped at max.
func ExponentialBackoff(base, max time.Duration) Backoff {
	validateBackoffBounds("exponential", base, max)
	return exponentialBackoff{base: base, max: max}
}

func (b exponentialBackoff) Delay(attempt int) time.Duration {
	if attempt <= 0 || b.base == 0 {
		return 0
	}
	if attempt > 63 {
		return capDuration(time.Duration(math.MaxInt64), b.max)
	}
	multiplier := int64(1) << (attempt - 1)
	delay := multiplyDuration(b.base, multiplier)
	return capDuration(delay, b.max)
}

func validateBackoffBounds(name string, base, max time.Duration) {
	if base < 0 {
		panic("retry: " + name + " backoff base must be non-negative")
	}
	if max < 0 {
		panic("retry: " + name + " backoff max must be non-negative")
	}
}

func multiplyDuration(base time.Duration, multiplier int64) time.Duration {
	if multiplier <= 0 || base <= 0 {
		return 0
	}
	if int64(base) > math.MaxInt64/multiplier {
		return time.Duration(math.MaxInt64)
	}
	return base * time.Duration(multiplier)
}

func capDuration(delay, max time.Duration) time.Duration {
	if delay < 0 {
		delay = time.Duration(math.MaxInt64)
	}
	if max > 0 && delay > max {
		return max
	}
	return delay
}
