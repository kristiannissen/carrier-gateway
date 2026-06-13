// Package handler provides HTTP handlers for pickup scheduling.
// This file is located at /internal/handler/pickups.go.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// BookPickup handles POST /api/pickups.
// Schedules a carrier collection at the warehouse.
func (c *Config) BookPickup(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		c.writeError(w, r, http.StatusRequestEntityTooLarge, "request body too large", "request body must not exceed 1 MB")
		return
	}

	var req adapter.PickupRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	if req.Carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", "carrier is required")
		return
	}
	if req.Pickup.Date == "" {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", "pickup.date is required")
		return
	}
	if req.Pickup.ReadyTime != "" && req.Pickup.CloseTime != "" && req.Pickup.ReadyTime >= req.Pickup.CloseTime {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", "pickup.readyTime must be before pickup.closeTime")
		return
	}

	ma, ok := c.resolveManifestAdapter(w, r, req.Carrier)
	if !ok {
		return
	}

	resp, err := ma.BookPickup(r.Context(), req)
	if err != nil {
		log.Error("failed to book pickup",
			zap.Error(err),
			zap.String("carrier", req.Carrier),
		)
		if errors.Is(err, adapter.ErrNotSupported) {
			c.writeError(w, r, http.StatusNotImplemented, "not supported", err.Error())
		} else {
			c.writeError(w, r, http.StatusInternalServerError, "pickup booking failed", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("failed to write pickup response", zap.Error(err))
	}
}

// UpdatePickup handles PUT /api/pickups/{confirmationNumber}.
// Modifies a previously scheduled pickup.
func (c *Config) UpdatePickup(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	confirmationNumber := mux.Vars(r)["confirmationNumber"]
	if confirmationNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing confirmation number", "confirmationNumber path parameter is required")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing carrier", "carrier query parameter is required")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		c.writeError(w, r, http.StatusRequestEntityTooLarge, "request body too large", "request body must not exceed 1 MB")
		return
	}

	var req adapter.PickupRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}
	req.Carrier = carrier

	if req.Pickup.ReadyTime != "" && req.Pickup.CloseTime != "" && req.Pickup.ReadyTime >= req.Pickup.CloseTime {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", "pickup.readyTime must be before pickup.closeTime")
		return
	}

	ma, ok := c.resolveManifestAdapter(w, r, carrier)
	if !ok {
		return
	}

	resp, err := ma.UpdatePickup(r.Context(), confirmationNumber, req)
	if err != nil {
		log.Error("failed to update pickup",
			zap.Error(err),
			zap.String("carrier", carrier),
			zap.String("confirmationNumber", confirmationNumber),
		)
		if errors.Is(err, adapter.ErrNotSupported) {
			c.writeError(w, r, http.StatusNotImplemented, "not supported",
				fmt.Sprintf("carrier %s does not support pickup update", carrier))
		} else {
			c.writeError(w, r, http.StatusInternalServerError, "pickup update failed", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("failed to write pickup update response", zap.Error(err))
	}
}

// CancelPickup handles DELETE /api/pickups/{confirmationNumber}.
// Cancels a previously scheduled pickup.
func (c *Config) CancelPickup(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	confirmationNumber := mux.Vars(r)["confirmationNumber"]
	if confirmationNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing confirmation number", "confirmationNumber path parameter is required")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing carrier", "carrier query parameter is required")
		return
	}

	ma, ok := c.resolveManifestAdapter(w, r, carrier)
	if !ok {
		return
	}

	if err := ma.CancelPickup(r.Context(), carrier, confirmationNumber); err != nil {
		log.Error("failed to cancel pickup",
			zap.Error(err),
			zap.String("carrier", carrier),
			zap.String("confirmationNumber", confirmationNumber),
		)
		if errors.Is(err, adapter.ErrNotSupported) {
			c.writeError(w, r, http.StatusNotImplemented, "not supported",
				fmt.Sprintf("carrier %s does not support pickup cancellation", carrier))
		} else {
			c.writeError(w, r, http.StatusInternalServerError, "pickup cancellation failed", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"confirmationNumber": confirmationNumber,
		"carrier":            carrier,
		"status":             "cancelled",
	}); err != nil {
		log.Error("failed to write pickup cancel response", zap.Error(err))
	}
}

// resolveManifestAdapter selects the carrier adapter and asserts it implements
// ManifestAdapter. On failure it writes the appropriate error response and
// returns false; callers must return immediately when ok is false.
func (c *Config) resolveManifestAdapter(w http.ResponseWriter, r *http.Request, carrier string) (adapter.ManifestAdapter, bool) {
	ca, err := c.Registry.Select(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return nil, false
	}
	ma, ok := ca.(adapter.ManifestAdapter)
	if !ok {
		c.writeError(w, r, http.StatusNotImplemented, "not supported",
			fmt.Sprintf("carrier %s does not support pickup or manifest operations", carrier))
		return nil, false
	}
	return ma, true
}
