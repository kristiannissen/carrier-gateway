// Package adapter provides the Evri (formerly Hermes UK) implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/evri.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
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

const (
	evriBaseURL = "https://api.evri.com"
	evriAuthURL = "https://api.evri.com/oauth/token" //nolint:gosec // URL, not a credential
)

// evriTokenCache caches an OAuth2 access token with its expiry time.
type evriTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not within 30 s of expiry.
func (c *evriTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// EvriAdapter implements CarrierAdapter for Evri (UK) using the Evri Classic API.
//
// Implemented: BookShipment, FetchLabel.
// Not implemented: TrackShipment, CancelShipment, UpdateShipment — the Evri
// Classic API exposes no endpoint for these operations.
//
// This carrier is UK-only: delivery addresses must be valid UK postcodes.
// All monetary values are in GBP.
type EvriAdapter struct {
	// ClientID is the Evri OAuth2 client ID.
	ClientID string
	// ClientSecret is the Evri OAuth2 client secret.
	ClientSecret string
	// BaseURL is the root of the Evri Classic API (override in tests).
	BaseURL string
	// AuthURL is the OAuth2 token endpoint (override in tests).
	AuthURL string
	HTTPClient *http.Client
	tokenCache evriTokenCache
	log        *zap.Logger
}

// NewEvriAdapter creates a production-ready EvriAdapter.
func NewEvriAdapter(clientID, clientSecret string, log *zap.Logger) *EvriAdapter {
	return &EvriAdapter{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		BaseURL:      evriBaseURL,
		AuthURL:      evriAuthURL,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		log:          log,
	}
}

// fetchToken obtains a new access token via the OAuth2 client credentials flow.
func (a *EvriAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.AuthURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("evri: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("evri: token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck
		return fmt.Errorf("evri: token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("evri: decode token response: %w", err)
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tok.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, refreshing if needed.
func (a *EvriAdapter) bearerToken(ctx context.Context) (string, error) {
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

// evriParcelType maps the gateway DeliveryType / colli characteristics to the
// Evri ParcelType enum. Defaults to STANDARD.
func evriParcelType(shipment Shipment) string {
	if strings.EqualFold(shipment.DeliveryType, "postable") {
		return "POSTABLE"
	}
	return "STANDARD"
}

// evriLabelAccept maps a gateway LabelFormat to the Accept header Evri expects.
// Evri supports PDF and ZPL via Accept header negotiation; PNG/EPL/ZPLGK are not
// offered by the Evri Classic API.
func evriLabelAccept(f LabelFormat) (string, error) {
	switch f {
	case LabelFormatPDF, "": // default
		return "application/pdf", nil
	case LabelFormatZPL, LabelFormatZPLGK:
		return "application/zpl", nil
	default:
		return "", unsupportedFormat("Evri", f, LabelFormatPDF, LabelFormatZPL)
	}
}

// evriLabelFormatParam maps a gateway LabelFormat to the Evri ?format= query
// parameter. Evri defines DEFAULT, THERMAL, and TWO_PER_PAGE.
func evriLabelFormatParam(f LabelFormat) string {
	switch f {
	case LabelFormatZPL, LabelFormatZPLGK:
		return "THERMAL"
	default:
		return "DEFAULT"
	}
}

// BookShipment books one or more parcels with Evri via POST /api/parcels.
//
// Each colli in the request becomes one parcel in the Evri batch. Evri uses
// clientUID for idempotency: when IdempotencyKey is set it is used as the
// prefix and the colli ID is appended; otherwise the colli ID alone is used.
//
// The first returned barcode is used as TrackingNumber on BookingResponse.
// All barcodes are returned in Colli[].TrackingNumber.
//
// UK-only: receiver address must contain a valid UK postcode. No country field
// is sent — Evri routes domestically within the UK only.
func (a *EvriAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("evri: shipment must contain at least one colli")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("evri: obtain bearer token: %w", err)
	}

	recv := request.Shipment.Receiver
	firstName, lastName := splitName(recv.Name)

	// Build address: Evri uses line1–line4 + postcode, no country field.
	line1 := strings.TrimSpace(recv.Street + " " + recv.HouseNumber)
	line2 := recv.Supplement
	line3 := recv.City
	line4 := ""

	deliveryAddr := map[string]any{
		"line1":    line1,
		"postcode": recv.PostalCode,
	}
	if line2 != "" {
		deliveryAddr["line2"] = line2
	}
	if line3 != "" {
		deliveryAddr["line3"] = line3
	}
	if line4 != "" {
		deliveryAddr["line4"] = line4
	}

	signatureRequired := hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired)
	var deliveryInstructions, safePlace string
	if fa, ok := getAddOn(request.Shipment.AddOns, AddOnFlexDelivery); ok {
		safePlace = fa.Instructions
	}
	if request.Shipment.ShipmentComment != "" {
		deliveryInstructions = request.Shipment.ShipmentComment
	}

	nextDay := strings.EqualFold(request.Shipment.ServiceTier, "next_day")

	parcels := make([]map[string]any, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		uid := c.ID
		if request.IdempotencyKey != "" {
			uid = request.IdempotencyKey + "_" + c.ID
		}

		ref := c.Reference
		if len(ref) > 20 {
			ref = ref[:20]
		}

		parcelDetails := map[string]any{
			"weightKg":    c.Weight,
			"type":        evriParcelType(request.Shipment),
		}
		if ref != "" {
			parcelDetails["deliveryReference"] = ref
		}
		if len(c.Items) > 0 {
			parcelDetails["itemDescription"] = c.Items[0].Description
		}
		if request.Shipment.Customs.CustomsValue > 0 {
			parcelDetails["estimatedParcelValuePounds"] = request.Shipment.Customs.CustomsValue
		}

		delivery := map[string]any{
			"deliveryAddress":   deliveryAddr,
			"firstName":        firstName,
			"lastName":         lastName,
			"signatureRequired": signatureRequired,
		}
		if recv.Email != "" {
			delivery["email"] = recv.Email
		}
		if recv.Phone != "" {
			delivery["telephone"] = recv.Phone
		}
		if deliveryInstructions != "" {
			delivery["deliveryInstructions"] = deliveryInstructions
		}
		if safePlace != "" {
			delivery["deliverySafePlace"] = safePlace
		}
		if nextDay {
			delivery["nextDay"] = true
		}

		parcels[i] = map[string]any{
			"clientUID":       uid,
			"parcelDetails":   parcelDetails,
			"deliveryDetails": delivery,
		}
	}

	payload := map[string]any{"parcels": parcels}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("evri: marshal booking request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/api/parcels", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("evri: create booking request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("evri: booking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("evri: read booking response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("evri: booking API returned %d: %s", resp.StatusCode, string(body))
	}

	var evriResp struct {
		ParcelSummaries []struct {
			ClientUID string `json:"clientUID"`
			Barcode   string `json:"barcode"`
			Status    string `json:"status"` // CREATED | INVALID
			Errors    []struct {
				Error            string   `json:"error"`
				ErrorDescription string   `json:"error_description"`
				ErrorPaths       []string `json:"error_paths"`
			} `json:"errors"`
		} `json:"parcelSummaries"`
	}
	if err := json.Unmarshal(body, &evriResp); err != nil {
		return nil, fmt.Errorf("evri: decode booking response: %w", err)
	}

	if len(evriResp.ParcelSummaries) == 0 {
		return nil, fmt.Errorf("evri: booking response contained no parcel summaries: %s", string(body))
	}

	// Collect errors from INVALID parcels.
	var bookingErrors []string
	colliResponses := make([]ColliResponse, 0, len(evriResp.ParcelSummaries))
	primaryBarcode := ""

	for i, ps := range evriResp.ParcelSummaries {
		switch ps.Status {
		case "INVALID":
			for _, e := range ps.Errors {
				bookingErrors = append(bookingErrors,
					fmt.Sprintf("parcel %s: %s — %s", ps.ClientUID, e.Error, e.ErrorDescription))
			}
		case "CREATED":
			if primaryBarcode == "" {
				primaryBarcode = ps.Barcode
			}
			colliID := ""
			if i < len(request.Shipment.Colli) {
				colliID = request.Shipment.Colli[i].ID
			}
			colliResponses = append(colliResponses, ColliResponse{
				ID:             colliID,
				TrackingNumber: ps.Barcode,
				Status:         "booked",
			})
		}
	}

	if primaryBarcode == "" {
		return nil, fmt.Errorf("evri: no parcels were successfully created: %v", bookingErrors)
	}

	a.log.Info("evri shipment booked",
		zap.String("trackingNumber", primaryBarcode),
		zap.Int("parcelCount", len(colliResponses)),
	)

	result := &BookingResponse{
		TrackingNumber: primaryBarcode,
		Carrier:        "evri",
		Status:         "booked",
		Colli:          colliResponses,
	}
	if len(bookingErrors) > 0 {
		result.Errors = bookingErrors
	}
	return result, nil
}

// TrackShipment is not supported by the Evri Classic API.
// The API exposes no tracking or status endpoint.
func (a *EvriAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("Evri", "shipment tracking",
		"the Evri Classic API does not expose a tracking endpoint; use the Evri consumer tracking website")
}

// FetchLabel retrieves the label for an existing Evri parcel via
// GET /api/labels/{barcode}.
//
// Supported formats: PDF (default), ZPL/ZPLGK (mapped to THERMAL).
// PNG, EPL are not offered by the Evri Classic API.
//
// The returned label bytes are base64-encoded in LabelResponse.Data.
func (a *EvriAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("evri: tracking number must not be empty")
	}

	acceptHeader, err := evriLabelAccept(req.Format)
	if err != nil {
		return nil, err
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("evri: obtain bearer token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/api/labels/%s?format=%s",
		a.BaseURL, url.PathEscape(req.TrackingNumber), evriLabelFormatParam(req.Format))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("evri: create label request: %w", err)
	}
	httpReq.Header.Set("Accept", acceptHeader)
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("evri: label API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusNotFound:
		return nil, fmt.Errorf("evri: barcode %s not found", req.TrackingNumber)
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("evri: label request unauthorized — check credentials")
	default:
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck
		return nil, fmt.Errorf("evri: label API returned %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("evri: read label response: %w", err)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "evri",
		Format:         req.Format,
		Data:           base64.StdEncoding.EncodeToString(data),
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment is not supported by the Evri Classic API.
func (a *EvriAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("Evri", "shipment cancellation",
		"the Evri Classic API does not expose a cancellation endpoint")
}

// UpdateShipment is not supported by the Evri Classic API.
func (a *EvriAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Evri", "post-booking update",
		"the Evri Classic API does not expose an update endpoint")
}
