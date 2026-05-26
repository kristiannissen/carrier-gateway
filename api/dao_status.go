package api
// /api/dao-status.go

import (
	"encoding/json"
	"net/http"
	"time"
)

func DAOStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"carrier":       "dao",
		"status":        "healthy",
		"latency_ms":    21,
		"last_checked":  time.Now().Format(time.RFC3339),
		"error_rate_0h": 0.0,
	})
}