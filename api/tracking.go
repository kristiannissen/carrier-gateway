package api
// /api/tracking.go

import (
	"encoding/json"
	"net/http"
	"time"
)

type TrackingStatus struct {
	TrackingID      string    `json:"tracking_id"`
	CarrierCode     string    `json:"carrier_code"`
	CurrentState    string    `json:"current_state"` // "PICKED_UP", "IN_TRANSIT", "DELIVERED", "EXCEPTION"
	LastDescription string    `json:"last_description"`
	UpdatedAt       string    `json:"updated_at"`
	ETA             *string   `json:"eta,omitempty"`
}

func TrackingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Extract the tracking ID parsed by Vercel router from URL query parameters
	trackingID := r.URL.Query().Get("id")
	if trackingID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing tracking ID parameter"})
		return
	}

	apiKey := r.Header.Get("X-PostNord-API-Key")

	// Mock logic matching tracking-v1.json if credentials are empty or explicitly set to 'mock'
	if apiKey == "" || apiKey == "mock" {
		etaTime := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
		
		response := TrackingStatus{
			TrackingID:      trackingID,
			CarrierCode:     "postnord",
			CurrentState:    "IN_TRANSIT",
			LastDescription: "Parcel has departed PostNord sorting hub in Aarhus",
			UpdatedAt:       time.Now().Format(time.RFC3339),
			ETA:             &etaTime,
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// -----------------------------------------------------------------
	// Production PostNord API Tracking endpoint request bridge
	// -----------------------------------------------------------------
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "Production tracking bridge not active yet. Please use 'mock' authentication key.",
	})
}