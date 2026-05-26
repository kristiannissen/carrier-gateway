package api
// api/bookings.go

import (
	"encoding/json"
	"net/http"
)

// BookingsHandler processes incoming fulfillment requests and creates simulated PostNord bookings.
func BookingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Simulating a successful PostNord booking response.
	response := map[string]interface{}{
		"booking_id":  "PN-MVP-88229911",
		"tracking_id": "SE776655443PN",
		"metadata": map[string]string{
			"carrier":      "postnord",
			"service_code": "17", // MyPack Home
			"status":       "created",
		},
	}
	
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}
