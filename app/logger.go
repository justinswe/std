package app

import (
	"io"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	_colorReset  = "\033[0m"
	_colorRed    = "\033[31m"
	_colorYellow = "\033[33m"
	_colorBlue   = "\033[34m"
	_colorCyan   = "\033[36m"
	_colorGray   = "\033[90m"
	_colorGreen  = "\033[32m"
)

func paddedLevelEncoder(level zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
	encoder.AppendString(paddedLevel(level))
}

func colorLevelEncoder(level zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
	color := _colorGray
	switch level {
	case zapcore.DebugLevel:
		color = _colorCyan
	case zapcore.InfoLevel:
		color = _colorGreen
	case zapcore.WarnLevel:
		color = _colorYellow
	case zapcore.ErrorLevel, zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		color = _colorRed
	}
	encoder.AppendString(color + paddedLevel(level) + _colorReset)
}

func paddedLevel(level zapcore.Level) string {
	name := level.CapitalString()
	const width = 5
	if len(name) >= width {
		return name
	}
	return name + "     "[:width-len(name)]
}

func colorCallerEncoder(caller zapcore.EntryCaller, encoder zapcore.PrimitiveArrayEncoder) {
	encoder.AppendString(_colorBlue + caller.TrimmedPath() + _colorReset)
}

// severityEncoder writes the syslog name for a level.
//
// zap's own CapitalString produces WARN and DPANIC, which are not syslog
// severities and are not names a log backend recognises; an unknown name is
// read as an unclassified entry, so a warning would rank below an info. WARNING
// and CRITICAL are the RFC 5424 names and are understood everywhere.
func severityEncoder(level zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
	switch level {
	case zapcore.WarnLevel:
		encoder.AppendString("WARNING")
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		encoder.AppendString("CRITICAL")
	default:
		encoder.AppendString(level.CapitalString())
	}
}

func consoleEncoderConfig(level zapcore.Level, debugFormat bool) zapcore.EncoderConfig {
	config := zapcore.EncoderConfig{
		TimeKey:          "time",
		LevelKey:         "level",
		MessageKey:       "msg",
		StacktraceKey:    "stack",
		LineEnding:       zapcore.DefaultLineEnding,
		EncodeLevel:      paddedLevelEncoder,
		EncodeTime:       zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeDuration:   zapcore.MillisDurationEncoder,
		EncodeCaller:     zapcore.ShortCallerEncoder,
		EncodeName:       zapcore.FullNameEncoder,
		ConsoleSeparator: " ",
	}

	if debugFormat || level <= zapcore.DebugLevel {
		config.TimeKey = ""
		config.LineEnding = "\n\n"
		config.ConsoleSeparator = "  "
	}
	if level <= zapcore.DebugLevel {
		config.CallerKey = "caller"
		config.EncodeLevel = colorLevelEncoder
		config.EncodeCaller = colorCallerEncoder
	}
	return config
}

// jsonEncoderConfig names the keys a log collector reads to classify an entry.
//
// severity, message and timestamp are the conventional names, so a collector
// promotes them without being told how this program is configured. The console
// config is deliberately not the starting point: its debug branches drop the
// timestamp and wrap the level in ANSI colour, both of which are corrupt inside
// a JSON string.
func jsonEncoderConfig(level zapcore.Level) zapcore.EncoderConfig {
	config := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "severity",
		NameKey:        "logger",
		MessageKey:     "message",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    severityEncoder,
		EncodeTime:     zapcore.RFC3339NanoTimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeName:     zapcore.FullNameEncoder,
	}
	if level <= zapcore.DebugLevel {
		config.CallerKey = "caller"
	}
	return config
}

func newStructuredLogger(level zapcore.Level, format string, debugFormat bool, output, errorOutput io.Writer) *zap.Logger {
	options := []zap.Option{
		zap.ErrorOutput(zapcore.Lock(zapcore.AddSync(errorOutput))),
	}
	if level <= zapcore.DebugLevel {
		options = append(options, zap.AddCaller())
	}

	var encoder zapcore.Encoder
	if format == _formatJSON {
		encoder = zapcore.NewJSONEncoder(jsonEncoderConfig(level))
	} else {
		encoder = zapcore.NewConsoleEncoder(consoleEncoderConfig(level, debugFormat))
		// Only in console format. An error logged through the errors package already
		// carries errorVerbose, a stack captured where the error was created, which is
		// more useful than zap's dump of the goroutine that logged it. Emitting both
		// doubles the size of the largest entries, and container stdout is a pipe
		// shared by every process in the image: a write past PIPE_BUF is not atomic, so
		// an oversized entry can interleave with another and arrive as unparseable
		// JSON, which loses the severity this format exists to carry.
		options = append(options, zap.AddStacktrace(zapcore.ErrorLevel))
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.Lock(zapcore.AddSync(output)),
		zap.NewAtomicLevelAt(level),
	)
	return zap.New(core, options...)
}
