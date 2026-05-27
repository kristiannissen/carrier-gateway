package api
// /api/dhl.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type DhlStrategy struct{}

func (s DhlStrategy) ExecuteBooking(req BookingRequest) (*BookingResult, error) {
	if req.Incoterm == "" {
		req.Incoterm = "DAP"
	}

	mockDhlID := fmt.Sprintf("DHL-EXPRESS-%d", time.Now().Unix())
	return &BookingResult{
		BookingID: mockDhlID,
		Status:    "completed (global-sandbox)",
		LabelURL:  fmt.Sprintf("https://mock-carrier-cdn.io/sandbox/dhl/labels/%s.pdf", mockDhlID),
	}, nil
}

// LookupServicePoints finder DHL Service Points
func (s DhlStrategy) LookupServicePoints(postalCode, countryCode string) ([]ServicePoint, error) {
	if strings.ToUpper(countryCode) != "DK" && strings.ToUpper(countryCode) != "DE" {
		return []ServicePoint{}, nil
	}

	return []ServicePoint{
		{
			ID:           "dhl-point-dk-401",
			Name         "DHL Service Point - Circle K",
			StreetName   "Vesterbrogade",
			StreetNumber: "142",
			PostalCode:   postalCode,
			City:         "København V",
			CountryCode:  countryCode,
			Type:         "shop",
		},
	}, nil
}

// GetTrackingStatus leverer realtids tracking på tværs af landegrænser
func (s DhlStrategy) GetTrackingStatus(trackingID string) (*TrackingResult, error) {
	now := time.Now()
	return &TrackingResult{
		TrackingID:    trackingID,
		CarrierCode:   "dhl",
		CurrentStatus: "delivered",
		EstimatedFull: now.Add(-1 * time.Hour),
		Events: []TrackingEvent{
			{
				Description: "Shipment information received",
				Status:      "info_received",
				Timestamp:   now.Add(-48 * time.Hour),
			},
			{
				Description: "Processed at DHL Hub Leipzig - Germany",
				Status:      "in_transit",
				Location:    "Leipzig Hub, Germany",
				Timestamp:   now.Add(-24 * time.Hour),
			},
			{
				Description: "Arrived at Delivery Facility in Kastrup - Denmark",
				Status:      "in_transit",
				Location:    "Kastrup Hub, Denmark",
				Timestamp:   now.Add(-5 * time.Hour),
			},
			{
				Description: "Shipment delivered - Signed by Jan Hansen",
				Status:      "delivered",
				Location:    "Recipient Address",
				Timestamp:   now.Add(-1 * time.Hour),
			},
		},
	}, nil
}

func init() {
	RegisterStrategy("dhl", DhlStrategy{})
}

func DhlBookingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req BookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	req.CarrierCode = "dhl"
	res, err := DhlStrategy{}.ExecuteBooking(req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{err.Error()}})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}