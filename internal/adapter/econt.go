// Package adapter provides the Econt Express implementation of CarrierAdapter.
// This file is located at /internal/adapter/econt.go.
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
	"time"

	"go.uber.org/zap"
)

const (
	econtLiveBase = "https://ee.econt.com/services"
	econtTestBase = "https://demo.econt.com/ee/services"

	econtPathCreateLabel   = "/Shipments/LabelService.createLabel.json"
	econtPathDeleteLabels  = "/Shipments/LabelService.deleteLabels.json"
	econtPathUpdateLabel   = "/Shipments/LabelService.updateLabel.json"
	econtPathCheckEditions = "/Shipments/LabelService.checkPossibleShipmentEditions.json"
	econtPathGetStatuses   = "/Shipments/ShipmentService.getShipmentStatuses.json"

	// econtShipmentTypePack is the default shipment type for standard parcels.
	econtShipmentTypePack = "pack"
	// econtShipmentTypeDocument is used for light shipments up to 0.5 kg.
	econtShipmentTypeDocument = "document"
)

// econtRoundTripper injects Basic Auth and Content-Type on every outgoing request.
type econtRoundTripper struct {
	inner    http.RoundTripper
	username string
	password string
}

// RoundTrip implements http.RoundTripper.
func (rt *econtRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.SetBasicAuth(rt.username, rt.password)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	return rt.inner.RoundTrip(r)
}

// ── wire format types ─────────────────────────────────────────────────────────

type econtError struct {
	Type        string       `json:"type,omitempty"`
	Message     string       `json:"message,omitempty"`
	Fields      []string     `json:"fields,omitempty"`
	InnerErrors []econtError `json:"innerErrors,omitempty"`
}

func (e *econtError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("econt: %s: %s", e.Type, e.Message)
}

type econtCountry struct {
	Code2 string `json:"code2,omitempty"`
}

type econtCity struct {
	PostCode string        `json:"postCode,omitempty"`
	Name     string        `json:"name,omitempty"`
	Country  *econtCountry `json:"country,omitempty"`
}

type econtAddress struct {
	City    *econtCity `json:"city,omitempty"`
	Street  string     `json:"street,omitempty"`
	Num     string     `json:"num,omitempty"`
	Quarter string     `json:"quarter,omitempty"`
	Other   string     `json:"other,omitempty"`
	Zip     string     `json:"zip,omitempty"`
}

type econtClientProfile struct {
	Name   string   `json:"name,omitempty"`
	Phones []string `json:"phones,omitempty"`
	Email  string   `json:"email,omitempty"`
}

type econtLabelServices struct {
	CDAmount              float64 `json:"cdAmount,omitempty"`
	CDType                string  `json:"cdType,omitempty"`
	CDCurrency            string  `json:"cdCurrency,omitempty"`
	DeclaredValueAmount   float64 `json:"declaredValueAmount,omitempty"`
	DeclaredValueCurrency string  `json:"declaredValueCurrency,omitempty"`
	SMSNotification       bool    `json:"smsNotification,omitempty"`
	DeliveryReceipt       bool    `json:"deliveryReceipt,omitempty"`
}

type econtCustomsItem struct {
	CN          string  `json:"cn,omitempty"`
	Description string  `json:"description,omitempty"`
	Sum         float64 `json:"sum,omitempty"`
	Currency    string  `json:"currency,omitempty"`
}

type econtShippingLabel struct {
	ShipmentNumber      string              `json:"shipmentNumber,omitempty"`
	SenderClient        *econtClientProfile `json:"senderClient,omitempty"`
	SenderAddress       *econtAddress       `json:"senderAddress,omitempty"`
	SenderOfficeCode    string              `json:"senderOfficeCode,omitempty"`
	EmailOnDelivery     string              `json:"emailOnDelivery,omitempty"`
	SmsOnDelivery       string              `json:"smsOnDelivery,omitempty"`
	ReceiverClient      *econtClientProfile `json:"receiverClient,omitempty"`
	ReceiverAddress     *econtAddress       `json:"receiverAddress,omitempty"`
	ReceiverOfficeCode  string              `json:"receiverOfficeCode,omitempty"`
	PackCount           int                 `json:"packCount,omitempty"`
	ShipmentType        string              `json:"shipmentType,omitempty"`
	Weight              float64             `json:"weight,omitempty"`
	ShipmentDescription string              `json:"shipmentDescription,omitempty"`
	OrderNumber         string              `json:"orderNumber,omitempty"`
	Services            *econtLabelServices `json:"services,omitempty"`
	CustomsList         []econtCustomsItem  `json:"customsList,omitempty"`
	CustomsInvoice      string              `json:"customsInvoice,omitempty"`
}

