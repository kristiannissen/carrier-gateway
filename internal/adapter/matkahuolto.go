// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/matkahuolto.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"go.uber.org/zap"
)

const (
	matkahuoltoBaseURL     = "https://extservices.matkahuolto.fi/mpaketti"
	matkahuoltoTestBaseURL = "https://extservicestest.matkahuolto.fi/mpaketti"
)

// MatkahuoltoAdapter implements CarrierAdapter for Matkahuolto (FI).
//
// Two APIs are used:
//   - Shipment document interface (XML POST /mhshipmentxml) — booking, cancellation,
//     and label retrieval (label is returned inline in the booking response).
//   - Tracking interface (GET /public/tracking) — shipment event history.
//
// Authentication is HTTP Basic Auth on both endpoints.
//
// Label caching: Matkahuolto returns the PDF label base64-encoded inside the
// booking response. There is no separate label-fetch endpoint. FetchLabel returns
// the cached copy stored at BookShipment time. Callers should persist the label
// from the booking response; labels for shipments booked outside this process
// cannot be fetched via FetchLabel.
//
// UpdateShipment is not implemented. The Matkahuolto change message (MessageType=C)
// requires resubmitting the complete original payload, which the stateless gateway
// does not cache. Callers that need to update a shipment should cancel and rebook.
type MatkahuoltoAdapter struct {
	// userID is the Matkahuolto account number (without leading zeros).
	userID   string
	password string
	// senderID is the account number placed in SenderId in shipment messages.
	// Falls back to userID when empty.
	senderID string
	// BaseURL is the shipment XML endpoint base. Overridable for tests.
	BaseURL string
	// TrackingURL is the tracking endpoint base. Overridable for tests.
	TrackingURL string
	log         *zap.Logger
	client      *http.Client
	// labelCache stores base64-encoded PDF labels keyed by shipment tracking number,
	// populated at BookShipment time and consumed by FetchLabel.
	labelCache sync.Map
	// cancelCache stores minimal booking context keyed by tracking number,
	// used to reconstruct the mandatory fields for a delete (cancel) message.
	cancelCache sync.Map
}

// mhCancelCtx holds the booking fields required to issue a cancel (MessageType=D).
type mhCancelCtx struct {
	senderID       string
	receiverName   string
	receiverPostal string
	receiverCity   string
	productCode    string
	packages       int
	weight         float64
}

// — XML wire types: request —————————————————————————————————————————————————

type mhShipmentRequest struct {
	XMLName  xml.Name      `xml:"MHShipmentRequest"`
	UserID   string        `xml:"UserId"`
	Password string        `xml:"Password"`
	Version  string        `xml:"Version"`
	Shipment mhShipmentXML `xml:"Shipment"`
}

// mhShipmentXML mirrors the flat Matkahuolto <Shipment> element.
// Only fields used by this adapter are present; the API ignores unknown elements.
type mhShipmentXML struct {
	ShipmentType   string  `xml:"ShipmentType"`
	MessageType    string  `xml:"MessageType"`
	ShipmentNumber string  `xml:"ShipmentNumber,omitempty"`
	Weight         float64 `xml:"Weight"`
	Packages       int     `xml:"Packages"`

	SenderID            string `xml:"SenderId"`
	SenderName1         string `xml:"SenderName1,omitempty"`
	SenderAddress       string `xml:"SenderAddress,omitempty"`
	SenderPostal        string `xml:"SenderPostal,omitempty"`
	SenderCity          string `xml:"SenderCity,omitempty"`
	SenderCountry       string `xml:"SenderCountry,omitempty"`
	SenderContactName   string `xml:"SenderContactName,omitempty"`
	SenderContactNumber string `xml:"SenderContactNumber,omitempty"`
	SenderEmail         string `xml:"SenderEmail,omitempty"`
	SenderReference     string `xml:"SenderReference,omitempty"`

	ReceiverName1         string `xml:"ReceiverName1"`
	ReceiverAddress       string `xml:"ReceiverAddress,omitempty"`
	ReceiverPostal        string `xml:"ReceiverPostal"`
	ReceiverCity          string `xml:"ReceiverCity"`
	ReceiverCountry       string `xml:"ReceiverCountry,omitempty"`
	ReceiverContactName   string `xml:"ReceiverContactName,omitempty"`
	ReceiverContactNumber string `xml:"ReceiverContactNumber,omitempty"`
	ReceiverEmail         string `xml:"ReceiverEmail,omitempty"`

	ProductCode     string `xml:"ProductCode"`
	Goods           string `xml:"Goods,omitempty"`
	SpecialHandling string `xml:"SpecialHandling,omitempty"`
}

