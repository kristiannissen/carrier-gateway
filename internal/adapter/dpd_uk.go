// Package adapter provides the DPD UK implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/dpd_uk.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DPDUKAdapter implements CarrierAdapter and ManifestAdapter for the DPD UK
// Shipping API (https://api.dpd.co.uk).
//
// Authentication is session-based: a GEOSession token is obtained via
// POST /user/?action=login with HTTP Basic credentials, then passed on every
// subsequent request via the GEOSession header. The token is fetched lazily on
// the first request and refreshed automatically on a 401 response.
//
// Required env vars:
//
//	DPD_UK_USERNAME     - DPD UK account username (email address)
//	DPD_UK_PASSWORD     - DPD UK account password
//	DPD_UK_USER_ID      - DPD UK account number used in the GEOClient header
//	DPD_UK_NETWORK_CODE - DPD service network code (default "1^12" = Next Day)
//
// Unsupported: TrackShipment, CancelShipment, UpdateShipment, all pickup and
// manifest methods. These return ErrNotSupported so the handler responds 501.
type DPDUKAdapter struct {
	username    string
	password    string
	userID      string
	networkCode string
	baseURL     string
	HTTPClient  *http.Client
	log         *zap.Logger

	mu         sync.Mutex
	geoSession string

	// shipmentIDs maps parcel number → integer shipment ID required by the
	// label endpoint. Populated at booking time; lives for the process lifetime.
	shipmentIDs sync.Map
}

// NewDPDUKAdapter creates a DPDUKAdapter. networkCode defaults to "1^12"
// (DPD Next Day) when empty.
func NewDPDUKAdapter(username, password, userID, networkCode string, log *zap.Logger) *DPDUKAdapter {
	if networkCode == "" {
		networkCode = "1^12"
	}
	return &DPDUKAdapter{
		username:    username,
		password:    password,
		userID:      userID,
		networkCode: networkCode,
		baseURL:     "https://api.dpd.co.uk",
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		log:         log,
	}
}

// ensureSession returns the cached GEOSession token, fetching one if the cache
// is empty. Callers must not hold mu before calling.
func (a *DPDUKAdapter) ensureSession(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.geoSession != "" {
		return a.geoSession, nil
	}
	return a.fetchSession(ctx)
}

