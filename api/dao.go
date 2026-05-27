package api
// /api/dao.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type DaoStrategy struct{}

// ExecuteBooking sender pakke-data afsted til DAO's integrations-API
func (s DaoStrategy) ExecuteBooking(req BookingRequest) (*BookingResult, error) {
	if len(req.Colli) == 0 {
		return nil, fmt.Errorf("DAO Validation Failure: At least one colli item is required")
	}

	mockDaoID := fmt.Sprintf("DAO-%d", time.Now().Unix())
	product := "Home Delivery"
	if req.Destination.Type == "locker" || req.Destination.Type == "shop" {
		product = fmt.Sprintf("Shop Delivery (Shop ID: %s)", req.Destination.ParcelShopID)
	}

	return &BookingResult{
		BookingID: mockDaoID,
		Status:    fmt.Sprintf("completed (%s - sandbox)", product),
		LabelURL:  fmt.Sprintf("https://mock-carrier-cdn.io/sandbox/dao/labels/%s.pdf", mockDaoID),
	}, nil
}

// LookupServicePoints returnerer aktive DAO Pakkeshops og Nærbokse i området
func (s DaoStrategy) LookupServicePoints(postalCode, countryCode string) ([]ServicePoint, error) {
	if strings.ToUpper(countryCode) != "DK" {
		return []ServicePoint{}, nil
	}

	return []ServicePoint{
		{
			ID:           "dao-shop-8000-1",
			Name         "DAO Pakkeshop - Føtex Food",
			StreetName   "Guldsmedgade",
			StreetNumber: "23",
			PostalCode:   postalCode,
			City:         "Aarhus",
			CountryCode:  "DK",
			Type:         "shop",
		},
		{
			ID:           "dao-box-8000-2",
			Name         "Nærboks - Banegårdspladsen",
			StreetName   "Banegårdspladsen",
			StreetNumber: "1A",
			PostalCode:   postalCode,
			City:         "Aarhus",
			CountryCode:  "DK",
			Type:         "locker",
		},
	}, nil
}

// GetTrackingStatus leverer hændelser fra DAO's distributionsnetværk
func (s DaoStrategy) GetTrackingStatus(trackingID string) (*TrackingResult, error) {
	now := time.Now()
	return &TrackingResult{
		TrackingID:    trackingID,
		CarrierCode:   "dao",
		CurrentStatus: "out_for_delivery",
		EstimatedFull: now.Add(2 * time.Hour),
		Events: []TrackingEvent{
			{
				Description: "DAO har modtaget data om pakken",
				Status:      "info_received",
				Timestamp:   now.Add(-12 * time.Hour),
			},
			{
				Description: "Pakken er ankommet til DAO Hub (Fredericia)",
				Status:      "in_transit",
				Location:    "DAO Hub Fredericia",
				Timestamp:   now.Add(-6 * time.Hour),
			},
			{
				Description: "Pakken er lastet på omdelerens bil",
				Status:      "out_for_delivery",
				Location:    "DAO Aarhus Syd",
				Timestamp:   now.Add(-45 * time.Minute),
			},
		},
	}, nil
}

func init() {
	RegisterStrategy("dao", DaoStrategy{})
}

func DaoBookingsHandler(w http.ResponseWriter, r *http.Request) {
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

	req.CarrierCode = "dao"
	res, err := DaoStrategy{}.ExecuteBooking(req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{err.Error()}})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}