// — XML wire types: shipment reply —————————————————————————————————————————

type mhShipmentReply struct {
	XMLName     xml.Name           `xml:"MHShipmentReply"`
	Version     string             `xml:"Version"`
	ErrorNbr    string             `xml:"ErrorNbr,omitempty"`
	ErrorMsg    string             `xml:"ErrorMsg,omitempty"`
	Shipment    *mhShipmentReplyEl `xml:"Shipment"`
	ShipmentPdf string             `xml:"ShipmentPdf,omitempty"`
	PdfName     string             `xml:"PdfName,omitempty"`
}

type mhShipmentReplyEl struct {
	ShipmentNumber  string `xml:"ShipmentNumber"`
	SenderReference string `xml:"SenderReference"`
	ActivationCode  string `xml:"ActivationCode,omitempty"`
	ErrorNbr        string `xml:"ErrorNbr,omitempty"`
	ErrorMsg        string `xml:"ErrorMsg,omitempty"`
}

// — XML wire types: tracking reply —————————————————————————————————————————

type mhTrackingReply struct {
	XMLName xml.Name       `xml:"MHTrackingEvents"`
	Events  []mhTrackEvent `xml:"Event"`
	Errors  []mhTrackError `xml:"Error"`
}

type mhTrackEvent struct {
	EventID              string `xml:"EventId"`
	ShipmentNumber       string `xml:"ShipmentNumber"`
	ParcelNumber         string `xml:"ParcelNumber"`
	SenderReference      string `xml:"SenderReference"`
	EventCode            string `xml:"EventCode"`
	EventTime            string `xml:"EventTime"`
	EventPlace           string `xml:"EventPlace"`
	OfficeCode           string `xml:"OfficeCode"`
	Signature            string `xml:"Signature"`
	Remarks              string `xml:"Remarks"`
	ReturnShipmentNumber string `xml:"ReturnShipmentNumber"`
}

type mhTrackError struct {
	EventID   string `xml:"EventId"`
	ErrorCode string `xml:"ErrorCode"`
	ErrorText string `xml:"ErrorText"`
}

// — Constructor ————————————————————————————————————————————————————————————

// NewMatkahuoltoAdapter returns a production-mode adapter.
//
// userID is the Matkahuolto account number (without leading zeros).
// senderID may be left empty; when so, userID is used as the sender account number.
func NewMatkahuoltoAdapter(userID, password, senderID string, log *zap.Logger) *MatkahuoltoAdapter {
	sid := senderID
	if sid == "" {
		sid = userID
	}
	return &MatkahuoltoAdapter{
		userID:      userID,
		password:    password,
		senderID:    sid,
		BaseURL:     matkahuoltoBaseURL,
		TrackingURL: matkahuoltoBaseURL,
		log:         log,
		client:      &http.Client{},
	}
}

// — CarrierAdapter —————————————————————————————————————————————————————————

