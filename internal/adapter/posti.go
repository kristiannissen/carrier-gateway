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
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// postiTokenCache holds a cached OAuth2 bearer token with its expiry time.
type postiTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not yet expired.
// A 30-second buffer guards against expiry mid-request.
func (c *postiTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// PostiAdapter implements the CarrierAdapter interface for Posti.
//
// Authentication:
//   - OAuth2 client_credentials via POST https://gateway-auth.posti.fi/api/v1/token.
//   - Access tokens expire after 3600 seconds; the adapter refreshes automatically.
type PostiAdapter struct {
	// ClientID is the OAuth2 client_id from the Posti Developer Portal.
	ClientID string
	// ClientSecret is the OAuth2 client_secret from the Posti Developer Portal.
	ClientSecret string
	// BaseURL is the versioned Posti Gateway API base URL.
	// Defaults to https://gateway.posti.fi/2025-04.
	BaseURL    string
	HTTPClient *http.Client
	tokenCache postiTokenCache
	log        *zap.Logger
}

// NewPostiAdapter creates a new PostiAdapter using OAuth2 client credentials.
func NewPostiAdapter(clientID, clientSecret string, log *zap.Logger) *PostiAdapter {
	return &PostiAdapter{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		BaseURL:      "https://gateway.posti.fi/2025-04",
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		log:          log,
	}
}

// fetchToken requests a new access token from the Posti token endpoint.
// The caller must hold no lock; fetchToken acquires tokenCache.mu internally.
func (a *PostiAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://gateway-auth.posti.fi/api/v1/token",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("failed to create Posti token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("posti token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Posti token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("posti token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to decode Posti token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("posti token response contained no access_token")
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// token returns a valid bearer token, refreshing if necessary.
func (a *PostiAdapter) token(ctx context.Context) (string, error) {
	a.tokenCache.mu.Lock()
	valid := a.tokenCache.valid()
	tok := a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()

	if valid {
		return tok, nil
	}
	if err := a.fetchToken(ctx); err != nil {
		return "", err
	}
	a.tokenCache.mu.Lock()
	tok = a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()
	return tok, nil
}

// BookShipment books a shipment with Posti.
func (a *PostiAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	// AddOns are not yet supported for Posti — log a warning if any are requested.
	if len(request.Shipment.AddOns) > 0 && a.log != nil {
		a.log.Debug("Posti adapter received add-ons but does not yet support them; add-ons will be ignored",
			zap.Int("count", len(request.Shipment.AddOns)),
		)
	}

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

	receiver := map[string]interface{}{
		"name":    request.Shipment.Receiver.Name,
		"country": request.Shipment.Receiver.Country,
		"phone":   request.Shipment.Receiver.Phone,
		"email":   request.Shipment.Receiver.Email,
	}
	if request.Shipment.Receiver.ServicePointID != "" {
		receiver["pickupPointId"] = request.Shipment.Receiver.ServicePointID
	} else {
		receiver["address"] = request.Shipment.Receiver.Street
		receiver["postalCode"] = request.Shipment.Receiver.PostalCode
		receiver["city"] = request.Shipment.Receiver.City
	}

	postiService := map[string]interface{}{
		"productCode": "2412",
		"addOnServices": []string{
			"2104",
		},
	}
	if request.Shipment.Receiver.ServicePointID != "" {
		postiService["pickupPoint"] = true
	}

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
			"receiver": receiver,
			"parcels":  parcels,
			"service":  postiService,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	tok, err := a.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain Posti token: %w", err)
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
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

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
		return nil, fmt.Errorf("posti API error: %s (%s)", postiResponse.Error.Message, postiResponse.Error.Code)
	}

	return &BookingResponse{
		TrackingNumber: postiResponse.TrackingCode,
		LabelURL:       postiResponse.LabelURL,
		Carrier:        "posti",
	}, nil
}

// CancelShipment is not supported for Posti.
func (a *PostiAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("Posti", "cancellation", "")
}

// UpdateShipment is not supported for Posti.
func (a *PostiAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Posti", "post-booking update", "")
}

// FetchLabel retrieves a shipping label from Posti.
// Posti only supports PDF format; other formats return an error.
func (a *PostiAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("Posti", req.Format, LabelFormatPDF)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	tok, err := a.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain Posti token: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/shipment/v1/labels/%s", a.BaseURL, req.TrackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Posti label request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+tok)

	return fetchLabelFromURL(ctx, a.HTTPClient, httpReq, req, "posti")
}

// TrackShipment tracks a shipment with Posti.
func (a *PostiAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	tok, err := a.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain Posti token: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/tracking/v1/shipments/%s", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

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
		return nil, fmt.Errorf("posti API error: %s (%s)", postiTrackingResponse.Error.Message, postiTrackingResponse.Error.Code)
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
