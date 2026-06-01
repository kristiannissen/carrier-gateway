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
)

// BringAdapter implements CarrierAdapter for Bring.
type BringAdapter struct {
	apiKey     string
	customerID string
	baseURL    string
	httpClient *http.Client // Use http.Client instead of resty.Client
}

// NewBringAdapter creates a new Bring adapter.
func NewBringAdapter(apiKey, customerID string) *BringAdapter {
	return &BringAdapter{
		apiKey:     apiKey,
		customerID: customerID,
		baseURL:    "https://api.bring.com",
		httpClient: http.DefaultClient, // Use http.DefaultClient
	}
}

// BookShipment books a shipment with Bring.
func (a *BringAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	// Prepare the request payload for Bring's API
	payload := map[string]interface{}{
		"customerId": a.customerID,
		"shipment": map[string]interface{}{
			"from": map[string]interface{}{
				"name":       request.Shipment.Sender.Name,
				"address":    request.Shipment.Sender.Street,
				"postalCode": request.Shipment.Sender.PostalCode,
				"city":       request.Shipment.Sender.City,
				"country":    request.Shipment.Sender.Country,
			},
			"to": map[string]interface{}{
				"name":       request.Shipment.Receiver.Name,
				"address":    request.Shipment.Receiver.Street,
				"postalCode": request.Shipment.Receiver.PostalCode,
				"city":       request.Shipment.Receiver.City,
				"country":    request.Shipment.Receiver.Country,
			},
			"parcels": []map[string]interface{}{
				{
					"weight": request.Shipment.Colli[0].Weight,
					"length": request.Shipment.Colli[0].Dimensions.Length,
					"width":  request.Shipment.Colli[0].Dimensions.Width,
					"height": request.Shipment.Colli[0].Dimensions.Height,
				},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	// Create a new request to Bring's API
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		a.baseURL+"/shipping/shipment",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-MyBring-API-Uid", a.customerID)

	// Send the request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Parse the response
	var bringResponse struct {
		ConsignmentNumber string `json:"consignmentNumber"`
		LabelURL          string `json:"labelUrl"`
	}
	if err := json.Unmarshal(body, &bringResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	// Return the standardized response
	return &BookingResponse{
		TrackingNumber: bringResponse.ConsignmentNumber,
		LabelURL:       bringResponse.LabelURL,
		Carrier:        "bring",
	}, nil
}

// GetServicePoints retrieves service points (parcel shops) for Bring.
func (a *BringAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	// Create a new request to Bring's service points API
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/pickup-points?postalCode=%s&country=%s&limit=10", a.baseURL, location.PostalCode, location.Country),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-MyBring-API-Uid", a.customerID)

	// Send the request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check for non-200 status codes
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Bring service points API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var bringServicePoints []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Address struct {
			Street     string `json:"street"`
			PostalCode string `json:"postalCode"`
			City       string `json:"city"`
			Country    string `json:"country"`
		} `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bringServicePoints); err != nil {
		return nil, fmt.Errorf("failed to decode service points response: %v", err)
	}

	// Convert Bring's service points to the standardized ServicePoint format
	var servicePoints []ServicePoint
	for _, sp := range bringServicePoints {
		servicePoints = append(servicePoints, ServicePoint{
			ID:   sp.ID,
			Name: sp.Name,
			Address: Address{
				Street:     sp.Address.Street,
				PostalCode: sp.Address.PostalCode,
				City:       sp.Address.City,
				Country:    sp.Address.Country,
			},
		})
	}

	return servicePoints, nil
}

// TrackShipment tracks a shipment with Bring.
func (a *BringAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	// Create a new request to Bring's tracking API
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/tracking/consignments/%s", a.baseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-MyBring-API-Uid", a.customerID)
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check for non-200 status codes
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Bring tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var bringTrackingResponse struct {
		ConsignmentNumber string `json:"consignmentNumber"`
		Status            string `json:"status"`
		Events            []struct {
			Timestamp string `json:"timestamp"`
			Status    string `json:"status"`
			Location  string `json:"location"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bringTrackingResponse); err != nil {
		return nil, fmt.Errorf("failed to decode tracking response: %v", err)
	}

	// Convert Bring's tracking events to the standardized TrackingEvent format
	var events []TrackingEvent
	for _, event := range bringTrackingResponse.Events {
		events = append(events, TrackingEvent{
			Timestamp: event.Timestamp,
			Status:    event.Status,
			Location:  event.Location,
		})
	}

	// Return the standardized response
	return &TrackingResponse{
		TrackingNumber: bringTrackingResponse.ConsignmentNumber,
		Status:         bringTrackingResponse.Status,
		Events:         events,
	}, nil
}
