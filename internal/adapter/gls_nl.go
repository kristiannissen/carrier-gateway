// Package adapter provides the GLS NL implementation of CarrierAdapter and ManifestAdapter.
// This file is located at /internal/adapter/gls_nl.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// GLSNLAdapter implements CarrierAdapter and ManifestAdapter for the GLS regional
// Shipping API used by GLS national subsidiaries (e.g. GLS Netherlands at
// api-portal.gls.nl). This API uses username/password in every request body and
// is distinct from the unified GLS Group OAuth2 API (carrier "gls").
//
// Configure via env vars GLS_{CC}_USERNAME, GLS_{CC}_PASSWORD, and
// GLS_{CC}_BASE_URL where CC is the ISO 3166-1 alpha-2 country code. The
// adapter is registered under the key "gls_{cc}" (e.g. "gls_nl", "gls_be").
//
// Labels are returned inline in the CreateLabel response and stored in each
// colli's LabelURL as a data URI. FetchLabel returns ErrNotSupported because
// this API has no reprint endpoint. TrackShipment is also not available.
type GLSNLAdapter struct {
	// Username is the MyGLS API account username.
	Username string
	// Password is the MyGLS API account password.
	Password string
	// BaseURL is the country-specific API root, e.g. https://api.mygls.nl.
	BaseURL string
	// Country is the lowercase ISO 3166-1 alpha-2 code (e.g. "nl").
	Country    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewGLSNLAdapter creates a GLSNLAdapter for the given credentials and country.
func NewGLSNLAdapter(username, password, baseURL, country string, log *zap.Logger) *GLSNLAdapter {
	return &GLSNLAdapter{
		Username:   username,
		Password:   password,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Country:    strings.ToLower(country),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}
}

// glsNLAuth builds the authentication and system-identity fields included on
// every request. The GLS NL API does not use a token; credentials are sent
// per-request in the JSON body.
func (a *GLSNLAdapter) glsNLAuth() map[string]any {
	return map[string]any{
		"Username":              a.Username,
		"Password":              a.Password,
		"ShippingSystemName":    "carrier-gateway",
		"ShippingSystemVersion": "1.0",
	}
}

// post marshals payload as JSON, POSTs to a.BaseURL+path, and returns the raw
// response body and HTTP status code.
func (a *GLSNLAdapter) post(ctx context.Context, path string, payload map[string]any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("gls_nl: marshal request for %s: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("gls_nl: create request for %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gls_nl: %s request failed: %w", path, err)
	}
	raw, err := readBody(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("gls_nl: read response for %s: %w", path, err)
	}
	return raw, resp.StatusCode, nil
}

// glsNLAddress builds a GLS NL address block from a gateway Address.
func glsNLAddress(a Address) map[string]any {
	addr := map[string]any{
		"Name1":       a.Name,
		"Street":      a.Street,
		"CountryCode": a.Country,
		"ZipCode":     a.PostalCode,
		"City":        a.City,
	}
	if a.HouseNumber != "" {
		addr["HouseNo"] = a.HouseNumber
	}
	if a.Supplement != "" {
		addr["Name2"] = a.Supplement
	}
	if a.Phone != "" {
		addr["Phone"] = a.Phone
	}
	if a.Email != "" {
		addr["Email"] = a.Email
	}
	return addr
}

// glsNLShipType returns "F" (freight) when any collo exceeds the 32 kg parcel
// limit; otherwise returns "P" (parcel).
func glsNLShipType(colli []Colli) string {
	for _, c := range colli {
		if c.Weight > 32 {
			return "F"
		}
	}
	return "P"
}

// glsNLUnitType maps a freight collo to the closest GLS UnitType based on
// weight and length. Used only when ShipType is "F".
func glsNLUnitType(c Colli) string {
	switch {
	case c.Dimensions.Length > 200:
		return "LG" // length collo (>200 cm)
	case c.Weight > 675:
		return "XL" // oversized pallet (≤1000 kg)
	case c.Weight > 350:
		return "BP" // block pallet (≤850 kg)
	case c.Weight > 300:
		return "PL" // standard pallet (≤675 kg)
	default:
		return "CO" // collo — default freight unit
	}
}

// BookShipment creates a GLS NL label via CreateLabel or, when
// DeliveryType is "return", via CreateShopReturn.
//
// Labels are returned inline from the API and stored in each colli's LabelURL
// field as a data URI (data:application/pdf;base64,...). There is no separate
// reprint endpoint — save the label from this response.
func (a *GLSNLAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("gls_nl: shipment must contain at least one colli")
	}

	if req.Shipment.DeliveryType == "return" {
		return a.bookShopReturn(ctx, req)
	}

	shipType := glsNLShipType(req.Shipment.Colli)

	units := make([]map[string]any, len(req.Shipment.Colli))
	for i, c := range req.Shipment.Colli {
		u := map[string]any{
			"UnitID": c.ID,
			"Weight": c.Weight,
		}
		if shipType == "F" {
			u["UnitType"] = glsNLUnitType(c)
		}
		if c.Reference != "" {
			u["AdditionalInfo1"] = c.Reference
		}
		units[i] = u
	}

	payload := a.glsNLAuth()
	payload["ShipType"] = shipType
	payload["LabelType"] = "pdf"
	payload["TrackingLinkType"] = "U"
	payload["Addresses"] = map[string]any{
		"DeliveryAddress": glsNLAddress(req.Shipment.Receiver),
	}
	payload["Units"] = units

	// Use the first colli reference as the shipment reference when present.
	if len(req.Shipment.Colli) > 0 && req.Shipment.Colli[0].Reference != "" {
		payload["Reference"] = req.Shipment.Colli[0].Reference
	}

	services := map[string]any{}
	var addOnWarnings []string

	// Parcel shop delivery: forward ParcelShopID from receiver.
	if req.Shipment.Receiver.ServicePointID != "" {
		services["ShopDeliveryParcelShopId"] = req.Shipment.Receiver.ServicePointID
	}

	for _, ao := range req.Shipment.AddOns {
		switch ao.Type {
		case AddOnEmailNotification:
			// GLS NL NotificationEmail block — basic notification without custom sender.
			payload["NotificationEmail"] = map[string]any{
				"SendMail": true,
			}
		case AddOnSMSNotification:
			addOnWarnings = append(addOnWarnings,
				"sms_notification: SMS is only available on ExpressService — not wired in this adapter")
		default:
			addOnWarnings = append(addOnWarnings,
				string(ao.Type)+": not supported by GLS NL API")
		}
	}

	if len(services) > 0 {
		payload["Services"] = services
	}

	raw, status, err := a.post(ctx, "/CreateLabel", payload)
	if err != nil {
		return nil, fmt.Errorf("gls_nl: post CreateLabel: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("gls_nl: CreateLabel returned %d: %s", status, string(raw))
	}

	return a.parseCreateLabelResponse(raw, req.Shipment.Colli, addOnWarnings)
}

// bookShopReturn creates a standalone return label via CreateShopReturn.
// Sender maps to PickupAddress (where the parcel is collected FROM — the
// consumer); Receiver maps to DeliveryAddress (where it is returned TO).
func (a *GLSNLAdapter) bookShopReturn(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	units := make([]map[string]any, len(req.Shipment.Colli))
	for i, c := range req.Shipment.Colli {
		u := map[string]any{
			"UnitID": c.ID,
			"Weight": c.Weight,
		}
		if c.Reference != "" {
			u["AdditionalInfo1"] = c.Reference
		}
		units[i] = u
	}

	payload := a.glsNLAuth()
	payload["ShipType"] = "P"
	payload["LabelType"] = "pdf"
	payload["TrackingLinkType"] = "U"
	payload["Addresses"] = map[string]any{
		"DeliveryAddress": glsNLAddress(req.Shipment.Receiver),
		"PickupAddress":   glsNLAddress(req.Shipment.Sender),
	}
	payload["Units"] = units

	if req.Shipment.Receiver.Email != "" {
		payload["SendLabelsByEmail"] = "true"
	}

	raw, status, err := a.post(ctx, "/CreateShopReturn", payload)
	if err != nil {
		return nil, fmt.Errorf("gls_nl: post CreateShopReturn: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("gls_nl: CreateShopReturn returned %d: %s", status, string(raw))
	}

	return a.parseCreateLabelResponse(raw, req.Shipment.Colli, nil)
}

// glsNLCreateLabelResp is the expected shape of a CreateLabel / CreateShopReturn
// response. The exact field names vary slightly across GLS national portals.
type glsNLCreateLabelResp struct {
	CreatedShipment *struct {
		UnitNumbers []string `json:"UnitNumbers"`
		// Label holds a single unit-level base64 label (pdf/zpl/pdfa6u).
		Label string `json:"Label"`
		// Labels holds shipment-level labels for multi-label formats (pdfa6s/pdf2a4/pdf4a4).
		Labels []string `json:"Labels"`
		// LabelShopReturn is the additional ShopReturn label returned when
		// ShopReturnService is requested alongside an outbound shipment.
		LabelShopReturn string `json:"LabelShopReturn"`
	} `json:"CreatedShipment"`
}

// parseCreateLabelResponse decodes a CreateLabel (or CreateShopReturn) API
// response and returns a BookingResponse with colli labels embedded as data URIs.
func (a *GLSNLAdapter) parseCreateLabelResponse(raw []byte, colli []Colli, addOnWarnings []string) (*BookingResponse, error) {
	var apiResp glsNLCreateLabelResp
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("gls_nl: decode label response: %w", err)
	}
	if apiResp.CreatedShipment == nil {
		return nil, fmt.Errorf("gls_nl: response missing CreatedShipment field: %s", string(raw))
	}

	cs := apiResp.CreatedShipment

	trackingNumber := ""
	if len(cs.UnitNumbers) > 0 {
		trackingNumber = cs.UnitNumbers[0]
	}

	a.log.Info("GLS NL shipment booked",
		zap.String("carrier", "gls_"+a.Country),
		zap.String("trackingNumber", trackingNumber),
	)

	colliResp := make([]ColliResponse, len(colli))
	for i, c := range colli {
		un := ""
		if i < len(cs.UnitNumbers) {
			un = cs.UnitNumbers[i]
		}

		// Prefer shipment-level multi-label (Labels[i]) over single label.
		labelB64 := ""
		switch {
		case i < len(cs.Labels) && cs.Labels[i] != "":
			labelB64 = cs.Labels[i]
		case i == 0 && cs.Label != "":
			labelB64 = cs.Label
		}

		labelURL := ""
		if labelB64 != "" {
			labelURL = "data:application/pdf;base64," + labelB64
		}

		colliResp[i] = ColliResponse{
			ID:             c.ID,
			TrackingNumber: un,
			LabelURL:       labelURL,
			Status:         "booked",
		}
	}

	return &BookingResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "gls_" + a.Country,
		Status:         "booked",
		Colli:          colliResp,
		AddOnWarnings:  addOnWarnings,
		BetaWarning:    "GLS NL regional adapter is in beta — validate in sandbox before production use",
	}, nil
}

