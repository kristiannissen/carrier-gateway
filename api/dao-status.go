package api
// /api/dao-status.go

import (
	"encoding/json"
	"net/http"
)

type DAOSystemStatusResponse struct {
	Gateway      string            `json:"gateway"`
	Version      string            `json:"version"`
	Integrations map[string]string `json:"integrations"`
}

func DAOStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	apiKey := r.Header.Get("X-DAO-API-Key")
	daoStatus := "operational"
	if apiKey == "broken" {
		daoStatus = "unhealthy"
	}

	response := DAOSystemStatusResponse{
		Gateway: "healthy",
		Version: "1.0.0-mvp",
		Integrations: map[string]string{
			"dao": daoStatus,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}