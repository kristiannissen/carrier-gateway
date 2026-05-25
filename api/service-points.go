package api
// api/service-points.go

import (
	"encoding/json"
	"net/http"
)

// ServicePointsHandler retrieves a normalized list of PostNord pickup locations for geographic queries.
func ServicePointsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Simulating a list of PostNord service points.
	response := []map[string]interface{}{
		{
			"service_point_id": "98765",
			"name":             "PostNord Pakkeshop - SuperBrugsen",
			"address":          "Roskildevej 12",
			"postal_code":      "2000",
			"city":             "Frederiksberg",
			"country":          "DK",
		},
	}
	
	json.NewEncoder(w).Encode(response)
}