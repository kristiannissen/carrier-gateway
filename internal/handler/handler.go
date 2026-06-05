// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/handler.go.
package handler

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/middleware"
)

// Config holds shared configuration for HTTP handlers.
type Config struct {
	Registry *adapter.Registry
	Log      *zap.Logger
	// MockMode is true when the service is running with mock adapters.
	// Captured at startup so the health endpoint reflects the actual
	// adapter state rather than re-reading the env var per request.
	MockMode bool
}

// ErrorResponse represents a standardized error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// loggerFor returns a child logger with the request ID attached as a field.
// Handlers should call this once at the top of their function and use the
// returned logger for all subsequent log calls within that request.
func (c *Config) loggerFor(r *http.Request) *zap.Logger {
	id := middleware.FromContext(r.Context())
	if id == "" {
		return c.Log
	}
	return c.Log.With(zap.String("requestID", id))
}

// writeError writes a standardized JSON error response and logs encoding failures.
func (c *Config) writeError(w http.ResponseWriter, r *http.Request, statusCode int, message, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(ErrorResponse{
		Error:   message,
		Details: details,
	}); err != nil {
		c.loggerFor(r).Error("failed to write error response", zap.Error(err))
	}
}

// selectAdapter delegates carrier selection to the Registry.
func (c *Config) selectAdapter(carrier string) (adapter.CarrierAdapter, error) {
	return c.Registry.Select(carrier)
}
