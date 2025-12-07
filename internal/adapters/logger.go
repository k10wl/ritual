package adapters

import (
	"log/slog"
	"os"

	"ritual/internal/core/ports"
)

// SlogLogger wraps *slog.Logger to implement ports.Logger interface
type SlogLogger struct {
	logger *slog.Logger
}

// Compile-time check to ensure SlogLogger implements ports.Logger
var _ ports.Logger = (*SlogLogger)(nil)

// NewSlogLogger creates a new SlogLogger with the default text handler
func NewSlogLogger() *SlogLogger {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return &SlogLogger{logger: logger}
}

// NewSlogLoggerWithLevel creates a new SlogLogger with the specified log level
func NewSlogLoggerWithLevel(level slog.Level) *SlogLogger {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	return &SlogLogger{logger: logger}
}

// NewSlogLoggerFromExisting wraps an existing *slog.Logger
func NewSlogLoggerFromExisting(logger *slog.Logger) *SlogLogger {
	if logger == nil {
		return NewSlogLogger()
	}
	return &SlogLogger{logger: logger}
}

// Info logs informational messages with structured key-value pairs
func (l *SlogLogger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// Warn logs warning messages with structured key-value pairs
func (l *SlogLogger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// Error logs error messages with structured key-value pairs
func (l *SlogLogger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// Debug logs debug messages with structured key-value pairs
func (l *SlogLogger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// NopLogger is a no-op logger that discards all log messages
// Useful for testing or when logging should be disabled
type NopLogger struct{}

// Compile-time check to ensure NopLogger implements ports.Logger
var _ ports.Logger = (*NopLogger)(nil)

// NewNopLogger creates a new NopLogger
func NewNopLogger() *NopLogger {
	return &NopLogger{}
}

// Info discards the message
func (l *NopLogger) Info(msg string, args ...any) {}

// Warn discards the message
func (l *NopLogger) Warn(msg string, args ...any) {}

// Error discards the message
func (l *NopLogger) Error(msg string, args ...any) {}

// Debug discards the message
func (l *NopLogger) Debug(msg string, args ...any) {}