// ── request / response envelopes ──────────────────────────────────────────────

type econtCreateLabelRequest struct {
	Label                  *econtShippingLabel `json:"label"`
	RequestCourierTimeFrom string              `json:"requestCourierTimeFrom,omitempty"`
	RequestCourierTimeTo   string              `json:"requestCourierTimeTo,omitempty"`
	Mode                   string              `json:"mode"`
}

type econtTrackingEvent struct {
	DestinationType      string `json:"destinationType,omitempty"`
	DestinationDetailsEn string `json:"destinationDetailsEn,omitempty"`
	OfficeNameEn         string `json:"officeNameEn,omitempty"`
	CityNameEn           string `json:"cityNameEn,omitempty"`
	CountryCode          string `json:"countryCode,omitempty"`
	Time                 string `json:"time,omitempty"`
}

type econtShipmentStatus struct {
	ShipmentNumber        string               `json:"shipmentNumber,omitempty"`
	CreatedTime           string               `json:"createdTime,omitempty"`
	SendTime              string               `json:"sendTime,omitempty"`
	DeliveryTime          string               `json:"deliveryTime,omitempty"`
	ShortDeliveryStatusEn string               `json:"shortDeliveryStatusEn,omitempty"`
	ExpectedDeliveryDate  string               `json:"expectedDeliveryDate,omitempty"`
	PDFURL                string               `json:"pdfURL,omitempty"`
	TrackingEvents        []econtTrackingEvent `json:"trackingEvents,omitempty"`
	TotalPrice            float64              `json:"totalPrice,omitempty"`
	Currency              string               `json:"currency,omitempty"`
}

type econtCreateLabelResponse struct {
	Label    *econtShipmentStatus `json:"label,omitempty"`
	Error    *econtError          `json:"error,omitempty"`
}

type econtDeleteLabelsRequest struct {
	ShipmentNumbers []string `json:"shipmentNumbers"`
}

type econtDeleteLabelsResultElement struct {
	ShipmentNum string      `json:"shipmentNum,omitempty"`
	Error       *econtError `json:"error,omitempty"`
}

type econtDeleteLabelsResponse struct {
	Results []econtDeleteLabelsResultElement `json:"results"`
}

type econtGetShipmentStatusesRequest struct {
	ShipmentNumbers []string `json:"shipmentNumbers"`
}

type econtShipmentStatusResultElement struct {
	Status *econtShipmentStatus `json:"status,omitempty"`
	Error  *econtError          `json:"error,omitempty"`
}

type econtGetShipmentStatusesResponse struct {
	ShipmentStatuses []econtShipmentStatusResultElement `json:"shipmentStatuses"`
}

type econtCheckEditionsRequest struct {
	ShipmentNums []int `json:"shipmentNums"`
}

type econtCheckEditionsResultElement struct {
	ShipmentNum              int      `json:"shipmentNum,omitempty"`
	PossibleShipmentEditions []string `json:"possibleShipmentEditions,omitempty"`
}

type econtCheckEditionsResponse struct {
	PossibleShipmentEditions []econtCheckEditionsResultElement `json:"possibleShipmentEditions"`
}

type econtUpdateLabelRequest struct {
	Label *econtShippingLabel `json:"label"`
}

type econtUpdateLabelResponse struct {
	Label *econtShipmentStatus `json:"label,omitempty"`
	Error *econtError          `json:"error,omitempty"`
}

// ── EcontAdapter ──────────────────────────────────────────────────────────────

