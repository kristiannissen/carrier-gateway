// Package handler provides HTTP handlers for the API.
// This file is located at /internal/handler/handler.go.
package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"logistics-gateway/internal/adapter"
)

// Config holds shared configuration for handlers.
type Config struct {
	Adapters map[string]adapter.CarrierAdapter
}

// ErrorResponse represents a standardized error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// writeError writes a standardized error response.
func writeError(w http.ResponseWriter, statusCode int, message, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(ErrorResponse{
		Error:   message,
		Details: details,
	}); err != nil {
		slog.Error("Failed to write error response", "error", err)
	}
}

// getAdapter retrieves the appropriate carrier adapter from the config.
// Returns an error if the carrier is not supported or not configured.
func (c *Config) getAdapter(carrier string) (adapter.CarrierAdapter, error) {
	adapter, exists := c.Adapters[carrier]
	if !exists {
		return nil, fmt.Errorf("carrier %s is not supported or not configured", carrier)
	}
	return adapter, nil
}
