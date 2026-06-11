// Package adapter provides the GLS implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/gls.go.
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

// glsTokenCache holds a cached OAuth2 access token with its expiry time.
type glsTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not yet expired.
// A 30-second buffer is applied to avoid using a token that expires mid-request.
func (c *glsTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// GLSAdapter implements CarrierAdapter for GLS using the ShipIT Farm API v1.
// Authentication uses the OAuth2 client credentials flow.
type GLSAdapter struct {
	// ClientID is the GLS OAuth2 client ID (mapped from GLS_API_KEY env var).
	ClientID string
	// ClientSecret is the GLS OAuth2 client secret.
	ClientSecret string
	// ContactID is the GLS-assigned shipper contact ID sent on every booking.
	ContactID  string
	BaseURL    string
	AuthURL    string
	HTTPClient *http.Client
	tokenCache glsTokenCache
	log        *zap.Logger
}

// NewGLSAdapter creates a new GLSAdapter with the given credentials.
func NewGLSAdapter(clientID, clientSecret, contactID string, log *zap.Logger) *GLSAdapter {
	return &GLSAdapter{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		ContactID:    contactID,
		BaseURL:      "https://api.gls-group.net/shipit-farm/v1/backend",
		AuthURL:      "https://api.gls-group.net/oauth2/v2/token",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// fetchToken obtains a new OAuth2 access token using the client credentials flow.
func (a *GLSAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.AuthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create GLS token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("GLS token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return fmt.Errorf("GLS token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode GLS token response: %w", err)
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, fetching a new one if expired.
func (a *GLSAdapter) bearerToken(ctx context.Context) (string, error) {
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

// glsAddress converts a unified Address to the GLS ShipIT Address schema.
func glsAddress(a Address) map[string]interface{} {
	addr := map[string]interface{}{
		"Name1":       a.Name,
		"Street":      a.Street,
		"Zipcode":     a.PostalCode,
		"City":        a.City,
		"CountryCode": a.Country,
	}
	if a.HouseNumber != "" {
		addr["StreetNumber"] = a.HouseNumber
	}
	if a.Phone != "" {
		addr["MobilePhoneNumber"] = a.Phone
	}
	if a.Email != "" {
		addr["Email"] = a.Email
	}
	return addr
}

// glsShipmentUnit converts a single Colli to a GLS ShipmentUnit.
func glsShipmentUnit(c Colli) map[string]interface{} {
	note := "Goods"
	if len(c.Items) > 0 {
		note = c.Items[0].Description
	}
	unit := map[string]interface{}{
		"Weight":                c.Weight,
		"Note1":                 note,
		"ShipmentUnitReference": []string{c.ID},
	}
	if c.Dimensions.Length > 0 || c.Dimensions.Width > 0 || c.Dimensions.Height > 0 {
		unit["Volume"] = map[string]interface{}{
			"Length":         fmt.Sprintf("%.0f", c.Dimensions.Length),
			"Width":          fmt.Sprintf("%.0f", c.Dimensions.Width),
			"Height":         fmt.Sprintf("%.0f", c.Dimensions.Height),
			"VolumetricType": "NON_CALIBRATED",
			"ScannerStation": "",
		}
	}
	return unit
}

// glsLabelFormat maps our LabelFormat to GLS TemplateSet and LabelFormat values.
func glsLabelFormat(f LabelFormat) (templateSet, labelFormat string) {
	switch f {
	case LabelFormatZPL, LabelFormatZPLGK:
		return "ZPL_200", "ZPL"
	default:
		return "NONE", "PDF"
	}
}

// BookShipment books a shipment with GLS.
//
// Wire format notes:
//   - OAuth2 Bearer token fetched and cached before each request.
//   - Content-Type: application/glsVersion1+json.
//   - Endpoint: POST /rs/shipments.
//   - Service array built dynamically from ServicePointID and AddOns.
//   - Labels returned inline in PrintData[0].Data[0] as base64.
func (a *GLSAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain GLS access token: %w", err)
	}

	units := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		units[i] = glsShipmentUnit(c)
	}

	category := "PRIVATE"
	if strings.EqualFold(request.Shipment.DeliveryType, "business") {
		category = "BUSINESS"
	}

	consignee := map[string]interface{}{
		"Category": category,
		"Address":  glsAddress(request.Shipment.Receiver),
	}

	shipment := map[string]interface{}{
		"Product":      "PARCEL",
		"ShippingDate": time.Now().UTC().Format(time.RFC3339),
		"Shipper": map[string]interface{}{
			"ContactID": a.ContactID,
			"Address":   glsAddress(request.Shipment.Sender),
		},
		"Consignee":    consignee,
		"ShipmentUnit": units,
	}

	// Build Service array from ServicePointID and AddOns — opt-in only.
	var services []map[string]interface{}

	isServicePoint := request.Shipment.Receiver.ServicePointID != "" ||
		strings.EqualFold(request.Shipment.DeliveryType, "servicepoint")
	if isServicePoint && request.Shipment.Receiver.ServicePointID != "" {
		services = append(services, map[string]interface{}{
			"Service":      map[string]interface{}{"ServiceName": "ShopDelivery"},
			"ShopDelivery": map[string]interface{}{"ServiceName": "ShopDelivery", "ParcelShopID": request.Shipment.Receiver.ServicePointID},
		})
	}

	wantSMS := hasAddOn(request.Shipment.AddOns, AddOnSMSNotification)
	wantEmail := hasAddOn(request.Shipment.AddOns, AddOnEmailNotification)
	if wantSMS || wantEmail {
		notificationType := "EMAIL"
		if wantSMS && wantEmail {
			notificationType = "SMS_AND_EMAIL"
		} else if wantSMS {
			notificationType = "SMS"
		}
		services = append(services, map[string]interface{}{
			"Service": map[string]interface{}{"ServiceName": "InfoService"},
			"InfoService": map[string]interface{}{
				"ServiceName":       "InfoService",
				"Email":             request.Shipment.Receiver.Email,
				"MobilePhoneNumber": request.Shipment.Receiver.Phone,
				"NotificationType":  notificationType,
			},
		})
	}

	if flex, ok := getAddOn(request.Shipment.AddOns, AddOnFlexDelivery); ok {
		flexDetail := map[string]interface{}{"ServiceName": "FlexDelivery"}
		if flex.Instructions != "" {
			flexDetail["DeliveryInstructions"] = flex.Instructions
		}
		services = append(services, map[string]interface{}{
			"Service":      map[string]interface{}{"ServiceName": "FlexDelivery"},
			"FlexDelivery": flexDetail,
		})
	}

	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		services = append(services, map[string]interface{}{
			"Service":         map[string]interface{}{"ServiceName": "DirectSignature"},
			"DirectSignature": map[string]interface{}{"ServiceName": "DirectSignature"},
		})
	}

	if hasAddOn(request.Shipment.AddOns, AddOnCashOnDelivery) {
		return nil, fmt.Errorf("GLS does not support cash on delivery")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnInsurance) {
		return nil, fmt.Errorf("GLS does not support insurance via this gateway")
	}

	if len(services) > 0 {
		shipment["Service"] = services
	}

	if request.Shipment.Customs.Incoterms != "" {
		shipment["IncotermCode"] = request.Shipment.Customs.Incoterms
	}

	templateSet, labelFormat := glsLabelFormat(LabelFormatPDF)
	payload := map[string]interface{}{
		"Shipment": shipment,
		"PrintingOptions": map[string]interface{}{
			"ReturnLabels": map[string]interface{}{
				"TemplateSet": templateSet,
				"LabelFormat": labelFormat,
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GLS request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/rs/shipments", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/glsVersion1+json")
	req.Header.Set("Accept", "application/glsVersion1+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GLS API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("GLS API returned status %d: %s", resp.StatusCode, string(body))
	}

	var glsResp struct {
		CreatedShipment struct {
			ParcelData []struct {
				TrackID      string `json:"TrackID"`
				ParcelNumber string `json:"ParcelNumber"`
			} `json:"ParcelData"`
			PrintData []struct {
				Data        []string `json:"Data"`
				LabelFormat string   `json:"LabelFormat"`
			} `json:"PrintData"`
		} `json:"CreatedShipment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("failed to decode GLS response: %w", err)
	}

	var trackingNumber string
	if len(glsResp.CreatedShipment.ParcelData) > 0 {
		trackingNumber = glsResp.CreatedShipment.ParcelData[0].TrackID
	}

	colliResponses := make([]ColliResponse, len(glsResp.CreatedShipment.ParcelData))
	for i, p := range glsResp.CreatedShipment.ParcelData {
		colliResponses[i] = ColliResponse{ID: p.ParcelNumber, TrackingNumber: p.TrackID, Status: "booked"}
	}

	return &BookingResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "gls",
		Status:         "booked",
		Colli:          colliResponses,
	}, nil
}

// CancelShipment is not yet supported for GLS.
// GLS cancellation API documentation is not yet available.
// Contact GLS directly to cancel a shipment.
func (a *GLSAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("GLS", "cancellation", "contact GLS directly")
}

// UpdateShipment is not yet supported for GLS.
// GLS post-booking update API documentation is not yet available.
func (a *GLSAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("GLS", "post-booking update", "")
}

// FetchLabel retrieves a GLS shipping label in the requested format.
func (a *GLSAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	switch req.Format {
	case LabelFormatPDF, LabelFormatZPL, LabelFormatZPLGK:
	default:
		return nil, unsupportedFormat("GLS", req.Format, LabelFormatPDF, LabelFormatZPL, LabelFormatZPLGK)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain GLS access token: %w", err)
	}

	_, labelFormat := glsLabelFormat(req.Format)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/rs/shipments/%s/labels?format=%s", a.BaseURL, req.TrackingNumber, labelFormat), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS label request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/glsVersion1+json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	return fetchLabelFromURL(ctx, a.HTTPClient, httpReq, req, "gls")
}

// TrackShipment retrieves GLS tracking via POST to /rs/tracking/parceldetails.
func (a *GLSAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain GLS access token: %w", err)
	}

	body, err := json.Marshal(map[string]interface{}{
		"TrackID":  trackingNumber,
		"DateFrom": "2000-01-01T00:00:00Z",
		"DateTo":   "2099-12-31T23:59:59Z",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GLS tracking request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/rs/tracking/parceldetails", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS tracking request: %w", err)
	}
	req.Header.Set("Content-Type", "application/glsVersion1+json")
	req.Header.Set("Accept", "application/glsVersion1+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GLS tracking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("GLS tracking API returned status %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		UnitDetail struct {
			TrackID string  `json:"TrackID"`
			Weight  float64 `json:"Weight"`
			Product string  `json:"Product"`
			History []struct {
				Date         string `json:"Date"`
				Location     string `json:"Location"`
				LocationCode string `json:"LocationCode"`
				Country      string `json:"Country"`
				StatusCode   string `json:"StatusCode"`
				Description  string `json:"description"`
			} `json:"History"`
		} `json:"UnitDetail"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("failed to decode GLS tracking response: %w", err)
	}

	events := make([]TrackingEvent, len(glsResp.UnitDetail.History))
	for i, h := range glsResp.UnitDetail.History {
		events[i] = TrackingEvent{
			Timestamp:        h.Date,
			Status:           h.StatusCode,
			NormalizedStatus: normalizeStatus("gls", h.StatusCode),
			Location:         h.Location,
			Details:          h.Description,
		}
	}

	rawStatus := ""
	if len(glsResp.UnitDetail.History) > 0 {
		rawStatus = glsResp.UnitDetail.History[0].StatusCode
	}

	return &TrackingResponse{
		TrackingNumber:   glsResp.UnitDetail.TrackID,
		Carrier:          "gls",
		Status:           rawStatus,
		NormalizedStatus: normalizeStatus("gls", rawStatus),
		OriginalStatus:   rawStatus,
		Events:           events,
	}, nil
}
