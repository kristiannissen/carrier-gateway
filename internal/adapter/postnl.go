// Package adapter provides the PostNL implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/postnl.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// PostNLAdapter implements CarrierAdapter and ReturnAdapter for PostNL.
// Authentication uses the apikey header on every request.
//
// Supported operations:
//   - BookShipment  → POST /shipment/delivery/v4/labelconfirm
//   - TrackShipment → GET  /shipment/v2/status/barcode/{barcode}
//   - FetchLabel    → POST /shipment/delivery/v4/label
//   - BookReturn    → POST /shipment/delivery/v4/return/generate
//   - FetchReturnLabel → POST /shipment/delivery/v4/label (same endpoint, return barcode)
//
// CancelShipment and UpdateShipment are not supported by the PostNL PNP v4 API.
type PostNLAdapter struct {
	// APIKey is the PostNL PNP API key, passed in the apikey header.
	APIKey string
	// CustomerNumber is the PostNL customer account number (6 digits).
	CustomerNumber string
	// CustomerCode is the PostNL customer code (4 chars, e.g. "ABCD").
	CustomerCode string
	// BaseURL is the base URL for all PostNL API calls.
	// Defaults to https://api.postnl.nl.
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewPostNLAdapter creates a PostNLAdapter ready for production use.
func NewPostNLAdapter(apiKey, customerNumber, customerCode string, log *zap.Logger) *PostNLAdapter {
	return &PostNLAdapter{
		APIKey:         apiKey,
		CustomerNumber: customerNumber,
		CustomerCode:   customerCode,
		BaseURL:        "https://api.postnl.nl",
		HTTPClient:     &http.Client{Timeout: 30 * time.Second},
		log:            log,
	}
}

// postnlAddress is the wire format for PostNL PNP v4 address objects.
type postnlAddress struct {
	City                string `json:"city,omitempty"`
	CountryIso          string `json:"countryIso"`
	HouseNumber         string `json:"houseNumber,omitempty"`
	HouseNumberAddition string `json:"houseNumberAddition,omitempty"`
	PostalCode          string `json:"postalCode,omitempty"`
	Street              string `json:"street,omitempty"`
	AddressLine         string `json:"addressLine,omitempty"`
	CompanyName         string `json:"companyName,omitempty"`
}

// postnlContact is the wire format for the optional contact block.
type postnlContact struct {
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
	Name        string `json:"name,omitempty"`
}

// postnlServices holds the optional services block for a PostNL PNP v4 shipment.
type postnlServices struct {
	InsuredValue        *float64           `json:"insuredValue,omitempty"`
	StatedAddressOnly   bool               `json:"statedAddressOnly,omitempty"`
	ReturnWhenNotHome   bool               `json:"returnWhenNotHome,omitempty"`
	MinimalAgeCheck     string             `json:"minimalAgeCheck,omitempty"`
	DeliveryConfirmation string            `json:"deliveryConfirmation,omitempty"`
	DeliveryWindow      *postnlDelivWindow `json:"deliveryWindow,omitempty"`
	Adrlq               bool               `json:"adrlq,omitempty"`
}

// postnlDelivWindow holds the delivery window service options.
type postnlDelivWindow struct {
	Service          string `json:"service,omitempty"`
	GuaranteedBefore string `json:"guaranteedBefore,omitempty"`
}

// postnlLabelSettings selects the output format for the label.
type postnlLabelSettings struct {
	OutputType string `json:"outputType"`
}

// postnlCustomsContent is a single line item in a customs declaration.
type postnlCustomsContent struct {
	Description     string  `json:"description"`
	Quantity        int     `json:"quantity"`
	Weight          int     `json:"weight"` // grams
	Value           float64 `json:"value"`
	Currency        string  `json:"currency"`
	CountryOfOrigin string  `json:"countryOfOrigin,omitempty"`
	HSCode          string  `json:"hsTariffCode,omitempty"`
}

// postnlCustoms is the international customs declaration block.
type postnlCustoms struct {
	TransactionCode string                 `json:"transactionCode,omitempty"`
	Currency        string                 `json:"currency,omitempty"`
	Content         []postnlCustomsContent `json:"content,omitempty"`
}

// postnlInternationalData carries the bundle type and optional customs declaration.
type postnlInternationalData struct {
	Bundle  string         `json:"bundle,omitempty"`
	Customs *postnlCustoms `json:"customs,omitempty"`
}

// postnlShipmentItem represents one parcel within a multi-piece shipment.
type postnlShipmentItem struct {
	Dimension *postnlDimension `json:"dimension,omitempty"`
}

// postnlDimension holds weight (grams) and optional physical dimensions (mm).
type postnlDimension struct {
	Weight int `json:"weight"` // grams
	Length int `json:"length,omitempty"`
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

// postnlBookRequest is the body sent to POST /shipment/delivery/v4/labelconfirm.
type postnlBookRequest struct {
	Receiver               postnlReceiver           `json:"receiver"`
	Sender                 postnlSender             `json:"sender"`
	ItemCount              int                      `json:"itemCount,omitempty"`
	Items                  []postnlShipmentItem     `json:"items,omitempty"`
	ShipmentType           string                   `json:"shipmentType"`
	HandoverDate           string                   `json:"handoverDate,omitempty"`
	LabelSettings          *postnlLabelSettings     `json:"labelSettings,omitempty"`
	Services               *postnlServices          `json:"services,omitempty"`
	InternationalShipmentData *postnlInternationalData `json:"internationalShipmentData,omitempty"`
}

// postnlReceiver is the receiver block in a PostNL booking request.
type postnlReceiver struct {
	Address postnlAddress  `json:"address"`
	Contact *postnlContact `json:"contact,omitempty"`
	Type    string         `json:"type,omitempty"` // "consumer" or "business"
}

// postnlSender is the sender (customer) block in a PostNL booking request.
type postnlSender struct {
	CustomerNumber string        `json:"customerNumber"`
	CustomerCode   string        `json:"customerCode,omitempty"`
	Address        postnlAddress `json:"address"`
	Contact        *postnlContact `json:"contact,omitempty"`
}

// postnlEmaOkResponse is the response body from /v4/labelconfirm and /v4/label.
type postnlEmaOkResponse struct {
	Items    []postnlEmaItem    `json:"items"`
	Warnings []postnlEmaWarning `json:"warnings"`
}

// postnlEmaItem is one item in the EmaOkResponse.
type postnlEmaItem struct {
	Barcode  string            `json:"barcode"`
	Labels   []postnlEmaLabel  `json:"labels"`
}

// postnlEmaLabel holds base64-encoded label data.
type postnlEmaLabel struct {
	Label      string `json:"label"`
	OutputType string `json:"outputType"`
	LabelType  string `json:"labelType"`
}

// postnlEmaWarning is a non-blocking warning in the booking response.
type postnlEmaWarning struct {
	Description string `json:"description"`
}

// postnlLabelRequest is the body sent to POST /shipment/delivery/v4/label
// when re-fetching an existing label by barcode.
type postnlLabelRequest struct {
	Receiver      postnlReceiver       `json:"receiver"`
	Sender        postnlSender         `json:"sender"`
	ShipmentType  string               `json:"shipmentType"`
	LabelSettings *postnlLabelSettings `json:"labelSettings,omitempty"`
	Items         []postnlLabelItem    `json:"items,omitempty"`
}

// postnlLabelItem carries an existing barcode when re-fetching a label.
type postnlLabelItem struct {
	Barcode string `json:"barcode"`
}

// postnlTrackingResponse is the response body from GET /shipment/v2/status/barcode/{barcode}.
type postnlTrackingResponse struct {
	CurrentStatus *postnlCurrentStatus `json:"CurrentStatus,omitempty"`
}

// postnlCurrentStatus is the CurrentStatus wrapper in the tracking response.
type postnlCurrentStatus struct {
	Shipment *postnlTrackedShipment `json:"Shipment,omitempty"`
}

// postnlTrackedShipment carries the status and event history for a single barcode.
type postnlTrackedShipment struct {
	Barcode string            `json:"Barcode"`
	Status  postnlStatus      `json:"Status"`
	Event   []postnlEvent     `json:"Event"`
}

// postnlStatus holds the shipment status from the ShippingStatus API.
type postnlStatus struct {
	TimeStamp          string `json:"TimeStamp"`
	StatusCode         string `json:"StatusCode"`
	StatusDescription  string `json:"StatusDescription"`
	PhaseCode          string `json:"PhaseCode"`
	PhaseDescription   string `json:"PhaseDescription"`
}

// postnlEvent is a single tracking event in the event history.
type postnlEvent struct {
	Code         string `json:"Code"`
	Description  string `json:"Description"`
	LocationCode string `json:"LocationCode"`
	TimeStamp    string `json:"TimeStamp"`
}

// postnlReturnRequest is the body for POST /shipment/delivery/v4/return/generate.
type postnlReturnRequest struct {
	Sender        postnlReturnSender        `json:"sender"`
	Receiver      postnlReturnReceiver      `json:"receiver"`
	LabelSettings postnlLabelSettings       `json:"labelSettings"`
	ShipmentType  string                    `json:"shipmentType"`
	ReturnOptions *postnlReturnOptions      `json:"returnOptions,omitempty"`
}

// postnlReturnSender is the sender block in a return request.
type postnlReturnSender struct {
	Address postnlAddress  `json:"address"`
	Contact *postnlContact `json:"contact,omitempty"`
}

// postnlReturnReceiver is the receiver (merchant) block in a return request.
type postnlReturnReceiver struct {
	CustomerNumber string        `json:"customerNumber"`
	CustomerCode   string        `json:"customerCode,omitempty"`
	Address        postnlAddress `json:"address"`
}

// postnlReturnOptions holds optional domestic return configuration.
type postnlReturnOptions struct {
	Domestic *postnlDomesticReturnOptions `json:"domestic,omitempty"`
}

// postnlDomesticReturnOptions configures the return period in days (20 or 35).
type postnlDomesticReturnOptions struct {
	ReturnPeriod int `json:"returnPeriod,omitempty"`
}

// postnlEUCountries is the set of EU countries reachable via the PostNL
// international bundle service. Domestic NL shipments use the standard flow.
// Source: postln_int_dom.md EU country list.
var postnlEUCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "HR": true, "CY": true,
	"CZ": true, "DK": true, "EE": true, "FI": true, "FR": true,
	"DE": true, "GR": true, "HU": true, "IE": true, "IT": true,
	"LV": true, "LT": true, "LU": true, "MT": true, "PL": true,
	"PT": true, "RO": true, "SK": true, "SI": true, "ES": true,
	"SE": true,
}

// postnlShipmentType maps the destination country and shipment content to the
// PostNL v4 shipmentType enum value.
// For letterbox-sized items use DeliveryType "letterbox".
// Defaults to "parcel" for all other cases.
func postnlShipmentType(deliveryType string) string {
	switch strings.ToLower(deliveryType) {
	case "letterbox":
		return "letterbox"
	case "packet":
		return "packet"
	case "letter":
		return "letter"
	default:
		return "parcel"
	}
}

// postnlOutputType maps a gateway LabelFormat to the PostNL outputType string.
func postnlOutputType(f LabelFormat) string {
	switch f {
	case LabelFormatZPL, LabelFormatZPLGK:
		return "zpl"
	default:
		return "pdf"
	}
}

// postnlBuildAddress converts a gateway Address to a PostNL wire address.
func postnlBuildAddress(a Address) postnlAddress {
	addr := postnlAddress{
		City:        a.City,
		CountryIso:  a.Country,
		PostalCode:  a.PostalCode,
		Street:      a.Street,
		CompanyName: a.Name,
	}
	if a.HouseNumber != "" {
		addr.HouseNumber = a.HouseNumber
		addr.AddressLine = strings.TrimSpace(a.Street + " " + a.HouseNumber)
	} else {
		addr.AddressLine = a.Street
	}
	if a.Supplement != "" {
		addr.HouseNumberAddition = a.Supplement
	}
	return addr
}

// postnlBuildContact builds a contact block when the address has a phone or email.
// Returns nil when both are absent.
func postnlBuildContact(a Address) *postnlContact {
	if a.Phone == "" && a.Email == "" {
		return nil
	}
	c := &postnlContact{
		Name:        a.Name,
		Email:       a.Email,
		PhoneNumber: a.Phone,
	}
	return c
}

// postnlBuildServices builds the services block from the shipment add-ons.
func postnlBuildServices(addOns []AddOn) *postnlServices {
	svc := &postnlServices{}
	hasService := false

	if hasAddOn(addOns, AddOnInsurance) {
		if ao, ok := getAddOn(addOns, AddOnInsurance); ok && ao.InsuranceValue > 0 {
			v := ao.InsuranceValue
			svc.InsuredValue = &v
			hasService = true
		}
	}
	if hasAddOn(addOns, AddOnStatedAddressOnly) {
		svc.StatedAddressOnly = true
		hasService = true
	}
	if hasAddOn(addOns, AddOnReturnWhenNotHome) {
		svc.ReturnWhenNotHome = true
		hasService = true
	}
	if hasAddOn(addOns, AddOnSignatureRequired) {
		svc.DeliveryConfirmation = "signature"
		hasService = true
	}
	if hasAddOn(addOns, AddOnDeliveryCode) {
		svc.DeliveryConfirmation = "deliveryCode"
		hasService = true
	}
	if hasAddOn(addOns, AddOnAgeCheck) {
		ao, _ := getAddOn(addOns, AddOnAgeCheck)
		age := ao.Instructions
		if age == "" {
			age = "18+" // safe default
		}
		svc.MinimalAgeCheck = age
		// Age check requires signature confirmation (PostNL service rule).
		svc.DeliveryConfirmation = "signature"
		hasService = true
	}
	if hasAddOn(addOns, AddOnDangerousGoodsLQ) {
		svc.Adrlq = true
		hasService = true
	}
	if hasAddOn(addOns, AddOnEveningDelivery) {
		svc.DeliveryWindow = &postnlDelivWindow{Service: "evening"}
		hasService = true
	}
	if ao, ok := getAddOn(addOns, AddOnGuaranteedBefore); ok {
		t := ao.Instructions // e.g. "10:00", "12:00", "17:00"
		if t == "" {
			t = "12:00"
		}
		svc.DeliveryWindow = &postnlDelivWindow{GuaranteedBefore: t}
		hasService = true
	}

	if !hasService {
		return nil
	}
	return svc
}

// postnlBuildItems builds the items slice from the shipment colli.
// Weight is converted from kg to grams; dimensions from cm to mm.
func postnlBuildItems(colli []Colli) []postnlShipmentItem {
	items := make([]postnlShipmentItem, 0, len(colli))
	for _, c := range colli {
		item := postnlShipmentItem{}
		if c.Weight > 0 || c.Dimensions.Length > 0 {
			dim := &postnlDimension{
				Weight: int(c.Weight * 1000), // kg → grams
				Length: int(c.Dimensions.Length * 10), // cm → mm
				Width:  int(c.Dimensions.Width * 10),
				Height: int(c.Dimensions.Height * 10),
			}
			item.Dimension = dim
		}
		items = append(items, item)
	}
	return items
}

// postnlBuildInternationalData constructs the internationalShipmentData block
// for EU and non-EU destinations. Returns nil for NL domestic shipments.
func postnlBuildInternationalData(s Shipment) *postnlInternationalData {
	dest := strings.ToUpper(s.Receiver.Country)
	if dest == "NL" {
		return nil
	}

	data := &postnlInternationalData{}

	if postnlEUCountries[dest] {
		data.Bundle = "track_trace"
	} else {
		data.Bundle = "insured"
	}

	if s.Customs.CustomsValue > 0 && len(s.Customs.Items) > 0 {
		tc := "11" // sale of goods default
		switch s.Customs.NatureOfCargo {
		case "GIFT":
			tc = "31"
		case "RETURNED_GOODS":
			tc = "21"
		case "COMMERCIAL_SAMPLE":
			tc = "32"
		case "DOCUMENTS":
			tc = "91"
		}

		content := make([]postnlCustomsContent, 0, len(s.Customs.Items))
		for _, it := range s.Customs.Items {
			cur := it.Currency
			if cur == "" {
				cur = s.Customs.CustomsCurrency
			}
			if cur == "" {
				cur = "EUR"
			}
			content = append(content, postnlCustomsContent{
				Description:     it.Description,
				Quantity:        it.Quantity,
				Weight:          int(it.NetWeight * 1000), // kg → grams
				Value:           it.Value,
				Currency:        cur,
				CountryOfOrigin: it.CountryOfOrigin,
				HSCode:          it.HSCode,
			})
		}
		cur := s.Customs.CustomsCurrency
		if cur == "" {
			cur = "EUR"
		}
		data.Customs = &postnlCustoms{
			TransactionCode: tc,
			Currency:        cur,
			Content:         content,
		}
	}

	return data
}

// do executes an HTTP request with the PostNL API key header and returns
// the response body. Caller is responsible for closing the body.
func (a *PostNLAdapter) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	url := a.BaseURL + path
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("postnl: create request: %w", err)
	}
	req.Header.Set("apikey", a.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("postnl: %s %s: %w", method, path, err)
	}
	return resp, nil
}

