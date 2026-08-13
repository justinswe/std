package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestPaddedLevel(t *testing.T) {
	tests := []struct {
		level zapcore.Level
		want  string
	}{
		{zapcore.DebugLevel, "DEBUG"},
		{zapcore.InfoLevel, "INFO "},
		{zapcore.WarnLevel, "WARN "},
		{zapcore.ErrorLevel, "ERROR"},
		{zapcore.DPanicLevel, "DPANIC"},
	}
	for _, tt := range tests {
		if got := paddedLevel(tt.level); got != tt.want {
			t.Errorf("paddedLevel(%s) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestStructuredLogger(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		var output bytes.Buffer
		logger := newStructuredLogger(zapcore.InfoLevel, _formatConsole, false, &output, &output)
		logger.Debug("hidden")
		logger.Info("ready", zap.String("service", "test"))

		got := output.String()
		if strings.Contains(got, "hidden") {
			t.Errorf("debug log was emitted: %q", got)
		}
		if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2} .* INFO  ready`).MatchString(got) {
			t.Errorf("normal output = %q", got)
		}
		if !strings.Contains(got, `{"service": "test"}`) {
			t.Errorf("normal output lacks field: %q", got)
		}
	})

	t.Run("debug", func(t *testing.T) {
		var output bytes.Buffer
		logger := newStructuredLogger(zapcore.DebugLevel, _formatConsole, false, &output, &output)
		logger.Debug("details")
		logger.Info("info")
		logger.Warn("warn")
		logger.Error("error")
		logger.DPanic("dpanic")

		got := output.String()
		if !strings.Contains(got, _colorCyan+"DEBUG"+_colorReset) {
			t.Errorf("debug output lacks colored level: %q", got)
		}
		for _, color := range []string{_colorGreen, _colorYellow, _colorRed} {
			if !strings.Contains(got, color) {
				t.Errorf("debug output lacks color %q: %q", color, got)
			}
		}
		if !strings.Contains(got, _colorBlue) || !strings.Contains(got, "logger_test.go") {
			t.Errorf("debug output lacks caller: %q", got)
		}
		if !strings.HasSuffix(got, "\n\n") {
			t.Errorf("debug output lacks spacing: %q", got)
		}
	})

	t.Run("debug format", func(t *testing.T) {
		var output bytes.Buffer
		logger := newStructuredLogger(zapcore.InfoLevel, _formatConsole, true, &output, &output)
		logger.Info("readable")
		got := output.String()
		if strings.Contains(got, "\033[") || !strings.HasSuffix(got, "\n\n") {
			t.Errorf("debug-format output = %q", got)
		}
	})

	t.Run("stacktrace", func(t *testing.T) {
		var output bytes.Buffer
		logger := newStructuredLogger(zapcore.ErrorLevel, _formatConsole, false, &output, &output)
		logger.Error("failed", zap.Error(errors.New("cause")))
		got := output.String()
		if !strings.Contains(got, "cause") || !strings.Contains(got, "logger_test.go") {
			t.Errorf("error output lacks error or stack: %q", got)
		}
	})

	t.Run("json", func(t *testing.T) {
		var output bytes.Buffer
		logger := newStructuredLogger(zapcore.InfoLevel, _formatJSON, true, &output, &output)
		logger.Warn("degraded", zap.String("service", "test"))
		logger.Error("failed", zap.Error(errors.New("cause")))

		lines := strings.Split(strings.TrimSpace(output.String()), "\n")
		if len(lines) != 2 {
			t.Fatalf("json output = %d lines, want 2: %q", len(lines), output.String())
		}

		var entry struct {
			Severity  string `json:"severity"`
			Message   string `json:"message"`
			Timestamp string `json:"timestamp"`
			Service   string `json:"service"`
			Stack     string `json:"stack"`
		}
		if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
			t.Fatalf("json.Unmarshal(%q) error = %v", lines[0], err)
		}
		// WARNING, not zap's WARN: a collector that does not recognise the name files
		// the entry as unclassified, which ranks a warning below an info.
		if entry.Severity != "WARNING" {
			t.Errorf("severity = %q, want WARNING", entry.Severity)
		}
		if entry.Message != "degraded" || entry.Service != "test" {
			t.Errorf("message = %q, service = %q", entry.Message, entry.Service)
		}
		// debugFormat is console-only, so json must still carry a timestamp.
		if _, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err != nil {
			t.Errorf("time.Parse(RFC3339Nano, %q) error = %v", entry.Timestamp, err)
		}

		if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
			t.Fatalf("json.Unmarshal(%q) error = %v", lines[1], err)
		}
		if entry.Severity != "ERROR" {
			t.Errorf("severity = %q, want ERROR", entry.Severity)
		}
		// The errors package supplies the useful stack as a field; zap's duplicate is
		// off in json so an error entry stays small enough to write atomically.
		if entry.Stack != "" {
			t.Errorf("stack = %q, want empty", entry.Stack)
		}
	})
}

func TestForwardLogger(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	undo := zap.ReplaceGlobals(zap.New(core))
	defer undo()

	Log.Debug("debug")
	Log.Info("info", zap.String("key", "value"))
	Log.Warn("warn")
	Log.Error("error")
	Log.DPanic("dpanic")
	Log.With(zap.String("scope", "with")).Info("with")
	Log.Named("name").Info("named")
	L().Info("L")
	S().Info("S")
	if err := Log.Sync(); err != nil {
		t.Fatalf("Log.Sync() error = %v", err)
	}

	for _, message := range []string{"debug", "info", "warn", "error", "dpanic", "with", "named", "L", "S"} {
		if logs.FilterMessage(message).Len() != 1 {
			t.Errorf("message %q was not forwarded", message)
		}
	}
}

func TestForwardLoggerPanics(t *testing.T) {
	core, _ := observer.New(zapcore.DebugLevel)
	logger := zap.New(core, zap.WithFatalHook(zapcore.WriteThenPanic))
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	assertPanics(t, func() { Log.Panic("panic") })
	assertPanics(t, func() { Log.Fatal("fatal") })
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("function did not panic")
		}
	}()
	fn()
}