// BookShipment creates a new shipment via the Matkahuolto shipment XML interface.
// The response label (base64 PDF) is stored in the label cache and also embedded
// in the first ColliResponse.LabelURL for immediate use.
func (a *MatkahuoltoAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	s := req.Shipment
	productCode := mhProductCode(s.DeliveryType, s.Receiver.Country)

	shipmentType := "N"
	if strings.EqualFold(s.DeliveryType, "return") {
		shipmentType = "R"
	}

	packages := len(s.Colli)
	if packages == 0 {
		packages = 1
	}

	goods := ""
	if len(s.Colli) > 0 && len(s.Colli[0].Items) > 0 {
		goods = s.Colli[0].Items[0].Description
	}

	payload := mhShipmentRequest{
		UserID:   a.userID,
		Password: a.password,
		Version:  "2.0",
		Shipment: mhShipmentXML{
			ShipmentType: shipmentType,
			MessageType:  "N",
			Weight:       s.TotalWeight,
			Packages:     packages,

			SenderID:            a.senderID,
			SenderName1:         s.Sender.Name,
			SenderAddress:       mhStreet(s.Sender),
			SenderPostal:        s.Sender.PostalCode,
			SenderCity:          s.Sender.City,
			SenderCountry:       s.Sender.Country,
			SenderContactName:   s.Sender.Name,
			SenderContactNumber: s.Sender.Phone,
			SenderEmail:         s.Sender.Email,
			SenderReference:     req.IdempotencyKey,

			ReceiverName1:         s.Receiver.Name,
			ReceiverAddress:       mhStreet(s.Receiver),
			ReceiverPostal:        s.Receiver.PostalCode,
			ReceiverCity:          s.Receiver.City,
			ReceiverCountry:       s.Receiver.Country,
			ReceiverContactName:   s.Receiver.Name,
			ReceiverContactNumber: s.Receiver.Phone,
			ReceiverEmail:         s.Receiver.Email,

			ProductCode: productCode,
			Goods:       goods,
		},
	}

	reply, err := a.postShipment(ctx, payload)
	if err != nil {
		return nil, err
	}

	trackingNumber := reply.Shipment.ShipmentNumber
	label := strings.TrimSpace(reply.ShipmentPdf)

	if label != "" {
		a.labelCache.Store(trackingNumber, label)
	}
	a.cancelCache.Store(trackingNumber, mhCancelCtx{
		senderID:       a.senderID,
		receiverName:   s.Receiver.Name,
		receiverPostal: s.Receiver.PostalCode,
		receiverCity:   s.Receiver.City,
		productCode:    productCode,
		packages:       packages,
		weight:         s.TotalWeight,
	})

	resp := &BookingResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "matkahuolto",
		Status:         "booked",
	}
	if label != "" {
		resp.Colli = []ColliResponse{{
			ID:             trackingNumber,
			TrackingNumber: trackingNumber,
			LabelURL:       label,
			Status:         "booked",
		}}
	}
	return resp, nil
}

// TrackShipment retrieves tracking events from the Matkahuolto tracking interface.
// Events are returned newest-first. When no events exist the shipment is treated
// as booked but not yet scanned.
func (a *MatkahuoltoAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	raw, err := a.fetchTracking(ctx, trackingNumber)
	if err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return &TrackingResponse{
			TrackingNumber:   trackingNumber,
			Carrier:          "matkahuolto",
			Status:           "02",
			NormalizedStatus: StatusBooked,
			OriginalStatus:   "02",
			Events:           []TrackingEvent{},
		}, nil
	}

	latest := raw[len(raw)-1]
	rawCode := latest.EventCode
	normalized := normalizeStatus("matkahuolto", rawCode)

	events := make([]TrackingEvent, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		e := raw[i]
		events = append(events, TrackingEvent{
			Timestamp:        e.EventTime,
			Status:           e.EventCode,
			NormalizedStatus: normalizeStatus("matkahuolto", e.EventCode),
			Location:         e.EventPlace,
			Details:          mhEventDescription(e.EventCode),
		})
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "matkahuolto",
		Status:           rawCode,
		NormalizedStatus: normalized,
		OriginalStatus:   rawCode,
		Events:           events,
	}, nil
}