// EcontAdapter implements CarrierAdapter for the Econt Express API v1.
//
// Authentication uses HTTP Basic Auth (ECONT_USERNAME / ECONT_PASSWORD).
// All requests are HTTP POST with a JSON body; the endpoint path encodes the
// service name and method (e.g. /Shipments/LabelService.createLabel.json).
//
// FetchLabel is a two-step operation: it calls getShipmentStatuses to obtain
// the pdfURL stored on the shipment status, then downloads that URL as PDF.
// Store the pdfURL from BookShipment's response.LabelURL to avoid the extra round-trip.
//
// Cancellation via deleteLabels only succeeds before the shipment is accepted
// by Econt. Once accepted, checkPossibleShipmentEditions must be consulted
// and CancelShipment returns an error if no editions are available.
type EcontAdapter struct {
	// BaseURL is the API root. Defaults to econtLiveBase.
	BaseURL string

	client *http.Client
	log    *zap.Logger
}

// NewEcontAdapter returns a production EcontAdapter using Basic Auth credentials.
func NewEcontAdapter(username, password string, log *zap.Logger) *EcontAdapter {
	rt := &econtRoundTripper{
		inner:    http.DefaultTransport,
		username: username,
		password: password,
	}
	return &EcontAdapter{
		BaseURL: econtLiveBase,
		client:  &http.Client{Timeout: 30 * time.Second, Transport: rt},
		log:     log,
	}
}

