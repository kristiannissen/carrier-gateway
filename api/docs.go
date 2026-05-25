package api

// api/docs.go

import (
	"encoding/json"
	"net/http"
	"strings"
)

// EndpointDocumentation defines the structural metadata for a single API route.
type EndpointDocumentation struct {
	Method      string            `json:"method"`
	Description string            `json:"description"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
}

// DocsHandler serves as the programmatic documentation engine for the Logistics Gateway.
func DocsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// The documentation is organized by endpoint to reflect the Phase 2 PostNord MVP scope.
	apiDocs := map[string]EndpointDocumentation{
		"/api/v1/bookings": {
			Method:      "POST",
			Description: "Creates a new carrier shipment booking and returns tracking IDs.",
			Constraints: []string{
				"Mandatory colli array with weight and dimensions",
				"Strict HS Code validation for Non-EU shipments (DDP/DAP)",
				"Supported carriers for MVP: postnord",
			},
		},
		"/api/v1/tracking/{id}": {
			Method:      "GET",
			Description: "Retrieves a normalized status timeline for a specific booking.",
			Constraints: []string{
				"Returns states: PICKED_UP, IN_TRANSIT, DELIVERED, EXCEPTION",
			},
		},
		"/api/v1/service-points": {
			Method:      "GET",
			Description: "Provides a unified lookup engine for cross-carrier package shops.",
			Parameters: map[string]string{
				"carrier": "Filter by provider (e.g., postnord)",
				"lat":     "Latitude for geographic sorting",
				"lng":     "Longitude for geographic sorting",
			},
		},
		"/api/v1/status": {
			Method:      "GET",
			Description: "Public-facing health check evaluating internal and carrier API availability.",
		},
	}

	// Logic to handle specific method documentation if requested via the path.
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/docs/"), "/")
	if len(pathParts) > 0 && pathParts != "" {
		methodKey := "/" + pathParts
		if doc, exists := apiDocs[methodKey]; exists {
			json.NewEncoder(w).Encode(doc)
			return
		}
		
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Documentation for the specified method not found"})
		return
	}

	// Default response returning the full documentation suite.
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(apiDocs)
}
