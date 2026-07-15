// Package adapter provides the DPD implementation of the CarrierAdapter and ManifestAdapter interfaces.
// This file is located at /internal/adapter/dpd.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// DPDAdapter implements CarrierAdapter and ManifestAdapter for the DPD Baltic
// Shipping API v1 (LT/LV/EE and compatible national endpoints).
//
// Authentication uses a pre-generated Bearer token issued by the DPD portal
// or via POST /auth/tokens. Tokens are long-lived; rotation is the operator's
// responsibility. Set DPD_<COUNTRY>_API_TOKEN and DPD_<COUNTRY>_BASE_URL.
//
// Unsupported operations (shipment update, pickup update, pickup cancel,
// manifest close) return ErrNotSupported so the handler returns HTTP 501.
type DPDAdapter struct {
	// Token is the Bearer token used on every request.
	Token string
	// BaseURL is the country-specific API root, e.g. https://esiunta.dpd.lt/api/v1.
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewDPDAdapter creates a DPDAdapter for the given token and base URL.
func NewDPDAdapter(token, baseURL string, log *zap.Logger) *DPDAdapter {
	return &DPDAdapter{
		Token:   token,
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// do executes an HTTP request with the Bearer token set and returns the response.
// The caller is responsible for closing the response body.
func (a *DPDAdapter) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Accept", "application/json")
	return a.HTTPClient.Do(req)
}

// readBody reads and closes the response body, returning the raw bytes.
func readBody(r io.ReadCloser) ([]byte, error) {
	defer r.Close() //nolint:errcheck
	return io.ReadAll(r)
}

// dpdAddress builds the DPD sender/receiver address block from a gateway Address.
func dpdAddress(a Address) map[string]any {
	addr := map[string]any{
		"name":       a.Name,
		"street":     a.Street,
		"city":       a.City,
		"postalCode": a.PostalCode,
		"country":    a.Country,
	}
	if a.HouseNumber != "" {
		addr["streetNo"] = a.HouseNumber
	}
	if a.Phone != "" {
		addr["phone"] = a.Phone
	}
	if a.Email != "" {
		addr["email"] = a.Email
	}
	if a.Supplement != "" {
		addr["contactInfo"] = a.Supplement
	}
	return addr
}

// BookShipment creates a DPD shipment and requests the label inline.
//
// Wire format: POST /shipments with a labelOptions block so the label is
// returned inside shipmentLabels in the same response. The DPD shipment UUID
// is stored in ShipmentID; the first parcel number (14 chars) is the
// TrackingNumber used for all subsequent operations.
//
// PUDO delivery is supported by setting Receiver.ServicePointID — the pudoId
// is forwarded in the receiverAddress block.
func (a *DPDAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	// Build parcels array — one entry per colli.
	parcels := make([]map[string]any, len(req.Shipment.Colli))
	for i, c := range req.Shipment.Colli {
		parcel := map[string]any{"weight": c.Weight}
		if c.Reference != "" {
			parcel["mpsReferences"] = []string{c.Reference}
		}
		parcels[i] = parcel
	}

	receiverAddr := dpdAddress(req.Shipment.Receiver)
	// PUDO: forward pudoId and drop street fields — carrier routes to the point.
	if req.Shipment.Receiver.ServicePointID != "" {
		receiverAddr["pudoId"] = req.Shipment.Receiver.ServicePointID
	}

	payload := map[string]any{
		"senderAddress":   dpdAddress(req.Shipment.Sender),
		"receiverAddress": receiverAddr,
		"parcels":         parcels,
		// labelOptions requests an inline label so we avoid a second round-trip.
		"labelOptions": map[string]any{
			"downloadLabel": true,
			"labelFormat":   dpdLabelFormat(LabelFormatPDF),
			"paperSize":     "A6",
		},
	}

	// COD add-on.
	if cod, ok := getAddOn(req.Shipment.AddOns, AddOnCashOnDelivery); ok {
		payload["additionalServices"] = []map[string]any{
			{"serviceAlias": "COD"},
		}
		_ = cod // COD amount mapping requires a confirmed serviceAlias from DPD support.
		a.log.Warn("DPD COD: serviceAlias must be confirmed with DPD support; COD amount not forwarded")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dpd: marshal shipment request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/shipments", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dpd: create shipment request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dpd: shipment request failed: %w", err)
	}

	raw, err := readBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dpd: read shipment response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("dpd: shipment API returned %d: %s", resp.StatusCode, string(raw))
	}

	var dpdResp struct {
		ID            string `json:"id"`
		ParcelNumbers []struct {
			ParcelNumber string `json:"parcelNumber"`
		} `json:"parcelNumbers"`
		ShipmentLabels *struct {
			Pages []struct {
				BinaryData []byte `json:"binaryData"`
			} `json:"pages"`
		} `json:"shipmentLabels"`
	}
	if err := json.Unmarshal(raw, &dpdResp); err != nil {
		return nil, fmt.Errorf("dpd: decode shipment response: %w", err)
	}

	trackingNumber := ""
	if len(dpdResp.ParcelNumbers) > 0 {
		trackingNumber = dpdResp.ParcelNumbers[0].ParcelNumber
	}

	a.log.Info("DPD shipment booked",
		zap.String("shipmentID", dpdResp.ID),
		zap.String("trackingNumber", trackingNumber),
	)

	bookingResp := &BookingResponse{
		ShipmentID:     dpdResp.ID,
		TrackingNumber: trackingNumber,
		Carrier:        "dpd",
		Status:         "booked",
		BetaWarning:    "DPD adapter is in beta — validate in sandbox before production use",
	}

	// Colli responses — DPD assigns one parcel number per parcel.
	colliResp := make([]ColliResponse, len(req.Shipment.Colli))
	for i, c := range req.Shipment.Colli {
		pn := ""
		if i < len(dpdResp.ParcelNumbers) {
			pn = dpdResp.ParcelNumbers[i].ParcelNumber
		}
		colliResp[i] = ColliResponse{ID: c.ID, TrackingNumber: pn, Status: "booked"}
	}
	bookingResp.Colli = colliResp

	return bookingResp, nil
}