// CancelShipment deletes a booked label via the DeleteLabel endpoint.
func (a *GLSNLAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("gls_nl: tracking number must not be empty")
	}

	payload := a.glsNLAuth()
	payload["unitNo"] = trackingNumber
	payload["shiptype"] = "P"

	raw, status, err := a.post(ctx, "/DeleteLabel", payload)
	if err != nil {
		return nil, fmt.Errorf("gls_nl: post DeleteLabel: %w", err)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return nil, fmt.Errorf("gls_nl: DeleteLabel returned %d: %s", status, string(raw))
	}

	a.log.Info("GLS NL label deleted",
		zap.String("carrier", "gls_"+a.Country),
		zap.String("trackingNumber", trackingNumber),
	)

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "gls_" + a.Country,
		Status:         "cancelled",
	}, nil
}

// TrackShipment is not available in the GLS NL regional API.
// Use the unified GLS Group adapter (carrier "gls") or the carrier tracking portal.
func (a *GLSNLAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("GLS NL", "tracking",
		"use the GLS tracking portal or the unified GLS Group adapter (carrier \"gls\")")
}

// FetchLabel is not supported because the GLS NL API has no label reprint endpoint.
// Labels are returned inline at booking time via colli[].labelUrl (data URI).
func (a *GLSNLAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, notSupported("GLS NL", "label reprint",
		"save the label from the booking response colli[].labelUrl")
}

