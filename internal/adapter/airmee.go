// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/airmee.go.
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

// AirmeeAdapter implements the CarrierAdapter interface for Airmee.
type AirmeeAdapter struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewAirmeeAdapter creates a new AirmeeAdapter instance.
// A private http.Client with a 5-second transport timeout is used by default;
// callers may inject their own client via the HTTPClient field for testing or
// custom timeout budgets.
func NewAirmeeAdapter(apiKey string, log *zap.Logger) *AirmeeAdapter {
	return &AirmeeAdapter{
		APIKey:  apiKey,
		BaseURL: "https://api.airmee.com/v1",
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		log: log,
	}
}

// BookShipment books a shipment with Airmee.
func (a *AirmeeAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	// Airmee uses a time window for delivery, so we set a default window of 2 hours from now.
	deliveryWindowStart := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	deliveryWindowEnd := time.Now().Add(4 * time.Hour).Format(time.RFC3339)

	payload := map[string]interface{}{
		"pickup": map[string]interface{}{
			"address": map[string]interface{}{
				"street":     request.Shipment.Sender.Street,
				"postalCode": request.Shipment.Sender.PostalCode,
				"city":       request.Shipment.Sender.City,
				"country":    request.Shipment.Sender.Country,
				"latitude":   0.0, // TODO: Add geocoding logic to get lat/long
				"longitude":  0.0, // TODO: Add geocoding logic to get lat/long
			},
			"contact": map[string]interface{}{
				"name":  request.Shipment.Sender.Name,
				"phone": request.Shipment.Sender.Phone,
				"email": request.Shipment.Sender.Email,
			},
			"timeWindow": map[string]interface{}{
				"start": deliveryWindowStart,
				"end":   deliveryWindowEnd,
			},
		},
		"delivery": map[string]interface{}{
			"address": map[string]interface{}{
				"street":     request.Shipment.Receiver.Street,
				"postalCode": request.Shipment.Receiver.PostalCode,
				"city":       request.Shipment.Receiver.City,
				"country":    request.Shipment.Receiver.Country,
				"latitude":   0.0, // TODO: Add geocoding logic to get lat/long
				"longitude":  0.0, // TODO: Add geocoding logic to get lat/long
			},
			"contact": map[string]interface{}{
				"name":  request.Shipment.Receiver.Name,
				"phone": request.Shipment.Receiver.Phone,
				"email": request.Shipment.Receiver.Email,
			},
			"timeWindow": map[string]interface{}{
				"start": deliveryWindowStart,
				"end":   deliveryWindowEnd,
			},
		},
		"parcels": []map[string]interface{}{
			{
				"reference": request.Shipment.Colli[0].ID,
				"weight":    request.Shipment.Colli[0].Weight,
				"dimensions": map[string]interface{}{
					"length": request.Shipment.Colli[0].Dimensions.Length,
					"width":  request.Shipment.Colli[0].Dimensions.Width,
					"height": request.Shipment.Colli[0].Dimensions.Height,
				},
			},
		},
		"metadata": map[string]interface{}{
			"customerReference": request.Shipment.Colli[0].ID,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.BaseURL+"/deliveries",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var airmeeResponse struct {
		ID          string `json:"id"`
		TrackingURL string `json:"trackingUrl"`
		Status      string `json:"status"`
		Error       struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &airmeeResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if airmeeResponse.Status == "error" || airmeeResponse.Error.Code != "" {
		return nil, fmt.Errorf("Airmee API error: %s (%s)", airmeeResponse.Error.Message, airmeeResponse.Error.Code)
	}

	return &BookingResponse{
		TrackingNumber: airmeeResponse.ID,
		LabelURL:       airmeeResponse.TrackingURL,
		Carrier:        "airmee",
	}, nil
}

// TrackShipment tracks a shipment with Airmee.
func (a *AirmeeAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/deliveries/%s", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var airmeeTrackingResponse struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Events []struct {
			Timestamp time.Time `json:"timestamp"`
			Type      string    `json:"type"`
			Message   string    `json:"message"`
			Location  struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
				Address   string  `json:"address"`
			} `json:"location"`
		} `json:"events"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &airmeeTrackingResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if airmeeTrackingResponse.Status == "error" || airmeeTrackingResponse.Error.Code != "" {
		return nil, fmt.Errorf("Airmee API error: %s (%s)", airmeeTrackingResponse.Error.Message, airmeeTrackingResponse.Error.Code)
	}

	var events []TrackingEvent
	for _, event := range airmeeTrackingResponse.Events {
		events = append(events, TrackingEvent{
			Timestamp: event.Timestamp.Format(time.RFC3339),
			Status:    event.Message,
			Location:  event.Location.Address,
		})
	}

	return &TrackingResponse{
		TrackingNumber: airmeeTrackingResponse.ID,
		Status:         airmeeTrackingResponse.Status,
		Events:         events,
	}, nil
}

// GetServicePoints always returns an empty list because Airmee does not offer
// a traditional service-point API; it supports only direct address pickup and
// delivery. The context is still checked so callers with a cancelled deadline
// get a predictable error rather than a silent empty result.
func (a *AirmeeAdapter) GetServicePoints(ctx context.Context, location Location) ([]ServicePoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("GetServicePoints: %w", err)
	}
	return []ServicePoint{}, nil
}
