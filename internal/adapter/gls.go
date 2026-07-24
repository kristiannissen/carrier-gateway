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

// GLSAdapter implements CarrierAdapter for GLS using the ShipIT Farm API v1
// for outbound shipments and the Shop Returns Customer Plus API v3 for returns.
// Authentication uses the OAuth2 client credentials flow.
type GLSAdapter struct {
	// ClientID is the GLS OAuth2 client ID (mapped from GLS_API_KEY env var).
	ClientID string
	// ClientSecret is the GLS OAuth2 client secret.
	ClientSecret string
	// ContactID is the GLS-assigned shipper contact ID sent on every booking.
	ContactID string
	// ReturnAppID is the GLS app-id path parameter for the Shop Returns API.
	ReturnAppID   string
	BaseURL       string
	ReturnBaseURL string
	AuthURL       string
	HTTPClient    *http.Client
	tokenCache    glsTokenCache
	log           *zap.Logger
}

// NewGLSAdapter creates a new GLSAdapter with the given credentials.
func NewGLSAdapter(clientID, clientSecret, contactID string, log *zap.Logger) *GLSAdapter {
	return &GLSAdapter{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		ContactID:     contactID,
		BaseURL:       "https://api.gls-group.net/shipit-farm/v1/backend",
		ReturnBaseURL: "https://api.gls-group.net/order-management/shop-returns/plus/v3",
		AuthURL:       "https://api.gls-group.net/oauth2/v2/token",
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
func glsAddress(a Address) map[string]any {
	addr := map[string]any{
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
func glsShipmentUnit(c Colli) map[string]any {
	note := "Goods"
	if len(c.Items) > 0 {
		note = c.Items[0].Description
	}
	unit := map[string]any{
		"Weight":                c.Weight,
		"Note1":                 note,
		"ShipmentUnitReference": []string{c.ID},
	}
	if c.Dimensions.Length > 0 || c.Dimensions.Width > 0 || c.Dimensions.Height > 0 {
		unit["Volume"] = map[string]any{
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
// GLS uses "ZEBRA" (not "ZPL") for the LabelFormat field; see Document schema.
func glsLabelFormat(f LabelFormat) (templateSet, labelFormat string) {
	switch f {
	case LabelFormatZPL, LabelFormatZPLGK:
		return "ZPL_200", "ZEBRA"
	default:
		return "NONE", "PDF"
	}
}

// BookShipment books a shipment with GLS.
//
// When DeliveryType is "return", the request is routed to the GLS Shop Returns
// Customer Plus API v3 via bookReturnShipment. All other delivery types use
// the ShipIT Farm API v1.
//
// Wire format notes (outbound):
//   - OAuth2 Bearer token fetched and cached before each request.
//   - Content-Type: application/glsVersion1+json.
//   - Endpoint: POST /rs/shipments.
//   - Service array built dynamically from ServicePointID and AddOns.
//   - Labels returned inline in PrintData[0].Data[0] as base64.
func (a *GLSAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	if strings.EqualFold(request.Shipment.DeliveryType, "return") {
		return a.bookReturnShipment(ctx, request)
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain GLS access token: %w", err)
	}

	units := make([]map[string]any, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		units[i] = glsShipmentUnit(c)
	}

	category := "PRIVATE"
	if strings.EqualFold(request.Shipment.DeliveryType, "business") {
		category = "BUSINESS"
	}

	consignee := map[string]any{
		"Category": category,
		"Address":  glsAddress(request.Shipment.Receiver),
	}

	shipment := map[string]any{
		"Product":      "PARCEL",
		"ShippingDate": time.Now().UTC().Format(time.RFC3339),
		"Shipper": map[string]any{
			"ContactID": a.ContactID,
			"Address":   glsAddress(request.Shipment.Sender),
		},
		"Consignee":    consignee,
		"ShipmentUnit": units,
	}

	// Build Service array from ServicePointID and AddOns — opt-in only.
	var services []map[string]any

	isServicePoint := request.Shipment.Receiver.ServicePointID != "" ||
		strings.EqualFold(request.Shipment.DeliveryType, "servicepoint")
	if isServicePoint && request.Shipment.Receiver.ServicePointID != "" {
		services = append(services, map[string]any{
			"Service":      map[string]any{"ServiceName": "ShopDelivery"},
			"ShopDelivery": map[string]any{"ServiceName": "ShopDelivery", "ParcelShopID": request.Shipment.Receiver.ServicePointID},
		})
	}

	// InfoService, FlexDelivery, and DirectSignature are not part of the GLS
	// ShipIT API v1 ShipmentService schema and are rejected by the API.
	// Note: email notification IS supported for return shipments via bookReturnShipment.
	if hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) {
		return nil, notSupported("GLS", "SMS notification add-on",
			"not available in ShipIT API v1 ShipmentService schema")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) {
		return nil, notSupported("GLS", "email notification add-on",
			"not available in ShipIT API v1 ShipmentService schema; use DeliveryType=return for return shipments with email")
	}
	if _, ok := getAddOn(request.Shipment.AddOns, AddOnFlexDelivery); ok {
		return nil, notSupported("GLS", "flex delivery add-on",
			"not available in ShipIT API v1 ShipmentService schema")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		return nil, notSupported("GLS", "signature required add-on",
			"not available in ShipIT API v1 ShipmentService schema")
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
	payload := map[string]any{
		"Shipment": shipment,
		"PrintingOptions": map[string]any{
			"ReturnLabels": map[string]any{
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

// glsReturnAddress is the address sub-object for GLS Shop Returns API v3.
type glsReturnAddress struct {
	Street      string `json:"street"`
	ZipCode     string `json:"zipCode"`
	City        string `json:"city"`
	CountryCode string `json:"countryCode"`
}

// glsReturnSender is the sender (customer returning the parcel) for GLS Shop Returns API v3.
type glsReturnSender struct {
	PersonName string           `json:"personName"`
	Email      string           `json:"email,omitempty"`
	Address    glsReturnAddress `json:"address"`
}

// glsReturnReceiver is the receiver (merchant) for GLS Shop Returns API v3.
type glsReturnReceiver struct {
	CompanyName string           `json:"companyName"`
	Address     glsReturnAddress `json:"address"`
}

// glsReturnConfirmationMail is the optional email notification for GLS Shop Returns API v3.
type glsReturnConfirmationMail struct {
	SendTo string `json:"sendTo"`
}

// glsReturnOptions holds optional settings for a GLS return order.
type glsReturnOptions struct {
	ConfirmationMail *glsReturnConfirmationMail `json:"confirmationMail,omitempty"`
}

// glsCreateReturnOrder is the POST body for GLS Shop Returns Customer Plus API v3.
type glsCreateReturnOrder struct {
	OriginalOrderReference string            `json:"originalOrderReference"`
	ReturnReason           string            `json:"returnReason"`
	Sender                 glsReturnSender   `json:"sender"`
	Receiver               glsReturnReceiver `json:"receiver"`
	LabelFormat            string            `json:"labelFormat,omitempty"`
	Options                *glsReturnOptions `json:"options,omitempty"`
}

// bookReturnShipment calls the GLS Shop Returns Customer Plus API v3
// (POST /{app-id}/return-orders/label). The sender/receiver roles are inverted
// compared to outbound: the customer (our Receiver) returns to the merchant (our Sender).
func (a *GLSAdapter) bookReturnShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if a.ReturnAppID == "" {
		return nil, fmt.Errorf("gls return: ReturnAppID must be configured")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("gls return: obtain bearer token: %w", err)
	}

	orderRef := ""
	if len(request.Shipment.Colli) > 0 {
		orderRef = request.Shipment.Colli[0].ID
	}

	_, labelFmt := glsLabelFormat(LabelFormatPDF)

	body := glsCreateReturnOrder{
		OriginalOrderReference: orderRef,
		ReturnReason:           "OTHER",
		Sender: glsReturnSender{
			PersonName: request.Shipment.Receiver.Name,
			Email:      request.Shipment.Receiver.Email,
			Address: glsReturnAddress{
				Street:      request.Shipment.Receiver.Street,
				ZipCode:     request.Shipment.Receiver.PostalCode,
				City:        request.Shipment.Receiver.City,
				CountryCode: request.Shipment.Receiver.Country,
			},
		},
		Receiver: glsReturnReceiver{
			CompanyName: request.Shipment.Sender.Name,
			Address: glsReturnAddress{
				Street:      request.Shipment.Sender.Street,
				ZipCode:     request.Shipment.Sender.PostalCode,
				City:        request.Shipment.Sender.City,
				CountryCode: request.Shipment.Sender.Country,
			},
		},
		LabelFormat: strings.ToLower(labelFmt), // API expects lowercase: "pdf", "zpl"
	}

	// Map AddOnEmailNotification → options.confirmationMail.sendTo.
	if hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) {
		email := request.Shipment.Receiver.Email
		if email != "" {
			body.Options = &glsReturnOptions{
				ConfirmationMail: &glsReturnConfirmationMail{SendTo: email},
			}
		}
	}

	payloadBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gls return: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s/return-orders/label", a.ReturnBaseURL, a.ReturnAppID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("gls return: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gls return: http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body) //nolint:errcheck
		return nil, fmt.Errorf("gls return: carrier returned %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		ReturnOrderID string `json:"returnOrderId"`
		References    struct {
			TrackID  string `json:"trackId"`
			ParcelID string `json:"parcelId"`
		} `json:"references"`
		Label struct {
			ContentType string `json:"contentType"`
			Content     string `json:"content"` // base64
		} `json:"label"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("gls return: decode response: %w", err)
	}

	a.log.Info("gls return shipment booked",
		zap.String("returnOrderID", glsResp.ReturnOrderID),
		zap.String("trackID", glsResp.References.TrackID),
		zap.Int("labelBytes", len(glsResp.Label.Content)),
	)

	colli := make([]ColliResponse, len(request.Shipment.Colli))
	for i := range request.Shipment.Colli {
		colli[i] = ColliResponse{
			ID:             request.Shipment.Colli[i].ID,
			TrackingNumber: glsResp.References.TrackID,
			Status:         "booked",
		}
	}

	return &BookingResponse{
		TrackingNumber: glsResp.References.TrackID,
		ShipmentID:     glsResp.ReturnOrderID,
		Carrier:        "gls",
		Status:         "booked",
		Colli:          colli,
	}, nil
}

// CancelShipment cancels a GLS parcel via POST /rs/shipments/cancel/{trackID}.
func (a *GLSAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain GLS access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/rs/shipments/cancel/%s", a.BaseURL, trackingNumber), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS cancel request: %w", err)
	}
	req.Header.Set("Accept", "application/glsVersion1+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GLS cancel API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("GLS cancel API returned status %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		TrackID string `json:"TrackID"`
		Result  string `json:"Result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("failed to decode GLS cancel response: %w", err)
	}

	return &CancelResponse{
		TrackingNumber: glsResp.TrackID,
		Carrier:        "gls",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported for GLS.
// No update/modify/amend endpoint exists anywhere in the ShipIT API v1 spec
// (APIdocs/GLS_Shipping_API_v0.8.pdf) — confirmed carrier limitation, not an
// unresearched gap.
func (a *GLSAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("GLS", "post-booking update",
		"no update/modify/amend endpoint exists in the ShipIT API")
}

// FetchLabel retrieves a GLS shipping label via POST /rs/shipments/reprintparcel.
//
// Wire format notes:
//   - Endpoint: POST /rs/shipments/reprintparcel (ReprintParcelRequestParameter body).
//   - Label data is returned in CreatedShipment.PrintData[0].Data[0] as base64.
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

	templateSet, labelFormat := glsLabelFormat(req.Format)
	body, err := json.Marshal(map[string]any{
		"TrackID": req.TrackingNumber,
		"PrintingOptions": map[string]any{
			"ReturnLabels": map[string]any{
				"TemplateSet": templateSet,
				"LabelFormat": labelFormat,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GLS reprint request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/rs/shipments/reprintparcel", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create GLS reprint request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/glsVersion1+json")
	httpReq.Header.Set("Accept", "application/glsVersion1+json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("GLS reprint API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("GLS reprint API returned status %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		CreatedShipment struct {
			PrintData []struct {
				Data        []string `json:"Data"`
				LabelFormat string   `json:"LabelFormat"`
			} `json:"PrintData"`
		} `json:"CreatedShipment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("failed to decode GLS reprint response: %w", err)
	}
	if len(glsResp.CreatedShipment.PrintData) == 0 ||
		len(glsResp.CreatedShipment.PrintData[0].Data) == 0 {
		return nil, fmt.Errorf("GLS reprint response contained no label data")
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "gls",
		Format:         req.Format,
		Data:           glsResp.CreatedShipment.PrintData[0].Data[0],
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
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

	// DetailsReferenceData only accepts TrackID (and optional reference fields);
	// DateFrom/DateTo belong to TULReferenceData on /rs/tracking/parcels.
	body, err := json.Marshal(map[string]string{
		"TrackID": trackingNumber,
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
				Description  string `json:"Description"`
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

// BookPickup schedules a sporadic (ad-hoc) collection via POST /rs/sporadiccollection.
//
// Wire format notes:
//   - Endpoint: POST /rs/sporadiccollection (SporadicCollection body).
//   - ContactID reuses the shipper contact configured for BookShipment.
//   - PreferredPickUpDate combines Pickup.Date and Pickup.ReadyTime (default
//     09:00 when unset) into an RFC3339 timestamp.
//   - Product is always "PARCEL", matching the hardcoded product on BookShipment.
//   - The response carries only EstimatedPickUpDate — GLS does not return a
//     booking reference, so ConfirmationNumber falls back to the requested date.
func (a *GLSAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("gls: pickup date must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("gls: obtain access token: %w", err)
	}

	readyTime := req.Pickup.ReadyTime
	if readyTime == "" {
		readyTime = "09:00"
	}
	preferredPickUpDate := fmt.Sprintf("%sT%s:00Z", req.Pickup.Date, readyTime)

	numParcels := req.EstimatedParcels
	if numParcels == 0 && len(req.TrackingNumbers) > 0 {
		numParcels = len(req.TrackingNumbers)
	}

	body := map[string]any{
		"ContactID":           a.ContactID,
		"PreferredPickUpDate": preferredPickUpDate,
		"Product":             "PARCEL",
	}
	if numParcels > 0 {
		body["NumberOfParcels"] = numParcels
	}
	if req.EstimatedWeight > 0 {
		body["ExpectedTotalWeight"] = req.EstimatedWeight
	}
	if req.Pickup.SpecialInstructions != "" {
		body["AdditionalInformation"] = req.Pickup.SpecialInstructions
	}

	payloadBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gls: marshal sporadic collection request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/rs/sporadiccollection", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("gls: create sporadic collection request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/glsVersion1+json")
	httpReq.Header.Set("Accept", "application/glsVersion1+json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gls: sporadic collection API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("gls: sporadic collection API returned status %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		EstimatedPickUpDate string `json:"EstimatedPickUpDate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("gls: decode sporadic collection response: %w", err)
	}

	a.log.Info("gls pickup booked",
		zap.String("preferredPickUpDate", preferredPickUpDate),
		zap.String("estimatedPickUpDate", glsResp.EstimatedPickUpDate),
	)

	return &PickupResponse{
		Carrier:            "gls",
		ConfirmationNumber: req.Pickup.Date, // GLS does not return a booking reference
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by the GLS ShipIT API.
// No update endpoint exists for a sporadic collection (confirmed carrier
// limitation, not an implementation gap) — reschedule by contacting GLS directly.
func (a *GLSAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("GLS", "pickup update", "no update endpoint exists for /rs/sporadiccollection")
}

// CancelPickup is not supported by the GLS ShipIT API.
// No cancellation endpoint exists for a sporadic collection (confirmed carrier
// limitation, not an implementation gap).
func (a *GLSAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("GLS", "pickup cancellation", "no cancellation endpoint exists for /rs/sporadiccollection")
}

// CloseManifest closes the shipping day via POST /rs/shipments/endofday.
//
// This is the operationally required end-of-day report: GLS drivers expect
// the account's shipments for the day to be closed out before collection. The
// call is account-wide — GLS returns every shipment booked for the given date
// rather than a filtered subset, so req.TrackingNumbers is not sent.
func (a *GLSAdapter) CloseManifest(ctx context.Context, req ManifestRequest) (*ManifestResponse, error) {
	if req.Date == "" {
		return nil, fmt.Errorf("gls: CloseManifest requires a date")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("gls: obtain access token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/rs/shipments/endofday?date=%s", a.BaseURL, url.QueryEscape(req.Date))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gls: create end-of-day request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/glsVersion1+json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gls: end-of-day API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("gls: end-of-day API returned status %d: %s", resp.StatusCode, string(b))
	}

	var glsResp struct {
		Shipments []json.RawMessage `json:"Shipments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&glsResp); err != nil {
		return nil, fmt.Errorf("gls: decode end-of-day response: %w", err)
	}

	a.log.Info("gls manifest closed",
		zap.String("date", req.Date),
		zap.Int("parcelsConfirmed", len(glsResp.Shipments)),
	)

	return &ManifestResponse{
		Carrier:          "gls",
		Date:             req.Date,
		Status:           "closed",
		ParcelsConfirmed: len(glsResp.Shipments),
		Warnings:         []string{},
	}, nil
}

// GetPickupAvailability is not supported by the GLS ShipIT API.
// No pre-flight availability endpoint exists (confirmed carrier limitation,
// not an implementation gap) — callers may proceed directly to BookPickup.
func (a *GLSAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("GLS", "pickup availability",
		"no availability endpoint exists in the ShipIT API — proceed directly to BookPickup")
}
