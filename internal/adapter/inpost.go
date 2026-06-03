// Package adapter provides the InPost implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/inpost.go.
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

// InPostAdapter implements CarrierAdapter for InPost.
type InPostAdapter struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	log *zap.Logger
}

// NewInPostAdapter creates a new InPostAdapter with the given API key.
func NewInPostAdapter(apiKey string, log *zap.Logger) *InPostAdapter {
	return &InPostAdapter{
		APIKey:     apiKey,
		BaseURL:    "https://api.inpost.pl/v1",
		HTTPClient: http.DefaultClient,
		log: log,
	}
}

// inpostParty builds the sender/recipient object expected by the InPost API.
// InPost requires address fields nested under "address" and "contact" keys,
// and splits street into streetName and houseNumber. Since the unified
// Address.Street combines both, it is forwarded as streetName with
// houseNumber left empty. Callers needing precise house numbers should
// populate Address.Street with the street name only and store the house
// number via a future Address.HouseNumber field.
func inpostParty(a Address) map[string]interface{} {
	return map[string]interface{}{
		"name": a.Name,
		"address": map[string]interface{}{
			"streetName":  a.Street,
			"houseNumber": "",
			"city":        a.City,
			"postalCode":  a.PostalCode,
			"country":     a.Country,
		},
		"contact": map[string]interface{}{
			"phone": a.Phone,
			"email": a.Email,
		},
	}
}

// inpostParcel converts a single Colli to the InPost parcel wire format.
// Weight is in kg and dimensions in cm, matching the unified model directly —
// no unit conversion is required.
func inpostParcel(index int, c Colli) map[string]interface{} {
	return map[string]interface{}{
		"id":     fmt.Sprintf("%d", index+1),
		"weight": c.Weight,
		"dimensions": map[string]interface{}{
			"length": c.Dimensions.Length,
			"width":  c.Dimensions.Width,
			"height": c.Dimensions.Height,
		},
	}
}

// BookShipment books a shipment with InPost and returns the booking response.
//
// The unified BookingRequest is transformed to the InPost wire format:
//   - Address fields are nested under "address" and "contact" keys.
//   - Receiver maps to "recipient".
//   - Parcels use sequential IDs; weight in kg and dimensions in cm require no conversion.
//   - An optional target locker ID is read from Shipment.Incoterms.
//   - The IdempotencyKey is forwarded as the shipment reference.
//   - The payload is wrapped in a top-level "shipment" object.
func (a *InPostAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	parcels := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		parcels[i] = inpostParcel(i, c)
	}

	service := map[string]interface{}{
		"id": "INPOST_STANDARD",
	}
	if request.Shipment.Incoterms != "" {
		service["targetLocker"] = request.Shipment.Incoterms
	}

	shipment := map[string]interface{}{
		"sender":    inpostParty(request.Shipment.Sender),
		"recipient": inpostParty(request.Shipment.Receiver),
		"parcels":   parcels,
		"service":   service,
	}

	if request.IdempotencyKey != "" {
		shipment["reference"] = request.IdempotencyKey
	}

	payloadBytes, err := json.Marshal(map[string]interface{}{"shipment": shipment})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal InPost request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		a.BaseURL+"/shipments",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create InPost request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("InPost API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("InPost API returned status %d: %s", resp.StatusCode, string(body))
	}

	var inpostResp struct {
		ShipmentID     string  `json:"shipmentId"`
		TrackingNumber string  `json:"trackingNumber"`
		LabelURL       string  `json:"labelUrl"`
		Status         string  `json:"status"`
		Cost           float64 `json:"cost"`
		Currency       string  `json:"currency"`
		LockerId       string  `json:"lockerId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inpostResp); err != nil {
		return nil, fmt.Errorf("failed to decode InPost response: %w", err)
	}

	return &BookingResponse{
		ShipmentID:     inpostResp.ShipmentID,
		TrackingNumber: inpostResp.TrackingNumber,
		LabelURL:       inpostResp.LabelURL,
		Carrier:        "inpost",
		Cost:           inpostResp.Cost,
		Currency:       inpostResp.Currency,
		Status:         inpostResp.Status,
		LockerId:       inpostResp.LockerId,
	}, nil
}

// TrackShipment retrieves the tracking status for an InPost shipment.
func (a *InPostAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/tracking/%s", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create InPost tracking request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("InPost tracking API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("InPost tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var inpostResp struct {
		TrackingNumber string `json:"trackingNumber"`
		Status         string `json:"status"`
		Events         []struct {
			Timestamp string `json:"timestamp"`
			Status    string `json:"status"`
			Location  string `json:"location"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inpostResp); err != nil {
		return nil, fmt.Errorf("failed to decode InPost tracking response: %w", err)
	}

	events := make([]TrackingEvent, len(inpostResp.Events))
	for i, e := range inpostResp.Events {
		events[i] = TrackingEvent{
			Timestamp: e.Timestamp,
			Status:    e.Status,
			Location:  e.Location,
		}
	}

	return &TrackingResponse{
		TrackingNumber: inpostResp.TrackingNumber,
		Carrier:        "inpost",
		Status:         inpostResp.Status,
		Events:         events,
	}, nil
}

// GetServicePoints retrieves InPost locker locations near the given location.
func (a *InPostAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/points?city=%s&postalCode=%s&countryCode=%s",
			a.BaseURL, location.City, location.PostalCode, location.Country),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create InPost service points request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("InPost service points API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("InPost service points API returned status %d: %s", resp.StatusCode, string(body))
	}

	var inpostResp struct {
		Items []struct {
			Name    string `json:"name"`
			Address struct {
				Street     string `json:"street"`
				PostalCode string `json:"postalCode"`
				City       string `json:"city"`
				Country    string `json:"country"`
			} `json:"address"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inpostResp); err != nil {
		return nil, fmt.Errorf("failed to decode InPost service points response: %w", err)
	}

	servicePoints := make([]ServicePoint, len(inpostResp.Items))
	for i, item := range inpostResp.Items {
		servicePoints[i] = ServicePoint{
			ID:   item.Name,
			Name: item.Name,
			Address: Address{
				Street:     item.Address.Street,
				PostalCode: item.Address.PostalCode,
				City:       item.Address.City,
				Country:    item.Address.Country,
			},
			Services: []string{"Locker"},
		}
	}

	return servicePoints, nil
}
