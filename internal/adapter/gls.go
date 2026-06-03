// Package adapter provides the GLS implementation of the CarrierAdapter interface.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"go.uber.org/zap"
)

// GLSAdapter implements CarrierAdapter for GLS using the ShipIT Farm API v1.
type GLSAdapter struct {
	// ContactID is the GLS-assigned shipper contact ID, required on every booking.
	ContactID  string
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	log *zap.Logger
}

// NewGLSAdapter creates a new GLSAdapter with the given contact ID and API key.
func NewGLSAdapter(contactID, apiKey string, log *zap.Logger) *GLSAdapter {
	return &GLSAdapter{
		ContactID:  contactID,
		APIKey:     apiKey,
		BaseURL:    "https://api.gls-group.net/shipit-farm/v1/backend",
		HTTPClient: http.DefaultClient,
		log: log,
	}
}

// glsAddress converts a unified Address to the GLS ShipIT Address schema.
// GLS uses PascalCase field names and "Zipcode" instead of "postalCode".
func glsAddress(a Address) map[string]interface{} {
	return map[string]interface{}{
		"Name1":             a.Name,
		"Street":            a.Street,
		"City":              a.City,
		"Zipcode":           a.PostalCode,
		"CountryCode":       a.Country,
		"MobilePhoneNumber": a.Phone,
		"Email":             a.Email,
	}
}

// glsShipmentUnit converts a single Colli to a GLS ShipmentUnit.
// Weight is kept in kg as a float — GLS does not require a unit conversion.
// The colli ID is forwarded as a ShipmentUnitReference.
func glsShipmentUnit(c Colli) map[string]interface{} {
	unit := map[string]interface{}{
		"Weight":                c.Weight,
		"ShipmentUnitReference": []string{c.ID},
	}
	if c.Dimensions.Length > 0 || c.Dimensions.Width > 0 || c.Dimensions.Height > 0 {
		unit["Volume"] = map[string]interface{}{
			"Length":         fmt.Sprintf("%.0f", c.Dimensions.Length),
			"Width":          fmt.Sprintf("%.0f", c.Dimensions.Width),
			"Height":         fmt.Sprintf("%.0f", c.Dimensions.Height),
			"VolumetricType": "NON_CALIBRATED",
			"ScannerStation": "",
		}
	}
	return unit
}

