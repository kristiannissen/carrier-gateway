package api
// /api/dao-tracking.go

import (
	"encoding/json"
	"net/http"
	"time"
)

type DAOTrackingStatus struct {
	TrackingID      string    `json:"tracking_id"`
	CarrierCode     string    `json:"carrier_code"`
	CurrentState    string    `json:"current_state"` // "PICKED_UP", "IN_TRANSIT", "DELIVERED", "EXCEPTION"
	LastDescription string    `json:"last_description"`
	UpdatedAt       string    `json:"updated_at"`
	ETA             *string   `json:"eta,omitempty"`
}

func DAOTrackingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	trackingID := r.URL.Query().Get("id")
	if trackingID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing tracking ID parameter"})
		return
	}

	apiKey := r.Header.Get("X-DAO-API-Key")

	if apiKey == "" || apiKey == "mock" {
		etaTime := time.Now().Add(12 * time.Hour).Format(time.RFC3339) // DAO omdeleles ofte hurtigt om natten
		
		response := DAOTrackingStatus{
			TrackingID:      trackingID,
			CarrierCode:     "dao",
			CurrentState:    "PICKED_UP",
			LastDescription: "Parcel collected by DAO distributor at sorting hub",
			UpdatedAt:       time.Now().Format(time.RFC3339),
			ETA:             &etaTime,
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "Live DAO tracking data stream is currently offline. Use 'mock' credentials.",
	})
}