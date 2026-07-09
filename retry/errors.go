package retry

import stderrors "errors"

type retryable interface {
	error
	Retry() bool
}

type retryableError struct {
	err error
}

func (e retryableError) Error() string {
	return e.err.Error()
}

func (e retryableError) Unwrap() error {
	return e.err
}

func (retryableError) Retry() bool {
	return true
}

type permanentError struct {
	err error
}

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
}

func (permanentError) Retry() bool {
	return false
}

func (permanentError) permanent() {}

type permanent interface {
	error
	permanent()
}

// Retryable marks err as safe to retry.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return retryableError{err: err}
}

// Permanent marks err as unsafe to retry.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsRetryable reports whether err or any wrapped error is marked retryable.
func IsRetryable(err error) bool {
	retryable, ok := stderrors.AsType[retryable](err)
	return ok && retryable.Retry()
}

// IsPermanent reports whether err or any wrapped error is marked permanent.
func IsPermanent(err error) bool {
	_, ok := stderrors.AsType[permanent](err)
	return ok
}
