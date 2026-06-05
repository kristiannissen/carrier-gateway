// Package handler provides the HTTP handler for retrieving service points.
// This file is located at /internal/handler/service_points.go.
package handler

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// GetServicePoints handles GET /service-points.
func (c *Config) GetServicePoints(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodGet {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	city := r.URL.Query().Get("city")
	postalCode := r.URL.Query().Get("postalCode")
	country := r.URL.Query().Get("country")
	carrier := r.URL.Query().Get("carrier")

	if carrier == "" {
		carrier = "postnord"
	}

	if city == "" || country == "" {
		c.writeError(w, r, http.StatusBadRequest, "city and country are required", "")
		return
	}

	carrierAdapter, err := c.getAdapter(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	location := adapter.Location{
		City:       city,
		PostalCode: postalCode,
		Country:    country,
	}

	servicePoints, err := carrierAdapter.GetServicePoints(r.Context(), location)
	if err != nil {
		log.Error("failed to get service points",
			zap.Error(err),
			zap.String("city", city),
			zap.String("postalCode", postalCode),
			zap.String("country", country),
			zap.String("carrier", carrier),
		)
		c.writeError(w, r, http.StatusInternalServerError, "failed to get service points", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(servicePoints); err != nil {
		log.Error("failed to write response", zap.Error(err))
	}
}
