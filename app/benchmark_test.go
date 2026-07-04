package app

import (
	"fmt"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func BenchmarkStructuredLogger(b *testing.B) {
	for _, level := range []zapcore.Level{zapcore.InfoLevel, zapcore.DebugLevel} {
		b.Run(level.String(), func(b *testing.B) {
			logger := newStructuredLogger(level, false, io.Discard, io.Discard)
			b.ReportAllocs()
			for b.Loop() {
				logger.Info("request completed", zap.String("service", "benchmark"), zap.Int("status", 200))
			}
		})
	}
}

func BenchmarkApplyEnvironment(b *testing.B) {
	for _, flagCount := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("flags-%d", flagCount), func(b *testing.B) {
			root := &cobra.Command{Use: "benchmark"}
			for i := range flagCount {
				root.Flags().String(fmt.Sprintf("flag-%d", i), "", "benchmark flag")
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := applyEnvironment(root, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