// FetchLabel returns the PDF label cached at BookShipment time.
// Only PDF format is supported; only shipments booked via this adapter instance
// have their labels available. Store the label from the booking response for
// long-term access.
func (a *MatkahuoltoAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF && req.Format != "" {
		return nil, unsupportedFormat("Matkahuolto", req.Format, LabelFormatPDF)
	}
	v, ok := a.labelCache.Load(req.TrackingNumber)
	if !ok {
		return nil, fmt.Errorf("matkahuolto: label for %s not in cache — store the label from the booking response", req.TrackingNumber)
	}
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "matkahuolto",
		Format:         LabelFormatPDF,
		Data:           v.(string),
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment sends a delete message (MessageType=D) to cancel the shipment.
// Cancellation requires the booking to have been made via this adapter instance
// (cancel context is cached at booking time). Only possible before carrier pickup.
func (a *MatkahuoltoAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	v, ok := a.cancelCache.Load(trackingNumber)
	if !ok {
		return nil, fmt.Errorf("matkahuolto: cancel context for %s not found — only shipments booked via this process can be cancelled", trackingNumber)
	}
	cc := v.(mhCancelCtx)

	payload := mhShipmentRequest{
		UserID:   a.userID,
		Password: a.password,
		Version:  "2.0",
		Shipment: mhShipmentXML{
			ShipmentType:   "N",
			MessageType:    "D",
			ShipmentNumber: trackingNumber,
			Weight:         cc.weight,
			Packages:       cc.packages,
			SenderID:       cc.senderID,
			ReceiverName1:  cc.receiverName,
			ReceiverPostal: cc.receiverPostal,
			ReceiverCity:   cc.receiverCity,
			ProductCode:    cc.productCode,
		},
	}

	reply, err := a.postShipment(ctx, payload)
	if err != nil {
		return nil, err
	}
	// ErrorNbr=0 signals successful delete per API spec.
	if reply.ErrorNbr != "" && reply.ErrorNbr != "0" {
		return nil, fmt.Errorf("matkahuolto: cancel failed (%s): %s", reply.ErrorNbr, reply.ErrorMsg)
	}

	a.cancelCache.Delete(trackingNumber)
	a.labelCache.Delete(trackingNumber)

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "matkahuolto",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported. The Matkahuolto change message (MessageType=C)
// requires the complete original payload which the stateless gateway does not cache.
// Cancel and rebook to change shipment details.
func (a *MatkahuoltoAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, ErrNotSupported
}

// — HTTP helpers ———————————————————————————————————————————————————————————

// postShipment marshals payload to XML and POSTs it to the shipment endpoint.
func (a *MatkahuoltoAdapter) postShipment(ctx context.Context, payload mhShipmentRequest) (*mhShipmentReply, error) {
	body, err := xml.Marshal(payload) // #nosec G117 — Password field required in XML body by Matkahuolto API spec
	if err != nil {
		return nil, fmt.Errorf("matkahuolto: marshal shipment request: %w", err)
	}
	body = append([]byte(xml.Header), body...)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/mhshipmentxml", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("matkahuolto: build shipment request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "text/xml")
	httpReq.SetBasicAuth(a.userID, a.password)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("matkahuolto: post shipment: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			a.log.Warn("matkahuolto: close shipment response body", zap.Error(cerr))
		}
	}()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("matkahuolto: read shipment response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		a.log.Warn("matkahuolto shipment API non-200",
			zap.Int("status", resp.StatusCode),
			zap.ByteString("body", raw),
		)
		return nil, fmt.Errorf("matkahuolto: shipment API returned HTTP %d", resp.StatusCode)
	}

	var reply mhShipmentReply
	if err := xml.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("matkahuolto: unmarshal shipment reply: %w", err)
	}

	// Root-level ErrorNbr (non-empty, non-zero) indicates a request error.
	if reply.ErrorNbr != "" && reply.ErrorNbr != "0" {
		return nil, fmt.Errorf("matkahuolto: API error %s: %s", reply.ErrorNbr, reply.ErrorMsg)
	}
	if reply.Shipment == nil && reply.ErrorNbr == "" {
		return nil, fmt.Errorf("matkahuolto: unexpected empty reply")
	}

	return &reply, nil
}

