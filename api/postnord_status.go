package api
// /api/postnord-status.go

import (
	"encoding/json"
	"net/http"
	"time"
)

func PostNordStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Status-opsigt er altid synkront (Read-only)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"carrier":       "postnord",
		"status":        "healthy",
		"latency_ms":    42,
		"last_checked":  time.Now().Format(time.RFC3339),
		"error_rate_0h": 0.0,
	})
}