// postnlCheckError reads a non-2xx response and wraps it as an error.
func postnlCheckError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("postnl: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// BookShipment books a PostNL shipment and returns the barcode and first label.
// It calls POST /shipment/delivery/v4/labelconfirm which combines labelling and
// pre-announcement in a single round-trip.
//
// Multicollo (itemCount > 1) is supported for parcel shipments. Letterbox and
// return products are single-parcel only — only the first colli is forwarded.
func (a *PostNLAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	s := req.Shipment

	shipType := postnlShipmentType(s.DeliveryType)
	items := postnlBuildItems(s.Colli)
	itemCount := len(items)
	if itemCount == 0 {
		itemCount = 1
	}
	// Letterbox and letter are single-parcel products.
	if shipType == "letterbox" || shipType == "letter" {
		if len(items) > 0 {
			items = items[:1]
		}
		itemCount = 1
	}

	senderAddr := postnlBuildAddress(s.Sender)
	receiverAddr := postnlBuildAddress(s.Receiver)

	receiverType := "consumer"
	if s.Receiver.ServicePointID != "" {
		receiverType = "business"
	}

	payload := postnlBookRequest{
		Receiver: postnlReceiver{
			Address: receiverAddr,
			Contact: postnlBuildContact(s.Receiver),
			Type:    receiverType,
		},
		Sender: postnlSender{
			CustomerNumber: a.CustomerNumber,
			CustomerCode:   a.CustomerCode,
			Address:        senderAddr,
			Contact:        postnlBuildContact(s.Sender),
		},
		ItemCount:    itemCount,
		Items:        items,
		ShipmentType: shipType,
		HandoverDate: time.Now().UTC().Format(time.RFC3339),
		LabelSettings: &postnlLabelSettings{
			OutputType: postnlOutputType(req.LabelFormat),
		},
		Services:                  postnlBuildServices(s.AddOns),
		InternationalShipmentData: postnlBuildInternationalData(s),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("postnl: book shipment: marshal request: %w", err)
	}

	a.log.Info("postnl: booking shipment",
		zap.String("shipmentType", shipType),
		zap.Int("itemCount", itemCount),
		zap.String("destination", s.Receiver.Country),
	)

	resp, err := a.do(ctx, http.MethodPost, "/shipment/delivery/v4/labelconfirm", body)
	if err != nil {
		return nil, fmt.Errorf("postnl: book shipment: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if err := postnlCheckError(resp); err != nil {
		return nil, fmt.Errorf("postnl: book shipment: %w", err)
	}

	var result postnlEmaOkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("postnl: book shipment: decode response: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("postnl: book shipment: empty items in response")
	}

	first := result.Items[0]
	if first.Barcode == "" {
		return nil, fmt.Errorf("postnl: book shipment: no barcode in response")
	}

	var labelData string
	if len(first.Labels) > 0 {
		labelData = first.Labels[0].Label
	}

	var warnings []string
	for _, w := range result.Warnings {
		if w.Description != "" {
			warnings = append(warnings, w.Description)
		}
	}

	booking := &BookingResponse{
		TrackingNumber: first.Barcode,
		Carrier:        "postnl",
		Status:         "booked",
		LabelURL:       labelData, // base64 data — caller decodes
	}
	if len(warnings) > 0 {
		booking.AddOnWarnings = warnings
	}

	a.log.Info("postnl: shipment booked",
		zap.String("barcode", first.Barcode),
		zap.Int("itemCount", itemCount),
	)
	return booking, nil
}

// TrackShipment retrieves the current status for a PostNL shipment by barcode.
// It calls GET /shipment/v2/status/barcode/{barcode}.
func (a *PostNLAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	path := "/shipment/v2/status/barcode/" + trackingNumber
	resp, err := a.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("postnl: track shipment %s: %w", trackingNumber, err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if err := postnlCheckError(resp); err != nil {
		return nil, fmt.Errorf("postnl: track shipment %s: %w", trackingNumber, err)
	}

	var result postnlTrackingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("postnl: track shipment %s: decode response: %w", trackingNumber, err)
	}

	if result.CurrentStatus == nil || result.CurrentStatus.Shipment == nil {
		return &TrackingResponse{
			TrackingNumber:   trackingNumber,
			Carrier:          "postnl",
			NormalizedStatus: StatusUnknown,
		}, nil
	}

	shipment := result.CurrentStatus.Shipment
	raw := shipment.Status.PhaseCode + ":" + shipment.Status.StatusCode

	// Normalize using phaseCode first, then statusCode as the raw key.
	normalised := normalizeStatus("postnl", raw)
	if normalised == StatusUnknown {
		normalised = normalizeStatus("postnl", shipment.Status.StatusCode)
	}

	events := make([]TrackingEvent, 0, len(shipment.Event))
	for _, e := range shipment.Event {
		ts := postnlParseTimestamp(e.TimeStamp)
		events = append(events, TrackingEvent{
			Timestamp:        ts,
			Status:           e.Code + " " + e.Description,
			NormalizedStatus: normalizeStatus("postnl", e.Code),
			Location:         e.LocationCode,
		})
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "postnl",
		Status:           shipment.Status.StatusDescription,
		NormalizedStatus: normalised,
		Events:           events,
	}, nil
}

// postnlParseTimestamp converts PostNL's "DD-MM-YYYY HH:MM:SS" wire format to
// RFC3339 UTC. Returns the original string on parse failure.
func postnlParseTimestamp(raw string) string {
	t, err := time.ParseInLocation("02-01-2006 15:04:05", raw, time.UTC)
	if err != nil {
		return raw
	}
	return t.UTC().Format(time.RFC3339)
}

// FetchLabel re-generates a PostNL label by calling POST /shipment/delivery/v4/label
// with the existing barcode. PostNL has no dedicated label-only fetch endpoint —
// the label endpoint accepts an existing barcode and returns the same label data.
//
// Note: FetchLabel requires a valid sender block. The adapter uses its configured
// customer credentials with a minimal placeholder address. Callers that need an
// exact address match should cache the original label from BookShipment.
func (a *PostNLAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	// PostNL /v4/label requires receiver and sender even for a re-fetch.
	// We use a minimal receiver and our own address.
	payload := postnlLabelRequest{
		Receiver: postnlReceiver{
			Address: postnlAddress{
				CountryIso: "NL",
				PostalCode: "1000AA",
				City:       "Amsterdam",
			},
		},
		Sender: postnlSender{
			CustomerNumber: a.CustomerNumber,
			CustomerCode:   a.CustomerCode,
			Address: postnlAddress{
				CountryIso: "NL",
			},
		},
		ShipmentType: "parcel",
		LabelSettings: &postnlLabelSettings{
			OutputType: postnlOutputType(req.Format),
		},
		Items: []postnlLabelItem{
			{Barcode: req.TrackingNumber},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("postnl: fetch label %s: marshal request: %w", req.TrackingNumber, err)
	}

	resp, err := a.do(ctx, http.MethodPost, "/shipment/delivery/v4/label", body)
	if err != nil {
		return nil, fmt.Errorf("postnl: fetch label %s: %w", req.TrackingNumber, err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if err := postnlCheckError(resp); err != nil {
		return nil, fmt.Errorf("postnl: fetch label %s: %w", req.TrackingNumber, err)
	}

	var result postnlEmaOkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("postnl: fetch label %s: decode response: %w", req.TrackingNumber, err)
	}

	if len(result.Items) == 0 || len(result.Items[0].Labels) == 0 {
		return nil, fmt.Errorf("postnl: fetch label %s: no label data in response", req.TrackingNumber)
	}

	label := result.Items[0].Labels[0]
	outputFmt := req.Format
	if outputFmt == "" {
		outputFmt = LabelFormatPDF
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "postnl",
		Format:         outputFmt,
		Data:           label.Label,
		MimeType:       MimeTypeForFormat(outputFmt),
	}, nil
}

// CancelShipment returns ErrNotSupported because PostNL does not expose a
// cancellation endpoint in the PNP v4 API. Shipments must be cancelled via the
// PostNL Business Centre or by calling PostNL customer support.
func (a *PostNLAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	return nil, notSupported("PostNL", "cancellation",
		"contact PostNL customer support or use the Business Centre portal")
}

// UpdateShipment returns ErrNotSupported because PostNL does not expose a
// post-booking update endpoint in the PNP v4 API.
func (a *PostNLAdapter) UpdateShipment(_ context.Context, req UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("PostNL", "shipment update",
		"post-booking updates are not available via the PostNL PNP v4 API")
}

// BookReturn creates a return shipment via POST /shipment/delivery/v4/return/generate.
//
// The sender in a PostNL return is the consumer returning the parcel; the receiver
// is the merchant (identified by the adapter's CustomerNumber). When ReturnRequest.Receiver
// is empty the adapter falls back to the configured customer address.
//
// PostNL returns currently support NL domestic only (printMethod: consumerPrint).
func (a *PostNLAdapter) BookReturn(ctx context.Context, req ReturnRequest) (*ReturnResponse, error) {
	receiverAddr := postnlBuildAddress(req.Receiver)
	if req.Receiver.Country == "" {
		receiverAddr = postnlAddress{CountryIso: "NL"}
	}

	payload := postnlReturnRequest{
		Sender: postnlReturnSender{
			Address: postnlBuildAddress(req.Sender),
			Contact: postnlBuildContact(req.Sender),
		},
		Receiver: postnlReturnReceiver{
			CustomerNumber: a.CustomerNumber,
			CustomerCode:   a.CustomerCode,
			Address:        receiverAddr,
		},
		LabelSettings: postnlLabelSettings{
			OutputType: "pdf",
		},
		ShipmentType: "parcel",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("postnl: book return: marshal request: %w", err)
	}

	a.log.Info("postnl: booking return shipment",
		zap.String("senderCountry", req.Sender.Country),
	)

	resp, err := a.do(ctx, http.MethodPost, "/shipment/delivery/v4/return/generate", body)
	if err != nil {
		return nil, fmt.Errorf("postnl: book return: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if err := postnlCheckError(resp); err != nil {
		return nil, fmt.Errorf("postnl: book return: %w", err)
	}

	var result postnlEmaOkResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("postnl: book return: decode response: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("postnl: book return: empty items in response")
	}

	first := result.Items[0]
	if first.Barcode == "" {
		return nil, fmt.Errorf("postnl: book return: no barcode in response")
	}

	a.log.Info("postnl: return shipment booked", zap.String("barcode", first.Barcode))

	return &ReturnResponse{
		TrackingNumber: first.Barcode,
		Carrier:        "postnl",
		Status:         "booked",
	}, nil
}

// FetchReturnLabel retrieves a return shipment label by barcode.
// PostNL uses the same /v4/label endpoint for both outbound and return labels.
func (a *PostNLAdapter) FetchReturnLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	return a.FetchLabel(ctx, req)
}
