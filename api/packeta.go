package api
// /api/packeta.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type PacketaStrategy struct{}

func (s PacketaStrategy) ExecuteBooking(req BookingRequest) (*BookingResult, error) {
	if req.Destination.CountryCode == "" {
		return nil, fmt.Errorf("Packeta API Error: Destination country code is structural requirement for Zásilkovna branch routing")
	}

	mockPacketaID := fmt.Sprintf("ZAS-CZ-%d", time.Now().Unix())
	return &BookingResult{
		BookingID: mockPacketaID,
		Status:    "completed (central-europe-sandbox)",
		LabelURL:  fmt.Sprintf("https://mock-carrier-cdn.io/sandbox/packeta/labels/%s.pdf", mockPacketaID),
	}, nil
}

// LookupServicePoints spytter Zásilkovna/Packeta filialer ud baseret på CZ/SK/PL/HU postnumre
func (s PacketaStrategy) LookupServicePoints(postalCode, countryCode string) ([]ServicePoint, error) {
	cc := strings.ToUpper(countryCode)
	if cc != "CZ" && cc != "SK" && cc != "PL" {
		return []ServicePoint{}, nil
	}

	return []ServicePoint{
		{
			ID:           "packeta-branch-prg1",
			Name         "Zásilkovna - Potraviny Večerka",
			StreetName   "Spálená",
			StreetNumber: "45",
			PostalCode:   postalCode,
			City:         "Praha",
			CountryCode:  cc,
			Type:         "shop",
		},
		{
			ID:           "packeta-box-prg2",
			Name         "Z-BOX - Main Railway Station",
			StreetName   "Wilsonova",
			StreetNumber: "300",
			PostalCode:   postalCode,
			City:         "Praha",
			CountryCode:  cc,
			Type:         "locker",
		},
	}, nil
}

// GetTrackingStatus leverer data fra det centraleuropæiske filialnetværk
func (s PacketaStrategy) GetTrackingStatus(trackingID string) (*TrackingResult, error) {
	now := time.Now()
	return &TrackingResult{
		TrackingID:    trackingID,
		CarrierCode:   "packeta",
		CurrentStatus: "in_transit",
		EstimatedFull: now.Add(48 * time.Hour),
		Events: []TrackingEvent{
			{
				Description: "Zásilka byla podána elektronicky",
				Status:      "info_received",
				Timestamp:   now.Add(-2 * time.Hour),
			},
		},
	}, nil
}

func init() {
	RegisterStrategy("packeta", PacketaStrategy{})
}

func PacketaBookingsHandler(w http.ResponseWriter, r *http.Request) {
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

	req.CarrierCode = "packeta"
	res, err := PacketaStrategy{}.ExecuteBooking(req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{err.Error()}})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}