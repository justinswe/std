package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
)

const maxStackDepth = 32

// FieldError describes an error associated with a field.
type FieldError struct {
	Field       string
	Description string
	Retryable   bool
}

// Error returns the field and its error description.
func (e FieldError) Error() string {
	return e.Field + ": " + e.Description
}

// Retry reports whether the failed field operation may be retried.
func (e FieldError) Retry() bool {
	return e.Retryable
}

type appError struct {
	message     string
	cause       error
	formatCause bool
	depth       uint8
	pcs         [maxStackDepth]uintptr
}

var (
	_ error         = (*appError)(nil)
	_ fmt.Formatter = (*appError)(nil)
	_ error         = FieldError{}
)

func (e *appError) Error() string {
	if e.cause == nil || !e.formatCause {
		return e.message
	}

	return e.message + "; caused by: " + e.cause.Error()
}

func (e *appError) Unwrap() error {
	return e.cause
}

// Format supports %s, %v, %+v, and %q formatting.
func (e *appError) Format(state fmt.State, verb rune) {
	switch verb {
	case 's', 'v':
		if verb == 'v' && state.Flag('+') {
			e.formatStack(state)
			return
		}
		_, _ = io.WriteString(state, e.Error())
	case 'q':
		_, _ = io.WriteString(state, strconv.Quote(e.Error()))
	default:
		_, _ = fmt.Fprintf(state, "%%!%c(%T=%s)", verb, e, e.Error())
	}
}

func (e *appError) formatStack(w io.Writer) {
	_, _ = io.WriteString(w, e.message)
	_, _ = io.WriteString(w, "\n")
	_ = e.writeStack(w)
	if e.cause != nil && e.formatCause {
		_, _ = fmt.Fprintf(w, "Caused by: %+v", e.cause)
	}
}

func (e *appError) writeStack(w io.Writer) error {
	if e.depth == 0 {
		return nil
	}

	frames := runtime.CallersFrames(e.pcs[:e.depth])
	for {
		frame, more := frames.Next()
		if _, err := fmt.Fprintf(w, "%s()\n\t%s:%d\n", frame.Function, frame.File, frame.Line); err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

// New returns an error containing message and a stack captured at the call site.
func New(message string) error {
	return newAppError(message, nil, false)
}

// Errorf formats an error and captures a stack at the call site.
// It follows fmt.Errorf semantics, including support for multiple %w operands.
func Errorf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if wrapsAny(err) {
		return newAppError(err.Error(), err, false)
	}
	return newAppError(err.Error(), nil, false)
}

func wrapsAny(err error) bool {
	switch err.(type) {
	case interface{ Unwrap() error }, interface{ Unwrap() []error }:
		return true
	default:
		return false
	}
}

// Wrap returns nil when err is nil. Otherwise, it adds message and a stack.
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}

	return newAppError(message, err, true)
}

// Wrapf returns nil when err is nil. Otherwise, it adds a formatted message and a stack.
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}

	return newAppError(fmt.Sprintf(format, args...), err, true)
}

func newAppError(message string, cause error, formatCause bool) error {
	err := &appError{
		message:     message,
		cause:       cause,
		formatCause: formatCause,
	}
	err.depth = uint8(runtime.Callers(3, err.pcs[:]))
	return err
}

// Is reports whether any error in err's tree matches target.
func Is(err, target error) bool {
	return stderrors.Is(err, target)
}

// As finds the first error in err's tree assignable to target.
func As(err error, target any) bool {
	return stderrors.As(err, target)
}

// AsType finds the first error in err's tree matching E.
func AsType[E error](err error) (E, bool) {
	return stderrors.AsType[E](err)
}

// IsCanceled reports whether err is or wraps context.Canceled.
func IsCanceled(err error) bool {
	return stderrors.Is(err, context.Canceled)
}

// Ignore calls fn and intentionally discards its error.
func Ignore(fn func() error) {
	_ = fn()
}

// IgnoreCtx calls fn with ctx and intentionally discards its error.
func IgnoreCtx(ctx context.Context, fn func(context.Context) error) {
	_ = fn(ctx)
}

// Any returns the first non-nil error, or nil if all errors are nil.
func Any(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Join returns an error that wraps the non-nil errors in errs.
func Join(errs ...error) error {
	return stderrors.Join(errs...)
}
