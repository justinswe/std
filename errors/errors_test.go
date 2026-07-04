package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

type customError struct {
	message string
}

func (e customError) Error() string {
	return e.message
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestNew(t *testing.T) {
	t.Parallel()

	err := New("failed")
	if got, want := err.Error(), "failed"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	formatted := fmt.Sprintf("%+v", err)
	if !strings.Contains(formatted, "TestNew") {
		t.Errorf("%%+v did not contain the caller: %q", formatted)
	}
	if strings.Contains(formatted, "0x0") {
		t.Errorf("%%+v contained an invalid frame: %q", formatted)
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()

	cause := stderrors.New("inner")
	err := Wrap(cause, "outer")
	if got, want := err.Error(), "outer; caused by: inner"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !stderrors.Is(err, cause) {
		t.Error("wrapped error does not match its cause")
	}
	if got := Wrap(nil, "outer"); got != nil {
		t.Errorf("Wrap(nil, ...) = %v, want nil", got)
	}
}

func TestWrapf(t *testing.T) {
	t.Parallel()

	cause := stderrors.New("inner")
	err := Wrapf(cause, "outer %d", 42)
	if got, want := err.Error(), "outer 42; caused by: inner"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !stderrors.Is(err, cause) {
		t.Error("wrapped error does not match its cause")
	}
	if got := Wrapf(nil, "outer %d", 42); got != nil {
		t.Errorf("Wrapf(nil, ...) = %v, want nil", got)
	}
}

func TestErrorfPreservesWrappedErrors(t *testing.T) {
	t.Parallel()

	first := stderrors.New("first")
	second := stderrors.New("second")
	err := Errorf("failed: %w and %w", first, second)

	if got, want := err.Error(), "failed: first and second"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	for _, target := range []error{first, second} {
		if !stderrors.Is(err, target) {
			t.Errorf("Errorf result does not match %q", target)
		}
	}
}

func TestErrorfWithoutWrappedError(t *testing.T) {
	t.Parallel()

	err := Errorf("failed: %d", 42)
	if got, want := err.Error(), "failed: 42"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got := stderrors.Unwrap(err); got != nil {
		t.Fatalf("errors.Unwrap(err) = %v, want nil", got)
	}
}

func TestAppErrorFormat(t *testing.T) {
	t.Parallel()

	err := Wrap(stderrors.New("inner"), "outer")
	tests := []struct {
		name       string
		format     string
		want       string
		wantCaller bool
	}{
		{name: "string", format: "%s", want: "outer; caused by: inner"},
		{name: "value", format: "%v", want: "outer; caused by: inner"},
		{name: "quoted", format: "%q", want: `"outer; caused by: inner"`},
		{name: "stack", format: "%+v", want: "Caused by: inner", wantCaller: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fmt.Sprintf(tt.format, err)
			if tt.wantCaller {
				if !strings.Contains(got, tt.want) || !strings.Contains(got, "TestAppErrorFormat") {
					t.Fatalf("Sprintf(%q, err) = %q, want cause and caller", tt.format, got)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("Sprintf(%q, err) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestAppErrorWriteStackReturnsWriterError(t *testing.T) {
	t.Parallel()

	want := stderrors.New("write failed")
	err := New("failed").(*appError)
	if got := err.writeStack(errorWriter{err: want}); !stderrors.Is(got, want) {
		t.Fatalf("writeStack() error = %v, want %v", got, want)
	}
}

func TestIsAsAndAsType(t *testing.T) {
	t.Parallel()

	original := customError{message: "custom"}
	err := Wrap(original, "outer")
	if !Is(err, original) {
		t.Error("Is did not find the wrapped error")
	}

	var target customError
	if !As(err, &target) || target != original {
		t.Fatalf("As target = %#v, want %#v", target, original)
	}

	typed, ok := AsType[customError](err)
	if !ok || typed != original {
		t.Fatalf("AsType result = (%#v, %t), want (%#v, true)", typed, ok, original)
	}

	var pathError *os.PathError
	if As(err, &pathError) {
		t.Fatalf("As unexpectedly matched %T", pathError)
	}
}

func TestIsCanceled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "canceled", err: context.Canceled, want: true},
		{name: "wrapped canceled", err: fmt.Errorf("operation: %w", context.Canceled), want: true},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "ordinary", err: stderrors.New("failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsCanceled(tt.err); got != tt.want {
				t.Errorf("IsCanceled(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestFieldError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  FieldError
		want string
	}{
		{name: "complete", err: FieldError{Field: "name", Description: "required"}, want: "name: required"},
		{name: "empty field", err: FieldError{Description: "required"}, want: ": required"},
		{name: "empty description", err: FieldError{Field: "name"}, want: "name: "},
		{name: "empty", want: ": "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFieldErrorRetry(t *testing.T) {
	t.Parallel()

	if !(FieldError{Retryable: true}).Retry() {
		t.Error("Retry() = false, want true")
	}
	if (FieldError{}).Retry() {
		t.Error("Retry() = true, want false")
	}
}

func TestAny(t *testing.T) {
	t.Parallel()

	want := stderrors.New("first")
	if got := Any(nil, want, stderrors.New("second")); got != want {
		t.Fatalf("Any() = %v, want %v", got, want)
	}
	if got := Any(nil, nil); got != nil {
		t.Fatalf("Any() = %v, want nil", got)
	}
}

func TestIgnore(t *testing.T) {
	t.Parallel()

	called := false
	Ignore(func() error {
		called = true
		return io.ErrUnexpectedEOF
	})
	if !called {
		t.Error("Ignore did not call its function")
	}

	ctx := context.WithValue(context.Background(), struct{}{}, "value")
	calledWithContext := false
	IgnoreCtx(ctx, func(got context.Context) error {
		calledWithContext = true
		if got != ctx {
			t.Errorf("IgnoreCtx context differs from its input")
		}
		return io.ErrUnexpectedEOF
	})
	if !calledWithContext {
		t.Error("IgnoreCtx did not call its function")
	}
}

func TestJoin(t *testing.T) {
	t.Parallel()

	first := stderrors.New("first")
	second := stderrors.New("second")
	err := Join(first, nil, second)
	if !stderrors.Is(err, first) || !stderrors.Is(err, second) {
		t.Fatalf("Join result does not contain both errors: %v", err)
	}
	if got := Join(); got != nil {
		t.Fatalf("Join() = %v, want nil", got)
	}
}
