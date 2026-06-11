// Package handler provides the HTTP handler for updating shipments.
// This file is located at /internal/handler/updates.go.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// UpdateShipment handles PATCH /api/bookings/{trackingNumber}.
// Query parameter: carrier (required).
// Body: JSON with any subset of phone, email, weight, servicePointId.
func (c *Config) UpdateShipment(w http.ResponseWriter, r *http.Request) {
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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	var req adapter.UpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	// Path parameter takes precedence over body field.
	req.TrackingNumber = trackingNumber
	req.Carrier = carrier

	if req.ReceiverPhone == "" && req.ReceiverEmail == "" && req.Weight == 0 && req.ServicePointID == "" {
		c.writeError(w, r, http.StatusBadRequest, "validation failed",
			"at least one field must be specified: phone, email, weight, servicePointId")
		return
	}

	carrierAdapter, err := c.selectAdapter(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	response, err := carrierAdapter.UpdateShipment(r.Context(), req)
	if err != nil {
		log.Error("failed to update shipment",
			zap.Error(err),
			zap.String("carrier", carrier),
			zap.String("trackingNumber", trackingNumber),
		)
		if errors.Is(err, adapter.ErrNotSupported) {
			c.writeError(w, r, http.StatusNotImplemented, "not supported", err.Error())
		} else {
			c.writeError(w, r, http.StatusInternalServerError, "update failed", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to write update response", zap.Error(err))
	}
}
