// Package adapter provides the Hermes Germany implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/hermes.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// hermesTokenCache holds a cached OAuth2 access token with its expiry time.
type hermesTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not expired.
// A 30-second buffer is applied to avoid using a token that expires mid-request.
func (c *hermesTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// HermesAdapter implements CarrierAdapter for Hermes Germany using the
// HSI Order API (booking/labels) and HSI Shipment Info API (tracking).
// Authentication uses the OAuth2 client credentials flow.
//
// Cancellation and post-booking updates are not supported by the HSI API.
// CancelShipment and UpdateShipment return ErrNotSupported.
type HermesAdapter struct {
	// ClientID is the Hermes OAuth2 client ID.
	ClientID string
	// ClientSecret is the Hermes OAuth2 client secret.
	ClientSecret string
	// OrderBaseURL is the base URL for the HSI Order API.
	OrderBaseURL string
	// InfoBaseURL is the base URL for the HSI Shipment Info API.
	InfoBaseURL string
	// AuthURL is the token endpoint URL.
	AuthURL    string
	HTTPClient *http.Client
	tokenCache hermesTokenCache
	log        *zap.Logger
}

// NewHermesAdapter creates a new HermesAdapter with the given credentials.
func NewHermesAdapter(clientID, clientSecret string, log *zap.Logger) *HermesAdapter {
	return &HermesAdapter{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		OrderBaseURL: "https://de-api.hermesworld.com/services/hsi",
		InfoBaseURL:  "https://de-api.hermesworld.com/services/hsi",
		AuthURL:      "https://authme.myhermes.de/authorization-facade/oauth2/access_token",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// fetchToken obtains a new OAuth2 access token using the client credentials flow.
func (a *HermesAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.AuthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("hermes: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("hermes: token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return fmt.Errorf("hermes: token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("hermes: decode token response: %w", err)
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, fetching a new one if expired.
func (a *HermesAdapter) bearerToken(ctx context.Context) (string, error) {
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

// hermesLabelAccept maps a LabelFormat to the Hermes Accept header value.
func hermesLabelAccept(f LabelFormat) string {
	switch f {
	case LabelFormatZPL, LabelFormatZPLGK:
		return "application/shippinglabel-zpl+json;dpi=203"
	default:
		return "application/shippinglabel-pdf+json"
	}
}

// splitName splits a full name into firstname and lastname on the first space.
// If there is no space, lastname receives the whole name and firstname is empty.
func splitName(full string) (firstname, lastname string) {
	idx := strings.Index(full, " ")
	if idx < 0 {
		return "", full
	}
	return full[:idx], full[idx+1:]
}

// hermesReceiverAddress converts a unified Address to the Hermes ReceiverAddress schema.
func hermesReceiverAddress(a Address) map[string]any {
	addr := map[string]any{
		"street":      a.Street,
		"zipCode":     a.PostalCode,
		"town":        a.City,
		"countryCode": a.Country,
	}
	if a.HouseNumber != "" {
		addr["houseNumber"] = a.HouseNumber
	}
	if a.Supplement != "" {
		addr["addressAddition"] = a.Supplement
	}
	return addr
}

// hermesSenderAddress converts a unified Address to the Hermes DivergentSenderAddress schema.
func hermesSenderAddress(a Address) map[string]any {
	addr := map[string]any{
		"street":      a.Street,
		"zipCode":     a.PostalCode,
		"town":        a.City,
		"countryCode": a.Country,
	}
	if a.HouseNumber != "" {
		addr["houseNumber"] = a.HouseNumber
	}
	if a.Supplement != "" {
		addr["addressAddition"] = a.Supplement
	}
	return addr
}

// hermesWeightGrams converts a weight in kg to grams, rounded to the nearest gram.
func hermesWeightGrams(kg float64) int {
	return int(math.Round(kg * 1000))
}

// BookShipment books a Hermes shipment and retrieves the label in a single API call
// using POST /shipmentorders/labels.
//
// Wire format notes:
//   - Weight: parcelWeight is in grams (kg × 1000).
//   - parcelShopDeliveryService requires customerAlertService with an email address.
//   - Return shipments use POST /returnorders/labels instead.
//   - Label is returned in labelImage as base64-encoded bytes.
//   - shipmentID is the carrier tracking number; shipmentOrderID is the internal order ID.
func (a *HermesAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("hermes: shipment must contain at least one colli")
	}

	if strings.EqualFold(request.Shipment.DeliveryType, "return") {
		return a.bookReturnShipment(ctx, request)
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("hermes: obtain bearer token: %w", err)
	}

	// Use total weight; Hermes handles a shipment as a single parcel.
	totalWeightGrams := hermesWeightGrams(request.Shipment.TotalWeight)

	firstname, lastname := splitName(request.Shipment.Receiver.Name)

	payload := map[string]any{
		"receiverName": map[string]any{
			"firstname": firstname,
			"lastname":  lastname,
		},
		"receiverAddress": hermesReceiverAddress(request.Shipment.Receiver),
		"parcel": map[string]any{
			"parcelWeight": totalWeightGrams,
			"productType":  "PARCEL",
		},
	}

	if request.IdempotencyKey != "" {
		payload["clientReference"] = request.IdempotencyKey
	}

	// Divergent sender — always include to override account defaults.
	sfn, sln := splitName(request.Shipment.Sender.Name)
	payload["senderName"] = map[string]any{
		"firstname": sfn,
		"lastname":  sln,
	}
	payload["senderAddress"] = hermesSenderAddress(request.Shipment.Sender)

	// Build service block.
	service := map[string]any{}

	// Email notification — required for parcel shop delivery.
	hasEmail := hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) ||
		request.Shipment.Receiver.Email != ""
	hasSMS := hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) ||
		request.Shipment.Receiver.Phone != ""

	switch {
	case hasEmail && hasSMS:
		service["customerAlertService"] = map[string]any{
			"notificationType":   "EMAIL_SMS",
			"notificationEmail":  request.Shipment.Receiver.Email,
			"notificationNumber": request.Shipment.Receiver.Phone,
		}
	case hasEmail:
		service["customerAlertService"] = map[string]any{
			"notificationType":  "EMAIL",
			"notificationEmail": request.Shipment.Receiver.Email,
		}
	case hasSMS:
		service["customerAlertService"] = map[string]any{
			"notificationType":   "SMS",
			"notificationNumber": request.Shipment.Receiver.Phone,
		}
	}

	// Parcel shop delivery.
	isServicePoint := request.Shipment.Receiver.ServicePointID != "" ||
		strings.EqualFold(request.Shipment.DeliveryType, "servicepoint")
	if isServicePoint && request.Shipment.Receiver.ServicePointID != "" {
		if _, ok := service["customerAlertService"]; !ok {
			// Parcel shop delivery requires customerAlertService.
			return nil, fmt.Errorf("hermes: parcel shop delivery requires receiver email or SMS notification")
		}
		psFn, psLn := splitName(request.Shipment.Receiver.Name)
		service["parcelShopDeliveryService"] = map[string]any{
			"psCustomerFirstName": psFn,
			"psCustomerLastName":  psLn,
			"psID":                request.Shipment.Receiver.ServicePointID,
			"psSelectionRule":     "SELECT_BY_ID",
		}
	}

	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		service["signatureService"] = true
	}

	if cod, ok := getAddOn(request.Shipment.AddOns, AddOnCashOnDelivery); ok {
		if cod.CODAmount <= 0 {
			return nil, fmt.Errorf("hermes: cash on delivery requires CODAmount > 0")
		}
		if cod.CODCurrency == "" {
			return nil, fmt.Errorf("hermes: cash on delivery requires CODCurrency")
		}
		if _, ok := service["customerAlertService"]; !ok {
			return nil, fmt.Errorf("hermes: cash on delivery requires customerAlertService (receiver email)")
		}
		service["cashOnDeliveryService"] = map[string]any{
			"amount":   cod.CODAmount,
			"currency": cod.CODCurrency,
		}
	}

	if _, ok := getAddOn(request.Shipment.AddOns, AddOnInsurance); ok {
		return nil, notSupported("Hermes", "insurance add-on", "not available in HSI Order API")
	}

	if len(service) > 0 {
		payload["service"] = service
	}

	// Customs for international shipments.
	if c := request.Shipment.Customs; c.Incoterms != "" || len(c.Items) > 0 {
		customs := map[string]any{}
		if c.CustomsCurrency != "" {
			customs["currency"] = c.CustomsCurrency
		}
		if c.CustomsValue > 0 {
			// Hermes expects value in minor currency units (cents).
			customs["value"] = int64(math.Round(c.CustomsValue * 100))
		}
		if len(c.Items) > 0 {
			items := make([]map[string]any, len(c.Items))
			for i, item := range c.Items {
				ci := map[string]any{
					"description": item.Description,
					"quantity":    item.Quantity,
					"value":       int64(math.Round(item.Value * 100)),
				}
				if item.HSCode != "" {
					ci["hsCode"] = item.HSCode
				}
				if item.CountryOfOrigin != "" {
					ci["countryCodeOfManufacture"] = item.CountryOfOrigin
				}
				if item.NetWeight > 0 {
					ci["weight"] = hermesWeightGrams(item.NetWeight)
				}
				items[i] = ci
			}
			customs["items"] = items
		}
		payload["customsAndTaxes"] = customs
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("hermes: marshal booking request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.OrderBaseURL+"/shipmentorders/labels", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("hermes: create booking request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/shippinglabel-pdf+json")
	httpReq.Header.Set("Accept-Language", "EN")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hermes: booking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hermes: read booking response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("hermes: booking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var hermesResp struct {
		ShipmentID        string `json:"shipmentID"`
		ShipmentOrderID   string `json:"shipmentOrderID"`
		LabelImage        string `json:"labelImage"`
		LabelMediatype    string `json:"labelMediatype"`
		ListOfResultCodes []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"listOfResultCodes"`
	}
	if err := json.Unmarshal(body, &hermesResp); err != nil {
		return nil, fmt.Errorf("hermes: decode booking response: %w", err)
	}

	if hermesResp.ShipmentID == "" {
		return nil, fmt.Errorf("hermes: booking response contained no shipmentID: %s", string(body))
	}

	a.log.Info("hermes shipment booked",
		zap.String("shipmentID", hermesResp.ShipmentID),
		zap.String("shipmentOrderID", hermesResp.ShipmentOrderID),
	)

	result := &BookingResponse{
		ShipmentID:     hermesResp.ShipmentOrderID,
		TrackingNumber: hermesResp.ShipmentID,
		Carrier:        "hermes",
		Status:         "booked",
	}

	if hermesResp.LabelImage != "" {
		result.Colli = []ColliResponse{{
			ID:             hermesResp.ShipmentID,
			TrackingNumber: hermesResp.ShipmentID,
			LabelURL:       hermesResp.LabelImage, // base64 inline
			Status:         "booked",
		}}
	}

	return result, nil
}

// bookReturnShipment creates a Hermes return order and label in a single call
// via POST /returnorders/labels. The receiver of the original shipment becomes
// the sender; the sender (merchant) becomes the receiver.
func (a *HermesAdapter) bookReturnShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("hermes return: obtain bearer token: %w", err)
	}

	sfn, sln := splitName(request.Shipment.Receiver.Name) // customer returning
	payload := map[string]any{
		"senderName": map[string]any{
			"firstname": sfn,
			"lastname":  sln,
		},
		"senderAddress": map[string]any{
			"street":      request.Shipment.Receiver.Street,
			"houseNumber": request.Shipment.Receiver.HouseNumber,
			"zipCode":     request.Shipment.Receiver.PostalCode,
			"town":        request.Shipment.Receiver.City,
			"countryCode": request.Shipment.Receiver.Country,
		},
	}

	if request.Shipment.TotalWeight > 0 {
		payload["parcel"] = map[string]any{
			"parcelWeight": hermesWeightGrams(request.Shipment.TotalWeight),
		}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("hermes return: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.OrderBaseURL+"/returnorders/labels", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("hermes return: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/shippinglabel-pdf+json")
	httpReq.Header.Set("Accept-Language", "EN")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hermes return: API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hermes return: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("hermes return: carrier returned %d: %s", resp.StatusCode, string(body))
	}

	var hermesResp struct {
		ShipmentID    string `json:"shipmentID"`
		ReturnOrderID string `json:"returnOrderID"`
		Shippinglabel string `json:"shippinglabel"`
	}
	if err := json.Unmarshal(body, &hermesResp); err != nil {
		return nil, fmt.Errorf("hermes return: decode response: %w", err)
	}

	if hermesResp.ShipmentID == "" {
		return nil, fmt.Errorf("hermes return: response contained no shipmentID: %s", string(body))
	}

	a.log.Info("hermes return shipment booked",
		zap.String("shipmentID", hermesResp.ShipmentID),
		zap.String("returnOrderID", hermesResp.ReturnOrderID),
	)

	return &BookingResponse{
		ShipmentID:     hermesResp.ReturnOrderID,
		TrackingNumber: hermesResp.ShipmentID,
		Carrier:        "hermes",
		Status:         "booked",
	}, nil
}

// TrackShipment retrieves the current tracking status for a Hermes shipment
// via GET /shipmentinfo?shipmentID=.
func (a *HermesAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("hermes: tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("hermes: obtain bearer token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/shipmentinfo?shipmentID=%s", a.InfoBaseURL,
		url.QueryEscape(trackingNumber))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("hermes: create tracking request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "EN")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hermes: tracking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hermes: read tracking response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hermes: tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var trackResp struct {
		Shipmentinfo []struct {
			ShipmentID string `json:"shipmentID"`
			Status     []struct {
				Timestamp    string `json:"timestamp"`
				Code         string `json:"code"`
				Description  string `json:"description"`
				ScanningUnit struct {
					Name        string `json:"name"`
					City        string `json:"city"`
					CountryCode string `json:"countryCode"`
				} `json:"scanningUnit"`
			} `json:"status"`
			DeliveryForecast *struct {
				Date     string `json:"date"`
				TimeSlot *struct {
					From string `json:"from"`
					To   string `json:"to"`
				} `json:"timeSlot"`
			} `json:"deliveryForecast"`
		} `json:"shipmentinfo"`
	}
	if err := json.Unmarshal(body, &trackResp); err != nil {
		return nil, fmt.Errorf("hermes: decode tracking response: %w", err)
	}

	if len(trackResp.Shipmentinfo) == 0 {
		return nil, fmt.Errorf("hermes: no tracking information found for %s", trackingNumber)
	}

	si := trackResp.Shipmentinfo[0]
	events := make([]TrackingEvent, len(si.Status))
	for i, s := range si.Status {
		location := s.ScanningUnit.City
		if s.ScanningUnit.Name != "" && s.ScanningUnit.City != "" {
			location = s.ScanningUnit.Name + ", " + s.ScanningUnit.City
		} else if s.ScanningUnit.Name != "" {
			location = s.ScanningUnit.Name
		}
		events[i] = TrackingEvent{
			Timestamp:        s.Timestamp,
			Status:           s.Code,
			NormalizedStatus: normalizeStatus("hermes", s.Code),
			Location:         location,
			Details:          s.Description,
		}
	}

	rawStatus := ""
	if len(si.Status) > 0 {
		rawStatus = si.Status[0].Code
	}

	result := &TrackingResponse{
		ShipmentID:       si.ShipmentID,
		TrackingNumber:   si.ShipmentID,
		Carrier:          "hermes",
		Status:           rawStatus,
		NormalizedStatus: normalizeStatus("hermes", rawStatus),
		OriginalStatus:   rawStatus,
		Events:           events,
	}

	if si.DeliveryForecast != nil {
		result.EstimatedDelivery = si.DeliveryForecast.Date
	}

	return result, nil
}

// FetchLabel retrieves a shipping label for an existing Hermes shipment order
// via POST /shipmentorders/{shipmentOrderID}/labels.
//
// Note: the path parameter is the shipmentOrderID (internal order ID), not the
// tracking number. The caller must supply the shipmentOrderID as TrackingNumber
// when using this method, or the request will fail.
//
// TODO: consider storing shipmentOrderID alongside the tracking number at
// booking time and passing it here explicitly.
func (a *HermesAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("hermes: tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("hermes: obtain bearer token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/shipmentorders/%s/labels", a.OrderBaseURL, req.TrackingNumber)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("hermes: create label request: %w", err)
	}
	httpReq.Header.Set("Accept", hermesLabelAccept(req.Format))
	httpReq.Header.Set("Accept-Language", "EN")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hermes: label API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hermes: read label response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hermes: label API returned status %d: %s", resp.StatusCode, string(body))
	}

	var labelResp struct {
		LabelImage string `json:"labelImage"`
	}
	if err := json.Unmarshal(body, &labelResp); err != nil {
		return nil, fmt.Errorf("hermes: decode label response: %w", err)
	}
	if labelResp.LabelImage == "" {
		return nil, fmt.Errorf("hermes: label response contained no labelImage data")
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "hermes",
		Format:         req.Format,
		Data:           labelResp.LabelImage,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment is not supported by Hermes.
// The HSI Order API only supports cancellation of pickup orders, not individual shipments.
func (a *HermesAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("Hermes", "shipment cancellation",
		"the HSI API does not support cancellation of individual shipment orders; contact Hermes customer service")
}

// UpdateShipment is not supported by Hermes.
func (a *HermesAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Hermes", "post-booking update", "")
}
