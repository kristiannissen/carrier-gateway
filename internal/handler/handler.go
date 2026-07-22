// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/handler.go.
package handler

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
	"github.com/kristiannissen/carrier-gateway/internal/middleware"
	"github.com/kristiannissen/carrier-gateway/internal/notification"
)

// maxRequestBodyBytes caps request body reads in handlers that write to the
// body directly (i.e. not routed through the idempotency middleware body read).
// Mirrors the limit enforced by middleware.maxRequestBodyBytes.
const maxRequestBodyBytes = 1 << 20 // 1 MB

// carrierErrorDetail is the detail message returned to callers when a
// carrier adapter call fails (HTTP 500 from this gateway). The underlying
// error is always logged in full via zap.Error before this constant is used,
// so nothing useful is lost — but the raw error is never echoed back to the
// caller. Some adapters (e.g. PostNord, DAO) authenticate via a URL query
// parameter, and a wrapped transport error (timeout, DNS failure) can embed
// that URL; returning err.Error() directly would risk leaking a live
// carrier credential to whoever called the gateway.
const carrierErrorDetail = "the carrier request failed; check the server logs for the request ID above"

// Config holds shared configuration for HTTP handlers.
type Config struct {
	Registry *adapter.Registry
	Log      *zap.Logger
	// MockMode is true when the service is running with mock adapters.
	// Captured at startup so the health endpoint reflects the actual
	// adapter state rather than re-reading the env var per request.
	MockMode bool
	// NotificationService dispatches shipment event webhooks. May be nil
	// when the feature is not configured; handlers must check before use.
	NotificationService *notification.Service
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
