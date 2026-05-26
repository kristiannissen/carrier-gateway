package api
// /api/dao-service-points.go

import (
	"encoding/json"
	"net/http"
)

type DAOAddressBlock struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	Zip     string `json:"zip"`
	Country string `json:"country"`
}

type DAOCoordinatesBlock struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type DAOServicePoint struct {
	ShopID      string              `json:"shop_id"`
	CarrierCode string              `json:"carrier_code"`
	Name        string              `json:"name"`
	Type        string              `json:"type"` // "parcel_shop" or "locker"
	Address     DAOAddressBlock     `json:"address"`
	Coordinates DAOCoordinatesBlock `json:"coordinates"`
}

func DAOServicePointsHandler(w http.ResponseWriter, r *http.Request) {
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

	apiKey := r.Header.Get("X-DAO-API-Key")

	if apiKey == "" || apiKey == "mock" {
		mockPoints := []DAOServicePoint{
			{
				ShopID:      "DAO-3312",
				CarrierCode: "dao",
				Name:        "DAO Pakkeshop 7-Eleven",
				Type:        "parcel_shop",
				Address: DAOAddressBlock{
					Street:  "Banegårdspladsen 4",
					City:    "Aarhus C",
					Zip:     zip,
					Country: "DK",
				},
				Coordinates: DAOCoordinatesBlock{
					Lat: 56.1502,
					Lng: 10.2045,
				},
			},
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockPoints)
		return
	}

	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "DAO Location service lookup requires functional production API tokens.",
	})
}