package api
// /api/legacy_transform.go

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
)

type LegacyShipment struct {
	XMLName   xml.Name `xml:"Shipment"`
	Courier   string   `xml:"Carrier"`
	WeightKG  float64  `xml:"TotalWeight"`
	Country   string   `xml:"CountryISO"`
}

func LegacyTransformHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var xmlShipment LegacyShipment
	if err := xml.NewDecoder(r.Body).Decode(&xmlShipment); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	normalizedReq := BookingRequest{
		CarrierCode: strings.ToLower(xmlShipment.Courier),
		Destination: Destination{CountryCode: xmlShipment.Country},
		Colli: []ColliItem{
			{
				WeightKG: xmlShipment.WeightKG,
			},
		},
	}

	// Her eksekveres vores rene Strategy Pattern!
	result, err := DispatchBooking(normalizedReq)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(result)
}