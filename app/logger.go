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

func newStructuredLogger(level zapcore.Level, debugFormat bool, output, errorOutput io.Writer) *zap.Logger {
	encoderConfig := zapcore.EncoderConfig{
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
		encoderConfig.TimeKey = ""
		encoderConfig.LineEnding = "\n\n"
		encoderConfig.ConsoleSeparator = "  "
	}

	options := []zap.Option{
		zap.ErrorOutput(zapcore.Lock(zapcore.AddSync(errorOutput))),
		zap.AddStacktrace(zapcore.ErrorLevel),
	}
	if level <= zapcore.DebugLevel {
		encoderConfig.CallerKey = "caller"
		encoderConfig.EncodeLevel = colorLevelEncoder
		encoderConfig.EncodeCaller = colorCallerEncoder
		options = append(options, zap.AddCaller())
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.Lock(zapcore.AddSync(output)),
		zap.NewAtomicLevelAt(level),
	)
	return zap.New(core, options...)
}
