// Package handler provides the HTTP handler for cancelling shipments.
// This file is located at /internal/handler/cancellations.go.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// CancelShipment handles DELETE /api/bookings/{trackingNumber}.
// Query parameter: carrier (required).
func (c *Config) CancelShipment(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	trackingNumber := mux.Vars(r)["trackingNumber"]
	if trackingNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing tracking number", "trackingNumber path parameter is required")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing carrier", "carrier query parameter is required")
		return
	}

	carrierAdapter, err := c.selectAdapter(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	response, err := carrierAdapter.CancelShipment(r.Context(), trackingNumber)
	if err != nil {
		log.Error("failed to cancel shipment",
			zap.Error(err),
			zap.String("carrier", carrier),
			zap.String("trackingNumber", trackingNumber),
		)
		c.writeError(w, r, http.StatusInternalServerError, "cancellation failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to write cancel response", zap.Error(err))
	}
}
