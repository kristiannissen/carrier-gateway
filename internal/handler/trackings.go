// Package handler provides the HTTP handler for tracking shipments.
// This file is located at /internal/handler/trackings.go.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// GetTracking handles GET /trackings/{trackingNumber}.
// Response: TrackingResponse (JSON) or ErrorResponse.
func (c *Config) GetTracking(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		c.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	vars := mux.Vars(r)
	trackingNumber := vars["trackingNumber"]
	if trackingNumber == "" {
		c.writeError(w, http.StatusBadRequest, "tracking number is required", "")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		carrier = "postnord"
	}

	carrierAdapter, err := c.getAdapter(carrier)
	if err != nil {
		c.writeError(w, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	response, err := carrierAdapter.TrackShipment(r.Context(), trackingNumber)
	if err != nil {
		c.Log.Error("failed to track shipment",
			zap.Error(err),
			zap.String("trackingNumber", trackingNumber),
			zap.String("carrier", carrier),
		)
		c.writeError(w, http.StatusInternalServerError, "tracking failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		c.Log.Error("failed to write response", zap.Error(err))
	}
}
