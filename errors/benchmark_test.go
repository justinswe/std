package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

var benchmarkError error

func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		benchmarkError = New("failed")
	}
}

func BenchmarkErrorf(b *testing.B) {
	cause := stderrors.New("inner")
	for b.Loop() {
		benchmarkError = Errorf("failed: %w", cause)
	}
}

func BenchmarkWrap(b *testing.B) {
	cause := stderrors.New("inner")
	for b.Loop() {
		benchmarkError = Wrap(cause, "outer")
	}
}

func BenchmarkError(b *testing.B) {
	err := Wrap(stderrors.New("inner"), "outer")
	b.ResetTimer()
	for b.Loop() {
		_ = err.Error()
	}
}

func BenchmarkFormatWithStack(b *testing.B) {
	err := Wrap(stderrors.New("inner"), "outer")
	b.ResetTimer()
	for b.Loop() {
		_ = fmt.Sprintf("%+v", err)
	}
}