// BookShipment books a shipment with GLS and returns the booking response.
//
// The unified BookingRequest is transformed to the GLS ShipIT wire format:
//   - Sender maps to Shipper with a required ContactID.
//   - Receiver maps to Consignee with a nested Address.
//   - Each colli becomes a ShipmentUnit; weight stays in kg.
//   - The payload is wrapped in ShipmentRequestData with PrintingOptions.
func (a *GLSAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	units := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		units[i] = glsShipmentUnit(c)
	}

	shipment := map[string]interface{}{
		"Product": "PARCEL",
		"Shipper": map[string]interface{}{
			"ContactID": a.ContactID,
			"Address":   glsAddress(request.Shipment.Sender),
		},
		"Consignee": map[string]interface{}{
			"Address": glsAddress(request.Shipment.Receiver),
		},
		"ShipmentUnit": units,
	}

	if request.Shipment.Incoterms != "" {
		shipment["IncotermCode"] = request.Shipment.Incoterms
	}

	payload := map[string]interface{}{
		"Shipment": shipment,
		"PrintingOptions": map[string]interface{}{
			"ReturnLabels": map[string]interface{}{
				"TemplateSet": "NONE",
				"LabelFormat": "PDF",
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GLS request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		a.BaseURL+"/rs/shipments",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/glsVersion1+json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GLS API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GLS API returned status %d: %s", resp.StatusCode, string(body))
	}

	var glsResp struct {
		CreatedShipment struct {
			ParcelData []struct {
				TrackID      string `json:"TrackID"`
				ParcelNumber string `json:"ParcelNumber"`
			} `json:"ParcelData"`
			PrintData []struct {
				Data        []string `json:"Data"`
				LabelFormat string   `json:"LabelFormat"`
			} `json:"PrintData"`
		} `json:"CreatedShipment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("failed to decode GLS response: %w", err)
	}

	var trackingNumber, labelURL string
	if len(glsResp.CreatedShipment.ParcelData) > 0 {
		trackingNumber = glsResp.CreatedShipment.ParcelData[0].TrackID
	}
	if len(glsResp.CreatedShipment.PrintData) > 0 && len(glsResp.CreatedShipment.PrintData[0].Data) > 0 {
		labelURL = glsResp.CreatedShipment.PrintData[0].Data[0]
	}

	colliResponses := make([]ColliResponse, len(glsResp.CreatedShipment.ParcelData))
	for i, p := range glsResp.CreatedShipment.ParcelData {
		colliResponses[i] = ColliResponse{
			ID:             p.ParcelNumber,
			TrackingNumber: p.TrackID,
			Status:         "booked",
		}
	}

	return &BookingResponse{
		TrackingNumber: trackingNumber,
		LabelURL:       labelURL,
		Carrier:        "gls",
		Status:         "booked",
		Colli:          colliResponses,
	}, nil
}

// TrackShipment retrieves the tracking status for a GLS shipment.
// GLS tracking uses a POST to /rs/tracking/parcels with a TULReferenceData body.
func (a *GLSAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	// GLS tracking requires a date range; default to a broad window.
	body, err := json.Marshal(map[string]interface{}{
		"TrackID":  trackingNumber,
		"DateFrom": "2000-01-01T00:00:00Z",
		"DateTo":   "2099-12-31T23:59:59Z",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GLS tracking request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		a.BaseURL+"/rs/tracking/parcels",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS tracking request: %w", err)
	}
	req.Header.Set("Content-Type", "application/glsVersion1+json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GLS tracking API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GLS tracking API returned status %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		UnitItems []struct {
			TrackID     string `json:"TrackID"`
			Status      string `json:"Status"`
			InitialDate string `json:"InitialDate"`
		} `json:"UnitItems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("failed to decode GLS tracking response: %w", err)
	}

	var events []TrackingEvent
	status := "Unknown"
	for _, item := range glsResp.UnitItems {
		status = item.Status
		events = append(events, TrackingEvent{
			Timestamp: item.InitialDate,
			Status:    item.Status,
		})
	}

	return &TrackingResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "gls",
		Status:         status,
		Events:         events,
	}, nil
}

// GetServicePoints retrieves GLS parcel shops near the given location.
// GLS uses a POST to /rs/parcelshop/address with a ParcelShopSearchLocation body.
func (a *GLSAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	body, err := json.Marshal(map[string]interface{}{
		"CountryCode": location.Country,
		"City":        location.City,
		"Zipcode":     location.PostalCode,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GLS service points request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		a.BaseURL+"/rs/parcelshop/address",
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS service points request: %w", err)
	}
	req.Header.Set("Content-Type", "application/glsVersion1+json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GLS service points API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GLS service points API returned status %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		ParcelShop []struct {
			ParcelShopID string `json:"ParcelShopID"`
			Address      struct {
				Name1       string `json:"Name1"`
				Street      string `json:"Street"`
				Zipcode     string `json:"Zipcode"`
				City        string `json:"City"`
				CountryCode string `json:"CountryCode"`
			} `json:"Address"`
		} `json:"ParcelShop"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("failed to decode GLS service points response: %w", err)
	}

	servicePoints := make([]ServicePoint, len(glsResp.ParcelShop))
	for i, shop := range glsResp.ParcelShop {
		servicePoints[i] = ServicePoint{
			ID:   shop.ParcelShopID,
			Name: shop.Address.Name1,
			Address: Address{
				Street:     shop.Address.Street,
				PostalCode: shop.Address.Zipcode,
				City:       shop.Address.City,
				Country:    shop.Address.CountryCode,
			},
		}
	}

	return servicePoints, nil
}
