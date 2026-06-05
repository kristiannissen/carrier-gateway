// Package adapter provides the PostNord implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/postnord.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// PostNordAdapter implements CarrierAdapter for PostNord.
type PostNordAdapter struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewPostNordAdapter creates a new PostNordAdapter with the given API key.
// A private http.Client with a 10-second transport timeout is used by default;
// callers may inject their own client via the HTTPClient field for testing or
// custom timeout budgets.
func NewPostNordAdapter(apiKey string, log *zap.Logger) *PostNordAdapter {
	return &PostNordAdapter{
		APIKey:  apiKey,
		BaseURL: "https://api.postnord.com",
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
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
func (a *PostNordAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
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
	if request.Shipment.Customs.Incoterms != "" {
		shipment["incoterms"] = request.Shipment.Customs.Incoterms
	}
	if request.Shipment.Customs.HSCode != "" {
		shipment["hsCode"] = request.Shipment.Customs.HSCode
	}

	payloadBytes, err := json.Marshal(map[string]interface{}{"shipment": shipment})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.BaseURL+"/rest/shipment/v1/booking",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", a.APIKey)

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
func (a *PostNordAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v1/tracking/%s", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord tracking request: %w", err)
	}
	req.Header.Set("X-API-Key", a.APIKey)

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