// CancelShipment deletes a DPD shipment by its shipment UUID.
//
// Wire format: DELETE /shipments?ids={shipmentID}.
// DPD removes the shipment from the user's list; the parcel ID remains
// allocated per DPD policy (chapter 7.5 of the API docs).
func (a *DPDAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("dpd: tracking number must not be empty")
	}

	url := fmt.Sprintf("%s/shipments?ids=%s", a.BaseURL, trackingNumber)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dpd: create cancel request: %w", err)
	}

	resp, err := a.do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dpd: cancel request failed: %w", err)
	}
	raw, _ := readBody(resp.Body)

	// DPD returns 204 on success.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dpd: cancel API returned %d: %s", resp.StatusCode, string(raw))
	}

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "dpd",
		Status:         "cancelled",
	}, nil
}

// TrackShipment retrieves parcel status from GET /status/tracking.
//
// The parcel number (14 numeric chars returned at booking) is used as pknr.
// detail=3 requests the advanced status schema (statusCode + serviceCode)
// which is required for accurate normalization. show_all=1 returns full history.
func (a *DPDAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("dpd: tracking number must not be empty")
	}

	url := fmt.Sprintf("%s/status/tracking?pknr=%s&detail=3&show_all=1", a.BaseURL, trackingNumber)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dpd: create tracking request: %w", err)
	}

	resp, err := a.do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dpd: tracking request failed: %w", err)
	}
	raw, err := readBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dpd: read tracking response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dpd: tracking API returned %d: %s", resp.StatusCode, string(raw))
	}

	// DPD returns an array, one element per requested parcel.
	var dpdResp []struct {
		ParcelNumber string `json:"parcelNumber"`
		Details      []struct {
			StatusCode     string `json:"statusCode"`
			ServiceCode    string `json:"serviceCode"`
			PrevStatusCode string `json:"prevStatusCode"`
			DateTime       string `json:"dateTime"`
			City           string `json:"city"`
			Depot          string `json:"depot"`
		} `json:"details"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &dpdResp); err != nil {
		return nil, fmt.Errorf("dpd: decode tracking response: %w", err)
	}

	if len(dpdResp) == 0 {
		return nil, fmt.Errorf("dpd: tracking response was empty for parcel %s", trackingNumber)
	}

	parcel := dpdResp[0]
	if parcel.Error != nil {
		return nil, fmt.Errorf("dpd: tracking error %d: %s", parcel.Error.Code, parcel.Error.Message)
	}

	events := make([]TrackingEvent, len(parcel.Details))
	for i, d := range parcel.Details {
		norm := normalizeDPDStatus(d.StatusCode, d.ServiceCode, d.PrevStatusCode)
		location := d.City
		if location == "" {
			location = d.Depot
		}
		events[i] = TrackingEvent{
			Timestamp:        d.DateTime,
			Status:           d.StatusCode,
			NormalizedStatus: norm,
			Location:         location,
		}
	}

	rawStatus := ""
	norm := TrackingStatus(StatusUnknown)
	if len(parcel.Details) > 0 {
		d := parcel.Details[0]
		rawStatus = d.StatusCode
		norm = normalizeDPDStatus(d.StatusCode, d.ServiceCode, d.PrevStatusCode)
	}

	return &TrackingResponse{
		TrackingNumber:   parcel.ParcelNumber,
		Carrier:          "dpd",
		Status:           rawStatus,
		NormalizedStatus: norm,
		OriginalStatus:   rawStatus,
		Events:           events,
	}, nil
}

// FetchLabel retrieves a label via POST /shipments/labels.
//
// DPD supports PDF and PNG formats. ZPL is not available in the Baltic API.
// The parcel number returned at booking is used as the parcelNumbers entry.
func (a *DPDAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	switch req.Format {
	case LabelFormatPDF, LabelFormatPNG:
	default:
		return nil, unsupportedFormat("DPD", req.Format, LabelFormatPDF, LabelFormatPNG)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("dpd: tracking number must not be empty")
	}

	payload := map[string]any{
		"parcelNumbers": []string{req.TrackingNumber},
		"downloadLabel": true,
		"labelFormat":   dpdLabelFormat(req.Format),
		"paperSize":     "A6",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dpd: marshal label request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/shipments/labels", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dpd: create label request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dpd: label request failed: %w", err)
	}
	raw, err := readBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dpd: read label response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dpd: label API returned %d: %s", resp.StatusCode, string(raw))
	}

	var labelResp struct {
		Pages []struct {
			BinaryData []byte `json:"binaryData"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(raw, &labelResp); err != nil {
		return nil, fmt.Errorf("dpd: decode label response: %w", err)
	}
	if len(labelResp.Pages) == 0 {
		return nil, fmt.Errorf("dpd: label response contained no pages")
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dpd",
		Format:         req.Format,
		Data:           base64.StdEncoding.EncodeToString(labelResp.Pages[0].BinaryData),
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// UpdateShipment is not supported by the DPD Baltic API.
// Cancel and rebook instead.
func (a *DPDAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DPD", "post-booking update", "cancel and rebook")
}

// BookPickup schedules a courier collection via POST /pickups.
//
// DPD requires pickupTimeFrom and pickupTimeTo in HH:mm format with minutes
// on the half-hour (00 or 30). The address block is mandatory.
// shipmentUuids links already-booked shipments to the pickup; pass ShipmentIDs
// from BookingResponse. When none are provided the count/weight fallback is used.
func (a *DPDAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("dpd: pickup.date is required")
	}
	if req.Pickup.ReadyTime == "" || req.Pickup.CloseTime == "" {
		return nil, fmt.Errorf("dpd: pickup.readyTime and pickup.closeTime are required for DPD")
	}

	addr := map[string]any{
		"name":        req.Contact.Name,
		"contactName": req.Contact.Name,
		"phone":       req.Contact.Phone,
		"street":      req.Address.Street,
		"city":        req.Address.City,
		"postalCode":  req.Address.PostalCode,
		"country":     req.Address.Country,
	}
	if req.Contact.Email != "" {
		addr["email"] = req.Contact.Email
	}
	if req.Address.HouseNumber != "" {
		addr["streetNo"] = req.Address.HouseNumber
	}

	payload := map[string]any{
		"pickupDate":     req.Pickup.Date,
		"pickupTimeFrom": req.Pickup.ReadyTime,
		"pickupTimeTo":   req.Pickup.CloseTime,
		"address":        addr,
	}

	if len(req.TrackingNumbers) > 0 {
		payload["shipmentUuids"] = req.TrackingNumbers
	} else {
		count := req.EstimatedParcels
		if count == 0 {
			count = 1
		}
		weight := req.EstimatedWeight
		if weight == 0 {
			weight = 1.0
		}
		payload["parcel"] = map[string]any{
			"count":  count,
			"weight": weight / float64(count),
		}
	}

	if req.Pickup.SpecialInstructions != "" {
		payload["messageToCourier"] = req.Pickup.SpecialInstructions
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dpd: marshal pickup request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/pickups", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dpd: create pickup request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dpd: pickup request failed: %w", err)
	}
	raw, err := readBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dpd: read pickup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("dpd: pickup API returned %d: %s", resp.StatusCode, string(raw))
	}

	// DPD echoes the request back with confirmed times.
	var dpdResp struct {
		PickupDateFrom string `json:"pickupDateFrom"`
		PickupDateTo   string `json:"pickupDateTo"`
	}
	_ = json.Unmarshal(raw, &dpdResp) // best-effort; confirmed times may not be present

	a.log.Info("DPD pickup booked",
		zap.String("date", req.Pickup.Date),
		zap.String("from", req.Pickup.ReadyTime),
		zap.String("to", req.Pickup.CloseTime),
	)

	return &PickupResponse{
		Carrier:            "dpd",
		ConfirmationNumber: req.Pickup.Date + "T" + req.Pickup.ReadyTime, // DPD does not issue a pickup ID
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by the DPD Baltic API.
// Cancel and rebook instead.
func (a *DPDAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DPD", "pickup update", "cancel and rebook")
}

// CancelPickup is not supported by the DPD Baltic API v1.
func (a *DPDAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DPD", "pickup cancellation", "not available in DPD Baltic API v1")
}

// CloseManifest is not supported by the DPD API.
// Pickup creation via BookPickup serves as the handover instruction.
func (a *DPDAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("DPD", "manifest close", "pickup creation serves as the handover instruction")
}

// GetPickupAvailability is not supported by DPD.
// DPD does not expose a pre-flight timeslot availability endpoint.
func (a *DPDAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DPD", "pickup availability", "")
}

// dpdLabelFormat maps LabelFormat to the DPD labelFormat string.
func dpdLabelFormat(f LabelFormat) string {
	switch f {
	case LabelFormatPNG:
		return "image/png"
	default:
		return "application/pdf"
	}
}

// normalizeDPDStatus maps DPD §6.1.4 statusCode + serviceCode + prevStatusCode
// to a gateway TrackingStatus. statusCode 13 means delivered to consignee OR
// returned to sender depending on serviceCode, so all three fields are required
// for an accurate mapping.
func normalizeDPDStatus(statusCode, serviceCode, prevStatusCode string) TrackingStatus {
	switch statusCode {
	case "01", "02", "05":
		return StatusPickedUp
	case "03":
		// serviceCode 298–301, 332 means outbound for return to sender.
		switch serviceCode {
		case "298", "299", "300", "301", "332":
			return StatusReturned
		}
		return StatusOutForDelivery
	case "04", "08", "14":
		return StatusFailed
	case "06", "09":
		return StatusInTransit
	case "10", "20":
		return StatusInTransit
	case "13":
		switch serviceCode {
		case "298", "299", "300", "301", "332":
			return StatusReturned
		}
		return StatusDelivered
	case "15":
		return StatusPickedUp
	case "23", "DODEI", "DOPKY":
		return StatusInTransit
	case "DODEY":
		return StatusDelivered
	case "DEYY":
		switch prevStatusCode {
		case "13", "03":
			return StatusDelivered
		case "04":
			return StatusFailed
		}
		return StatusInTransit
	}
	return StatusUnknown
}
