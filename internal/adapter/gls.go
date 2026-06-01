// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/gls.go.
package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GLSAdapter implements the CarrierAdapter interface for GLS.
type GLSAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewGLSAdapter creates a new GLSAdapter instance.
func NewGLSAdapter(apiKey string) *GLSAdapter {
	return &GLSAdapter{
		apiKey:     apiKey,
		baseURL:    "https://api.gls-group.eu/api/v1",
		httpClient: http.DefaultClient,
	}
}

// BookShipment books a shipment with GLS.
func (a *GLSAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	// Prepare the request payload for GLS's API
	payload := map[string]interface{}{
		"shipment": map[string]interface{}{
			"sender": map[string]interface{}{
				"name":       request.Shipment.Sender.Name,
				"address":    request.Shipment.Sender.Street,
				"postalCode": request.Shipment.Sender.PostalCode,
				"city":       request.Shipment.Sender.City,
				"country":    request.Shipment.Sender.Country,
				"phone":      request.Shipment.Sender.Phone,
			},
			"receiver": map[string]interface{}{
				"name":       request.Shipment.Receiver.Name,
				"address":    request.Shipment.Receiver.Street,
				"postalCode": request.Shipment.Receiver.PostalCode,
				"city":       request.Shipment.Receiver.City,
				"country":    request.Shipment.Receiver.Country,
				"phone":      request.Shipment.Receiver.Phone,
			},
			"parcels": []map[string]interface{}{
				{
					"weight":    request.Shipment.Colli[0].Weight,
					"length":    request.Shipment.Colli[0].Dimensions.Length,
					"width":     request.Shipment.Colli[0].Dimensions.Width,
					"height":    request.Shipment.Colli[0].Dimensions.Height,
					"reference": request.Shipment.Colli[0].ID,
				},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	// Create a new request to GLS's API
	req, err := http.NewRequest(
		http.MethodPost,
		a.baseURL+"/shipments",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

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
	var glsResponse struct {
		TrackingNumber string `json:"trackingNumber"`
		LabelURL       string `json:"labelUrl"`
	}
	if err := json.Unmarshal(body, &glsResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	// Return the standardized response
	return &BookingResponse{
		TrackingNumber: glsResponse.TrackingNumber,
		LabelURL:       glsResponse.LabelURL,
		Carrier:        "gls",
	}, nil
}

// TrackShipment tracks a shipment with GLS.
func (a *GLSAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	// Create a new request to GLS's tracking API
	req, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("%s/tracking/%s", a.baseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

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
	var glsTrackingResponse struct {
		TrackingNumber string `json:"trackingNumber"`
		Status         string `json:"status"`
		Events         []struct {
			Timestamp string `json:"timestamp"`
			Status    string `json:"status"`
			Location  string `json:"location"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &glsTrackingResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	// Convert GLS's tracking events to the standardized format
	var events []TrackingEvent
	for _, event := range glsTrackingResponse.Events {
		events = append(events, TrackingEvent{
			Timestamp: event.Timestamp,
			Status:    event.Status,
			Location:  event.Location,
		})
	}

	// Return the standardized response
	return &TrackingResponse{
		TrackingNumber: glsTrackingResponse.TrackingNumber,
		Status:         glsTrackingResponse.Status,
		Events:         events,
	}, nil
}

// GetServicePoints retrieves service points (parcel shops) for GLS.
func (a *GLSAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	// Create a new request to GLS's service points API
	req, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("%s/parcelshops?city=%s&country=%s", a.baseURL, location.City, location.Country),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

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
	var glsServicePoints []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Address struct {
			Street     string `json:"street"`
			PostalCode string `json:"postalCode"`
			City       string `json:"city"`
			Country    string `json:"country"`
		} `json:"address"`
	}
	if err := json.Unmarshal(body, &glsServicePoints); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	// Convert GLS's service points to the standardized ServicePoint format
	var servicePoints []ServicePoint
	for _, sp := range glsServicePoints {
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
