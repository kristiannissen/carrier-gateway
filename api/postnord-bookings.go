package api
// /api/bookings.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Mapping JSON-schema structures to Go types
type CustomsValue struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type Customs struct {
	HSCode           string       `json:"hs_code"`
	CountryOfOrigin string       `json:"country_of_origin"`
	CustomsValue     CustomsValue `json:"customs_value"`
}

type Destination struct {
	Type           string  `json:"type"` // "home_delivery" or "service_point"
	ServicePointID *string `json:"service_point_id,omitempty"`
	CarrierCode    *string `json:"carrier_code,omitempty"`
}

type ColliItem struct {
	Weight             float64 `json:"weight"`
	Length             float64 `json:"length"`
	Width              float64 `json:"width"`
	Height             float64 `json:"height"`
	ContentDescription string  `json:"content_description"`
}

type BookingRequest struct {
	Colli              []ColliItem `json:"colli"`
	Destination        Destination `json:"destination"`
	Incoterm           string      `json:"incoterm"` // "DAP" or "DDP"
	Customs            *Customs    `json:"customs,omitempty"`
	IncludeReturnLabel bool        `json:"include_return_label,omitempty"`
}

type BookingResponse struct {
	BookingID      string   `json:"booking_id"`
	TrackingID     string   `json:"tracking_id"`
	CarrierCode    string   `json:"carrier_code"`
	LabelURL       string   `json:"label_url"`
	ReturnLabelURL *string  `json:"return_label_url,omitempty"`
	CreatedAt      string   `json:"created_at"`
	Errors         []string `json:"errors,omitempty"`
}

func BookingsHandler(w http.ResponseWriter, r *http.Request) {
	// Handle GET requests for labels based on vercel.json routes
	if r.Method == http.MethodGet {
		handleAssetDownload(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Extract API keys from headers
	apiKey := r.Header.Get("X-PostNord-API-Key")

	var req BookingRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
		return
	}

	// Validation based on bookings-v1.json requirements (English error messages)
	var validationErrors []string
	if len(req.Colli) == 0 {
		validationErrors = append(validationErrors, "At least one colli item is required.")
	}
	if req.Destination.Type == "" {
		validationErrors = append(validationErrors, "Destination type ('home_delivery' or 'service_point') is required.")
	}
	if req.Incoterm != "DAP" && req.Incoterm != "DDP" {
		validationErrors = append(validationErrors, "Incoterm must be either DAP or DDP.")
	}

	if len(validationErrors) > 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{"errors": validationErrors})
		return
	}

	// If credentials are missing, or explicitly calling 'mock'
	if apiKey == "" || apiKey == "mock" {
		mockResponse := BookingResponse{
			BookingID:   fmt.Sprintf("BK-PN-%d", time.Now().Unix()),
			TrackingID:  "1Z-MOCK-POSTNORD-9998",
			CarrierCode: "postnord",
			LabelURL:    fmt.Sprintf("https://%s/api/v1/bookings/MOCK9998/label", r.Host),
			CreatedAt:   time.Now().Format(time.RFC3339),
		}

		if req.IncludeReturnLabel {
			retURL := fmt.Sprintf("https://%s/api/v1/bookings/MOCK9998/return-label", r.Host)
			mockResponse.ReturnLabelURL = &retURL
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(mockResponse)
		return
	}

	// -----------------------------------------------------------------
	// Production PostNord API integration bridge placeholder
	// -----------------------------------------------------------------
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Real PostNord API bridge is under construction. Use 'mock' in X-PostNord-API-Key header for testing.",
	})
}

// Handles asset downloading via verified query parameters from vercel.json
func handleAssetDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	asset := r.URL.Query().Get("asset")

	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Missing shipment ID")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s-%s.pdf\"", asset, id))

	fmt.Fprintf(w, "%%PDF-1.4 [MOCK LABEL DATA FOR POSTNORD ID: %s, TYPE: %s]", id, asset)
}