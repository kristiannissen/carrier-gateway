// Package handler provides the HTTP handler for label retrieval.
// This file is located at /internal/handler/labels.go.
package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// validLabelFormats is the set of accepted label format strings.
var validLabelFormats = map[string]adapter.LabelFormat{
	"PDF":   adapter.LabelFormatPDF,
	"PNG":   adapter.LabelFormatPNG,
	"ZPL":   adapter.LabelFormatZPL,
	"EPL":   adapter.LabelFormatEPL,
	"ZPLGK": adapter.LabelFormatZPLGK,
}

// GetLabel handles GET /api/labels/{trackingNumber}.
//
// Query parameters:
//   - carrier: required; defaults to "postnord"
//   - format:  optional; defaults to "PDF"
//
// Returns a JSON body with base64-encoded label data ready for printing.
func (c *Config) GetLabel(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodGet {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	vars := mux.Vars(r)
	trackingNumber := vars["trackingNumber"]
	if trackingNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing tracking number", "trackingNumber path parameter is required")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		carrier = "postnord"
	}

	formatStr := strings.ToUpper(r.URL.Query().Get("format"))
	if formatStr == "" {
		formatStr = "PDF"
	}

	format, ok := validLabelFormats[formatStr]
	if !ok {
		c.writeError(w, r, http.StatusBadRequest, "invalid label format",
			"supported formats: PDF, PNG, ZPL, EPL, ZPLGK")
		return
	}

	carrierAdapter, err := c.selectAdapter(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	resp, err := carrierAdapter.FetchLabel(r.Context(), adapter.LabelRequest{
		TrackingNumber: trackingNumber,
		Format:         format,
	})
	if err != nil {
		log.Error("failed to fetch label",
			zap.Error(err),
			zap.String("carrier", carrier),
			zap.String("trackingNumber", trackingNumber),
			zap.String("format", formatStr),
		)
		c.writeError(w, r, http.StatusInternalServerError, "label fetch failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("failed to write label response", zap.Error(err))
	}
}

// GetReturnLabel handles GET /api/returns/{trackingNumber}/label.
//
// Query parameters:
//   - carrier: required
//   - format:  optional; defaults to "PDF"
//
// Returns a JSON body with base64-encoded return label data.
// Only carriers implementing ReturnAdapter support this endpoint; others return HTTP 501.
func (c *Config) GetReturnLabel(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	vars := mux.Vars(r)
	trackingNumber := vars["trackingNumber"]
	if trackingNumber == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing tracking number", "trackingNumber path parameter is required")
		return
	}

	carrier := r.URL.Query().Get("carrier")
	if carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "missing carrier", "carrier query parameter is required")
		return
	}

	formatStr := strings.ToUpper(r.URL.Query().Get("format"))
	if formatStr == "" {
		formatStr = "PDF"
	}

	format, ok := validLabelFormats[formatStr]
	if !ok {
		c.writeError(w, r, http.StatusBadRequest, "invalid label format",
			"supported formats: PDF, ZPL, ZPLGK, EPL")
		return
	}

	ca, err := c.selectAdapter(carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	ra, ok := ca.(adapter.ReturnAdapter)
	if !ok {
		c.writeError(w, r, http.StatusNotImplemented, "not supported",
			"carrier "+carrier+" does not support return label retrieval")
		return
	}

	resp, err := ra.FetchReturnLabel(r.Context(), adapter.LabelRequest{
		TrackingNumber: trackingNumber,
		Format:         format,
	})
	if err != nil {
		log.Error("failed to fetch return label",
			zap.Error(err),
			zap.String("carrier", carrier),
			zap.String("trackingNumber", trackingNumber),
			zap.String("format", formatStr),
		)
		c.writeError(w, r, http.StatusInternalServerError, "return label fetch failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("failed to write return label response", zap.Error(err))
	}
}
