// Package handler provides the HTTP handler for return shipment booking.
// This file is located at /internal/handler/returns.go.
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

// GetReturnShipment handles GET /api/returns/{id}?carrier=.
// Returns details for a single return shipment. Carrier must implement ReturnQuerier.
func (c *Config) GetReturnShipment(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	shipmentID := mux.Vars(r)["id"]
	if shipmentID == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing shipment ID", "id path parameter is required")
		return
	}
	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing carrier", "carrier query parameter is required")
		return
	}

	ca, err := c.Registry.Select(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}
	rq, ok := ca.(adapter.ReturnQuerier)
	if !ok {
		c.writeError(w, r, http.StatusNotImplemented, "not supported",
			fmt.Sprintf("carrier %s does not support return shipment queries", carrier))
		return
	}

	info, err := rq.GetReturnShipment(r.Context(), shipmentID)
	if err != nil {
		log.Error("failed to get return shipment",
			zap.Error(err),
			zap.String("carrier", carrier),
			zap.String("shipmentID", shipmentID),
		)
		if errors.Is(err, adapter.ErrNotSupported) {
			c.writeError(w, r, http.StatusNotImplemented, "not supported", err.Error())
		} else {
			c.writeError(w, r, http.StatusInternalServerError, "get return shipment failed", carrierErrorDetail)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(info); err != nil {
		log.Error("failed to write get-return-shipment response", zap.Error(err))
	}
}

// BookReturn handles POST /api/returns.
//
// Routing priority:
//  1. If the selected carrier implements ReturnAdapter, it is used directly.
//     This covers InPost and any future carriers that adopt the generic interface.
//  2. If the carrier is "omniva", the Omniva-specific type path is used.
//  3. Otherwise HTTP 501 is returned.
//
// Request body for ReturnAdapter carriers: adapter.ReturnRequest (JSON).
// Response:                                adapter.ReturnResponse (JSON, HTTP 201).
//
// Request body for Omniva:   adapter.OmnivaReturnRequest (JSON).
// Response:                  adapter.OmnivaReturnResult  (JSON, HTTP 201).
func (c *Config) BookReturn(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

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

	ca, err := c.Registry.Select(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	// ── Generic ReturnAdapter path ────────────────────────────────────────────
	if ra, ok := ca.(adapter.ReturnAdapter); ok {
		var req adapter.ReturnRequest
		if err := json.Unmarshal(body, &req); err != nil {
			c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
			return
		}
		req.Carrier = carrier

		if req.Sender.Name == "" && req.Sender.Email == "" {
			c.writeError(w, r, http.StatusBadRequest, "validation failed",
				"sender.name or sender.email is required")
			return
		}

		result, err := ra.BookReturn(r.Context(), req)
		if err != nil {
			log.Error("book return failed",
				zap.Error(err),
				zap.String("carrier", carrier),
			)
			if errors.Is(err, adapter.ErrNotSupported) {
				c.writeError(w, r, http.StatusNotImplemented, "not supported", err.Error())
			} else {
				c.writeError(w, r, http.StatusInternalServerError, "return booking failed", carrierErrorDetail)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Error("failed to write return response", zap.Error(err))
		}
		return
	}

	// ── Omniva-specific path ──────────────────────────────────────────────────
	if carrier != "omniva" {
		c.writeError(w, r, http.StatusNotImplemented, "not supported",
			"carrier "+carrier+" does not support return booking via this endpoint")
		return
	}

	var req adapter.OmnivaReturnRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	if req.OriginalBarcode == "" {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", "originalBarcode is required")
		return
	}

	// BookReturn is Omniva-specific. Type-assert to the concrete adapter types
	// rather than introducing a new interface, making the Omniva dependency explicit.
	var result *adapter.OmnivaReturnResult
	switch oa := ca.(type) {
	case *adapter.OmnivaAdapter:
		result, err = oa.BookReturn(r.Context(), req)
	case *adapter.MockOmnivaAdapter:
		result, err = oa.BookReturn(r.Context(), req)
	default:
		c.writeError(w, r, http.StatusInternalServerError, "adapter error",
			"omniva adapter is not of expected type — this is a configuration error")
		return
	}

	if err != nil {
		log.Error("omniva: book return failed",
			zap.Error(err),
			zap.String("originalBarcode", req.OriginalBarcode),
		)
		if errors.Is(err, adapter.ErrNotSupported) {
			c.writeError(w, r, http.StatusNotImplemented, "not supported", err.Error())
		} else {
			c.writeError(w, r, http.StatusInternalServerError, "return booking failed", carrierErrorDetail)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Error("omniva: failed to write return response", zap.Error(err))
	}
}
