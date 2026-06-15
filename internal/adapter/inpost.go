// Package adapter provides the InPost implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/inpost.go.
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

// inpostTokenCache holds a cached OAuth 2.1 access token with its expiry time.
// Access is guarded by mu to make the adapter safe for concurrent use.
type inpostTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not expired.
// A 30-second buffer prevents using a token that expires mid-request.
func (c *inpostTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// InPostAdapter implements CarrierAdapter for InPost using the InPost Group API 2025.
// Authentication uses the OAuth 2.1 Client Credentials flow; tokens are cached and
// refreshed automatically. Cancellation and post-booking updates are not supported
// by the InPost API — both methods return ErrNotSupported.
type InPostAdapter struct {
	// ClientID is the OAuth 2.1 client ID from the InPost Merchant Portal.
	ClientID string
	// ClientSecret is the OAuth 2.1 client secret.
	ClientSecret string
	// OrgID is the organization identifier required on all API path parameters.
	OrgID string
	// BaseURL is the InPost Group API base URL.
	BaseURL string
	// AuthURL is the OAuth 2.1 token endpoint.
	AuthURL    string
	HTTPClient *http.Client
	tokenCache inpostTokenCache
	log        *zap.Logger
}

// NewInPostAdapter creates a new InPostAdapter with the given OAuth 2.1 credentials.
// clientID, clientSecret, and orgID are obtained from the InPost Merchant Portal
// or by contacting the InPost Integration Team.
func NewInPostAdapter(clientID, clientSecret, orgID string, log *zap.Logger) *InPostAdapter {
	return &InPostAdapter{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		OrgID:        orgID,
		BaseURL:      "https://api.inpost-group.com",
		AuthURL:      "https://api.inpost-group.com/oauth2/token",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// fetchToken obtains a new OAuth 2.1 access token using the Client Credentials flow
// and stores it in the token cache.
func (a *InPostAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)
	// Request all scopes needed by the adapter at token creation time.
	form.Set("scope", strings.Join([]string{
		"api:shipments:read",
		"api:shipments:write",
		"api:returns:read",
		"api:returns:write",
		"api:one-time-pickups:read",
		"api:one-time-pickups:write",
		"api:tracking:read",
		"api:points:read",
	}, " "))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.AuthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("inpost: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("inpost: token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return fmt.Errorf("inpost: token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("inpost: decode token response: %w", err)
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, fetching a new one when the cache
// is empty or within 30 seconds of expiry.
func (a *InPostAdapter) bearerToken(ctx context.Context) (string, error) {
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

// inpostParty builds a sender/recipient object for the InPost Shipping API v2.
// The unified Address.Name is placed in companyName. firstName/lastName are derived
// by splitting on the first space — the InPost API requires both fields.
func inpostParty(a Address) map[string]any {
	first, last := inpostSplitName(a.Name)
	return map[string]any{
		"companyName": a.Name,
		"firstName":   first,
		"lastName":    last,
		"email":       a.Email,
		"phone":       a.Phone,
	}
}

// inpostSplitName splits "First Last" on the first space.
// When no space is found the full value is firstName and lastName is "-".
func inpostSplitName(name string) (first, last string) {
	if idx := strings.Index(name, " "); idx >= 0 {
		return name[:idx], name[idx+1:]
	}
	return name, "-"
}

// inpostParcel converts a single Colli to the InPost Shipping API v2 parcel format.
// Weight is converted from kg to grams and encoded as a string per the API spec.
// Dimensions remain in centimetres, also encoded as strings.
func inpostParcel(c Colli) map[string]any {
	weightG := int(math.Round(c.Weight * 1000))
	p := map[string]any{
		"type": "STANDARD",
		"dimensions": map[string]any{
			"length": fmt.Sprintf("%.0f", c.Dimensions.Length),
			"width":  fmt.Sprintf("%.0f", c.Dimensions.Width),
			"height": fmt.Sprintf("%.0f", c.Dimensions.Height),
			"unit":   "CM",
		},
		"weight": map[string]any{
			"amount": fmt.Sprintf("%d", weightG),
			"unit":   "G",
		},
	}
	if c.ID != "" {
		p["references"] = map[string]any{
			"custom": map[string]any{"externalId": c.ID},
		}
	}
	return p
}

// inpostOrigin builds the origin object for the InPost Shipping API v2.
// All gateway shipments use APM (locker drop-off) as the shipping method.
// GB shipments must include a subdivisionCode (e.g. "GB-ENG", "GB-NIR").
func inpostOrigin(sender Address) map[string]any {
	origin := map[string]any{
		"countryCode":    sender.Country,
		"shippingMethod": "APM",
	}
	if sender.State != "" {
		origin["subdivisionCode"] = sender.State
	}
	return origin
}

// inpostDestination builds the destination object for the InPost Shipping API v2.
// When ServicePointID is set the destination is an APM locker (pointId).
// Otherwise a street address object is used for home delivery.
func inpostDestination(receiver Address) map[string]any {
	if receiver.ServicePointID != "" {
		return map[string]any{
			"countryCode": receiver.Country,
			"pointId":     receiver.ServicePointID,
		}
	}
	dest := map[string]any{
		"countryCode": receiver.Country,
		"city":        receiver.City,
		"postalCode":  receiver.PostalCode,
		"street":      receiver.Street,
	}
	if receiver.HouseNumber != "" {
		dest["houseNumber"] = receiver.HouseNumber
	}
	return dest
}

// inpostCustomsClearance maps the gateway Customs struct to the shipment-level
// InPost customsClearance object. Returns nil when no customs data is present.
// Required for GB→IE (Type 3), GB→GG/JE (Type 2), and GB-GBN→GB-NIR (Type 1) routes.
func inpostCustomsClearance(c Customs) map[string]any {
	if c.Incoterms == "" && c.ImporterOfRecord == "" && c.NatureOfCargo == "" {
		return nil
	}
	cc := map[string]any{}
	if c.Incoterms != "" {
		cc["incoterm"] = c.Incoterms
	}
	if c.ImporterOfRecord != "" {
		cc["eoriNumber"] = c.ImporterOfRecord
	}
	if c.NatureOfCargo != "" {
		cc["exportReason"] = c.NatureOfCargo
	}
	if c.InvoiceNumber != "" {
		inv := map[string]any{"number": c.InvoiceNumber}
		if c.InvoiceDate != "" {
			inv["issueDate"] = c.InvoiceDate
		}
		cc["invoice"] = inv
	}
	if c.CustomsValue > 0 && c.CustomsCurrency != "" {
		cc["shippingCost"] = map[string]any{
			"amount":   c.CustomsValue,
			"currency": c.CustomsCurrency,
		}
	}
	return cc
}

// inpostParcelCustomsClearance maps Customs.Items to the per-parcel customs object.
// Returns nil when no line items are present. Contents array is capped at 10 per
// the InPost API constraint.
func inpostParcelCustomsClearance(c Customs) map[string]any {
	if len(c.Items) == 0 {
		return nil
	}
	items := c.Items
	if len(items) > 10 {
		items = items[:10]
	}
	contents := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"description": item.Description,
			"quantity":    item.Quantity,
			"unitValue": map[string]any{
				"amount":   item.Value,
				"currency": item.Currency,
			},
		}
		if item.HSCode != "" {
			entry["hsCode"] = item.HSCode
		}
		if item.CountryOfOrigin != "" {
			entry["productOriginCountryCode"] = item.CountryOfOrigin
		}
		if item.NetWeight > 0 {
			entry["unitWeight"] = map[string]any{
				"amount": item.NetWeight,
				"unit":   "KG",
			}
		}
		contents = append(contents, entry)
	}
	pcc := map[string]any{"contents": contents}
	if c.CustomsValue > 0 && c.CustomsCurrency != "" {
		pcc["value"] = c.CustomsValue
		pcc["currency"] = c.CustomsCurrency
	}
	return pcc
}

// inpostLabelAccept maps a LabelFormat to the InPost Accept header value.
// ZPL defaults to 203 dpi; ZPL 300 dpi is requested via LabelFormatZPLGK.
// EPL is 203 dpi and available for Poland domestic shipments only.
// PDF defaults to A6 format; the caller may override with the size parameter.
func inpostLabelAccept(f LabelFormat, size string) string {
	if size == "" {
		size = "A6"
	}
	switch f {
	case LabelFormatZPL:
		return "text/zpl;dpi=203"
	case LabelFormatZPLGK:
		return "text/zpl;dpi=300"
	case LabelFormatEPL:
		return "text/epl2;dpi=203"
	default:
		return "application/pdf;format=" + size
	}
}

// BookShipment books a shipment with InPost using the Shipping API v2.
//
// The unified BookingRequest is mapped to the InPost wire format:
//   - OAuth 2.1 token is fetched or served from cache automatically.
//   - X-Deduplication-Id is forwarded from IdempotencyKey when present.
//   - Destination is an APM locker (pointId) when ServicePointID is set,
//     otherwise a street address for home delivery.
//   - Weight is converted from kg to grams; dimensions stay in centimetres.
//   - Customs clearance is wired when Customs data is present.
//   - Drop-off code (label-less) is enabled when ReturnFunctionality == "labelless".
func (a *InPostAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	parcelCC := inpostParcelCustomsClearance(request.Shipment.Customs)
	parcels := make([]map[string]any, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		p := inpostParcel(c)
		if parcelCC != nil {
			p["customsClearance"] = parcelCC
		}
		parcels[i] = p
	}

	payload := map[string]any{
		"sender":      inpostParty(request.Shipment.Sender),
		"recipient":   inpostParty(request.Shipment.Receiver),
		"origin":      inpostOrigin(request.Shipment.Sender),
		"destination": inpostDestination(request.Shipment.Receiver),
		"parcels":     parcels,
	}

	if request.IdempotencyKey != "" {
		payload["references"] = map[string]any{
			"custom": map[string]any{"invoiceNumber": request.IdempotencyKey},
		}
	}
	if cc := inpostCustomsClearance(request.Shipment.Customs); cc != nil {
		payload["customsClearance"] = cc
	}
	if strings.EqualFold(request.Shipment.ReturnFunctionality, "labelless") {
		payload["enableDropOffCode"] = true
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("inpost: marshal booking request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/shipping/v2/organizations/%s/shipments", a.BaseURL, a.OrgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("inpost: create booking request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if request.IdempotencyKey != "" {
		req.Header.Set("X-Deduplication-Id", request.IdempotencyKey)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inpost: booking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read booking response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, inpostAPIError("booking", resp.StatusCode, body)
	}

	var inpostResp struct {
		TrackingNumber string `json:"trackingNumber"`
	}
	if err := json.Unmarshal(body, &inpostResp); err != nil {
		return nil, fmt.Errorf("inpost: decode booking response: %w", err)
	}

	return &BookingResponse{
		TrackingNumber: inpostResp.TrackingNumber,
		Carrier:        "inpost",
		Status:         "booked",
	}, nil
}

// CancelShipment is not supported for InPost.
func (a *InPostAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("InPost", "cancellation", "")
}

// UpdateShipment is not supported for InPost.
func (a *InPostAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("InPost", "post-booking update", "")
}

// FetchLabel retrieves a shipping label from InPost Shipping API v2.
// Supported formats: PDF (A6 default, A4 via LabelFormatPDF with size hint),
// ZPL 203 dpi (LabelFormatZPL), ZPL 300 dpi (LabelFormatZPLGK),
// EPL2 203 dpi (LabelFormatEPL, Poland domestic only).
func (a *InPostAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	accept := inpostLabelAccept(req.Format, "A6")
	endpoint := fmt.Sprintf("%s/shipping/v2/organizations/%s/shipments/%s/label",
		a.BaseURL, a.OrgID, req.TrackingNumber)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("inpost: create label request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", accept)

	return fetchLabelFromURL(ctx, a.HTTPClient, httpReq, req, "inpost")
}

// TrackShipment retrieves tracking events for an InPost shipment using the
// Tracking API v1. V1 event schema is requested explicitly and is supported
// indefinitely per the InPost API versioning policy.
func (a *InPostAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	u, err := url.Parse(a.BaseURL + "/tracking/v1/parcels")
	if err != nil {
		return nil, fmt.Errorf("inpost: parse tracking URL: %w", err)
	}
	q := u.Query()
	q.Set("trackingNumbers", trackingNumber)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("inpost: create tracking request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-inpost-event-version", "V1")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inpost: tracking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read tracking response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, inpostAPIError("tracking", resp.StatusCode, body)
	}

	var trackResp struct {
		Parcels []struct {
			TrackingNumber string  `json:"trackingNumber"`
			Status         *string `json:"status"`
			Events         []struct {
				EventTimestamp string  `json:"eventTimestamp"`
				EventCode      string  `json:"eventCode"`
				Status         *string `json:"status"`
				Location       struct {
					Address string `json:"address"`
					City    string `json:"city"`
					Country string `json:"country"`
					Name    string `json:"name"`
				} `json:"location"`
			} `json:"events"`
		} `json:"parcels"`
	}
	if err := json.Unmarshal(body, &trackResp); err != nil {
		return nil, fmt.Errorf("inpost: decode tracking response: %w", err)
	}
	if len(trackResp.Parcels) == 0 {
		return nil, fmt.Errorf("inpost: tracking number %q not found", trackingNumber)
	}

	p := trackResp.Parcels[0]

	events := make([]TrackingEvent, len(p.Events))
	for i, e := range p.Events {
		loc := e.Location.City
		if loc != "" && e.Location.Country != "" {
			loc = loc + ", " + e.Location.Country
		}
		events[i] = TrackingEvent{
			Timestamp:        e.EventTimestamp,
			Status:           e.EventCode,
			NormalizedStatus: normalizeStatus("inpost", e.EventCode),
			Location:         loc,
		}
	}

	rawStatus := ""
	if p.Status != nil {
		rawStatus = *p.Status
	}
	if len(p.Events) > 0 {
		rawStatus = p.Events[0].EventCode
	}

	return &TrackingResponse{
		TrackingNumber:   p.TrackingNumber,
		Carrier:          "inpost",
		Status:           rawStatus,
		NormalizedStatus: normalizeStatus("inpost", rawStatus),
		OriginalStatus:   rawStatus,
		Events:           events,
	}, nil
}

// inpostAPIError constructs a descriptive error from a non-success InPost API response.
// 406 means an unsupported Accept header was sent (label format).
// 422 indicates a field-level validation failure — the body contains details.
// 429 means the rate limit was exceeded.
func inpostAPIError(operation string, statusCode int, body []byte) error {
	switch statusCode {
	case http.StatusNotAcceptable:
		return fmt.Errorf("inpost: %s: unsupported label format (406) — check Accept header", operation)
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("inpost: %s: validation failed (422): %s", operation, string(body))
	case http.StatusTooManyRequests:
		return fmt.Errorf("inpost: %s: rate limit exceeded (429)", operation)
	default:
		return fmt.Errorf("inpost: %s: API returned status %d: %s", operation, statusCode, string(body))
	}
}
