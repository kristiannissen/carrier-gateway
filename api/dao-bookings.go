package api
// /api/dao-bookings.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type DAOCustomsValue struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type DAOCustoms struct {
	HSCode           string          `json:"hs_code"`
	CountryOfOrigin string          `json:"country_of_origin"`
	CustomsValue     DAOCustomsValue `json:"customs_value"`
}

type DAODestination struct {
	Type           string  `json:"type"` // "home_delivery" or "service_point"
	ServicePointID *string `json:"service_point_id,omitempty"`
	CarrierCode    *string `json:"carrier_code,omitempty"`
}

type DAOColliItem struct {
	Weight             float64 `json:"weight"`
	Length             float64 `json:"length"`
	Width              float64 `json:"width"`
	Height             float64 `json:"height"`
	ContentDescription string  `json:"content_description"`
}

type DAOBookingRequest struct {
	Colli              []DAOColliItem `json:"colli"`
	Destination        DAODestination `json:"destination"`
	Incoterm           string         `json:"incoterm"` // "DAP" or "DDP"
	Customs            *DAOCustoms    `json:"customs,omitempty"`
	IncludeReturnLabel bool           `json:"include_return_label,omitempty"`
}

type DAOBookingResponse struct {
	BookingID      string   `json:"booking_id"`
	TrackingID     string   `json:"tracking_id"`
	CarrierCode    string   `json:"carrier_code"`
	LabelURL       string   `json:"label_url"`
	ReturnLabelURL *string  `json:"return_label_url,omitempty"`
	CreatedAt      string   `json:"created_at"`
	Errors         []string `json:"errors,omitempty"`
}

func DAOBookingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		handleDAOLabelDownload(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	apiKey := r.Header.Get("X-DAO-API-Key")

	var req DAOBookingRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
		return
	}

	var validationErrors []string
	if len(req.Colli) == 0 {
		validationErrors = append(validationErrors, "At least one colli item is required.")
	}
	if req.Destination.Type == "" {
		validationErrors = append(validationErrors, "Destination type ('home_delivery' or 'service_point') is required.")
	}

	if len(validationErrors) > 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{"errors": validationErrors})
		return
	}

	if apiKey == "" || apiKey == "mock" {
		mockResponse := DAOBookingResponse{
			BookingID:   fmt.Sprintf("BK-DAO-%d", time.Now().Unix()),
			TrackingID:  "DAO-MOCK-776189210",
			CarrierCode: "dao",
			LabelURL:    fmt.Sprintf("https://%s/api/v1/dao-bookings/MOCK7761/label", r.Host),
			CreatedAt:   time.Now().Format(time.RFC3339),
		}

		if req.IncludeReturnLabel {
			retURL := fmt.Sprintf("https://%s/api/v1/dao-bookings/MOCK7761/return-label", r.Host)
			mockResponse.ReturnLabelURL = &retURL
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(mockResponse)
		return
	}

	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "Production DAO API bridge is under construction. Use 'mock' in X-DAO-API-Key header.",
	})
}

func handleDAOLabelDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	asset := r.URL.Query().Get("asset")

	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Missing shipment ID")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"dao-%s-%s.pdf\"", asset, id))
	fmt.Fprintf(w, "%%PDF-1.4 [MOCK LABEL DATA FOR DAO ID: %s, TYPE: %s]", id, asset)
}