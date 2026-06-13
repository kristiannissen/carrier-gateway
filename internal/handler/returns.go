// Package handler provides the HTTP handler for Omniva return shipment booking.
// This file is located at /internal/handler/returns.go.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// BookOmnivaReturn handles POST /api/returns.
//
// This endpoint is Omniva-specific: it registers a return shipment against an
// already-delivered Omniva shipment. The original barcode must be for a shipment
// that was registered under the same Omniva customerCode.
//
// The carrier query parameter must be "omniva". Attempting to call this endpoint
// with any other carrier returns HTTP 422.
//
// Request body: adapter.OmnivaReturnRequest (JSON)
// Response:     adapter.OmnivaReturnResult  (JSON, HTTP 201).
func (c *Config) BookOmnivaReturn(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodPost {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only POST is supported")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing carrier", "carrier query parameter is required")
		return
	}

	if carrier != "omniva" {
		c.writeError(w, r, http.StatusUnprocessableEntity, "unsupported carrier",
			"return booking via this endpoint is only supported for carrier=omniva")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		c.writeError(w, r, http.StatusRequestEntityTooLarge, "request body too large", "request body must not exceed 1 MB")
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

	ca, err := c.Registry.Select(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	// BookReturn is Omniva-specific. We type-assert to the concrete adapter types
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
			c.writeError(w, r, http.StatusInternalServerError, "return booking failed", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Error("omniva: failed to write return response", zap.Error(err))
	}
}
