package api
// /api/legacy-transform.go

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
)

// LegacyShipment defines the old-school XML structure used by legacy WMS systems
type LegacyShipment struct {
	XMLName   xml.Name `xml:"Shipment"`
	Courier   string   `xml:"Carrier"`        // "POSTNORD" or "DAO"
	Receiver  string   `xml:"ReceiverName"`
	Street    string   `xml:"StreetAddress"`
	Country   string   `xml:"CountryISO"`     // fx "DK", "NO"
	WeightKG  float64  `xml:"TotalWeight"`
	Incoterm  string   `xml:"IncotermCode"`   // fx "DDP", "DAP"
	HSCode    string   `xml:"TariffCode"`     // Gamle systemers navn for HS-kode
	GoodsDesc string   `xml:"ContentDescription"`
}

func LegacyTransformHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// 1. Læs den rå XML-strøm fra det gamle lagersystem
	var xmlShipment LegacyShipment
	if err := xml.NewDecoder(r.Body).Decode(&xmlShipment); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Malformed legacy XML payload. Unable to parse stream.",
		})
		return
	}

	// 2. Normalisering: Gør kurer-koden klar til vores interne strategi-grid
	carrierTarget := strings.ToLower(xmlShipment.Courier)
	if carrierTarget != "postnord" && carrierTarget != "dao" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Legacy Translation Exception: Unknown carrier engine '" + xmlShipment.Courier + "'",
		})
		return
	}

	// 3. Transformation: Map XML over i vores nye stramme BookingRequest struct (api/core.go)
	normalizedReq := BookingRequest{
		CarrierCode:        carrierTarget,
		IncludeReturnLabel: true, // Legacy B2B kræver altid twin-label her
		Destination: Destination{
			CountryCode: xmlShipment.Country,
			Type:        "home",
		},
		Colli: []ColliItem{
        		{WeightKG: 4.5, Dimensions: Dimensions{LengthCM: 10, WidthCM: 10, HeightCM: 10}},
    		},
	}

	// Hvis det er en Non-EU forsendelse, map'er vi automatisk told-data med over
	if xmlShipment.Country != "DK" && xmlShipment.Country != "SE" && xmlShipment.Country != "DE" {
		if xmlShipment.HSCode != "" {
			normalizedReq.Incoterm = xmlShipment.Incoterm
			normalizedReq.CustomsItems = []CustomsItem{
				{
					HSCode:      xmlShipment.HSCode,
					Description: xmlShipment.GoodsDesc,
					Value:       99.0, // Mock standard-værdi hvis det gamle system mangler den
					Currency:    "EUR",
				},
			}
		}
	}

	// 4. Intern videresendelse: Vi konverterer vores nye struct til JSON og kalder den rigtige handler direkte
	jsonBytes, err := json.Marshal(normalizedReq)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Forbered et nyt HTTP-request internt til vores rigtige maskinrum
	internalReq, _ := http.NewRequest(http.MethodPost, "/api/v1/"+carrierTarget+"-bookings", bytes.NewBuffer(jsonBytes))
	internalReq.Header.Set("Content-Type", "application/json")
	// Vi arver eksekverings-måden (sync/async) direkte fra det gamle systems HTTP-headere
	if r.Header.Get("X-Execution-Mode") != "" {
		internalReq.Header.Set("X-Execution-Mode", r.Header.Get("X-Execution-Mode"))
	}

	// Vi trigger den korrekte handler på tværs af strategien symmetrisk
	if carrierTarget == "postnord" {
		PostNordBookingsHandler(w, internalReq)
	} else if carrierTarget == "dao" {
		DAOBookingsHandler(w, internalReq)
	}
}
