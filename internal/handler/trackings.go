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
func (c *Config) GetTracking(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodGet {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	vars := mux.Vars(r)
	trackingNumber := vars["trackingNumber"]
	if trackingNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "tracking number is required", "")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		carrier = "postnord"
	}

	carrierAdapter, err := c.selectAdapter(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	response, err := carrierAdapter.TrackShipment(r.Context(), trackingNumber)
	if err != nil {
		log.Error("failed to track shipment",
			zap.Error(err),
			zap.String("trackingNumber", trackingNumber),
			zap.String("carrier", carrier),
		)
		c.writeError(w, r, http.StatusInternalServerError, "tracking failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to write response", zap.Error(err))
	}
}
