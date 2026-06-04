// Package handler provides the HTTP handler for health checks.
// This file is located at /internal/handler/health.go.
package handler

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// HealthCheck handles GET /health.
// Response: Simple JSON status.
func (c *Config) HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"message": "Method not allowed"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		c.Log.Error("failed to write health response", zap.Error(err))
	}
}