// do executes an HTTP POST to path, serialises body as JSON, and decodes the
// response JSON into dst. A non-2xx HTTP status is returned as an error with
// the raw body included for debugging.
func (a *EcontAdapter) do(ctx context.Context, path string, body any, dst any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("econt: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("econt: build request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("econt: POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("econt: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("econt: HTTP %d: %s", resp.StatusCode, raw)
	}

	if dst != nil {
		if err := json.Unmarshal(raw, dst); err != nil {
			return fmt.Errorf("econt: decode response: %w", err)
		}
	}
	return nil
}

// fetchURL downloads a URL using the same authenticated client and returns the raw bytes.
// Used to retrieve the label PDF from pdfURL.
func (a *EcontAdapter) fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("econt: build label request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("econt: fetch label PDF: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("econt: fetch label PDF: HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("econt: read label PDF: %w", err)
	}
	return raw, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// econtAddressFrom converts a gateway Address to the Econt address wire type.
// city.name + city.postCode identify the city; country is mapped via code2.
// street + num are used when HouseNumber is present; otherwise fullAddress fallback
// is not set — Econt requires either street+num or quarter+other.
func econtAddressFrom(addr Address) *econtAddress {
	a := &econtAddress{
		City: &econtCity{
			Name:     addr.City,
			PostCode: addr.PostalCode,
		},
		Street: addr.Street,
		Num:    addr.HouseNumber,
		Zip:    addr.PostalCode,
	}
	if addr.Country != "" {
		a.City.Country = &econtCountry{Code2: addr.Country}
	}
	if addr.Supplement != "" {
		a.Other = addr.Supplement
	}
	return a
}

// econtClientFrom converts a gateway Address to the Econt client profile wire type.
func econtClientFrom(addr Address) *econtClientProfile {
	c := &econtClientProfile{Name: addr.Name}
	if addr.Phone != "" {
		c.Phones = []string{addr.Phone}
	}
	if addr.Email != "" {
		c.Email = addr.Email
	}
	return c
}

// econtShipmentType selects the Econt ShipmentType from the gateway Shipment.
// Uses document for very light single-colli shipments; defaults to pack.
func econtShipmentType(s Shipment) string {
	if s.TotalWeight <= 0.5 && len(s.Colli) == 1 && s.ServiceTier == "document" {
		return econtShipmentTypeDocument
	}
	return econtShipmentTypePack
}

// econtServices converts gateway AddOns to an Econt services block.
// Returns nil when no relevant add-ons are present.
func econtServices(addOns []AddOn, _ Address) *econtLabelServices {
	svc := &econtLabelServices{}
	for _, ao := range addOns {
		switch ao.Type {
		case AddOnCashOnDelivery:
			svc.CDAmount = ao.CODAmount
			svc.CDType = "get"
			svc.CDCurrency = ao.CODCurrency
		case AddOnInsurance:
			svc.DeclaredValueAmount = ao.InsuranceValue
			svc.DeclaredValueCurrency = ao.InsuranceCurrency
		case AddOnSMSNotification:
			svc.SMSNotification = true
		case AddOnSignatureRequired:
			svc.DeliveryReceipt = true
		}
	}
	if svc.CDAmount == 0 && svc.DeclaredValueAmount == 0 && !svc.SMSNotification && !svc.DeliveryReceipt {
		return nil
	}
	return svc
}

// econtCustomsList converts the gateway Customs block to Econt customsList entries.
// Econt requires TARIC codes (cn field), description, sum, and currency per line item.
func econtCustomsList(c Customs) []econtCustomsItem {
	if len(c.Items) == 0 {
		return nil
	}
	items := make([]econtCustomsItem, 0, len(c.Items))
	for _, item := range c.Items {
		items = append(items, econtCustomsItem{
			CN:          item.HSCode,
			Description: item.Description,
			Sum:         item.Value * float64(item.Quantity),
			Currency:    c.CustomsCurrency,
		})
	}
	return items
}

// econtLabelFrom builds an Econt ShippingLabel from a gateway BookingRequest.
func econtLabelFrom(r BookingRequest) *econtShippingLabel {
	s := r.Shipment

	packCount := len(s.Colli)
	if packCount == 0 {
		packCount = 1
	}

	label := &econtShippingLabel{
		SenderClient:   econtClientFrom(s.Sender),
		SenderAddress:  econtAddressFrom(s.Sender),
		ReceiverClient: econtClientFrom(s.Receiver),
		PackCount:      packCount,
		ShipmentType:   econtShipmentType(s),
		Weight:         s.TotalWeight,
		Services:       econtServices(s.AddOns, s.Receiver),
	}

	// Receiver routing: office code when ServicePointID is set; otherwise address.
	if s.Receiver.ServicePointID != "" {
		label.ReceiverOfficeCode = s.Receiver.ServicePointID
	} else {
		label.ReceiverAddress = econtAddressFrom(s.Receiver)
	}

	if s.Receiver.Email != "" {
		label.EmailOnDelivery = s.Receiver.Email
	}

	if len(s.Colli) > 0 && len(s.Colli[0].Items) > 0 {
		label.ShipmentDescription = s.Colli[0].Items[0].Description
	}

	if r.IdempotencyKey != "" {
		label.OrderNumber = r.IdempotencyKey
	}

	if customs := econtCustomsList(s.Customs); len(customs) > 0 {
		label.CustomsList = customs
		label.CustomsInvoice = s.Customs.InvoiceNumber
	}

	return label
}

// econtTrackingEventToGateway converts a single Econt tracking event to the gateway type.
func econtTrackingEventToGateway(e econtTrackingEvent) TrackingEvent {
	ns := normalizeEcontEventType(e.DestinationType)
	loc := e.CityNameEn
	if e.OfficeNameEn != "" {
		loc = e.OfficeNameEn
		if e.CityNameEn != "" {
			loc = e.OfficeNameEn + ", " + e.CityNameEn
		}
	}
	if e.CountryCode != "" && loc != "" {
		loc = loc + ", " + e.CountryCode
	}
	return TrackingEvent{
		Timestamp:        e.Time,
		Status:           e.DestinationType,
		NormalizedStatus: ns,
		Location:         loc,
		Details:          e.DestinationDetailsEn,
	}
}

// ── CarrierAdapter ────────────────────────────────────────────────────────────

// BookShipment creates a shipment label on the Econt platform.
//
// The shipment is created with mode=create. The returned LabelURL holds the
// pdfURL from Econt so the caller can retrieve the label directly without a
// separate FetchLabel call.
func (a *EcontAdapter) BookShipment(ctx context.Context, r BookingRequest) (*BookingResponse, error) {
	payload := econtCreateLabelRequest{
		Label: econtLabelFrom(r),
		Mode:  "create",
	}

	var result econtCreateLabelResponse
	if err := a.do(ctx, econtPathCreateLabel, payload, &result); err != nil {
		return nil, fmt.Errorf("econt: book shipment: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("econt: book shipment: %w", result.Error)
	}
	if result.Label == nil {
		return nil, fmt.Errorf("econt: book shipment: empty response from API")
	}

	num := result.Label.ShipmentNumber
	a.log.Info("econt: shipment booked", zap.String("shipmentNumber", num))

	colliID := ""
	if len(r.Shipment.Colli) > 0 {
		colliID = r.Shipment.Colli[0].ID
	}

	return &BookingResponse{
		TrackingNumber: num,
		LabelURL:       result.Label.PDFURL,
		Carrier:        "econt",
		Status:         "booked",
		Colli: []ColliResponse{{
			ID:             colliID,
			TrackingNumber: num,
			LabelURL:       result.Label.PDFURL,
			Status:         "booked",
		}},
	}, nil
}

// TrackShipment retrieves the current status and tracking events for a shipment.
func (a *EcontAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	payload := econtGetShipmentStatusesRequest{
		ShipmentNumbers: []string{trackingNumber},
	}

	var result econtGetShipmentStatusesResponse
	if err := a.do(ctx, econtPathGetStatuses, payload, &result); err != nil {
		return nil, fmt.Errorf("econt: track shipment: %w", err)
	}

	if len(result.ShipmentStatuses) == 0 {
		return nil, fmt.Errorf("econt: track shipment: no status returned for %s", trackingNumber)
	}

	elem := result.ShipmentStatuses[0]
	if elem.Error != nil {
		return nil, fmt.Errorf("econt: track shipment: %w", elem.Error)
	}
	if elem.Status == nil {
		return nil, fmt.Errorf("econt: track shipment: nil status for %s", trackingNumber)
	}

	raw := elem.Status.ShortDeliveryStatusEn
	ns := normalizeStatus("econt", raw)

	events := make([]TrackingEvent, 0, len(elem.Status.TrackingEvents))
	for _, e := range elem.Status.TrackingEvents {
		events = append(events, econtTrackingEventToGateway(e))
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "econt",
		Status:            raw,
		NormalizedStatus:  ns,
		OriginalStatus:    raw,
		EstimatedDelivery: elem.Status.ExpectedDeliveryDate,
		Events:            events,
	}, nil
}

// FetchLabel retrieves the shipping label PDF for a booked shipment.
//
// Econt does not expose a dedicated label endpoint. This method calls
// getShipmentStatuses to retrieve the pdfURL stored on the shipment, then
// downloads that URL. Only PDF format is supported.
func (a *EcontAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != "" && req.Format != LabelFormatPDF {
		return nil, fmt.Errorf("econt: only PDF labels are supported, got %s", req.Format)
	}

	payload := econtGetShipmentStatusesRequest{
		ShipmentNumbers: []string{req.TrackingNumber},
	}

	var result econtGetShipmentStatusesResponse
	if err := a.do(ctx, econtPathGetStatuses, payload, &result); err != nil {
		return nil, fmt.Errorf("econt: fetch label — get status: %w", err)
	}

	if len(result.ShipmentStatuses) == 0 {
		return nil, fmt.Errorf("econt: fetch label: shipment %s not found", req.TrackingNumber)
	}

	elem := result.ShipmentStatuses[0]
	if elem.Error != nil {
		return nil, fmt.Errorf("econt: fetch label: %w", elem.Error)
	}
	if elem.Status == nil || elem.Status.PDFURL == "" {
		return nil, fmt.Errorf("econt: fetch label: no pdfURL available for %s", req.TrackingNumber)
	}

	pdfBytes, err := a.fetchURL(ctx, elem.Status.PDFURL)
	if err != nil {
		return nil, fmt.Errorf("econt: fetch label: %w", err)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "econt",
		Format:         LabelFormatPDF,
		Data:           base64.StdEncoding.EncodeToString(pdfBytes),
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment deletes a label that has not yet been accepted by Econt.
//
// Once a shipment has been accepted (scanned at an Econt office or handed to
// a courier), deleteLabels will be rejected by the API. In that case this
// method returns a descriptive error; the caller should use the Econt portal
// to cancel the shipment manually.
func (a *EcontAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	payload := econtDeleteLabelsRequest{
		ShipmentNumbers: []string{trackingNumber},
	}

	var result econtDeleteLabelsResponse
	if err := a.do(ctx, econtPathDeleteLabels, payload, &result); err != nil {
		return nil, fmt.Errorf("econt: cancel shipment: %w", err)
	}

	if len(result.Results) > 0 && result.Results[0].Error != nil {
		e := result.Results[0].Error
		return nil, fmt.Errorf("econt: cancel shipment %s: %w", trackingNumber, e)
	}

	a.log.Info("econt: shipment cancelled", zap.String("shipmentNumber", trackingNumber))

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "econt",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment applies partial updates to a booked shipment.
//
// The method first calls checkPossibleShipmentEditions to verify the shipment
// can still be edited. If no editions are available (already accepted and in
// transit) an error is returned. Supported updates: receiver phone, receiver
// email, weight, and office code (ServicePointID).
func (a *EcontAdapter) UpdateShipment(ctx context.Context, req UpdateRequest) (*UpdateResponse, error) {
	// Econt shipment numbers are integers in the check endpoint.
	numInt, err := strconv.Atoi(req.TrackingNumber)
	if err != nil {
		return nil, fmt.Errorf("econt: update shipment: shipment number %q is not numeric: %w", req.TrackingNumber, err)
	}

	checkPayload := econtCheckEditionsRequest{ShipmentNums: []int{numInt}}
	var checkResult econtCheckEditionsResponse
	if err := a.do(ctx, econtPathCheckEditions, checkPayload, &checkResult); err != nil {
		return nil, fmt.Errorf("econt: update shipment — check editions: %w", err)
	}

	if len(checkResult.PossibleShipmentEditions) == 0 ||
		len(checkResult.PossibleShipmentEditions[0].PossibleShipmentEditions) == 0 {
		return nil, fmt.Errorf("econt: update shipment %s: no editions available — shipment may already be in transit", req.TrackingNumber)
	}

	label := &econtShippingLabel{ShipmentNumber: req.TrackingNumber}

	if req.ReceiverPhone != "" || req.ReceiverEmail != "" {
		label.ReceiverClient = &econtClientProfile{}
		if req.ReceiverPhone != "" {
			label.ReceiverClient.Phones = []string{req.ReceiverPhone}
		}
		if req.ReceiverEmail != "" {
			label.ReceiverClient.Email = req.ReceiverEmail
		}
	}

	if req.Weight != 0 {
		label.Weight = req.Weight
	}

	if req.ServicePointID != "" {
		label.ReceiverOfficeCode = req.ServicePointID
		// Clear the address so Econt routes to the office rather than the door.
		label.ReceiverAddress = nil
	}

	var updateResult econtUpdateLabelResponse
	if err := a.do(ctx, econtPathUpdateLabel, econtUpdateLabelRequest{Label: label}, &updateResult); err != nil {
		return nil, fmt.Errorf("econt: update shipment: %w", err)
	}

	if updateResult.Error != nil {
		return nil, fmt.Errorf("econt: update shipment: %w", updateResult.Error)
	}

	updated := make([]string, 0, 4)
	if req.ReceiverPhone != "" {
		updated = append(updated, "phone")
	}
	if req.ReceiverEmail != "" {
		updated = append(updated, "email")
	}
	if req.Weight != 0 {
		updated = append(updated, "weight")
	}
	if req.ServicePointID != "" {
		updated = append(updated, "servicePointId")
	}

	a.log.Info("econt: shipment updated",
		zap.String("shipmentNumber", req.TrackingNumber),
		zap.Strings("updatedFields", updated),
	)

	return &UpdateResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "econt",
		Status:         "updated",
		UpdatedFields:  updated,
	}, nil
}

// ── status mapping ────────────────────────────────────────────────────────────

// econtEventTypeStatuses maps Econt trackingEvents.destinationType values to
// gateway TrackingStatus. The destinationType is the fine-grained event type
// returned in the trackingEvents array.
var econtEventTypeStatuses = map[string]TrackingStatus{
	"client":                    StatusBooked,
	"courier_direction":         StatusPickedUp,
	"in_pickup_courier":         StatusPickedUp,
	"in_pickup_office":          StatusInTransit,
	"office":                    StatusInTransit,
	"courier":                   StatusOutForDelivery,
	"in_delivery_courier":       StatusOutForDelivery,
	"in_delivery_office":        StatusOutForDelivery,
	"arrival_departure_from_hub": StatusInTransit,
	"first_try":                 StatusFailed,
	"second_try":                StatusFailed,
	"failed_delivery":           StatusFailed,
	"redirect":                  StatusInTransit,
	"instruction":               StatusInTransit,
	"return":                    StatusReturned,
	"is_returning_to_sender":    StatusReturned,
	"returned_to_sender":        StatusReturned,
	"destroy":                   StatusFailed,
}

// normalizeEcontEventType maps an Econt destinationType to a gateway TrackingStatus.
// Unknown types fall back to StatusInTransit.
func normalizeEcontEventType(destinationType string) TrackingStatus {
	if s, ok := econtEventTypeStatuses[destinationType]; ok {
		return s
	}
	return StatusInTransit
}
