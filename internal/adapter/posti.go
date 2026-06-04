// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/posti.go.
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

// PostiAdapter implements the CarrierAdapter interface for Posti.
type PostiAdapter struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewPostiAdapter creates a new PostiAdapter instance.
// A private http.Client with a 10-second transport timeout is used by default;
// callers may inject their own client via the HTTPClient field for testing or
// custom timeout budgets.
func NewPostiAdapter(apiKey string, log *zap.Logger) *PostiAdapter {
	return &PostiAdapter{
		APIKey:  apiKey,
		BaseURL: "https://api.posti.com",
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

// BookShipment books a shipment with Posti.
func (a *PostiAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	payload := map[string]interface{}{
		"shipment": map[string]interface{}{
			"sender": map[string]interface{}{
				"name":       request.Shipment.Sender.Name,
				"address":    request.Shipment.Sender.Street,
				"postalCode": request.Shipment.Sender.PostalCode,
				"city":       request.Shipment.Sender.City,
				"country":    request.Shipment.Sender.Country,
				"phone":      request.Shipment.Sender.Phone,
				"email":      request.Shipment.Sender.Email,
			},
			"receiver": map[string]interface{}{
				"name":       request.Shipment.Receiver.Name,
				"address":    request.Shipment.Receiver.Street,
				"postalCode": request.Shipment.Receiver.PostalCode,
				"city":       request.Shipment.Receiver.City,
				"country":    request.Shipment.Receiver.Country,
				"phone":      request.Shipment.Receiver.Phone,
				"email":      request.Shipment.Receiver.Email,
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
			"service": map[string]interface{}{
				"productCode": "2412", // Posti's standard parcel product code
				"addOnServices": []string{
					"2104", // SMS notification
				},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.BaseURL+"/shipment/v1/shipments",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("X-Posti-API-Key", a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var postiResponse struct {
		ShipmentID   string `json:"shipmentId"`
		TrackingCode string `json:"trackingCode"`
		LabelURL     string `json:"labelUrl"`
		Status       string `json:"status"`
		Error        struct {
			Code        string `json:"code"`
			Message     string `json:"message"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &postiResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if postiResponse.Status != "OK" && postiResponse.Error.Code != "" {
		return nil, fmt.Errorf("Posti API error: %s (%s)", postiResponse.Error.Message, postiResponse.Error.Code)
	}

	return &BookingResponse{
		TrackingNumber: postiResponse.TrackingCode,
		LabelURL:       postiResponse.LabelURL,
		Carrier:        "posti",
	}, nil
}

// TrackShipment tracks a shipment with Posti.
func (a *PostiAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/tracking/v1/shipments/%s", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("X-Posti-API-Key", a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var postiTrackingResponse struct {
		ShipmentID   string `json:"shipmentId"`
		TrackingCode string `json:"trackingCode"`
		Status       string `json:"status"`
		Events       []struct {
			Timestamp string `json:"timestamp"`
			EventCode string `json:"eventCode"`
			EventName string `json:"eventName"`
			Location  string `json:"location"`
			Country   string `json:"country"`
		} `json:"events"`
		Error struct {
			Code        string `json:"code"`
			Message     string `json:"message"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &postiTrackingResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if postiTrackingResponse.Status != "OK" && postiTrackingResponse.Error.Code != "" {
		return nil, fmt.Errorf("Posti API error: %s (%s)", postiTrackingResponse.Error.Message, postiTrackingResponse.Error.Code)
	}

	var events []TrackingEvent
	for _, event := range postiTrackingResponse.Events {
		events = append(events, TrackingEvent{
			Timestamp: event.Timestamp,
			Status:    event.EventName,
			Location:  event.Location,
		})
	}

	return &TrackingResponse{
		TrackingNumber: postiTrackingResponse.TrackingCode,
		Status:         postiTrackingResponse.Status,
		Events:         events,
	}, nil
}

// GetServicePoints retrieves service points (e.g., Posti pickup points) for Posti.
func (a *PostiAdapter) GetServicePoints(ctx context.Context, location Location) ([]ServicePoint, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/pickup-points/v1/nearest?postalCode=%s&country=%s&limit=10", a.BaseURL, location.PostalCode, location.Country),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("X-Posti-API-Key", a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var postiServicePoints struct {
		PickupPoints []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Address struct {
				Street     string `json:"street"`
				PostalCode string `json:"postalCode"`
				City       string `json:"city"`
				Country    string `json:"country"`
			} `json:"address"`
			Coordinates struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"coordinates"`
			OpeningHours []struct {
				Day       string `json:"day"`
				OpenTime  string `json:"openTime"`
				CloseTime string `json:"closeTime"`
			} `json:"openingHours"`
			Distance float64 `json:"distance"`
		} `json:"pickupPoints"`
		Error struct {
			Code        string `json:"code"`
			Message     string `json:"message"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &postiServicePoints); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if postiServicePoints.Error.Code != "" {
		return nil, fmt.Errorf("Posti API error: %s (%s)", postiServicePoints.Error.Message, postiServicePoints.Error.Code)
	}

	var servicePoints []ServicePoint
	for _, sp := range postiServicePoints.PickupPoints {
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
