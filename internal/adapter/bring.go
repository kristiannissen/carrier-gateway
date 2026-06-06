// Package adapter provides the Bring implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/bring.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// BringAdapter implements CarrierAdapter for Bring.
type BringAdapter struct {
	APIKey     string
	CustomerID string
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewBringAdapter creates a new BringAdapter with the given API key and customer ID.
// A private http.Client with a 10-second transport timeout is used by default;
// callers may inject their own client via the HTTPClient field for testing or
// custom timeout budgets.
func NewBringAdapter(apiKey, customerID string, log *zap.Logger) *BringAdapter {
	return &BringAdapter{
		APIKey:     apiKey,
		CustomerID: customerID,
		BaseURL:    "https://api.bring.com",
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

// bringParcel converts a single Colli to the Bring parcel wire format.
// Bring uses explicit unit suffixes: weightInKg, lengthInCm, widthInCm, heightInCm.
func bringParcel(c Colli) map[string]interface{} {
	parcel := map[string]interface{}{
		"weightInKg": c.Weight,
		"lengthInCm": c.Dimensions.Length,
		"widthInCm":  c.Dimensions.Width,
		"heightInCm": c.Dimensions.Height,
	}
	if len(c.Items) > 0 {
		items := make([]map[string]interface{}, len(c.Items))
		for i, item := range c.Items {
			items[i] = map[string]interface{}{
				"description": item.Description,
				"weight":      item.Weight,
				"quantity":    item.Quantity,
				"value":       item.Value,
			}
		}
		parcel["items"] = items
	}
	return parcel
}

// BookShipment books a shipment with Bring and returns the booking response.
//
// The unified BookingRequest is transformed to the Bring wire format:
//   - Sender maps to "from", receiver to "to".
//   - All colli are mapped to "parcels" with Bring's unit-suffixed dimension keys.
//   - The customer ID is sent both in the payload and as the X-MyBring-API-Uid header.
func (a *BringAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	parcels := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		parcels[i] = bringParcel(c)
	}

	to := map[string]interface{}{
		"name":    request.Shipment.Receiver.Name,
		"country": request.Shipment.Receiver.Country,
	}
	if request.Shipment.Receiver.ServicePointID != "" {
		to["pickupPointId"] = request.Shipment.Receiver.ServicePointID
	} else {
		to["address"] = request.Shipment.Receiver.Street
		to["postalCode"] = request.Shipment.Receiver.PostalCode
		to["city"] = request.Shipment.Receiver.City
	}

	service := map[string]interface{}{
		"product": "Servicepakke",
	}
	if request.Shipment.Receiver.ServicePointID != "" {
		service["pickupPoint"] = true
	}

	payload := map[string]interface{}{
		"customerId": a.CustomerID,
		"shipment": map[string]interface{}{
			"from": map[string]interface{}{
				"name":       request.Shipment.Sender.Name,
				"address":    request.Shipment.Sender.Street,
				"postalCode": request.Shipment.Sender.PostalCode,
				"city":       request.Shipment.Sender.City,
				"country":    request.Shipment.Sender.Country,
			},
			"to":      to,
			"parcels": parcels,
			"service": service,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Bring request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.BaseURL+"/shipping/shipment",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bring request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("X-MyBring-API-Uid", a.CustomerID)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Bring API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Bring API returned status %d: %s", resp.StatusCode, string(body))
	}

	var bringResp struct {
		ConsignmentNumber string  `json:"consignmentNumber"`
		LabelURL          string  `json:"labelUrl"`
		Cost              float64 `json:"cost"`
		Currency          string  `json:"currency"`
		ServiceLevel      string  `json:"serviceLevel"`
		Status            string  `json:"status"`
		PickupPointID     string  `json:"pickupPointId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bringResp); err != nil {
		return nil, fmt.Errorf("failed to decode Bring response: %w", err)
	}

	return &BookingResponse{
		TrackingNumber: bringResp.ConsignmentNumber,
		LabelURL:       bringResp.LabelURL,
		Carrier:        "bring",
		Cost:           bringResp.Cost,
		Currency:       bringResp.Currency,
		ServiceLevel:   bringResp.ServiceLevel,
		Status:         bringResp.Status,
		ServicePointID: bringResp.PickupPointID,
	}, nil
}

// TrackShipment retrieves the tracking status for a Bring shipment.
func (a *BringAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/tracking/%s", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bring tracking request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("X-MyBring-API-Uid", a.CustomerID)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Bring tracking API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Bring tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var bringResp struct {
		ConsignmentNumber string `json:"consignmentNumber"`
		Status            string `json:"status"`
		EstimatedDelivery string `json:"estimatedDelivery"`
		Events            []struct {
			Timestamp string `json:"timestamp"`
			Status    string `json:"status"`
			Location  string `json:"location"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bringResp); err != nil {
		return nil, fmt.Errorf("failed to decode Bring tracking response: %w", err)
	}

	events := make([]TrackingEvent, len(bringResp.Events))
	for i, e := range bringResp.Events {
		events[i] = TrackingEvent{
			Timestamp: e.Timestamp,
			Status:    e.Status,
			Location:  e.Location,
		}
	}

	return &TrackingResponse{
		TrackingNumber:    bringResp.ConsignmentNumber,
		Carrier:           "bring",
		Status:            bringResp.Status,
		EstimatedDelivery: bringResp.EstimatedDelivery,
		Events:            events,
	}, nil
}


