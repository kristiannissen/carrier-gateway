package api
// /api/docs.go

import (
	"fmt"
	"net/http"
	"strings"
)

func DocsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Læs 'method' query-parameteren, som Vercel automatisk mapper via vercel.json
	method := r.URL.Query().Get("method")

	// Fallback: Hvis Vercels regex i routeren ikke fangede den, piller vi den ud af stien
	if method == "" {
		path := r.URL.Path // f.eks. "/api/v1/docs/bookings"
		pathParts := strings.Split(strings.Trim(path, "/"), "/")
		
		// Hvis der er dele nok i stien (f.eks. ["api", "v1", "docs", "bookings"])
		if len(pathParts) >= 4 {
			method = pathParts[3]
		}
	}

	// Hvis der overhovedet ikke er angivet en specifik metode, viser vi overblikket
	if method == "" {
		fmt.Fprintf(w, `{"status": "Documentation API Live", "available_docs": ["bookings", "service-points", "tracking", "status"]}`)
		return
	}

	// Hvis der er spurgt efter en specifik metode
	switch strings.ToLower(method) {
	case "bookings":
		fmt.Fprintf(w, `{"endpoint": "/api/v1/bookings", "method": "POST", "description": "Creates a new booking, currently focusing on PostNord MVP."}`)
	case "tracking":
		fmt.Fprintf(w, `{"endpoint": "/api/v1/tracking/{id}", "method": "GET", "description": "Fetch tracking information for a specific shipment."}`)
	default:
		fmt.Fprintf(w, `{"status": "Documentation Live", "requested_method": "%s", "info": "No specific documentation payload written for this resource yet."}`, method)
	}
}