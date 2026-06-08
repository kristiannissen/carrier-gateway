// Package adapter provides the DHL eCommerce Europe implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/dhl.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// dhlTokenCache holds a cached OAuth2 access token with its expiry time.
type dhlTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not yet expired.
// A 30-second buffer is applied to avoid using a token that expires mid-request.
func (c *dhlTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// DHLAdapter implements CarrierAdapter for DHL eCommerce Europe (eConnect API).
//
// Authentication: OAuth2 client credentials via GET /ccc/v1/auth/accesstoken.
// Booking: POST /ccc/send-cpan — same token used for tracking.
// Tracking: GET /track/shipments (DHL Unified Tracking API, service=ecommerce-europe).
// Cancel: Not supported via API — contact DHL customer service.
// Update: Not supported via API — contact DHL customer service.
type DHLAdapter struct {
	// ClientID is the OAuth2 client_id (eConnect credential).
	ClientID string
	// ClientSecret is the OAuth2 client_secret (eConnect credential).
	ClientSecret string
	// CustomerID is the DHL customerIdentification value sent in the sender block.
	CustomerID string
	// BookingBaseURL is the eConnect API base URL.
	// Production: https://api.dhl.com
	BookingBaseURL string
	// TrackingBaseURL is the Unified Tracking API base URL.
	// Production: https://api.dhl.com/track
	TrackingBaseURL string
	HTTPClient      *http.Client
	tokenCache      dhlTokenCache
	log             *zap.Logger
}

// NewDHLAdapter creates a new DHLAdapter.
// clientID and clientSecret are the eConnect OAuth2 credentials.
// customerID is the DHL customerIdentification value.
func NewDHLAdapter(clientID, clientSecret, customerID string, log *zap.Logger) *DHLAdapter {
	return &DHLAdapter{
		ClientID:        clientID,
		ClientSecret:    clientSecret,
		CustomerID:      customerID,
		BookingBaseURL:  "https://api.dhl.com",
		TrackingBaseURL: "https://api.dhl.com/track",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// fetchToken obtains a new OAuth2 access token using the client credentials flow.
// Endpoint: GET /ccc/v1/auth/accesstoken with HTTP Basic auth.
func (a *DHLAdapter) fetchToken(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.BookingBaseURL+"/ccc/v1/auth/accesstoken", nil)
	if err != nil {
		return fmt.Errorf("failed to create DHL token request: %w", err)
	}
	req.SetBasicAuth(a.ClientID, a.ClientSecret)
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DHL token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read DHL token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DHL token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to decode DHL token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("DHL token response contained no access_token")
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, fetching a new one if expired.
func (a *DHLAdapter) bearerToken(ctx context.Context) (string, error) {
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

// dhlProduct maps our DeliveryType to the DHL product code.
// Returns are handled by the return.network product.
// Service point delivery uses parcelconnect with a parcelshop recipient type.
func dhlProduct(deliveryType string) string {
	switch strings.ToLower(deliveryType) {
	case "return":
		return "ParcelEurope.return.network"
	default:
		return "ParcelEurope.parcelconnect"
	}
}

// dhlRecipientType maps our DeliveryType to the DHL recipient address type.
func dhlRecipientType(deliveryType string, hasServicePoint bool) string {
	if hasServicePoint || strings.EqualFold(deliveryType, "servicepoint") {
		return "parcelshop"
	}
	return "doorstep"
}

// dhlStreet concatenates street and house number into a single string.
func dhlStreet(a Address) string {
	if a.HouseNumber != "" {
		return a.Street + " " + a.HouseNumber
	}
	return a.Street
}

// dhlSenderBlock builds the cPAN sender address block.
func (a *DHLAdapter) dhlSenderBlock(addr Address) map[string]interface{} {
	sender := map[string]interface{}{
		"type":                   "default",
		"name":                   addr.Name,
		"street1":                dhlStreet(addr),
		"postcode":               addr.PostalCode,
		"city":                   addr.City,
		"country":                addr.Country,
		"customerIdentification": a.CustomerID,
	}
	if addr.Phone != "" {
		sender["mobileNr"] = addr.Phone
	}
	if addr.Email != "" {
		sender["email"] = addr.Email
	}
	if addr.Supplement != "" {
		sender["street2"] = addr.Supplement
	}
	return sender
}

// dhlRecipientBlock builds the cPAN recipient address block.
// Service point delivery uses an array with parcelshop + doorstep entries.
// Home/business delivery uses a single doorstep object.
func dhlRecipientBlock(addr Address, deliveryType string) interface{} {
	hasServicePoint := addr.ServicePointID != ""
	recipientType := dhlRecipientType(deliveryType, hasServicePoint)

	base := map[string]interface{}{
		"type":     recipientType,
		"name":     addr.Name,
		"postcode": addr.PostalCode,
		"city":     addr.City,
		"country":  addr.Country,
	}
	if addr.Phone != "" {
		base["mobileNr"] = addr.Phone
	}
	if addr.Email != "" {
		base["email"] = addr.Email
	}

	if hasServicePoint {
		// Service point delivery: array with parcelshop entry (identified by
		// street1Nr = service point ID) followed by doorstep fallback.
		parcelshop := map[string]interface{}{
			"type":     "parcelshop",
			"name":     addr.Name,
			"street1Nr": addr.ServicePointID,
			"postcode": addr.PostalCode,
			"city":     addr.City,
			"country":  addr.Country,
		}
		if addr.Email != "" {
			parcelshop["email"] = addr.Email
		}
		doorstep := map[string]interface{}{
			"type":    "doorstep",
			"name":    addr.Name,
			"street1": dhlStreet(addr),
			"postcode": addr.PostalCode,
			"city":    addr.City,
			"country": addr.Country,
		}
		if addr.Phone != "" {
			doorstep["mobileNr"] = addr.Phone
		}
		if addr.Email != "" {
			doorstep["email"] = addr.Email
		}
		return []interface{}{parcelshop, doorstep}
	}

	if addr.Street != "" {
		base["street1"] = dhlStreet(addr)
	}
	if addr.Supplement != "" {
		base["street2"] = addr.Supplement
	}
	return base
}

// dhlPhysicalFeatures builds the features.physical block.
// DHL expects weight in kg (string format "nn.nnn") and dimensions in metres (string "nn.nn").
func dhlPhysicalFeatures(colli Colli) map[string]interface{} {
	physical := map[string]interface{}{
		"grossWeight": fmt.Sprintf("%.3f", colli.Weight),
	}
	if colli.Dimensions.Length > 0 {
		physical["length"] = fmt.Sprintf("%.2f", colli.Dimensions.Length/100.0) // cm → m
	}
	if colli.Dimensions.Width > 0 {
		physical["width"] = fmt.Sprintf("%.2f", colli.Dimensions.Width/100.0)
	}
	if colli.Dimensions.Height > 0 {
		physical["height"] = fmt.Sprintf("%.2f", colli.Dimensions.Height/100.0)
	}
	return physical
}

// BookShipment books a shipment with DHL eCommerce Europe via POST /ccc/send-cpan.
//
// Wire format notes:
//   - Payload: dataElement.{parcelOriginOrganization, parcelDestinationOrganization,
//     labelDetails, general, cPAN}.
//   - Label returned inline as base64 in response.shipment.label.
//   - Tracking number returned in response.shipment.shipmentId.
//   - Dimensions in the request must be in metres (cm / 100).
//   - Weight in kg as a string, format "nn.nnn".
//   - COD via SEPA: features.cod.sepa.{amount, currency, bankAccountHolder, IBAN, BIC, intendedPurpose}.
//   - Insurance: features.extraInsurance "A" or "B" per contract.
//   - Return booking uses product ParcelEurope.return.network.
//   - Labelless return: labelDetails.qrCode = true (limited countries).
func (a *DHLAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain DHL access token: %w", err)
	}

	product := dhlProduct(request.Shipment.DeliveryType)
	isReturn := strings.EqualFold(request.Shipment.DeliveryType, "return")
	isLabelless := isReturn && strings.EqualFold(request.Shipment.ReturnFunctionality, "labelless")

	// Label details.
	labelFormat := "pdf"
	labelDetails := map[string]interface{}{
		"label":       true,
		"formatLabel": labelFormat,
		"size":        "10x15",
	}
	if isLabelless {
		labelDetails["label"] = false
		labelDetails["qrCode"] = true
		labelDetails["formatQrCode"] = "png"
	}

	// Physical features (first colli only — DHL eConnect is single-parcel per cPAN).
	colli := request.Shipment.Colli[0]
	features := map[string]interface{}{
		"physical": dhlPhysicalFeatures(colli),
	}

	// COD add-on — SEPA only, ParcelEurope.parcelconnect only.
	if cod, ok := getAddOn(request.Shipment.AddOns, AddOnCashOnDelivery); ok {
		if cod.CODAmount <= 0 {
			return nil, fmt.Errorf("cash_on_delivery add-on requires CODAmount > 0")
		}
		if cod.CODCurrency == "" {
			return nil, fmt.Errorf("cash_on_delivery add-on requires CODCurrency")
		}
		if cod.CODAccountNumber == "" {
			return nil, fmt.Errorf("cash_on_delivery add-on requires CODAccountNumber (IBAN)")
		}
		features["cod"] = map[string]interface{}{
			"sepa": map[string]interface{}{
				"amount":            fmt.Sprintf("%.2f", cod.CODAmount),
				"currency":          cod.CODCurrency,
				"bankAccountHolder": request.Shipment.Receiver.Name,
				"IBAN":              cod.CODAccountNumber,
				"BIC":               "",
				"intendedPurpose":   request.IdempotencyKey,
			},
		}
	}

	// Insurance add-on — clause A or B per contract.
	if _, ok := getAddOn(request.Shipment.AddOns, AddOnInsurance); ok {
		features["extraInsurance"] = "A"
	}

	// Signature required is not a standard DHL eConnect feature code —
	// log a warning and skip.
	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		if a.log != nil {
			a.log.Warn("DHL eConnect does not support signature_required add-on; ignored")
		}
	}

	// Flex delivery is not a standard DHL eConnect feature code — log and skip.
	if hasAddOn(request.Shipment.AddOns, AddOnFlexDelivery) {
		if a.log != nil {
			a.log.Warn("DHL eConnect does not support flex_delivery add-on; ignored")
		}
	}

	cPAN := map[string]interface{}{
		"addresses": map[string]interface{}{
			"sender":    a.dhlSenderBlock(request.Shipment.Sender),
			"recipient": dhlRecipientBlock(request.Shipment.Receiver, request.Shipment.DeliveryType),
		},
		"features": features,
	}

	// Customer reference number.
	if request.IdempotencyKey != "" {
		sender := cPAN["addresses"].(map[string]interface{})["sender"].(map[string]interface{})
		sender["referenceNr"] = request.IdempotencyKey
	}

	general := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"product":   product,
	}

	dataElement := map[string]interface{}{
		"parcelOriginOrganization":      request.Shipment.Sender.Country,
		"parcelDestinationOrganization": request.Shipment.Receiver.Country,
		"labelDetails":                  labelDetails,
		"general":                       general,
		"cPAN":                          cPAN,
	}

	payload := map[string]interface{}{
		"dataElement": dataElement,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DHL booking request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BookingBaseURL+"/ccc/send-cpan", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create DHL booking request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DHL booking request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DHL booking response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DHL booking returned status %d: %s", resp.StatusCode, string(body))
	}

	var dhlResp struct {
		Response struct {
			Status        string `json:"status"`
			StatusCode    string `json:"statusCode"`
			StatusMessage string `json:"statusMessage"`
			Shipment      struct {
				ShipmentID  string `json:"shipmentId"`
				RoutingCode string `json:"routingCode"`
				Label       string `json:"label"`   // base64 PDF/PNG/ZPL
				QRCode      string `json:"qrCode"`  // base64 QR code for labelless returns
				URL         string `json:"url"`     // deep link for consumer
			} `json:"shipment"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &dhlResp); err != nil {
		return nil, fmt.Errorf("failed to decode DHL booking response: %w", err)
	}

	if dhlResp.Response.StatusCode != "OK" {
		return nil, fmt.Errorf("DHL booking failed: %s", dhlResp.Response.StatusMessage)
	}

	result := &BookingResponse{
		TrackingNumber: dhlResp.Response.Shipment.ShipmentID,
		Carrier:        "dhl",
		Status:         "booked",
	}

	// Inline label.
	if dhlResp.Response.Shipment.Label != "" {
		result.Colli = []ColliResponse{
			{
				ID:             dhlResp.Response.Shipment.ShipmentID,
				TrackingNumber: dhlResp.Response.Shipment.ShipmentID,
				LabelURL:       dhlResp.Response.Shipment.Label,
				Status:         "booked",
			},
		}
	}

	// QR code for labelless returns — surfaced via LabelURL on the booking response.
	if dhlResp.Response.Shipment.QRCode != "" {
		result.LabelURL = dhlResp.Response.Shipment.URL
		result.Colli = []ColliResponse{
			{
				ID:             dhlResp.Response.Shipment.ShipmentID,
				TrackingNumber: dhlResp.Response.Shipment.ShipmentID,
				LabelURL:       dhlResp.Response.Shipment.QRCode,
				Status:         "booked",
			},
		}
	}

	return result, nil
}

// TrackShipment retrieves DHL tracking via the Unified Tracking API.
// Endpoint: GET /track/shipments?trackingNumber=&service=ecommerce-europe
// Auth: Bearer token from OAuth2 (same credentials as booking).
func (a *DHLAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain DHL access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/shipments?trackingNumber=%s&service=ecommerce-europe&language=en",
			a.TrackingBaseURL, trackingNumber), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DHL tracking request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DHL tracking request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DHL tracking response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("DHL tracking: shipment %s not found", trackingNumber)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DHL tracking returned status %d: %s", resp.StatusCode, string(body))
	}

	var trackResp struct {
		Shipments []struct {
			ID      string `json:"id"`
			Service string `json:"service"`
			Status  struct {
				Timestamp   string `json:"timestamp"`
				StatusCode  string `json:"statusCode"`
				Status      string `json:"status"`
				Description string `json:"description"`
				Location    struct {
					Address struct {
						AddressLocality string `json:"addressLocality"`
						CountryCode     string `json:"countryCode"`
					} `json:"address"`
				} `json:"location"`
			} `json:"status"`
			EstimatedTimeOfDelivery string `json:"estimatedTimeOfDelivery"`
			Events                  []struct {
				Timestamp   string `json:"timestamp"`
				StatusCode  string `json:"statusCode"`
				Status      string `json:"status"`
				Description string `json:"description"`
				Location    struct {
					Address struct {
						AddressLocality string `json:"addressLocality"`
						CountryCode     string `json:"countryCode"`
					} `json:"address"`
				} `json:"location"`
			} `json:"events"`
		} `json:"shipments"`
	}

	if err := json.Unmarshal(body, &trackResp); err != nil {
		return nil, fmt.Errorf("failed to decode DHL tracking response: %w", err)
	}

	if len(trackResp.Shipments) == 0 {
		return nil, fmt.Errorf("DHL tracking: no shipments found for %s", trackingNumber)
	}

	s := trackResp.Shipments[0]

	events := make([]TrackingEvent, len(s.Events))
	for i, e := range s.Events {
		loc := e.Location.Address.AddressLocality
		if loc == "" {
			loc = e.Location.Address.CountryCode
		}
		events[i] = TrackingEvent{
			Timestamp: e.Timestamp,
			Status:    e.StatusCode,
			Location:  loc,
			Details:   e.Description,
		}
	}

	return &TrackingResponse{
		TrackingNumber:    s.ID,
		Carrier:           "dhl",
		Status:            s.Status.StatusCode,
		EstimatedDelivery: s.EstimatedTimeOfDelivery,
		Events:            events,
	}, nil
}

// FetchLabel retrieves a DHL shipping label via GET /ccc/label-reprint.
// Requires prior authorisation from DHL (label stored for max 10 days).
// Only PDF format is supported.
func (a *DHLAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("DHL", req.Format, LabelFormatPDF)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain DHL access token: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/ccc/label-reprint?pieceId=%s&customerId=%s",
			a.BookingBaseURL, req.TrackingNumber, a.CustomerID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DHL label reprint request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("DHL label reprint request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DHL label reprint response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DHL label reprint returned status %d: %s", resp.StatusCode, string(body))
	}

	var labelResp struct {
		Response struct {
			StatusCode string `json:"statusCode"`
			Label      string `json:"label"` // base64 PDF
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &labelResp); err != nil {
		return nil, fmt.Errorf("failed to decode DHL label reprint response: %w", err)
	}

	if labelResp.Response.Label == "" {
		return nil, fmt.Errorf("DHL label reprint returned no label data for %s", req.TrackingNumber)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dhl",
		Format:         LabelFormatPDF,
		Data:           labelResp.Response.Label,
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment is not supported for DHL eCommerce Europe via API.
// Cancellations must be requested via DHL customer service.
func (a *DHLAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, fmt.Errorf("DHL does not support cancellation via API — contact DHL customer service")
}

// UpdateShipment is not supported for DHL eCommerce Europe via API.
// Modifications must be requested via DHL customer service.
func (a *DHLAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, fmt.Errorf("DHL does not support post-booking updates via API — contact DHL customer service")
}
