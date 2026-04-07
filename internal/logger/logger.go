// Package logger provides a structured, levelled application logger built on
// top of go.uber.org/zap.  Call Init once at startup; use the package-level
// helpers (Info, Error, …) everywhere else.
package logger

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/go-orca/go-orca/internal/config"
)

var global *zap.Logger

// Init configures the global logger from the supplied LoggingConfig.
// It must be called before any log helpers are used.
func Init(cfg config.LoggingConfig) error {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return fmt.Errorf("logger: invalid level %q: %w", cfg.Level, err)
	}

	var zapCfg zap.Config
	if cfg.Format == "console" {
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)

	l, err := zapCfg.Build(zap.AddCallerSkip(1))
	if err != nil {
		return fmt.Errorf("logger: build error: %w", err)
	}

	global = l
	return nil
}

// L returns the global logger. Panics if Init has not been called.
func L() *zap.Logger {
	if global == nil {
		panic("logger: Init() has not been called")
	}
	return global
}

// With creates a child logger with additional fields.
func With(fields ...zap.Field) *zap.Logger { return L().With(fields...) }

// Debug logs at DEBUG level.
func Debug(msg string, fields ...zap.Field) { L().Debug(msg, fields...) }

// Info logs at INFO level.
func Info(msg string, fields ...zap.Field) { L().Info(msg, fields...) }

// Warn logs at WARN level.
func Warn(msg string, fields ...zap.Field) { L().Warn(msg, fields...) }

// Error logs at ERROR level.
func Error(msg string, fields ...zap.Field) { L().Error(msg, fields...) }

// Fatal logs at FATAL level then calls os.Exit(1).
func Fatal(msg string, fields ...zap.Field) { L().Fatal(msg, fields...) }

// Sync flushes any buffered log entries. Should be called on shutdown.
func Sync() { _ = L().Sync() }
