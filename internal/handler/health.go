// Package handler provides the HTTP handler for health checks.
// This file is located at /internal/handler/health.go.
package handler

import (
	"encoding/json"
	"net/http"
)

// HealthCheck handles GET /health.
// Response: Simple JSON status.
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "only GET is supported")
		return
	}

	// Return health status
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		// If we can't write the response, the server is in a bad state
		w.WriteHeader(http.StatusInternalServerError)
	}
}
