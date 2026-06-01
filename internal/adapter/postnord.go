// Package adapter provides the PostNord implementation of the CarrierAdapter interface.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
)

// PostNordAdapter implements CarrierAdapter for PostNord.
type PostNordAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewPostNordAdapter creates a new PostNord adapter.
func NewPostNordAdapter(apiKey string) *PostNordAdapter {
	return &PostNordAdapter{
		apiKey:     apiKey,
		baseURL:    "https://api.postnord.com",
		httpClient: http.DefaultClient,
	}
}

// NewPostNordAdapterFromEnv creates a new PostNord adapter from environment variables.
func NewPostNordAdapterFromEnv() *PostNordAdapter {
	apiKey := os.Getenv("POSTNORD_API_KEY")
	if apiKey == "" {
		slog.Warn("POSTNORD_API_KEY not set.")
		return nil
	}
	return NewPostNordAdapter(apiKey)
}

// BookShipment books a shipment with PostNord.
func (a *PostNordAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	if request.IdempotencyKey != "" {
		slog.Warn(
			"PostNord does not support idempotency. Ignoring Idempotency-Key.",
			"idempotencyKey", request.IdempotencyKey,
		)
	}

	// Map colli to PostNord parcels
	parcels := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		parcels[i] = map[string]interface{}{
			"weight":    c.Weight,
			"length":    c.Dimensions.Length,
			"width":     c.Dimensions.Width,
			"height":    c.Dimensions.Height,
			"reference": c.ID,
		}
	}

	// Build PostNord request body
	postNordRequest := map[string]interface{}{
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
		"parcels": parcels,
	}

	if request.CallbackURL != "" {
		postNordRequest["callbackUrl"] = request.CallbackURL
	}
	if request.Shipment.Incoterms != "" {
		postNordRequest["incoterms"] = request.Shipment.Incoterms
	}
	if request.Shipment.HSCode != "" {
		postNordRequest["hsCode"] = request.Shipment.HSCode
	}

	payloadBytes, err := json.Marshal(postNordRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		fmt.Sprintf("%s/rest/shipment/v1/booking?apikey=%s", a.baseURL, a.apiKey),
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord API call failed: %v", err)
	}
	defer resp.Body.Close()

	// PostNord returns 201 Created on success
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PostNord API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response BookingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	response.Carrier = "postnord"
	return &response, nil
}

// TrackShipment retrieves the tracking status for a PostNord shipment.
func (a *PostNordAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v1/tracking/%s?apikey=%s", a.baseURL, trackingNumber, a.apiKey),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord tracking API call failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PostNord tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var trackingResponse TrackingResponse
	if err := json.NewDecoder(resp.Body).Decode(&trackingResponse); err != nil {
		return nil, fmt.Errorf("failed to decode tracking response: %v", err)
	}

	trackingResponse.Carrier = "postnord"
	return &trackingResponse, nil
}

// GetServicePoints retrieves available PostNord service points for a location.
func (a *PostNordAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v1/servicepoints?postalCode=%s&city=%s&countryCode=%s&apikey=%s",
			a.baseURL, location.PostalCode, location.City, location.Country, a.apiKey),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord service points API call failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PostNord service points API returned status %d: %s", resp.StatusCode, string(body))
	}

	var servicePoints []ServicePoint
	if err := json.NewDecoder(resp.Body).Decode(&servicePoints); err != nil {
		return nil, fmt.Errorf("failed to decode service points response: %v", err)
	}

	return servicePoints, nil
}