// fetchTracking calls the Matkahuolto tracking interface by shipment/parcel ID.
func (a *MatkahuoltoAdapter) fetchTracking(ctx context.Context, trackingNumber string) ([]mhTrackEvent, error) {
	params := url.Values{"ids": []string{trackingNumber}}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.TrackingURL+"/public/tracking?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("matkahuolto: build tracking request: %w", err)
	}
	httpReq.SetBasicAuth(a.userID, a.password)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("matkahuolto: tracking request: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			a.log.Warn("matkahuolto: close tracking response body", zap.Error(cerr))
		}
	}()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("matkahuolto: read tracking response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		a.log.Warn("matkahuolto tracking API non-200",
			zap.Int("status", resp.StatusCode),
			zap.ByteString("body", raw),
		)
		return nil, fmt.Errorf("matkahuolto: tracking API returned HTTP %d", resp.StatusCode)
	}

	var reply mhTrackingReply
	if err := xml.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("matkahuolto: unmarshal tracking reply: %w", err)
	}

	if len(reply.Errors) > 0 {
		e := reply.Errors[0]
		if e.ErrorCode != "" {
			return nil, fmt.Errorf("matkahuolto: tracking error %s: %s", e.ErrorCode, e.ErrorText)
		}
	}

	return reply.Events, nil
}

// — Mapping helpers ————————————————————————————————————————————————————————

// mhProductCode selects the Matkahuolto product code from delivery type and
// destination country. Defaults to parcel-point delivery (80 / 95) when
// deliveryType is empty, as that is the most common Finnish e-commerce product.
func mhProductCode(deliveryType, country string) string {
	domestic := country == "" || strings.EqualFold(country, "FI")
	switch strings.ToLower(deliveryType) {
	case "home":
		if domestic {
			return "34" // Kotijakelu
		}
		return "97" // Ulkomaan Kotijakelu
	case "business":
		if domestic {
			return "30" // Jakopaketti
		}
		return "96" // Ulkomaan Jakopaketti
	case "servicepoint":
		if domestic {
			return "80" // Lähellä-paketti
		}
		return "95" // Ulkomaan Lähellä-paketti
	case "return":
		if domestic {
			return "81" // Asiakaspalautus
		}
		return "91" // Ulkomaan Asiakaspalautus
	default:
		if domestic {
			return "80"
		}
		return "95"
	}
}

// mhStreet concatenates street and house number for the Matkahuolto address field.
func mhStreet(a Address) string {
	if a.HouseNumber == "" {
		return a.Street
	}
	return a.Street + " " + a.HouseNumber
}

// mhEventDescription returns the human-readable label for a Matkahuolto event code.
func mhEventDescription(code string) string {
	descriptions := map[string]string{
		"02":  "Electronic advance notice received",
		"08":  "Picked up from sender",
		"10":  "Received at departing parcel point",
		"12":  "Consolidated",
		"15":  "Received for carriage",
		"25":  "Loaded for main transport",
		"35":  "Received at destination terminal",
		"40":  "Waiting to be loaded for delivery",
		"41":  "Waiting to be loaded for parcel point",
		"45":  "Loaded for delivery",
		"46":  "Loaded for delivery to parcel point",
		"47":  "Delivered to parcel point",
		"48":  "Received at parcel point",
		"50":  "Ready to be collected",
		"55":  "First arrival notification sent",
		"56":  "Second arrival notification sent",
		"57":  "Manual arrival notification sent",
		"60":  "Handed over to the recipient",
		"61":  "Handed over to the proxy",
		"62":  "Handover cancelled",
		"65":  "COD paid to sender",
		"70":  "Returned uncollected",
		"97":  "Delivery attempt unsuccessful",
		"104": "Deviation added",
	}
	if d, ok := descriptions[code]; ok {
		return d
	}
	return fmt.Sprintf("Event %s", code)
}
