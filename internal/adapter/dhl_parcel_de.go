// Package adapter provides the DHL Parcel DE pickup implementation of ManifestAdapter.
// This file is located at /internal/adapter/dhl_parcel_de.go.
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
	"time"

	"go.uber.org/zap"
)

// DHLParcelDEAdapter implements ManifestAdapter for the DHL Parcel DE pickup API
// (api-eu.dhl.com/parcel/de/transportation/pickup/v3).
//
// This adapter covers pickup scheduling only — it does not implement CarrierAdapter.
// Booking, tracking, and label operations for the DHL Parcel DE product are handled
// via the DHL eConnect API (see dhl.go).
//
// Authentication: ROPC (Resource Owner Password Credentials) Bearer token.
// Token endpoint: POST /parcel/de/user/v1/authenticate/apitoken
// (application/x-www-form-urlencoded, grant_type=password).
//
// Pickup types (auto-detected from adapter configuration):
//   - Bedarfsabholung (BDA): agreed pickup location (AsID set), ≤10 parcels, free of charge.
//   - Einmalige Abholung (EMA): agreed pickup location (AsID set), >10 parcels or heavy goods.
//   - Einzelabholung (EZA): any German address, BillingNumber required, paid service.
//
// GetPickupAvailability returns ErrNotSupported — proceed to BookPickup directly.
// CloseManifest returns ErrNotSupported — DHL Parcel DE has no manifest close API.
type DHLParcelDEAdapter struct {
	// Username is the ROPC login (DHL customer portal username).
	Username string
	// Password is the ROPC password (DHL customer portal password).
	Password string
	// ClientID is the OAuth2 client_id issued by DHL.
	ClientID string
	// ClientSecret is the OAuth2 client_secret issued by DHL.
	ClientSecret string
	// AsID is the agreed DHL service point identifier for Bedarfsabholung and
	// Einmalige Abholung pickups. Format: AS followed by 10 digits (e.g. "AS1234567890").
	// When empty, the adapter books an Einzelabholung using the address from PickupRequest.
	AsID string
	// BillingNumber is the DHL billing number required for Einzelabholung (any-address,
	// paid pickups). Format: 10 digits + 2 digits + 2 alphanumeric chars (e.g. "123456789001AB").
	// Required when AsID is empty; ignored for Bedarfsabholung and Einmalige Abholung.
	BillingNumber string
	// DefaultTransportationType is the shipment transport type sent to the API.
	// Defaults to "PAKET". Other values: ROLLBEHAELTER, WECHSELBEHAELTER, PALETTEN, SPERRGUT.
	DefaultTransportationType string
	// BaseURL is the DHL Parcel DE API base URL.
	// Production: https://api-eu.dhl.com/parcel/de/transportation/pickup/v3
	BaseURL string
	// AuthURL is the token endpoint.
	// Production: https://api-eu.dhl.com/parcel/de/user/v1/authenticate/apitoken
	AuthURL    string
	HTTPClient *http.Client
	tokenCache tokenCache
	log        *zap.Logger
}