// fetchSession performs POST /user/?action=login and stores the returned token.
// Caller must hold mu.
func (a *DPDUKAdapter) fetchSession(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/user/?action=login", nil)
	if err != nil {
		return "", fmt.Errorf("dpd_uk: create login request: %w", err)
	}
	creds := base64.StdEncoding.EncodeToString([]byte(a.username + ":" + a.password))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Length", "0")
	req.Header.Set("GEOClient", a.username+"/"+a.userID)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("dpd_uk: login request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("dpd_uk: read login response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dpd_uk: login returned %d: %s", resp.StatusCode, string(body))
	}

	var loginResp struct {
		Data struct {
			GeoSession string `json:"geoSession"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return "", fmt.Errorf("dpd_uk: decode login response: %w", err)
	}
	if loginResp.Data.GeoSession == "" {
		return "", fmt.Errorf("dpd_uk: login returned empty geoSession")
	}

	a.geoSession = loginResp.Data.GeoSession
	a.log.Info("DPD UK session established")
	return a.geoSession, nil
}

// invalidateSession clears the cached token so the next call re-authenticates.
func (a *DPDUKAdapter) invalidateSession() {
	a.mu.Lock()
	a.geoSession = ""
	a.mu.Unlock()
}

// do executes req with GEO auth headers set. On a 401 it invalidates the
// session and returns an error — the caller should retry once after re-login.
func (a *DPDUKAdapter) do(ctx context.Context, req *http.Request) (*http.Response, []byte, error) {
	session, err := a.ensureSession(ctx)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("GEOClient", a.username+"/"+a.userID)
	req.Header.Set("GEOSession", session)
	// Only set Accept if the caller has not already specified one
	// (e.g. FetchLabel uses text/html for the label endpoint).
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("dpd_uk: HTTP request failed: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close() //nolint:errcheck,gosec
		a.invalidateSession()
		return nil, nil, fmt.Errorf("dpd_uk: session rejected (401) — session cleared, retry the operation")
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, fmt.Errorf("dpd_uk: read response body: %w", err)
	}
	return resp, body, nil
}

// dpdUKAddress converts a gateway Address to the DPD UK wire format.
// DPD UK uses organisation/postcode/town/street rather than name/postalCode/city.
func dpdUKAddress(a Address) map[string]any {
	street := a.Street
	if a.HouseNumber != "" {
		street = a.Street + " " + a.HouseNumber
	}
	addr := map[string]any{
		"organisation": a.Name,
		"countryCode":  a.Country,
		"postcode":     a.PostalCode,
		"street":       street,
		"town":         a.City,
	}
	if a.Supplement != "" {
		addr["locality"] = a.Supplement
	}
	if a.State != "" {
		addr["county"] = a.State
	}
	return addr
}

// BookShipment creates a DPD UK shipment via POST /shipping/shipment.
//
// Wire format notes:
//   - Auth via GEOClient + GEOSession headers.
//   - Payload: job_id/invoice/consolidate wrapper + consignment[] array.
//   - collectionDate defaults to today at 16:00 UTC.
//   - networkCode selects the DPD service (default "1^12" = Next Day).
//   - notificationDetails on deliveryDetails picks up Receiver.Email/Phone.
//   - shippingRef1 carries IdempotencyKey or the first colli reference.
//   - Response: data.shipmentId (integer) + data.consignmentDetail[].parcelNumbers[].
//   - The 14-digit parcel number is returned as TrackingNumber.
//   - shipmentId is stored internally for FetchLabel (which requires it).
func (a *DPDUKAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("dpd_uk: shipment must contain at least one colli")
	}

	totalWeight := 0.0
	for _, c := range req.Shipment.Colli {
		totalWeight += c.Weight
	}

	collectionDate := time.Now().UTC().Format("2006-01-02") + "T16:00:00"

	collectionDetails := map[string]any{
		"contactDetails": map[string]any{
			"contactName": req.Shipment.Sender.Name,
			"telephone":   req.Shipment.Sender.Phone,
		},
		"address": dpdUKAddress(req.Shipment.Sender),
	}

	deliveryDetails := map[string]any{
		"contactDetails": map[string]any{
			"contactName": req.Shipment.Receiver.Name,
			"telephone":   req.Shipment.Receiver.Phone,
		},
		"address": dpdUKAddress(req.Shipment.Receiver),
	}
	if req.Shipment.Receiver.Email != "" || req.Shipment.Receiver.Phone != "" {
		notif := map[string]any{}
		if req.Shipment.Receiver.Email != "" {
			notif["email"] = req.Shipment.Receiver.Email
		}
		if req.Shipment.Receiver.Phone != "" {
			notif["mobile"] = req.Shipment.Receiver.Phone
		}
		deliveryDetails["notificationDetails"] = notif
	}

	consignment := map[string]any{
		"consignmentNumber":    nil,
		"consignmentRef":       nil,
		"parcels":              []any{},
		"collectionDetails":    collectionDetails,
		"deliveryDetails":      deliveryDetails,
		"networkCode":          a.networkCode,
		"numberOfParcels":      len(req.Shipment.Colli),
		"totalWeight":          totalWeight,
		"shippingRef1":         "",
		"shippingRef2":         "",
		"shippingRef3":         "",
		"customsValue":         nil,
		"deliveryInstructions": "",
		"parcelDescription":    "",
		"liabilityValue":       nil,
		"liability":            false,
	}

	// References — idempotency key takes priority over colli reference.
	switch {
	case req.IdempotencyKey != "":
		consignment["shippingRef1"] = req.IdempotencyKey
	case len(req.Shipment.Colli) > 0 && req.Shipment.Colli[0].Reference != "":
		consignment["shippingRef1"] = req.Shipment.Colli[0].Reference
	}

	// Flex delivery.
	if flex, ok := getAddOn(req.Shipment.AddOns, AddOnFlexDelivery); ok {
		consignment["deliveryInstructions"] = flex.Instructions
	}

	// Insurance maps to DPD UK liability.
	if ins, ok := getAddOn(req.Shipment.AddOns, AddOnInsurance); ok {
		consignment["liability"] = true
		consignment["liabilityValue"] = ins.InsuranceValue
	}

	// Customs value (basic — full customs declaration not available on this endpoint).
	if req.Shipment.Customs.CustomsValue > 0 {
		consignment["customsValue"] = req.Shipment.Customs.CustomsValue
	}

	payload := map[string]any{
		"job_id":               nil,
		"collectionOnDelivery": false,
		"invoice":              nil,
		"collectionDate":       collectionDate,
		"consolidate":          false,
		"consignment":          []any{consignment},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dpd_uk: marshal shipment request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/shipping/shipment", bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("dpd_uk: create shipment request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, body, err := a.do(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("dpd_uk: shipment request: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("dpd_uk: shipment API returned %d: %s", resp.StatusCode, string(body))
	}

	var dpdResp struct {
		Error any `json:"error"`
		Data  struct {
			ShipmentID        int  `json:"shipmentId"`
			Consolidated      bool `json:"consolidated"`
			ConsignmentDetail []struct {
				ConsignmentNumber string   `json:"consignmentNumber"`
				ParcelNumbers     []string `json:"parcelNumbers"`
			} `json:"consignmentDetail"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &dpdResp); err != nil {
		return nil, fmt.Errorf("dpd_uk: decode shipment response: %w", err)
	}
	if len(dpdResp.Data.ConsignmentDetail) == 0 {
		return nil, fmt.Errorf("dpd_uk: response contained no consignment detail: %s", string(body))
	}

	detail := dpdResp.Data.ConsignmentDetail[0]
	trackingNumber := ""
	if len(detail.ParcelNumbers) > 0 {
		trackingNumber = detail.ParcelNumbers[0]
	}
	shipmentID := fmt.Sprintf("%d", dpdResp.Data.ShipmentID)

	// Cache parcel number → shipment ID for FetchLabel.
	a.shipmentIDs.Store(trackingNumber, shipmentID)

	a.log.Info("DPD UK shipment booked",
		zap.String("shipmentID", shipmentID),
		zap.String("consignmentNumber", detail.ConsignmentNumber),
		zap.String("trackingNumber", trackingNumber),
	)

	colliResp := make([]ColliResponse, len(req.Shipment.Colli))
	for i, c := range req.Shipment.Colli {
		pn := ""
		if i < len(detail.ParcelNumbers) {
			pn = detail.ParcelNumbers[i]
		}
		colliResp[i] = ColliResponse{ID: c.ID, TrackingNumber: pn, Status: "booked"}
	}

	return &BookingResponse{
		ShipmentID:     shipmentID,
		TrackingNumber: trackingNumber,
		Carrier:        "dpd_uk",
		Status:         "booked",
		Colli:          colliResp,
		BetaWarning:    "DPD UK adapter is in beta — validate in sandbox before production use",
	}, nil
}

// FetchLabel retrieves the shipping label via GET /shipping/shipment/{shipmentId}/label/.
//
// The endpoint requires the integer shipmentId (not the parcel number). The adapter
// resolves it from the in-memory map populated at booking time. Pass the parcel
// number returned by BookShipment as LabelRequest.TrackingNumber. Only PDF is supported.
func (a *DPDUKAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("DPD UK", req.Format, LabelFormatPDF)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("dpd_uk: tracking number must not be empty")
	}

	// Resolve shipmentId from the cached parcel → shipment mapping.
	shipmentID := req.TrackingNumber
	if v, ok := a.shipmentIDs.Load(req.TrackingNumber); ok {
		shipmentID = v.(string)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/shipping/shipment/%s/label/", a.baseURL, shipmentID), nil)
	if err != nil {
		return nil, fmt.Errorf("dpd_uk: create label request: %w", err)
	}
	// DPD UK label endpoint returns raw content; override the default JSON accept.
	httpReq.Header.Set("Accept", "text/html")

	resp, body, err := a.do(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("dpd_uk: label request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dpd_uk: label API returned %d: %s", resp.StatusCode, string(body))
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dpd_uk",
		Format:         LabelFormatPDF,
		Data:           base64.StdEncoding.EncodeToString(body),
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// TrackShipment is not yet supported for DPD UK.
// The tracking endpoint is not confirmed from the available API documentation.
// Use the DPD UK consumer tracking portal at https://track.dpd.co.uk.
func (a *DPDUKAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("DPD UK", "shipment tracking",
		"tracking endpoint not yet confirmed — use https://track.dpd.co.uk")
}

// CancelShipment is not yet supported for DPD UK.
// The cancellation endpoint is not confirmed from the available API documentation.
// Cancel shipments via the DPD UK Shipping portal.
func (a *DPDUKAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DPD UK", "shipment cancellation",
		"cancellation endpoint not yet confirmed — cancel via the DPD UK Shipping portal")
}

// UpdateShipment is not supported for DPD UK.
func (a *DPDUKAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DPD UK", "post-booking update", "cancel and rebook")
}

// BookPickup is not supported for DPD UK.
// DPD UK handles collection via the collectionDate field set at booking time.
func (a *DPDUKAdapter) BookPickup(_ context.Context, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DPD UK", "pickup booking",
		"set the collection date via collectionDate at booking time")
}

// UpdatePickup is not supported for DPD UK.
func (a *DPDUKAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DPD UK", "pickup update", "")
}

// CancelPickup is not supported for DPD UK.
func (a *DPDUKAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DPD UK", "pickup cancellation", "")
}

// CloseManifest is not supported for DPD UK.
func (a *DPDUKAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("DPD UK", "manifest close", "")
}

// GetPickupAvailability is not supported for DPD UK.
func (a *DPDUKAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DPD UK", "pickup availability", "")
}
