// Package handler provides the HTTP handler for tracking shipments.
// This file is located at /internal/handler/trackings.go.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
)

// GetTracking handles GET /trackings/{trackingNumber}.
// Response: TrackingResponse (JSON) or ErrorResponse.
func (c *Config) GetTracking(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	// Extract tracking number from URL
	vars := mux.Vars(r)
	trackingNumber := vars["trackingNumber"]
	if trackingNumber == "" {
		writeError(w, http.StatusBadRequest, "tracking number is required", "")
		return
	}

	// Extract carrier from query parameters (default to "postnord" if not provided)
	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		carrier = "postnord"
	}

	// Get the appropriate carrier adapter
	carrierAdapter, err := c.getAdapter(carrier)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	// Track the shipment
	response, err := carrierAdapter.TrackShipment(trackingNumber)
	if err != nil {
		slog.Error("Failed to track shipment",
			"error", err,
			"trackingNumber", trackingNumber,
			"carrier", carrier,
		)
		writeError(w, http.StatusInternalServerError, "tracking failed", err.Error())
		return
	}

	// Return the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to write response", "error", err)
	}
}
