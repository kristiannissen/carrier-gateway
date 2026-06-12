// Package adapter provides the FedEx implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/fedex.go.
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

// fedexTokenCache holds a cached OAuth2 bearer token with its expiry time.
type fedexTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not yet expired.
// A 30-second buffer is applied to avoid using a token that expires mid-request.
func (c *fedexTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// FedExAdapter implements CarrierAdapter for FedEx.
//
// Authentication:
//   - OAuth2 Bearer token via POST /oauth/token with form-encoded body.
//   - grant_type=client_credentials for standard integrators.
//   - For CSP/integrator child accounts use grant_type=csp_credentials with
//     ChildKey and ChildSecret in addition to ClientID and ClientSecret.
//   - Token lifetime is 3600 seconds; the adapter refreshes automatically.
//
// Default service type selection:
//   - Same sender/receiver country: FEDEX_GROUND
//   - Cross-border: FEDEX_INTERNATIONAL_PRIORITY
//
// Pending implementation:
//   - FetchLabel: inline in Ship API response; reprint endpoint TBD
type FedExAdapter struct {
	// ClientID is the API Key from the FedEx Developer Portal project.
	ClientID string
	// ClientSecret is the Secret Key from the FedEx Developer Portal project.
	ClientSecret string
	// AccountNumber is the FedEx account number — required by the Ship API.
	AccountNumber string
	// GrantType controls which OAuth2 flow is used.
	// "client_credentials" for standard B2B.
	// "csp_credentials" for Integrator/Compatible customers with child accounts.
	// "client_pc_credentials" for Proprietary Parent Child customers.
	GrantType string
	// ChildKey is the Customer Key for csp_credentials and client_pc_credentials flows.
	ChildKey string
	// ChildSecret is the Customer Password for csp_credentials and client_pc_credentials flows.
	ChildSecret string
	// BaseURL is the FedEx API base URL.
	// Production: https://apis.fedex.com
	// Sandbox:    https://apis-sandbox.fedex.com
	BaseURL    string
	HTTPClient *http.Client
	tokenCache fedexTokenCache
	log        *zap.Logger
}

// NewFedExAdapter creates a new FedExAdapter for standard B2B integrators.
// clientID and clientSecret are the API Key and Secret Key from the FedEx Developer Portal.
// accountNumber is the FedEx account number used in shipping requests.
func NewFedExAdapter(clientID, clientSecret, accountNumber string, log *zap.Logger) *FedExAdapter {
	return &FedExAdapter{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		AccountNumber: accountNumber,
		GrantType:     "client_credentials",
		BaseURL:       "https://apis.fedex.com",
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		log:           log,
	}
}

// fetchToken obtains a new OAuth2 bearer token via POST /oauth/token.
// The request body is application/x-www-form-urlencoded.
func (a *FedExAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", a.GrantType)
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)
	if a.ChildKey != "" {
		form.Set("child_Key", a.ChildKey)
	}
	if a.ChildSecret != "" {
		form.Set("child_secret", a.ChildSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/oauth/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create FedEx token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("FedEx token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read FedEx token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("FedEx token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to decode FedEx token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("FedEx token response contained no access_token")
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, fetching a new one if expired or absent.
func (a *FedExAdapter) bearerToken(ctx context.Context) (string, error) {
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

// newFedExRequest builds an authenticated JSON request ready for the FedEx APIs.
func (a *FedExAdapter) newFedExRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain FedEx bearer token: %w", err)
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create FedEx request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-locale", "en_US")
	return req, nil
}

// ── Wire types ────────────────────────────────────────────────────────────────

type fedexAccountNumber struct {
	Value string `json:"value"`
}

// fedexShipRequest is the top-level body for POST /ship/v1/shipments.
type fedexShipRequest struct {
	AccountNumber        fedexAccountNumber     `json:"accountNumber"`
	LabelResponseOptions string                 `json:"labelResponseOptions"`
	RequestedShipment    fedexRequestedShipment `json:"requestedShipment"`
}

type fedexRequestedShipment struct {
	ServiceType               string                 `json:"serviceType"`
	PackagingType             string                 `json:"packagingType"`
	PickupType                string                 `json:"pickupType"`
	Shipper                   fedexParty             `json:"shipper"`
	Recipients                []fedexParty           `json:"recipients"`
	ShippingChargesPayment    fedexPayment           `json:"shippingChargesPayment"`
	TotalWeight               fedexWeight            `json:"totalWeight"`
	LabelSpecification        fedexLabelSpec         `json:"labelSpecification"`
	RequestedPackageLineItems []fedexPackageLineItem `json:"requestedPackageLineItems"`
}

type fedexParty struct {
	Address fedexAddress `json:"address"`
	Contact fedexContact `json:"contact"`
}

type fedexAddress struct {
	StreetLines         []string `json:"streetLines"`
	City                string   `json:"city"`
	StateOrProvinceCode string   `json:"stateOrProvinceCode,omitempty"`
	PostalCode          string   `json:"postalCode"`
	CountryCode         string   `json:"countryCode"`
	Residential         bool     `json:"residential"`
}

type fedexContact struct {
	PersonName  string `json:"personName,omitempty"`
	PhoneNumber string `json:"phoneNumber"`
	CompanyName string `json:"companyName,omitempty"`
}

type fedexPayment struct {
	PaymentType string `json:"paymentType"`
}

type fedexWeight struct {
	Units string  `json:"units"`
	Value float64 `json:"value"`
}

type fedexDimensions struct {
	Length int    `json:"length"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Units  string `json:"units"`
}

type fedexLabelSpec struct {
	ImageType      string `json:"imageType"`
	LabelStockType string `json:"labelStockType"`
}

type fedexPackageLineItem struct {
	Weight     fedexWeight      `json:"weight"`
	Dimensions *fedexDimensions `json:"dimensions,omitempty"`
}

// fedexShipResponse is the top-level response from POST /ship/v1/shipments.
type fedexShipResponse struct {
	TransactionID string          `json:"transactionId"`
	Output        fedexShipOutput `json:"output"`
}

type fedexShipOutput struct {
	TransactionShipments []fedexTransactionShipment `json:"transactionShipments"`
}

type fedexTransactionShipment struct {
	MasterTrackingNumber string               `json:"masterTrackingNumber"`
	PieceResponses       []fedexPieceResponse `json:"pieceResponses"`
}

type fedexPieceResponse struct {
	TrackingNumber   string          `json:"trackingNumber"`
	PackageDocuments []fedexLabelDoc `json:"packageDocuments"`
}

// fedexLabelDoc holds an inline encoded label or a retrieval URL.
type fedexLabelDoc struct {
	EncodedLabel string `json:"encodedLabel"`
	DocType      string `json:"docType"`
	URL          string `json:"url"`
}

// fedexCancelRequest is the body for PUT /ship/v1/shipments/cancel.
type fedexCancelRequest struct {
	AccountNumber   fedexAccountNumber `json:"accountNumber"`
	TrackingNumber  string             `json:"trackingNumber"`
	DeletionControl string             `json:"deletionControl,omitempty"`
}

// fedexTrackRequest is the body for POST /track/v1/trackingnumbers.
type fedexTrackRequest struct {
	IncludeDetailedScans bool                `json:"includeDetailedScans"`
	TrackingInfo         []fedexTrackingInfo `json:"trackingInfo"`
}

type fedexTrackingInfo struct {
	TrackingNumberInfo fedexTrackingNumberInfo `json:"trackingNumberInfo"`
}

type fedexTrackingNumberInfo struct {
	TrackingNumber string `json:"trackingNumber"`
}

// fedexTrackResponse is the top-level response from POST /track/v1/trackingnumbers.
// The output field schema in the spec is empty; in practice it contains
// completeTrackResults matching TrackingNumbersResponse.
type fedexTrackResponse struct {
	Output fedexTrackOutput `json:"output"`
}

type fedexTrackOutput struct {
	CompleteTrackResults []fedexCompleteTrackResult `json:"completeTrackResults"`
}

type fedexCompleteTrackResult struct {
	TrackingNumber string             `json:"trackingNumber"`
	TrackResults   []fedexTrackResult `json:"trackResults"`
}

type fedexTrackResult struct {
	LatestStatusDetail fedexStatusDetail  `json:"latestStatusDetail"`
	ScanEvents         []fedexScanEvent   `json:"scanEvents"`
	DateAndTimes       []fedexDateAndTime `json:"dateAndTimes"`
}

type fedexStatusDetail struct {
	Code           string `json:"code"`
	DerivedCode    string `json:"derivedCode"`
	Description    string `json:"description"`
	StatusByLocale string `json:"statusByLocale"`
}

type fedexScanEvent struct {
	Date              string          `json:"date"`
	EventType         string          `json:"eventType"`
	DerivedStatusCode string          `json:"derivedStatusCode"`
	EventDescription  string          `json:"eventDescription"`
	DerivedStatus     string          `json:"derivedStatus"`
	ScanLocation      fedexAddressVO1 `json:"scanLocation"`
}

// fedexAddressVO1 mirrors the AddressVO1 schema used for scan locations.
type fedexAddressVO1 struct {
	City                string `json:"city"`
	StateOrProvinceCode string `json:"stateOrProvinceCode"`
	CountryCode         string `json:"countryCode"`
}

type fedexDateAndTime struct {
	DateTime string `json:"dateTime"`
	Type     string `json:"type"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// fedexServiceType picks a FedEx service type based on sender/receiver countries.
// Same-country shipments default to FEDEX_GROUND; cross-border defaults to
// FEDEX_INTERNATIONAL_PRIORITY. A future extension can expose this as a
// carrier-specific override on BookingRequest.
func fedexServiceType(s Shipment) string {
	if s.Sender.Country == s.Receiver.Country {
		return "FEDEX_GROUND"
	}
	return "FEDEX_INTERNATIONAL_PRIORITY"
}

// fedexPartyFrom maps a gateway Address to a FedEx Party.
// FedEx requires streetLines (array) rather than a single street field.
// HouseNumber is appended to Street on the same line — FedEx does not have
// a separate house-number field.
func fedexPartyFrom(addr Address) fedexParty {
	streetLine := addr.Street
	if addr.HouseNumber != "" {
		streetLine += " " + addr.HouseNumber
	}
	lines := []string{streetLine}
	if addr.Supplement != "" {
		lines = append(lines, addr.Supplement)
	}

	return fedexParty{
		Address: fedexAddress{
			StreetLines:         lines,
			City:                addr.City,
			StateOrProvinceCode: addr.State,
			PostalCode:          addr.PostalCode,
			CountryCode:         addr.Country,
		},
		Contact: fedexContact{
			PersonName:  addr.Name,
			PhoneNumber: addr.Phone,
		},
	}
}

// fedexPackageItems converts gateway Colli to FedEx RequestedPackageLineItems.
// Dimensions are converted from float64 cm to integer cm by rounding up to
// avoid underreporting.
func fedexPackageItems(colli []Colli) []fedexPackageLineItem {
	items := make([]fedexPackageLineItem, len(colli))
	for i, c := range colli {
		item := fedexPackageLineItem{
			Weight: fedexWeight{Units: "KG", Value: c.Weight},
		}
		d := c.Dimensions
		if d.Length > 0 || d.Width > 0 || d.Height > 0 {
			item.Dimensions = &fedexDimensions{
				Length: int(math.Ceil(d.Length)),
				Width:  int(math.Ceil(d.Width)),
				Height: int(math.Ceil(d.Height)),
				Units:  "CM",
			}
		}
		items[i] = item
	}
	return items
}

// ── CarrierAdapter methods ────────────────────────────────────────────────────

// BookShipment books a FedEx shipment via POST /ship/v1/shipments.
//
// Service type is derived from sender/receiver countries (see fedexServiceType).
// Labels are returned inline as base64-encoded PDF in the response and surfaced
// as data URIs in each ColliResponse.LabelURL.
func (a *FedExAdapter) BookShipment(ctx context.Context, r BookingRequest) (*BookingResponse, error) {
	shipReq := fedexShipRequest{
		AccountNumber:        fedexAccountNumber{Value: a.AccountNumber},
		LabelResponseOptions: "LABEL",
		RequestedShipment: fedexRequestedShipment{
			ServiceType:            fedexServiceType(r.Shipment),
			PackagingType:          "YOUR_PACKAGING",
			PickupType:             "USE_SCHEDULED_PICKUP",
			Shipper:                fedexPartyFrom(r.Shipment.Sender),
			Recipients:             []fedexParty{fedexPartyFrom(r.Shipment.Receiver)},
			ShippingChargesPayment: fedexPayment{PaymentType: "SENDER"},
			TotalWeight:            fedexWeight{Units: "KG", Value: r.Shipment.TotalWeight},
			LabelSpecification: fedexLabelSpec{
				ImageType:      "PDF",
				LabelStockType: "PAPER_7X475",
			},
			RequestedPackageLineItems: fedexPackageItems(r.Shipment.Colli),
		},
	}

	body, err := json.Marshal(shipReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx ship request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPost, "/ship/v1/shipments", body)
	if err != nil {
		return nil, err
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx ship request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx ship response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx ship API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var shipResp fedexShipResponse
	if err := json.Unmarshal(respBody, &shipResp); err != nil {
		return nil, fmt.Errorf("failed to decode FedEx ship response: %w", err)
	}
	if len(shipResp.Output.TransactionShipments) == 0 {
		return nil, fmt.Errorf("FedEx ship response contained no transaction shipments")
	}

	txn := shipResp.Output.TransactionShipments[0]

	colli := make([]ColliResponse, 0, len(txn.PieceResponses))
	for i, piece := range txn.PieceResponses {
		cr := ColliResponse{
			TrackingNumber: piece.TrackingNumber,
			Status:         "booked",
		}
		if i < len(r.Shipment.Colli) {
			cr.ID = r.Shipment.Colli[i].ID
		}
		if len(piece.PackageDocuments) > 0 {
			if doc := piece.PackageDocuments[0]; doc.EncodedLabel != "" {
				cr.LabelURL = "data:application/pdf;base64," + doc.EncodedLabel
			}
		}
		colli = append(colli, cr)
	}

	masterTN := txn.MasterTrackingNumber
	if masterTN == "" && len(colli) > 0 {
		masterTN = colli[0].TrackingNumber
	}

	var labelURL string
	if len(colli) > 0 {
		labelURL = colli[0].LabelURL
	}

	a.log.Info("FedEx shipment booked",
		zap.String("masterTrackingNumber", masterTN),
		zap.Int("packages", len(colli)),
	)

	return &BookingResponse{
		TrackingNumber: masterTN,
		LabelURL:       labelURL,
		Carrier:        "fedex",
		Status:         "booked",
		Colli:          colli,
	}, nil
}

// TrackShipment retrieves FedEx shipment status via POST /track/v1/trackingnumbers.
//
// The top-level status is taken from latestStatusDetail.code; individual scan
// events are sourced from scanEvents[]. Estimated delivery is surfaced when
// a dateAndTimes entry with type ESTIMATED_DELIVERY is present.
func (a *FedExAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	trackReq := fedexTrackRequest{
		IncludeDetailedScans: true,
		TrackingInfo: []fedexTrackingInfo{
			{TrackingNumberInfo: fedexTrackingNumberInfo{TrackingNumber: trackingNumber}},
		},
	}

	body, err := json.Marshal(trackReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx track request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPost, "/track/v1/trackingnumbers", body)
	if err != nil {
		return nil, err
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx track request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx track response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx track API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var trackResp fedexTrackResponse
	if err := json.Unmarshal(respBody, &trackResp); err != nil {
		return nil, fmt.Errorf("failed to decode FedEx track response: %w", err)
	}

	if len(trackResp.Output.CompleteTrackResults) == 0 {
		return nil, fmt.Errorf("FedEx track response contained no results for %s", trackingNumber)
	}
	ctr := trackResp.Output.CompleteTrackResults[0]
	if len(ctr.TrackResults) == 0 {
		return nil, fmt.Errorf("FedEx track response contained no track results for %s", trackingNumber)
	}
	result := ctr.TrackResults[0]

	// Derive top-level status from latestStatusDetail.code.
	rawStatus := result.LatestStatusDetail.Code
	if rawStatus == "" {
		rawStatus = result.LatestStatusDetail.DerivedCode
	}
	normalized := normalizeStatus("fedex", rawStatus)

	// Build event list from scan events (newest first from FedEx).
	events := make([]TrackingEvent, 0, len(result.ScanEvents))
	for _, e := range result.ScanEvents {
		evtCode := e.EventType
		if evtCode == "" {
			evtCode = e.DerivedStatusCode
		}
		events = append(events, TrackingEvent{
			Timestamp:        e.Date,
			Status:           evtCode,
			NormalizedStatus: normalizeStatus("fedex", evtCode),
			Location:         fedexLocation(e.ScanLocation),
			Details:          e.EventDescription,
		})
	}

	// Estimated delivery from dateAndTimes.
	var estimatedDelivery string
	for _, dt := range result.DateAndTimes {
		if dt.Type == "ESTIMATED_DELIVERY" || dt.Type == "ACTUAL_DELIVERY" {
			estimatedDelivery = dt.DateTime
			break
		}
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "fedex",
		Status:            rawStatus,
		NormalizedStatus:  normalized,
		OriginalStatus:    rawStatus,
		Events:            events,
		EstimatedDelivery: estimatedDelivery,
	}, nil
}

// fedexLocation formats a FedEx AddressVO1 scan location into a human-readable string.
func fedexLocation(addr fedexAddressVO1) string {
	if addr.City == "" {
		return addr.CountryCode
	}
	parts := addr.City
	if addr.StateOrProvinceCode != "" {
		parts += ", " + addr.StateOrProvinceCode
	}
	if addr.CountryCode != "" {
		parts += ", " + addr.CountryCode
	}
	return parts
}

// FetchLabel retrieves a FedEx shipping label.
//
// FedEx labels are returned inline in the BookShipment response as base64.
// A dedicated reprint endpoint may exist — pending documentation.
func (a *FedExAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, notSupported("FedEx", "label fetch", "label reprint endpoint pending — spec not yet available")
}

// CancelShipment cancels a FedEx shipment via PUT /ship/v1/shipments/cancel.
// All packages in the shipment are cancelled (DeletionControl: DELETE_ALL_PACKAGES).
func (a *FedExAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	cancelReq := fedexCancelRequest{
		AccountNumber:   fedexAccountNumber{Value: a.AccountNumber},
		TrackingNumber:  trackingNumber,
		DeletionControl: "DELETE_ALL_PACKAGES",
	}

	body, err := json.Marshal(cancelReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx cancel request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPut, "/ship/v1/shipments/cancel", body)
	if err != nil {
		return nil, err
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx cancel request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx cancel response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx cancel API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	a.log.Info("FedEx shipment cancelled", zap.String("trackingNumber", trackingNumber))

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "fedex",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment applies post-booking updates to a FedEx shipment.
//
// FedEx does not support post-booking updates via the Ship API.
// Address corrections require a cancel-and-rebook cycle.
func (a *FedExAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("FedEx", "post-booking update", "")
}