// NewDHLParcelDEAdapter constructs a DHLParcelDEAdapter ready for production use.
// Provide asID for agreed-location pickups (Bedarfsabholung/Einmalige Abholung), or
// billingNumber for any-address paid pickups (Einzelabholung). Both may be set when
// the account supports multiple pickup types.
func NewDHLParcelDEAdapter(username, password, clientID, clientSecret string, log *zap.Logger) *DHLParcelDEAdapter {
	return &DHLParcelDEAdapter{
		Username:                  username,
		Password:                  password,
		ClientID:                  clientID,
		ClientSecret:              clientSecret,
		DefaultTransportationType: "PAKET",
		BaseURL:                   "https://api-eu.dhl.com/parcel/de/transportation/pickup/v3",
		AuthURL:                   "https://api-eu.dhl.com/parcel/de/user/v1/authenticate/apitoken",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// fetchToken obtains a new Bearer token via ROPC (grant_type=password).
func (a *DHLParcelDEAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", a.Username)
	form.Set("password", a.Password)
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.AuthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create DHL Parcel DE token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DHL Parcel DE token request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read DHL Parcel DE token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DHL Parcel DE token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("decode DHL Parcel DE token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("DHL Parcel DE token response contained no access_token")
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, refreshing if expired.
func (a *DHLParcelDEAdapter) bearerToken(ctx context.Context) (string, error) {
	a.tokenCache.mu.Lock()
	valid := a.tokenCache.valid()
	token := a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()

	if valid {
		return token, nil
	}
	if err := a.fetchToken(ctx); err != nil {
		return "", err
	}
	a.tokenCache.mu.Lock()
	token = a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()
	return token, nil
}

// doRequest executes an authenticated JSON request and returns the response body.
// Non-2xx responses are returned as an error containing the response body.
func (a *DHLParcelDEAdapter) doRequest(ctx context.Context, method, path string, payload []byte) ([]byte, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtain DHL Parcel DE token: %w", err)
	}

	var body io.Reader
	if len(payload) > 0 {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create DHL Parcel DE request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DHL Parcel DE %s %s: %w", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read DHL Parcel DE response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("DHL Parcel DE %s %s returned %d: %s", method, path, resp.StatusCode, respBody)
	}
	return respBody, nil
}

// transportationType returns the configured transport type, defaulting to "PAKET".
func (a *DHLParcelDEAdapter) transportationType() string {
	if a.DefaultTransportationType != "" {
		return a.DefaultTransportationType
	}
	return "PAKET"
}

// dhlParcelDEPickupLocation is the discriminated union for pickup location.
// Exactly one of ID or address must be set.
type dhlParcelDEPickupLocation struct {
	ID      *dhlParcelDELocationID      `json:"id,omitempty"`
	Address *dhlParcelDELocationAddress `json:"address,omitempty"`
}

// dhlParcelDELocationID identifies an agreed DHL service point by AsID.
type dhlParcelDELocationID struct {
	// AsID is the agreed service point identifier, e.g. "AS1234567890".
	AsID string `json:"asId"`
}

// dhlParcelDELocationAddress is a freeform German address for Einzelabholung.
type dhlParcelDELocationAddress struct {
	AddressStreet string `json:"addressStreet"`
	AddressHouse  string `json:"addressHouse,omitempty"`
	PostalCode    string `json:"postalCode"`
	City          string `json:"city"`
	CountryCode   string `json:"countryCode"`
}

// dhlParcelDEPickupOrder is the request body for POST /orders.
type dhlParcelDEPickupOrder struct {
	PickupLocation dhlParcelDEPickupLocation `json:"pickupLocation"`
	PickupDetails  dhlParcelDEPickupDetails  `json:"pickupDetails"`
	ShipmentDetails dhlParcelDEShipmentDetails `json:"shipmentDetails"`
	BillingNumber  string                    `json:"billingNumber,omitempty"`
}

// dhlParcelDEPickupDetails holds the requested collection date.
type dhlParcelDEPickupDetails struct {
	// PickupDate is YYYY-MM-DD or "ASAP".
	PickupDate string `json:"pickupDate"`
}

// dhlParcelDEShipmentDetails lists the shipments to be collected.
type dhlParcelDEShipmentDetails struct {
	Shipments []dhlParcelDEShipment `json:"shipments"`
}

// dhlParcelDEShipment describes a single shipment in the pickup order.
type dhlParcelDEShipment struct {
	// TransportationType is one of PAKET, ROLLBEHAELTER, WECHSELBEHAELTER, PALETTEN, SPERRGUT.
	TransportationType string `json:"transportationType"`
	// Quantity is the number of parcels of this type. Defaults to 1 when EstimatedParcels is 0.
	Quantity int `json:"quantity"`
}

// dhlParcelDEConfirmation is the response body from POST /orders.
type dhlParcelDEConfirmation struct {
	// OrderID is the 32-character pickup order reference.
	OrderID    string `json:"orderID"`
	PickupDate string `json:"pickupDate"`
	// FreeOfCharge indicates whether this pickup is charged to the account.
	FreeOfCharge bool   `json:"freeOfCharge"`
	PickupType   string `json:"pickupType"` // BDA, EZA, EMA
}

// buildPickupOrder constructs the API request body from PickupRequest.
// It auto-detects the pickup type from adapter configuration:
//   - AsID set → agreed-location pickup (Bedarfsabholung or Einmalige Abholung).
//   - AsID empty + BillingNumber set → Einzelabholung (any German address, paid).
func (a *DHLParcelDEAdapter) buildPickupOrder(req PickupRequest) (*dhlParcelDEPickupOrder, error) {
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("DHL Parcel DE: pickup date is required")
	}

	quantity := req.EstimatedParcels
	if quantity <= 0 {
		quantity = 1
	}

	order := &dhlParcelDEPickupOrder{
		PickupDetails: dhlParcelDEPickupDetails{
			PickupDate: req.Pickup.Date,
		},
		ShipmentDetails: dhlParcelDEShipmentDetails{
			Shipments: []dhlParcelDEShipment{
				{
					TransportationType: a.transportationType(),
					Quantity:           quantity,
				},
			},
		},
	}

	switch {
	case a.AsID != "":
		order.PickupLocation = dhlParcelDEPickupLocation{
			ID: &dhlParcelDELocationID{AsID: a.AsID},
		}
	case a.BillingNumber != "":
		if req.Address.Street == "" || req.Address.PostalCode == "" || req.Address.City == "" {
			return nil, fmt.Errorf("DHL Parcel DE: street, postal code, and city are required for Einzelabholung")
		}
		order.PickupLocation = dhlParcelDEPickupLocation{
			Address: &dhlParcelDELocationAddress{
				AddressStreet: req.Address.Street,
				AddressHouse:  req.Address.HouseNumber,
				PostalCode:    req.Address.PostalCode,
				City:          req.Address.City,
				CountryCode:   countryCodeOrDefault(req.Address.Country, "DE"),
			},
		}
		order.BillingNumber = a.BillingNumber
	default:
		return nil, fmt.Errorf("DHL Parcel DE: either AsID (agreed pickup location) or BillingNumber (Einzelabholung) must be configured")
	}

	return order, nil
}

// countryCodeOrDefault returns country if non-empty, otherwise fallback.
func countryCodeOrDefault(country, fallback string) string {
	if country != "" {
		return country
	}
	return fallback
}

// BookPickup schedules a carrier collection via POST /orders.
//
// The pickup type is determined automatically:
//   - AsID set on adapter → Bedarfsabholung (≤10 parcels) or Einmalige Abholung (>10 parcels).
//   - BillingNumber set on adapter, no AsID → Einzelabholung at the address in req.Address.
func (a *DHLParcelDEAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	order, err := a.buildPickupOrder(req)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("DHL Parcel DE: marshal pickup order: %w", err)
	}

	respBody, err := a.doRequest(ctx, http.MethodPost, "/orders", payload)
	if err != nil {
		return nil, fmt.Errorf("DHL Parcel DE: book pickup: %w", err)
	}

	var confirmation dhlParcelDEConfirmation
	if err := json.Unmarshal(respBody, &confirmation); err != nil {
		return nil, fmt.Errorf("DHL Parcel DE: decode pickup confirmation: %w", err)
	}
	if confirmation.OrderID == "" {
		return nil, fmt.Errorf("DHL Parcel DE: pickup confirmation missing orderID")
	}

	a.log.Info("DHL Parcel DE pickup booked",
		zap.String("orderID", confirmation.OrderID),
		zap.String("pickupDate", confirmation.PickupDate),
		zap.String("pickupType", confirmation.PickupType),
		zap.Bool("freeOfCharge", confirmation.FreeOfCharge),
	)

	return &PickupResponse{
		Carrier:            "dhl_parcel_de",
		ConfirmationNumber: confirmation.OrderID,
		Date:               confirmation.PickupDate,
		Status:             "booked",
	}, nil
}

// UpdatePickup cancels the existing pickup and books a new one, since the DHL
// Parcel DE API provides no update endpoint.
func (a *DHLParcelDEAdapter) UpdatePickup(ctx context.Context, confirmationNumber string, req PickupRequest) (*PickupResponse, error) {
	if err := a.CancelPickup(ctx, "dhl_parcel_de", confirmationNumber); err != nil {
		return nil, fmt.Errorf("DHL Parcel DE: cancel existing pickup before update: %w", err)
	}
	resp, err := a.BookPickup(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("DHL Parcel DE: rebook pickup after cancel: %w", err)
	}
	resp.Status = "updated"
	return resp, nil
}

// CancelPickup cancels a scheduled pickup via DELETE /orders?orderID=confirmationNumber.
func (a *DHLParcelDEAdapter) CancelPickup(ctx context.Context, _ string, confirmationNumber string) error {
	if confirmationNumber == "" {
		return fmt.Errorf("DHL Parcel DE: confirmationNumber is required for cancellation")
	}

	path := "/orders?orderID=" + url.QueryEscape(confirmationNumber)
	if _, err := a.doRequest(ctx, http.MethodDelete, path, nil); err != nil {
		return fmt.Errorf("DHL Parcel DE: cancel pickup %s: %w", confirmationNumber, err)
	}

	a.log.Info("DHL Parcel DE pickup cancelled", zap.String("orderID", confirmationNumber))
	return nil
}

// CloseManifest is not supported for DHL Parcel DE.
// The carrier closes shipments automatically; no end-of-day manifest call is required.
func (a *DHLParcelDEAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("DHL Parcel DE", "close manifest", "shipments are processed automatically — no manifest close is required")
}

// GetPickupAvailability is not supported for DHL Parcel DE.
// Proceed to BookPickup directly; the API validates the pickup date and returns an
// error if the requested date is unavailable.
func (a *DHLParcelDEAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DHL Parcel DE", "pickup availability", "proceed to BookPickup directly")
}
