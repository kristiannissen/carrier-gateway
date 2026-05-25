package api
// api/tracking.go

import (
	"encoding/json"
	"net/http"
)

// TrackingHandler provides the current transit status of a shipment mapped to normalized gateway states.
func TrackingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Simulating a PostNord shipment in transit.
	response := map[string]interface{}{
		"state":       "IN_TRANSIT",
		"description": "The parcel has been processed at the PostNord hub in Brøndby.",
		"timestamp":   "2026-05-25T14:45:00Z",
		"events": []map[string]string{
			{
				"state": "PICKED_UP",
				"time":  "2026-05-24T10:00:00Z",
			},
		},
	}
	
	json.NewEncoder(w).Encode(response)
}