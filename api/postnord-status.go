package api
// /api/status.go

import (
	"encoding/json"
	"net/http"
)

type IntegrationsStatus struct {
	PostNord string `json:"postnord"`
}

type SystemStatusResponse struct {
	Gateway      string             `json:"gateway"`
	Version      string             `json:"version"`
	Integrations IntegrationsStatus `json:"integrations"`
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// In a real scenario, you would check the actual ping or health of the carrier endpoint here
	apiKey := r.Header.Get("X-PostNord-API-Key")

	var postnordStatus string
	if apiKey == "broken" {
		postnordStatus = "unhealthy"
	} else {
		postnordStatus = "operational"
	}

	response := SystemStatusResponse{
		Gateway: "healthy",
		Version: "1.0.0-mvp",
		Integrations: IntegrationsStatus{
			PostNord: postnordStatus,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}