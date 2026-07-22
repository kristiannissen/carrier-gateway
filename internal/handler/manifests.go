// Package handler provides the HTTP handler for manifest close operations.
// This file is located at /internal/handler/manifests.go.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// CloseManifest handles POST /api/manifests.
// Retrieves or generates the handover document for a carrier and shipping day.
// For carriers such as GLS that require an explicit close call before the driver
// arrives, this also submits that instruction to the carrier.
func (c *Config) CloseManifest(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		c.writeError(w, r, http.StatusRequestEntityTooLarge, "request body too large", "request body must not exceed 1 MB")
		return
	}

	var req adapter.ManifestRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	if req.Carrier == "" {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", "carrier is required")
		return
	}
	if req.Date == "" {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", "date is required")
		return
	}

	ma, ok := c.resolveManifestAdapter(w, r, req.Carrier)
	if !ok {
		return
	}

	resp, err := ma.CloseManifest(r.Context(), req)
	if err != nil {
		log.Error("failed to close manifest",
			zap.Error(err),
			zap.String("carrier", req.Carrier),
			zap.String("date", req.Date),
		)
		if errors.Is(err, adapter.ErrNotSupported) {
			c.writeError(w, r, http.StatusNotImplemented, "not supported",
				fmt.Sprintf("carrier %s does not support manifest close", req.Carrier))
		} else {
			c.writeError(w, r, http.StatusInternalServerError, "manifest close failed", carrierErrorDetail)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error("failed to write manifest response", zap.Error(err))
	}
}