// UpdateShipment is not supported by the GLS NL API. Cancel and rebook instead.
func (a *GLSNLAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("GLS NL", "post-booking update", "cancel and rebook")
}

// BookPickup creates a pickup instruction via the CreatePickup endpoint.
//
// CreatePickup requires three address blocks: RequesterAddress, PickupAddress,
// and DeliveryAddress. When only a single pickup address is provided all three
// default to that address.
func (a *GLSNLAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	pickupAddr := map[string]any{
		"Name1":       req.Contact.Name,
		"Street":      req.Address.Street,
		"CountryCode": req.Address.Country,
		"ZipCode":     req.Address.PostalCode,
		"City":        req.Address.City,
	}
	if req.Address.HouseNumber != "" {
		pickupAddr["HouseNo"] = req.Address.HouseNumber
	}
	if req.Contact.Phone != "" {
		pickupAddr["Phone"] = req.Contact.Phone
	}
	if req.Contact.Email != "" {
		pickupAddr["Email"] = req.Contact.Email
	}

	// Build units from tracking numbers when available; fall back to estimated count.
	units := make([]map[string]any, 0)
	if len(req.TrackingNumbers) > 0 {
		for i, tn := range req.TrackingNumbers {
			units = append(units, map[string]any{
				"UnitID": fmt.Sprintf("%c", 'A'+i%26),
				"unitNo": tn,
			})
		}
	} else {
		count := req.EstimatedParcels
		if count == 0 {
			count = 1
		}
		perUnit := req.EstimatedWeight / float64(count)
		if perUnit <= 0 {
			perUnit = 1.0
		}
		for i := 0; i < count; i++ {
			units = append(units, map[string]any{
				"UnitID": fmt.Sprintf("%c", 'A'+i%26),
				"Weight": perUnit,
			})
		}
	}

	payload := a.glsNLAuth()
	payload["ShipType"] = "P"
	payload["Addresses"] = map[string]any{
		"PickupAddress":    pickupAddr,
		"DeliveryAddress":  pickupAddr,
		"RequesterAddress": pickupAddr,
	}
	payload["Units"] = units

	if req.Pickup.Date != "" {
		payload["PickupDate"] = req.Pickup.Date
	}

	raw, status, err := a.post(ctx, "/CreatePickup", payload)
	if err != nil {
		return nil, fmt.Errorf("gls_nl: post CreatePickup: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("gls_nl: CreatePickup returned %d: %s", status, string(raw))
	}

	// Best-effort parse of confirmation number from response.
	var apiResp struct {
		PickupConfirmationNumber string `json:"PickupConfirmationNumber"`
	}
	_ = json.Unmarshal(raw, &apiResp) //nolint:errcheck // best-effort; not all portals return a confirmation

	confirmationNumber := apiResp.PickupConfirmationNumber
	if confirmationNumber == "" {
		// Fall back to a synthetic key so the caller can reference the pickup.
		confirmationNumber = req.Pickup.Date
	}

	a.log.Info("GLS NL pickup booked",
		zap.String("carrier", "gls_"+a.Country),
		zap.String("confirmation", confirmationNumber),
		zap.String("date", req.Pickup.Date),
	)

	return &PickupResponse{
		Carrier:            "gls_" + a.Country,
		ConfirmationNumber: confirmationNumber,
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by the GLS NL API. Cancel and rebook instead.
func (a *GLSNLAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("GLS NL", "pickup update", "cancel and rebook")
}

// CancelPickup deletes a pickup instruction via the DeletePickup endpoint.
func (a *GLSNLAdapter) CancelPickup(ctx context.Context, _, confirmationNumber string) error {
	if confirmationNumber == "" {
		return fmt.Errorf("gls_nl: confirmation number must not be empty")
	}

	payload := a.glsNLAuth()
	payload["unitNo"] = confirmationNumber
	payload["shiptype"] = "P"

	raw, status, err := a.post(ctx, "/DeletePickup", payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("gls_nl: DeletePickup returned %d: %s", status, string(raw))
	}

	a.log.Info("GLS NL pickup cancelled",
		zap.String("carrier", "gls_"+a.Country),
		zap.String("confirmation", confirmationNumber),
	)
	return nil
}

// CloseManifest confirms all tracking numbers in the request via ConfirmLabel.
//
// The GLS NL API uses per-unit confirmation rather than a single end-of-day
// call. Each tracking number is confirmed individually. A partial failure
// returns an error listing the unit numbers that could not be confirmed.
func (a *GLSNLAdapter) CloseManifest(ctx context.Context, req ManifestRequest) (*ManifestResponse, error) {
	if len(req.TrackingNumbers) == 0 {
		return nil, fmt.Errorf("gls_nl: CloseManifest requires at least one tracking number")
	}

	var confirmed, failed []string

	for _, tn := range req.TrackingNumbers {
		payload := a.glsNLAuth()
		payload["unitNo"] = tn
		payload["shiptype"] = "p"

		raw, status, err := a.post(ctx, "/ConfirmLabel", payload)
		if err != nil {
			a.log.Warn("GLS NL ConfirmLabel error",
				zap.String("carrier", "gls_"+a.Country),
				zap.String("trackingNumber", tn),
				zap.Error(err),
			)
			failed = append(failed, tn)
			continue
		}
		if status != http.StatusOK && status != http.StatusNoContent {
			a.log.Warn("GLS NL ConfirmLabel non-OK",
				zap.String("carrier", "gls_"+a.Country),
				zap.String("trackingNumber", tn),
				zap.Int("status", status),
				zap.String("body", string(raw)),
			)
			failed = append(failed, tn)
			continue
		}
		confirmed = append(confirmed, tn)
	}

	if len(failed) > 0 {
		return nil, fmt.Errorf("gls_nl: CloseManifest: %d/%d units failed confirmation: %v",
			len(failed), len(req.TrackingNumbers), failed)
	}

	a.log.Info("GLS NL manifest closed",
		zap.String("carrier", "gls_"+a.Country),
		zap.String("date", req.Date),
		zap.Int("confirmed", len(confirmed)),
	)

	return &ManifestResponse{
		Carrier:          "gls_" + a.Country,
		Date:             req.Date,
		Status:           "closed",
		ParcelsConfirmed: len(confirmed),
		Warnings:         []string{},
	}, nil
}

// GetPickupAvailability is not supported by the GLS NL API.
func (a *GLSNLAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("GLS NL", "pickup availability", "")
}
