// Package adapter provides the PostNord implementation of the CarrierAdapter interface.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
)

// PostNordAdapter implements CarrierAdapter for PostNord.
type PostNordAdapter struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// NewPostNordAdapter creates a new PostNordAdapter with the given API key.
func NewPostNordAdapter(apiKey string) *PostNordAdapter {
	return &PostNordAdapter{
		APIKey:     apiKey,
		BaseURL:    "https://api.postnord.com",
		HTTPClient: http.DefaultClient,
	}
}

// NewPostNordAdapterFromEnv creates a PostNordAdapter from the POSTNORD_API_KEY
// environment variable. Returns nil if the variable is unset.
func NewPostNordAdapterFromEnv() *PostNordAdapter {
	apiKey := os.Getenv("POSTNORD_API_KEY")
	if apiKey == "" {
		slog.Warn("POSTNORD_API_KEY not set")
		return nil
	}
	return NewPostNordAdapter(apiKey)
}

// postNordParty builds the sender/recipient object expected by the PostNord API.
func postNordParty(a Address) map[string]interface{} {
	return map[string]interface{}{
		"name": a.Name,
		"address": map[string]interface{}{
			"street":     a.Street,
			"city":       a.City,
			"postalCode": a.PostalCode,
			"country":    a.Country,
		},
		"contact": map[string]interface{}{
			"phone": a.Phone,
			"email": a.Email,
		},
	}
}

// postNordParcel converts a single Colli to the PostNord parcel format.
// The parcel ID is the 1-based position in the colli slice.
// Weight is converted from kg to grams as required by the PostNord API.
func postNordParcel(index int, c Colli) map[string]interface{} {
	return map[string]interface{}{
		"id":     fmt.Sprintf("%d", index+1),
		"weight": int(math.Round(c.Weight * 1000)), // kg → grams
		"dimensions": map[string]interface{}{
			"length": c.Dimensions.Length,
			"width":  c.Dimensions.Width,
			"height": c.Dimensions.Height,
		},
	}
}

// BookShipment books a shipment with PostNord and returns the booking response.
//
// The unified BookingRequest is transformed to the PostNord wire format:
//   - Address fields are nested under "address" and "contact" keys.
//   - The receiver is mapped to "recipient".
//   - Colli weights are converted from kg to grams.
//   - All parcels are wrapped in a top-level "shipment" object.
func (a *PostNordAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	parcels := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		parcels[i] = postNordParcel(i, c)
	}

	shipment := map[string]interface{}{
		"sender":    postNordParty(request.Shipment.Sender),
		"recipient": postNordParty(request.Shipment.Receiver),
		"service": map[string]interface{}{
			"id":      "1700",
			"product": "Parcels",
		},
		"parcels": parcels,
	}

	if request.IdempotencyKey != "" {
		shipment["idempotencyKey"] = request.IdempotencyKey
	}
	if request.Shipment.Incoterms != "" {
		shipment["incoterms"] = request.Shipment.Incoterms
	}
	if request.Shipment.HSCode != "" {
		shipment["hsCode"] = request.Shipment.HSCode
	}

	payloadBytes, err := json.Marshal(map[string]interface{}{"shipment": shipment})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		fmt.Sprintf("%s/rest/shipment/v1/booking?apikey=%s", a.BaseURL, a.APIKey),
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PostNord API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response BookingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode PostNord response: %w", err)
	}

	response.Carrier = "postnord"
	return &response, nil
}

// TrackShipment retrieves the tracking status for a shipment from PostNord.
func (a *PostNordAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v1/tracking/%s?apikey=%s", a.BaseURL, trackingNumber, a.APIKey),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord tracking request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord tracking API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PostNord tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response TrackingResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode PostNord tracking response: %w", err)
	}

	response.Carrier = "postnord"
	return &response, nil
}

// GetServicePoints retrieves available PostNord service points near the given location.
func (a *PostNordAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v1/servicepoints?postalCode=%s&city=%s&countryCode=%s&apikey=%s",
			a.BaseURL, location.PostalCode, location.City, location.Country, a.APIKey),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord service points request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord service points API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PostNord service points API returned status %d: %s", resp.StatusCode, string(body))
	}

	var servicePoints []ServicePoint
	if err := json.NewDecoder(resp.Body).Decode(&servicePoints); err != nil {
		return nil, fmt.Errorf("failed to decode PostNord service points response: %w", err)
	}

	return servicePoints, nil
}
