// Package handler provides the HTTP handler for health checks.
// This file is located at /internal/handler/health.go.
package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
)

// serviceStart records when the process started so uptime can be reported.
var serviceStart = time.Now()

// HealthResponse is the response body for GET /api/health.
type HealthResponse struct {
	// Status is always "ok" when the handler is reachable.
	Status string `json:"status"`

	// Uptime is the duration the service has been running, e.g. "3h22m10s".
	Uptime string `json:"uptime"`

	// MockMode is true when MOCK_MODE=true — no real carrier API calls are made.
	MockMode bool `json:"mockMode"`

	// Carriers lists every registered carrier and whether it is running in
	// production or mock mode.
	Carriers map[string]string `json:"carriers"`
}

// HealthCheck handles GET /api/health.
// Returns service uptime, mock mode status, and the operational mode of each
// registered carrier adapter.
func (c *Config) HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"method not allowed"}`))
		return
	}

	mockMode := os.Getenv("MOCK_MODE") == "true"

	carriers := make(map[string]string, len(c.Registry.Carriers()))
	for _, name := range c.Registry.Carriers() {
		if mockMode {
			carriers[name] = "mock"
		} else {
			carriers[name] = "production"
		}
	}

	resp := HealthResponse{
		Status:   "ok",
		Uptime:   time.Since(serviceStart).Round(time.Second).String(),
		MockMode: mockMode,
		Carriers: carriers,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.loggerFor(r).Error("failed to write health response", zap.Error(err))
	}
}
