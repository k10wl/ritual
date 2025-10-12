package adapters

import (
	"log/slog"
	"ritual/internal/core/ports"
)

// Logger is a slog-based implementation of Logger interface
type Logger struct {
	logger *slog.Logger
}

// NewLogger creates a new slog-based logger
func NewLogger() ports.Logger {
	return &Logger{
		logger: slog.Default(),
	}
}

// Debug logs a debug message with optional attributes
func (l *Logger) Debug(msg string, attrs ...any) {
	l.logger.Debug(msg, attrs...)
}

// Info logs an info message with optional attributes
func (l *Logger) Info(msg string, attrs ...any) {
	l.logger.Info(msg, attrs...)
}

// Warn logs a warning message with optional attributes
func (l *Logger) Warn(msg string, attrs ...any) {
	l.logger.Warn(msg, attrs...)
}

// Error logs an error message with optional attributes
func (l *Logger) Error(msg string, attrs ...any) {
	l.logger.Error(msg, attrs...)
}

// With returns a new logger with the given attributes
func (l *Logger) With(attrs ...any) ports.Logger {
	return &Logger{
		logger: l.logger.With(attrs...),
	}
}
