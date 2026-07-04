package app

import "go.uber.org/zap"

// ForwardLogger forwards logging calls to the global zap logger.
type ForwardLogger struct{}

// Log exposes structured logging backed by the global zap logger.
var Log ForwardLogger

// Debug writes a debug message with optional structured fields.
func (ForwardLogger) Debug(msg string, fields ...zap.Field) {
	zap.L().Debug(msg, fields...)
}

// Info writes an info message with optional structured fields.
func (ForwardLogger) Info(msg string, fields ...zap.Field) {
	zap.L().Info(msg, fields...)
}

// Warn writes a warning message with optional structured fields.
func (ForwardLogger) Warn(msg string, fields ...zap.Field) {
	zap.L().Warn(msg, fields...)
}

// Error writes an error message with optional structured fields.
func (ForwardLogger) Error(msg string, fields ...zap.Field) {
	zap.L().Error(msg, fields...)
}

// DPanic writes a development panic message with optional structured fields.
func (ForwardLogger) DPanic(msg string, fields ...zap.Field) {
	zap.L().DPanic(msg, fields...)
}

// Panic writes a panic message with optional structured fields and then panics.
func (ForwardLogger) Panic(msg string, fields ...zap.Field) {
	zap.L().Panic(msg, fields...)
}

// Fatal writes a fatal message with optional structured fields and exits.
func (ForwardLogger) Fatal(msg string, fields ...zap.Field) {
	zap.L().Fatal(msg, fields...)
}

// With returns a logger augmented with the supplied fields.
func (ForwardLogger) With(fields ...zap.Field) *zap.Logger {
	return zap.L().With(fields...)
}

// Named returns a logger named with the supplied string.
func (ForwardLogger) Named(name string) *zap.Logger {
	return zap.L().Named(name)
}

// Sync flushes any buffered log entries.
func (ForwardLogger) Sync() error {
	return zap.L().Sync()
}

// L returns the structured global zap logger.
func L() *zap.Logger {
	return zap.L()
}

// S returns the sugared global zap logger.
func S() *zap.SugaredLogger {
	return zap.S()
}
