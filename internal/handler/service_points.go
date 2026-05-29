// Package handler provides the HTTP handler for retrieving service points.
// This file is located at /internal/handler/service_points.go.
package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"logistics-gateway/internal/adapter"
)

// GetServicePoints handles GET /service-points.
// Query parameters: city, postalCode, country, carrier (default: postnord).
// Response: []ServicePoint (JSON) or ErrorResponse.
func (c *Config) GetServicePoints(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	// Extract query parameters
	city := r.URL.Query().Get("city")
	postalCode := r.URL.Query().Get("postalCode")
	country := r.URL.Query().Get("country")
	carrier := r.URL.Query().Get("carrier")

	// Default to PostNord if carrier is not specified
	if carrier == "" {
		carrier = "postnord"
	}

	// Validate required parameters
	if city == "" || country == "" {
		writeError(w, http.StatusBadRequest, "city and country are required", "")
		return
	}

	// Get the appropriate carrier adapter
	adapter, err := c.getAdapter(carrier)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	// Create location object
	location := adapter.Location{
		City:       city,
		PostalCode: postalCode,
		Country:    country,
	}

	// Get service points
	servicePoints, err := adapter.GetServicePoints(location)
	if err != nil {
		slog.Error("Failed to get service points",
			"error", err,
			"location", fmt.Sprintf("%s, %s, %s", city, postalCode, country),
			"carrier", carrier,
		)
		writeError(w, http.StatusInternalServerError, "failed to get service points", err.Error())
		return
	}

	// Return the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(servicePoints); err != nil {
		slog.Error("Failed to write response", "error", err)
	}
}
