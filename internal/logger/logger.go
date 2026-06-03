// Package logger provides a shared zap logger constructor.
// This file is located at /internal/logger/logger.go.
package logger

import (
	"os"

	"go.uber.org/zap"
)

// New returns a development logger when LOG_ENV=development,
// otherwise returns a production JSON logger.
func New() (*zap.Logger, error) {
	if os.Getenv("LOG_ENV") == "development" {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}
