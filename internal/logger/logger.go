// Package logger provides a shared zap logger constructor.
// This file is located at /internal/logger/logger.go.
package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New returns a configured zap logger based on environment variables.
//
// LOG_ENV=development selects the human-readable console encoder with
// debug-level logging enabled by default.
//
// In all other environments a production JSON encoder is used. The log level
// defaults to info but can be overridden with LOG_LEVEL (e.g. LOG_LEVEL=debug
// enables payload logging without changing the environment mode).
//
// Accepted LOG_LEVEL values: debug, info, warn, error, dpanic, panic, fatal.
func New() (*zap.Logger, error) {
	if os.Getenv("LOG_ENV") == "development" {
		return zap.NewDevelopment()
	}

	cfg := zap.NewProductionConfig()

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		var l zapcore.Level
		if err := l.UnmarshalText([]byte(level)); err == nil {
			cfg.Level = zap.NewAtomicLevelAt(l)
		}
	}

	return cfg.Build()
}
