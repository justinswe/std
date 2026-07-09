package retry

import (
	"context"

	apperrors "github.com/justinswe/std/errors"
)

// Operation is a retryable function.
type Operation func(context.Context) error

// OperationValue is a retryable function that returns a value.
type OperationValue[T any] func(context.Context) (T, error)

// Do runs fn until it succeeds, returns a permanent error, exhausts attempts,
// or ctx is cancelled.
func Do(ctx context.Context, fn Operation, opts ...Option) error {
	if fn == nil {
		return apperrors.New("retry: nil operation")
	}
	_, err := DoValue(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	}, opts...)
	return err
}

// DoValue runs fn until it succeeds, returns a permanent error, exhausts
// attempts, or ctx is cancelled.
func DoValue[T any](ctx context.Context, fn OperationValue[T], opts ...Option) (T, error) {
	c := newConfig(opts...)
	return doValue(ctx, fn, c)
}

func doValue[T any](ctx context.Context, fn OperationValue[T], c config) (T, error) {
	var zero T
	if ctx == nil {
		return zero, apperrors.New("retry: nil context")
	}
	if fn == nil {
		return zero, apperrors.New("retry: nil operation")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	start := c.now()
	for attempt := 1; ; attempt++ {
		value, err := fn(ctx)
		if err == nil {
			return value, nil
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if !c.shouldRetry(err) || attempt >= c.maxAttempts {
			return zero, err
		}

		delay := c.delay(attempt)
		if c.maxElapsed > 0 && c.now().Sub(start)+delay > c.maxElapsed {
			return zero, err
		}
		if c.notify != nil {
			c.notify(Event{
				Attempt:     attempt,
				MaxAttempts: c.maxAttempts,
				Delay:       delay,
				Err:         err,
			})
		}
		if err := c.sleep(ctx, delay); err != nil {
			return zero, retryContextError(ctx, err)
		}
	}
}
