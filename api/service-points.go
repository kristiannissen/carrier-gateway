package api
// /api/service-points.go

import (
	"encoding/json"
	"net/http"
)

type AddressBlock struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	Zip     string `json:"zip"`
	Country string `json:"country"`
}

type CoordinatesBlock struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type ServicePoint struct {
	ShopID      string           `json:"shop_id"`
	CarrierCode string           `json:"carrier_code"`
	Name        string           `json:"name"`
	Type        string           `json:"type"` // "parcel_shop" or "locker"
	Address     AddressBlock     `json:"address"`
	Coordinates CoordinatesBlock `json:"coordinates"`
}

func ServicePointsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	zip := r.URL.Query().Get("zip")
	if zip == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing required 'zip' query parameter"})
		return
	}

	apiKey := r.Header.Get("X-PostNord-API-Key")

	// Mock response payload conforming to service-points-v1.json requirements
	if apiKey == "" || apiKey == "mock" {
		mockPoints := []ServicePoint{
			{
				ShopID:      "PN-98211",
				CarrierCode: "postnord",
				Name:        "PostNord Pakkeboks Coop Kvickly",
				Type:        "locker",
				Address: AddressBlock{
					Street:  "Åboulevarden 70",
					City:    "Aarhus C",
					Zip:     zip,
					Country: "DK",
				},
				Coordinates: CoordinatesBlock{
					Lat: 56.1567,
					Lng: 10.2101,
				},
			},
			{
				ShopID:      "PN-11024",
				CarrierCode: "postnord",
				Name:        "Føtex Posthus",
				Type:        "parcel_shop",
				Address: AddressBlock{
					Street:  "Guldsmedgade 27",
					City:    "Aarhus C",
					Zip:     zip,
					Country: "DK",
				},
				Coordinates: CoordinatesBlock{
					Lat: 56.1589,
					Lng: 10.2082,
				},
			},
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockPoints)
		return
	}

	// -----------------------------------------------------------------
	// Production PostNord Service Point/Nearest location lookup
	// -----------------------------------------------------------------
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "Live PostNord location service query requires functional production tokens.",
	})
}