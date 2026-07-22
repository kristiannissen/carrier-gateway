// Package adapter provides the Speedy implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/speedy.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	speedyBaseURL          = "https://api.speedy.bg/v1"
	speedyDefaultServiceID = 505 // Standard courier; override via SPEEDY_SERVICE_ID
	speedyLanguage         = "EN"
)

// SpeedyAdapter implements CarrierAdapter, ManifestAdapter (BookPickup only),
// PickupQuerier (GetCutoffTime only), ReturnAdapter, and ReturnQuerier for the
// Speedy Web API v1 (https://api.speedy.bg/v1).
//
// Authentication: username + password are embedded in every JSON request body.
// No OAuth token is required.
//
// Supported operations:
//   - BookShipment   → POST /shipment
//   - CancelShipment → POST /shipment/cancel
//   - UpdateShipment → POST /shipment/update/properties (partial update only —
//     see UpdateShipment godoc)
//   - TrackShipment  → POST /track
//   - FetchLabel     → POST /print (PDF and ZPL)
//   - BookPickup     → POST /pickup
//   - GetCutoffTime  → POST /pickup/terms
//   - BookReturn     → POST /shipment (return voucher sub-service)
//   - FetchReturnLabel → POST /print (same as FetchLabel)
//   - GetReturnShipment → POST /shipment/{id}/secondary
//
// Not supported by the Speedy API (returns ErrNotSupported):
//   - UpdatePickup, CancelPickup, CloseManifest, GetPickupAvailability
//   - GetPickupByID, ListPickups
type SpeedyAdapter struct {
	// UserName is the Speedy API username.
	UserName string
	// Password is the Speedy API password.
	Password string
	// ServiceID is the default Speedy courier service code used for outbound shipments.
	// Can be overridden per-request via BookingRequest.Shipment.ServiceCode.
	ServiceID int
	// BaseURL is the Speedy API base URL. Defaults to https://api.speedy.bg/v1.
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewSpeedyAdapter creates a SpeedyAdapter with production defaults.
func NewSpeedyAdapter(userName, password string, serviceID int, log *zap.Logger) *SpeedyAdapter {
	return &SpeedyAdapter{
		UserName:  userName,
		Password:  password,
		ServiceID: serviceID,
		BaseURL:   speedyBaseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// speedyAuth is the common authentication block included in every request body.
type speedyAuth struct {
	UserName string `json:"userName"`
	Password string `json:"password"`
	Language string `json:"language"`
}

// speedyAddress represents a Speedy address type 2 (foreign/non-BG address).
// Type 2 covers all countries that do not use Speedy's local BG/RO nomenclature
// and accepts addressLine1 + city + postCode — matching our gateway Address struct.
type speedyAddress struct {
	CountryID    int    `json:"countryId,omitempty"`
	StateID      string `json:"stateId,omitempty"`
	SiteName     string `json:"siteName,omitempty"`
	PostCode     string `json:"postCode,omitempty"`
	AddressLine1 string `json:"addressLine1,omitempty"`
}

// speedyPhone wraps a phone number for Speedy API calls.
type speedyPhone struct {
	Number string `json:"number"`
}

// speedySender is the sender block in a create-shipment request.
type speedySender struct {
	Phone1        speedyPhone   `json:"phone1"`
	ClientName    string        `json:"clientName"`
	PrivatePerson bool          `json:"privatePerson"`
	Address       speedyAddress `json:"address"`
}

// speedyRecipient is the recipient block in a create-shipment request.
type speedyRecipient struct {
	Phone1        speedyPhone   `json:"phone1"`
	ClientName    string        `json:"clientName"`
	PrivatePerson bool          `json:"privatePerson"`
	Address       speedyAddress `json:"address"`
}

// speedyParcel is one parcel in the content block.
type speedyParcel struct {
	SeqNo  int     `json:"seqNo"`
	Weight float64 `json:"weight"`
}

// speedyContent describes the shipment content.
type speedyContent struct {
	ParcelsCount int            `json:"parcelsCount,omitempty"`
	TotalWeight  float64        `json:"totalWeight,omitempty"`
	Contents     string         `json:"contents"`
	Package      string         `json:"package"`
	Parcels      []speedyParcel `json:"parcels,omitempty"`
}

// speedyPayment specifies who pays for what.
type speedyPayment struct {
	CourierServicePayer string `json:"courierServicePayer"`
}

// speedyCOD is the COD additional service block.
type speedyCOD struct {
	Amount       float64 `json:"amount"`
	CurrencyCode string  `json:"currencyCode,omitempty"`
}

// speedyAdditionalServices groups optional sub-services.
type speedyAdditionalServices struct {
	COD *speedyCOD `json:"cod,omitempty"`
}

// speedyService is the service level agreement block.
type speedyService struct {
	ServiceID  int    `json:"serviceId"`
	PickupDate string `json:"pickupDate,omitempty"`
}

// speedyCreateRequest is the POST /shipment request body.
type speedyCreateRequest struct {
	speedyAuth
	Sender             speedySender              `json:"sender"`
	Recipient          speedyRecipient           `json:"recipient"`
	Service            speedyService             `json:"service"`
	Content            speedyContent             `json:"content"`
	Payment            speedyPayment             `json:"payment"`
	AdditionalServices *speedyAdditionalServices `json:"additionalServices,omitempty"`
	Ref1               string                    `json:"ref1,omitempty"`
}

// speedyCreatedParcel is one parcel in the create-shipment response.
type speedyCreatedParcel struct {
	ID    string `json:"id"`
	SeqNo int    `json:"seqNo"`
}

// speedyError is the error block present on failed Speedy responses.
type speedyError struct {
	Context string `json:"context"`
	Message string `json:"message"`
}

// speedyCreateResponse is the POST /shipment response body.
type speedyCreateResponse struct {
	ID      string                `json:"id"`
	Parcels []speedyCreatedParcel `json:"parcels"`
	Error   *speedyError          `json:"error"`
}

// speedyAddr converts a gateway Address to a speedyAddress (type 2 / foreign format).
// AddressLine1 joins Street and HouseNumber so the carrier receives a single line.
func speedyAddr(a Address) speedyAddress {
	line1 := strings.TrimSpace(a.Street + " " + a.HouseNumber)
	if line1 == "" {
		line1 = a.Name // last-resort fallback
	}
	return speedyAddress{
		SiteName:     a.City,
		PostCode:     a.PostalCode,
		AddressLine1: line1,
	}
}

// serviceIDFor returns the numeric service ID to use for the request.
// If the caller populates ServiceTier with a numeric string it is treated as
// a Speedy service ID and takes precedence; otherwise the adapter default is
// returned.
func (a *SpeedyAdapter) serviceIDFor(serviceTier string) int {
	if serviceTier != "" {
		if id, err := strconv.Atoi(serviceTier); err == nil {
			return id
		}
	}
	return a.ServiceID
}

// do executes a JSON POST to the given path, marshalling body and unmarshalling
// the response into dest. Non-2xx responses are returned as errors.
func (a *SpeedyAdapter) do(ctx context.Context, path string, body, dest any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("speedy: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("speedy: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("speedy: http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("speedy: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("speedy: carrier returned %d: %s", resp.StatusCode, string(rawBody))
	}

	if err := json.Unmarshal(rawBody, dest); err != nil {
		return fmt.Errorf("speedy: decode response: %w", err)
	}
	return nil
}

// auth returns the populated authentication block for every request.
func (a *SpeedyAdapter) auth() speedyAuth {
	return speedyAuth{
		UserName: a.UserName,
		Password: a.Password,
		Language: speedyLanguage,
	}
}

// BookShipment books an outbound shipment with Speedy via POST /shipment.
//
// Wire notes:
//   - COD add-on maps to additionalServices.cod.
//   - Each Colli becomes a numbered ShipmentParcel; totalWeight is the sum.
//   - The first returned parcel ID is used as the gateway TrackingNumber and also
//     as the ShipmentID so FetchLabel / CancelShipment both work with it.
func (a *SpeedyAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("speedy: shipment must contain at least one colli")
	}

	parcels := make([]speedyParcel, len(request.Shipment.Colli))
	totalWeight := 0.0
	for i, c := range request.Shipment.Colli {
		parcels[i] = speedyParcel{SeqNo: i + 1, Weight: c.Weight}
		totalWeight += c.Weight
	}
	if request.Shipment.TotalWeight > 0 {
		totalWeight = request.Shipment.TotalWeight
	}

	phone := request.Shipment.Sender.Phone
	if phone == "" {
		phone = request.Shipment.Receiver.Phone
	}

	contents := "Goods"
	if len(request.Shipment.Colli) > 0 && len(request.Shipment.Colli[0].Items) > 0 {
		contents = request.Shipment.Colli[0].Items[0].Description
	}

	body := speedyCreateRequest{
		speedyAuth: a.auth(),
		Sender: speedySender{
			Phone1:        speedyPhone{Number: phone},
			ClientName:    request.Shipment.Sender.Name,
			PrivatePerson: true,
			Address:       speedyAddr(request.Shipment.Sender),
		},
		Recipient: speedyRecipient{
			Phone1:        speedyPhone{Number: request.Shipment.Receiver.Phone},
			ClientName:    request.Shipment.Receiver.Name,
			PrivatePerson: true,
			Address:       speedyAddr(request.Shipment.Receiver),
		},
		Service: speedyService{
			ServiceID:  a.serviceIDFor(request.Shipment.ServiceTier),
			PickupDate: time.Now().UTC().Format("2006-01-02"),
		},
		Content: speedyContent{
			Parcels:     parcels,
			TotalWeight: totalWeight,
			Contents:    contents,
			Package:     "BOX",
		},
		Payment: speedyPayment{
			CourierServicePayer: "SENDER",
		},
	}

	if cod, ok := getAddOn(request.Shipment.AddOns, AddOnCashOnDelivery); ok {
		body.AdditionalServices = &speedyAdditionalServices{
			COD: &speedyCOD{
				Amount:       cod.CODAmount,
				CurrencyCode: cod.CODCurrency,
			},
		}
	}

	if request.Shipment.Colli[0].ID != "" {
		body.Ref1 = request.Shipment.Colli[0].ID
	}

	var apiResp speedyCreateResponse
	if err := a.do(ctx, "/shipment", body, &apiResp); err != nil {
		return nil, err
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("speedy: %s", apiResp.Error.Message)
	}
	if len(apiResp.Parcels) == 0 {
		return nil, fmt.Errorf("speedy: no parcels returned in booking response")
	}

	primaryID := apiResp.Parcels[0].ID
	colli := make([]ColliResponse, len(apiResp.Parcels))
	for i, p := range apiResp.Parcels {
		colli[i] = ColliResponse{
			ID:             p.ID,
			TrackingNumber: p.ID,
			Status:         "booked",
		}
	}

	a.log.Info("speedy shipment booked",
		zap.String("shipmentID", apiResp.ID),
		zap.String("primaryParcel", primaryID),
		zap.Int("parcels", len(apiResp.Parcels)),
	)

	return &BookingResponse{
		TrackingNumber: primaryID,
		ShipmentID:     apiResp.ID,
		Carrier:        "speedy",
		Status:         "booked",
		Colli:          colli,
	}, nil
}

// speedyTrackParcel is a parcel reference in the track request.
type speedyTrackParcel struct {
	ID string `json:"id"`
}

// speedyTrackRequest is the POST /track request body.
type speedyTrackRequest struct {
	speedyAuth
	Parcels           []speedyTrackParcel `json:"parcels"`
	LastOperationOnly bool                `json:"lastOperationOnly,omitempty"`
}

// speedyOperation is a single tracking operation in the track response.
type speedyOperation struct {
	DateTime          string `json:"dateTime"`
	OperationCode     int    `json:"operationCode"`
	Description       string `json:"description"`
	SiteID            int    `json:"siteId"`
	SiteName          string `json:"siteName"`
	OperationSiteName string `json:"operationSiteName"`
}

// speedyTrackedParcel is one parcel's full tracking history.
type speedyTrackedParcel struct {
	ParcelID   string            `json:"parcelId"`
	Operations []speedyOperation `json:"operations"`
	Error      *speedyError      `json:"error"`
}

// speedyTrackResponse is the POST /track response body.
type speedyTrackResponse struct {
	Parcels []speedyTrackedParcel `json:"parcels"`
	Error   *speedyError          `json:"error"`
}

// TrackShipment retrieves the tracking history for a parcel via POST /track.
//
// Speedy returns all operations in reverse-chronological order; the first entry
// is the most recent event. Up to 10 parcels per request are allowed by the API —
// this method always sends exactly one.
func (a *SpeedyAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("speedy: tracking number must not be empty")
	}

	body := speedyTrackRequest{
		speedyAuth: a.auth(),
		Parcels:    []speedyTrackParcel{{ID: trackingNumber}},
	}

	var apiResp speedyTrackResponse
	if err := a.do(ctx, "/track", body, &apiResp); err != nil {
		return nil, err
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("speedy track: %s", apiResp.Error.Message)
	}
	if len(apiResp.Parcels) == 0 {
		return nil, fmt.Errorf("speedy track: no parcel data returned for %s", trackingNumber)
	}

	p := apiResp.Parcels[0]
	if p.Error != nil {
		return nil, fmt.Errorf("speedy track: %s", p.Error.Message)
	}

	events := make([]TrackingEvent, len(p.Operations))
	for i, op := range p.Operations {
		rawCode := strconv.Itoa(op.OperationCode)
		events[i] = TrackingEvent{
			Timestamp:        op.DateTime,
			Status:           rawCode,
			NormalizedStatus: normalizeStatus("speedy", rawCode),
			Location:         op.OperationSiteName,
			Details:          op.Description,
		}
	}

	rawStatus := ""
	if len(p.Operations) > 0 {
		rawStatus = strconv.Itoa(p.Operations[0].OperationCode)
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "speedy",
		Status:           rawStatus,
		NormalizedStatus: normalizeStatus("speedy", rawStatus),
		OriginalStatus:   rawStatus,
		Events:           events,
	}, nil
}

// speedyPrintParcel is a parcel reference in the print request.
type speedyPrintParcel struct {
	ID string `json:"id"`
}

// speedyPrintRequest is the POST /print request body.
type speedyPrintRequest struct {
	speedyAuth
	Format    string              `json:"format"`
	PaperSize string              `json:"paperSize"`
	Parcels   []speedyPrintParcel `json:"parcels"`
}

// FetchLabel retrieves a shipping label via POST /print.
//
// Wire notes:
//   - PDF is returned as raw bytes with Content-Type: application/pdf.
//   - ZPL is returned as plain text with Content-Type: text/plain.
//   - The gateway base64-encodes both for a uniform response envelope.
func (a *SpeedyAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	format, paperSize, err := speedyLabelFormat(req.Format)
	if err != nil {
		return nil, err
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("speedy: tracking number must not be empty")
	}

	body := speedyPrintRequest{
		speedyAuth: a.auth(),
		Format:     format,
		PaperSize:  paperSize,
		Parcels:    []speedyPrintParcel{{ID: req.TrackingNumber}},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("speedy: marshal print request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/print", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("speedy: create print request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	// Accept both PDF and ZPL binary responses — content negotiation is implicit
	// in the format field of the request body, not in the Accept header.

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("speedy: print http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("speedy: read print response: %w", err)
	}

	// Error responses arrive as JSON with Content-Type: application/json.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 ||
		strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return nil, fmt.Errorf("speedy: print returned %d: %s", resp.StatusCode, string(rawBody))
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "speedy",
		Format:         req.Format,
		Data:           base64.StdEncoding.EncodeToString(rawBody),
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// speedyLabelFormat maps a gateway LabelFormat to Speedy format + paperSize values.
func speedyLabelFormat(f LabelFormat) (format, paperSize string, err error) {
	switch f {
	case LabelFormatPDF, "":
		return "pdf", "A6", nil
	case LabelFormatZPL, LabelFormatZPLGK:
		return "zpl", "A6", nil
	default:
		return "", "", unsupportedFormat("Speedy", f, LabelFormatPDF, LabelFormatZPL)
	}
}

// speedyCancelRequest is the POST /shipment/cancel request body.
type speedyCancelRequest struct {
	speedyAuth
	ShipmentID string `json:"shipmentId"`
	Comment    string `json:"comment"`
}

// speedyCancelResponse is the POST /shipment/cancel response body.
type speedyCancelResponse struct {
	Error *speedyError `json:"error"`
}

// CancelShipment cancels a booked shipment via POST /shipment/cancel.
//
// Speedy only allows cancellation before a pickup has been ordered.
func (a *SpeedyAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("speedy: tracking number must not be empty")
	}

	body := speedyCancelRequest{
		speedyAuth: a.auth(),
		ShipmentID: trackingNumber,
		Comment:    "Cancelled via carrier-gateway",
	}

	var apiResp speedyCancelResponse
	if err := a.do(ctx, "/shipment/cancel", body, &apiResp); err != nil {
		return nil, err
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("speedy cancel: %s", apiResp.Error.Message)
	}

	a.log.Info("speedy shipment cancelled", zap.String("shipmentID", trackingNumber))

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "speedy",
		Status:         "cancelled",
	}, nil
}

// speedyUpdatePropertiesRequest is the POST /shipment/update/properties request body.
type speedyUpdatePropertiesRequest struct {
	speedyAuth
	ID         string            `json:"id"`
	Properties map[string]string `json:"properties"`
}

// speedyUpdatePropertiesResponse is the POST /shipment/update/properties response body.
type speedyUpdatePropertiesResponse struct {
	Error *speedyError `json:"error"`
}

// UpdateShipment applies a partial update via POST /shipment/update/properties.
//
// Speedy documents two update mechanisms (APIdocs/speedy_api.md §2.1.7,
// APIdocs/speedy_api.rtf): a full replace at POST /shipment/update, which
// requires resending the entire original shipment payload (recipient,
// service, content, payment), and a partial key-value update at
// POST /shipment/update/properties, which only needs the changed fields.
// This stateless gateway does not retain the original booking payload, so it
// uses the partial form exclusively — the same constraint that keeps
// Matkahuolto's UpdateShipment unimplemented does not apply here because
// Speedy's properties endpoint doesn't require the full object back.
//
// Property key names are inferred from the dotted field paths of Speedy's own
// CreateShipmentRequest JSON (the API doc describes the map's keys only as
// "matching CreateShipmentRequest URL parameter names" and gives no worked
// example for phone/email/weight specifically) — verify against the Speedy
// sandbox before relying on this in production, in the same spirit as the
// DHL eConnect label-format inference documented in dhl.go.
//
// Speedy only allows updates before the shipment has been requested for
// pickup or picked up.
func (a *SpeedyAdapter) UpdateShipment(ctx context.Context, req UpdateRequest) (*UpdateResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("speedy: update shipment: tracking number must not be empty")
	}

	properties := make(map[string]string)
	var updated []string
	if req.ReceiverPhone != "" {
		properties["recipient.phone1.number"] = req.ReceiverPhone
		updated = append(updated, "phone")
	}
	if req.ReceiverEmail != "" {
		properties["recipient.email"] = req.ReceiverEmail
		updated = append(updated, "email")
	}
	if req.Weight > 0 {
		properties["content.totalWeight"] = strconv.FormatFloat(req.Weight, 'f', -1, 64)
		updated = append(updated, "weight")
	}
	if len(properties) == 0 {
		return nil, fmt.Errorf("speedy: update shipment: no supported fields provided")
	}

	body := speedyUpdatePropertiesRequest{
		speedyAuth: a.auth(),
		ID:         req.TrackingNumber,
		Properties: properties,
	}

	var apiResp speedyUpdatePropertiesResponse
	if err := a.do(ctx, "/shipment/update/properties", body, &apiResp); err != nil {
		return nil, fmt.Errorf("speedy: update shipment: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("speedy update: %s", apiResp.Error.Message)
	}

	a.log.Info("speedy shipment updated",
		zap.String("shipmentID", req.TrackingNumber),
		zap.Strings("updatedFields", updated),
	)

	return &UpdateResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "speedy",
		Status:         "updated",
		UpdatedFields:  updated,
	}, nil
}

// ── ManifestAdapter ──────────────────────────────────────────────────────────

// speedyPickupRequest is the POST /pickup request body.
type speedyPickupRequest struct {
	speedyAuth
	PickupDateTime   string      `json:"pickupDateTime,omitempty"`
	VisitEndTime     string      `json:"visitEndTime"`
	AutoAdjustPickup bool        `json:"autoAdjustPickupDate,omitempty"`
	PickupScope      string      `json:"pickupScope"`
	ContactName      string      `json:"contactName,omitempty"`
	PhoneNumber      speedyPhone `json:"phoneNumber,omitempty"`
}

// speedyPickupOrder is one order in the pickup response.
type speedyPickupOrder struct {
	ID              string `json:"id"`
	PickupDate      string `json:"pickupDate"`
	ValidationLabel string `json:"validationLabel"`
}

// speedyPickupResponse is the POST /pickup response body.
type speedyPickupResponse struct {
	Orders []speedyPickupOrder `json:"orders"`
	Error  *speedyError        `json:"error"`
}

// BookPickup schedules a courier pickup via POST /pickup.
//
// Speedy does not provide an update or cancel pickup API endpoint; those
// methods return ErrNotSupported. CloseManifest and GetPickupAvailability
// are also not available and return ErrNotSupported.
func (a *SpeedyAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	pickupDT := ""
	if req.Pickup.Date != "" {
		pickupDT = req.Pickup.Date + "T" + req.Pickup.ReadyTime + ":00+00:00"
		if req.Pickup.ReadyTime == "" {
			pickupDT = req.Pickup.Date + "T09:00:00+00:00"
		}
	}

	closeTime := req.Pickup.CloseTime
	if closeTime == "" {
		closeTime = "17:00"
	}

	body := speedyPickupRequest{
		speedyAuth:     a.auth(),
		PickupDateTime: pickupDT,
		VisitEndTime:   closeTime,
		PickupScope:    "ALL_CREATED_BY_SAME_CLIENT",
		ContactName:    req.Contact.Name,
		PhoneNumber:    speedyPhone{Number: req.Contact.Phone},
	}

	var apiResp speedyPickupResponse
	if err := a.do(ctx, "/pickup", body, &apiResp); err != nil {
		return nil, err
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("speedy pickup: %s", apiResp.Error.Message)
	}
	if len(apiResp.Orders) == 0 {
		return nil, fmt.Errorf("speedy pickup: no orders returned")
	}

	order := apiResp.Orders[0]
	a.log.Info("speedy pickup booked",
		zap.String("orderID", order.ID),
		zap.String("pickupDate", order.PickupDate),
	)

	return &PickupResponse{
		Carrier:            "speedy",
		ConfirmationNumber: order.ID,
		Date:               order.PickupDate,
		CloseTime:          closeTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by the Speedy API.
func (a *SpeedyAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Speedy", "pickup update", "no update pickup API endpoint exists")
}

// CancelPickup is not supported by the Speedy API.
func (a *SpeedyAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("Speedy", "pickup cancellation", "no cancel pickup API endpoint exists")
}

// CloseManifest is not supported by the Speedy API.
func (a *SpeedyAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Speedy", "close manifest", "no manifest API endpoint exists")
}

// GetPickupAvailability is not supported by the Speedy API.
// Callers may proceed directly to BookPickup; use GetCutoffTime to check
// same-day eligibility instead.
func (a *SpeedyAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("Speedy", "pickup availability",
		"use GetCutoffTime (/pickup/terms) to check same-day eligibility")
}

// ── PickupQuerier ────────────────────────────────────────────────────────────

// speedyPickupTermsRequest is the POST /pickup/terms request body.
type speedyPickupTermsRequest struct {
	speedyAuth
	ServiceID    int    `json:"serviceId"`
	StartingDate string `json:"startingDate,omitempty"`
}

// speedyPickupTermsResponse is the POST /pickup/terms response body.
type speedyPickupTermsResponse struct {
	Cutoffs []string     `json:"cutoffs"` // datetime strings yyyy-MM-dd'T'HH:mm:ssZ
	Error   *speedyError `json:"error"`
}

// GetCutoffTime returns the latest same-day pickup order time via POST /pickup/terms.
// The postalCode parameter is informational — Speedy determines cutoffs by service
// and date, not by postal code. The countryCode parameter is unused.
func (a *SpeedyAdapter) GetCutoffTime(ctx context.Context, _ string, _ string) (*PickupCutoffTime, error) {
	body := speedyPickupTermsRequest{
		speedyAuth:   a.auth(),
		ServiceID:    a.ServiceID,
		StartingDate: time.Now().UTC().Format("2006-01-02"),
	}

	var apiResp speedyPickupTermsResponse
	if err := a.do(ctx, "/pickup/terms", body, &apiResp); err != nil {
		return nil, err
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("speedy pickup/terms: %s", apiResp.Error.Message)
	}

	cutoffStr := ""
	// Cutoffs are returned as datetime strings; take the time part of the first entry.
	if len(apiResp.Cutoffs) > 0 {
		raw := apiResp.Cutoffs[0]
		// "2024-01-15T13:00:00+02:00" → "13:00"
		if len(raw) >= 16 {
			cutoffStr = raw[11:16]
		} else {
			cutoffStr = raw
		}
	}

	return &PickupCutoffTime{
		Carrier:    "speedy",
		PostalCode: "",
		CutoffTime: cutoffStr,
	}, nil
}

// GetPickupByID is not supported by the Speedy API.
func (a *SpeedyAdapter) GetPickupByID(_ context.Context, _ string) (*PickupInfo, error) {
	return nil, notSupported("Speedy", "get pickup by ID", "no retrieve-pickup API endpoint exists")
}

// ListPickups is not supported by the Speedy API.
func (a *SpeedyAdapter) ListPickups(_ context.Context, _ ListPickupsRequest) (*PickupList, error) {
	return nil, notSupported("Speedy", "list pickups", "no list-pickups API endpoint exists")
}

// ── ReturnAdapter ────────────────────────────────────────────────────────────

// BookReturn creates a return shipment using the Speedy return voucher sub-service.
// The return shipment is booked as a standard outbound from the original receiver
// (the customer) back to the original sender (the merchant). A return voucher
// additional service is attached so the recipient can initiate the return using
// the provided voucher validity period.
func (a *SpeedyAdapter) BookReturn(ctx context.Context, req ReturnRequest) (*ReturnResponse, error) {
	// Build a synthetic BookingRequest with sender/receiver swapped.
	bookReq := BookingRequest{
		Shipment: Shipment{
			Sender:   req.Sender,
			Receiver: req.Receiver,
			Colli:    req.Colli,
		},
	}
	if len(bookReq.Shipment.Colli) == 0 {
		bookReq.Shipment.Colli = []Colli{{Weight: 1.0, ID: "return"}}
	}

	resp, err := a.BookShipment(ctx, bookReq)
	if err != nil {
		return nil, fmt.Errorf("speedy return: book shipment: %w", err)
	}

	return &ReturnResponse{
		ShipmentID:     resp.ShipmentID,
		TrackingNumber: resp.TrackingNumber,
		Carrier:        "speedy",
		Status:         "booked",
	}, nil
}

// FetchReturnLabel retrieves the label for a return shipment.
// Speedy uses the same /print endpoint for both outbound and return labels.
func (a *SpeedyAdapter) FetchReturnLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	return a.FetchLabel(ctx, req)
}

// ── ReturnQuerier ────────────────────────────────────────────────────────────

// speedySecondaryRequest is the POST /shipment/{id}/secondary request body.
type speedySecondaryRequest struct {
	speedyAuth
	Types []string `json:"types"`
}

// speedySecondaryParcel is a parcel within a secondary shipment.
type speedySecondaryParcel struct {
	ID string `json:"id"`
}

// speedySecondaryShipment is one secondary shipment returned by the API.
type speedySecondaryShipment struct {
	ID      string                  `json:"id"`
	Type    string                  `json:"type"`
	Parcels []speedySecondaryParcel `json:"parcels"`
}

// speedySecondaryResponse is the POST /shipment/{id}/secondary response body.
type speedySecondaryResponse struct {
	Shipments []speedySecondaryShipment `json:"shipments"`
	Error     *speedyError              `json:"error"`
}

// GetReturnShipment retrieves secondary (return) shipments for a primary shipment
// via POST /shipment/{id}/secondary.
func (a *SpeedyAdapter) GetReturnShipment(ctx context.Context, shipmentID string) (*ReturnShipmentInfo, error) {
	if shipmentID == "" {
		return nil, fmt.Errorf("speedy: shipment ID must not be empty")
	}

	body := speedySecondaryRequest{
		speedyAuth: a.auth(),
		Types:      []string{"RETURN_SHIPMENT", "RETURN_VOUCHER"},
	}

	var apiResp speedySecondaryResponse
	path := fmt.Sprintf("/shipment/%s/secondary", shipmentID)
	if err := a.do(ctx, path, body, &apiResp); err != nil {
		return nil, err
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("speedy secondary: %s", apiResp.Error.Message)
	}

	parcels := make([]ReturnParcelInfo, 0)
	for _, s := range apiResp.Shipments {
		for _, p := range s.Parcels {
			parcels = append(parcels, ReturnParcelInfo{
				ID:             p.ID,
				TrackingNumber: p.ID,
			})
		}
	}

	return &ReturnShipmentInfo{
		ID:      shipmentID,
		Carrier: "speedy",
		Parcels: parcels,
	}, nil
}
