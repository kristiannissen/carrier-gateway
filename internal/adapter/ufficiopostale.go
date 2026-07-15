// Package adapter provides the Ufficio Postale (Poste Italiane via openapi.it) implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/ufficiopostale.go.
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

const (
	ufficioPostaleBaseURL    = "https://ws.ufficiopostale.com"
	ufficioPostaleSandboxURL = "https://test.ws.ufficiopostale.com"
)

// ufficioPostaleProduct maps a ServiceTier value to the API path segment.
var ufficioPostaleProduct = map[string]string{
	"":                   "raccomandate",
	"raccomandata":       "raccomandate",
	"raccomandata_smart": "raccomandate_smart",
	"ordinaria":          "ordinarie",
	"atti_giudiziari":    "atti_giudiziari",
}

// ufficioPostaleStatusMap normalises Ufficio Postale event type codes to the
// gateway's TrackingStatus. Codes are from the API spec section on tracking.
var ufficioPostaleStatusMap = map[string]TrackingStatus{
	"00":  StatusBooked,         // Accettato Online / Accettata online
	"01":  StatusDelivered,      // Consegnato
	"03":  StatusFailed,         // Non Consegnabile
	"10":  StatusInTransit,      // In transit — not in spec but common
	"20":  StatusOutForDelivery, // In distribuzione
	"30":  StatusFailed,         // In Giacenza / Inviato in Giacenza
	"40":  StatusFailed,         // Inesitato
	"70":  StatusInTransit,      // Accettato CAD
	"91":  StatusFailed,         // Mancata consegna per forza maggiore
	"93":  StatusInTransit,      // Accettato CAN / Accettato CAD
	"100": StatusBooked,         // Accettato Online
	"110": StatusBooked,         // Spedizione Stampata
}

// normaliseUfficioPostaleStatus maps a raw event type code to a TrackingStatus,
// falling back to StatusUnknown for unrecognised codes.
func normaliseUfficioPostaleStatus(code string) TrackingStatus {
	if s, ok := ufficioPostaleStatusMap[code]; ok {
		return s
	}
	return StatusUnknown
}

// UfficioPostaleAdapter implements CarrierAdapter for Ufficio Postale
// (Poste Italiane, delivered via the openapi.it gateway).
//
// Implemented: BookShipment, TrackShipment.
// Not implemented: FetchLabel, CancelShipment, UpdateShipment — the API has
// no label, cancellation, or post-booking update endpoint.
//
// This is a document-mailing service: Poste Italiane prints and posts the
// letter on the sender's behalf. The document content is taken from
// Shipment.ShipmentComment. The product (raccomandata, ordinaria, etc.) is
// controlled via Shipment.ServiceTier.
//
// Italy-domestic only for most products. The sender address must be in Italy.
type UfficioPostaleAdapter struct {
	// APIKey is the Bearer token issued by openapi.it.
	APIKey string
	// BaseURL is the API root (override in tests).
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewUfficioPostaleAdapter creates a production-ready UfficioPostaleAdapter.
func NewUfficioPostaleAdapter(apiKey string, sandbox bool, log *zap.Logger) *UfficioPostaleAdapter {
	base := ufficioPostaleBaseURL
	if sandbox {
		base = ufficioPostaleSandboxURL
	}
	return &UfficioPostaleAdapter{
		APIKey:     apiKey,
		BaseURL:    base,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}
}

// upAddressFields builds the mittente or destinatario address sub-object from
// an Address. State is used as the Italian province code (2-letter, e.g. "RM").
func upAddressFields(a Address) map[string]any {
	m := map[string]any{
		"indirizzo": strings.TrimSpace(a.Street),
		"comune":    a.City,
		"cap":       a.PostalCode,
		"nazione":   a.Country,
	}
	if a.HouseNumber != "" {
		m["civico"] = a.HouseNumber
	}
	if a.State != "" {
		m["provincia"] = a.State
	}
	return m
}

// upRecipient builds a destinatario object from an Address.
func upRecipient(a Address) map[string]any {
	r := upAddressFields(a)
	first, last := splitName(a.Name)
	if last != "" {
		r["nome"] = first
		r["cognome"] = last
	} else {
		r["ragione_sociale"] = first
	}
	if a.Email != "" {
		r["email"] = a.Email
	}
	return r
}

// upSender builds a mittente object from an Address.
func upSender(a Address) map[string]any {
	s := upAddressFields(a)
	first, last := splitName(a.Name)
	if last != "" {
		s["nome"] = first
		s["cognome"] = last
	} else {
		s["ragione_sociale"] = first
	}
	if a.Email != "" {
		s["email"] = a.Email
	}
	return s
}

// upProductPath returns the API path segment for the given service tier.
// Returns an error for unsupported tiers.
func upProductPath(tier string) (string, error) {
	p, ok := ufficioPostaleProduct[strings.ToLower(tier)]
	if !ok {
		return "", fmt.Errorf("ufficiopostale: unsupported service tier %q; accepted: raccomandata, raccomandata_smart, ordinaria, atti_giudiziari", tier)
	}
	return p, nil
}

// upDocumentContent derives the letter body from the booking request.
// It uses ShipmentComment when set, otherwise falls back to a concatenation
// of item descriptions across all colli. Callers should set ShipmentComment
// to the actual letter content (plain text, HTML, or a PDF URL/base64).
func upDocumentContent(req BookingRequest) string {
	if req.Shipment.ShipmentComment != "" {
		return req.Shipment.ShipmentComment
	}
	var parts []string
	for _, c := range req.Shipment.Colli {
		for _, item := range c.Items {
			if item.Description != "" {
				parts = append(parts, item.Description)
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return "Documento"
}

// upBookingPayload assembles the JSON request body for a product POST endpoint.
func upBookingPayload(req BookingRequest, document string) map[string]any {
	payload := map[string]any{
		"mittente":    upSender(req.Shipment.Sender),
		"destinatari": []map[string]any{upRecipient(req.Shipment.Receiver)},
		"documento":   document,
		"opzioni": map[string]any{
			"autoconfirm": true,
		},
	}
	if req.CallbackURL != "" {
		payload["callback"] = map[string]any{
			"url":    req.CallbackURL,
			"method": "JSON",
		}
	}
	return payload
}

// upAPIResponse is the envelope returned by every Ufficio Postale endpoint.
type upAPIResponse struct {
	Data    json.RawMessage `json:"data"`
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Error   any             `json:"error"`
}

// upBookingItem is a single entry in the PostalServiceResponse data array,
// representing one booked mailing.
type upBookingItem struct {
	Destinatari []struct {
		ID                 string `json:"id"`
		State              string `json:"state"`
		NumeroRaccomandata string `json:"NumeroRaccomandata"`
	} `json:"destinatari"`
}

// upTrackingEvent is a single tracking entry from GET /tracking/{id}.
type upTrackingEvent struct {
	Timestamp   int64  `json:"timestamp"`
	Descrizione string `json:"descrizione"`
	Type        string `json:"type"`
	IsFinal     bool   `json:"definitivo"` //nolint:misspell // Italian API field name
}

// do executes an HTTP request against the Ufficio Postale API and returns the
// decoded envelope. It handles auth and common error handling in one place.
func (a *UfficioPostaleAdapter) do(ctx context.Context, method, path string, body any) (*upAPIResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("ufficiopostale: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ufficiopostale: %s %s returned %d: %s", method, path, resp.StatusCode, string(raw))
	}

	var envelope upAPIResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("ufficiopostale: decode response: %w", err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("ufficiopostale: API error: %s", envelope.Message)
	}
	return &envelope, nil
}

// BookShipment submits a postal mailing to Ufficio Postale.
//
// The product is selected by Shipment.ServiceTier:
//   - "" or "raccomandata" → Raccomandata (default, tracked)
//   - "raccomandata_smart" → Raccomandata Smart
//   - "ordinaria"          → Posta Ordinaria (untracked)
//   - "atti_giudiziari"   → Atti Giudiziari
//
// The document content (the letter body Poste Italiane will print and post)
// is taken from Shipment.ShipmentComment. Set it to plain text, HTML,
// a PDF URL (http/https), or a base64 PDF ("data:application/pdf;base64,…").
//
// Bookings are auto-confirmed (autoconfirm=true). No shipping label is
// returned — Poste Italiane handles printing and dispatch internally.
func (a *UfficioPostaleAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	product, err := upProductPath(request.Shipment.ServiceTier)
	if err != nil {
		return nil, err
	}

	document := upDocumentContent(request)
	payload := upBookingPayload(request, document)

	envelope, err := a.do(ctx, http.MethodPost, "/"+product+"/", payload)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: book shipment: %w", err)
	}

	// data is an array of booking items (one per send).
	var items []upBookingItem
	if err := json.Unmarshal(envelope.Data, &items); err != nil {
		return nil, fmt.Errorf("ufficiopostale: decode booking data: %w", err)
	}
	if len(items) == 0 || len(items[0].Destinatari) == 0 {
		return nil, fmt.Errorf("ufficiopostale: booking response contained no recipients")
	}

	dest := items[0].Destinatari[0]
	trackingNumber := dest.NumeroRaccomandata
	if trackingNumber == "" {
		// Untracked products (ordinaria) have no NumeroRaccomandata; use the internal ID.
		trackingNumber = dest.ID
	}

	a.log.Info("ufficiopostale shipment booked",
		zap.String("product", product),
		zap.String("trackingNumber", trackingNumber),
		zap.String("internalID", dest.ID),
	)

	// Encode product and internal ID together so FetchLabel can reconstruct
	// the accettazione path without needing a separate lookup.
	// Format: "{product}/{internalID}" — e.g. "raccomandate/000000000000000000000000".
	shipmentID := product + "/" + dest.ID

	return &BookingResponse{
		ShipmentID:     shipmentID,
		TrackingNumber: trackingNumber,
		Carrier:        "ufficiopostale",
		Status:         strings.ToLower(dest.State),
		BetaWarning:    "Ufficio Postale adapter is in beta — cancellation and update are not supported",
	}, nil
}

// TrackShipment fetches the current status for a tracked mailing via
// GET /tracking/{trackingNumber}.
//
// Tracking is only available for products that return a NumeroRaccomandata:
// Raccomandate, Atti Giudiziari, and Raccomandata Smart. Ordinaria and bulk
// products do not expose tracking data and will return an empty event list.
func (a *UfficioPostaleAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("ufficiopostale: tracking number must not be empty")
	}

	envelope, err := a.do(ctx, http.MethodGet, "/tracking/"+trackingNumber, nil)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: track shipment %s: %w", trackingNumber, err)
	}

	var events []upTrackingEvent
	if err := json.Unmarshal(envelope.Data, &events); err != nil {
		return nil, fmt.Errorf("ufficiopostale: decode tracking data for %s: %w", trackingNumber, err)
	}

	trackingEvents := make([]TrackingEvent, 0, len(events))
	var latestStatus TrackingStatus
	var latestRaw string

	for _, e := range events {
		ns := normaliseUfficioPostaleStatus(e.Type)
		ts := time.Unix(e.Timestamp, 0).UTC().Format(time.RFC3339)
		trackingEvents = append(trackingEvents, TrackingEvent{
			Timestamp:        ts,
			Status:           e.Descrizione,
			NormalizedStatus: ns,
			Details:          e.Descrizione,
		})
		// Last event wins — the API returns events in chronological order.
		latestStatus = ns
		latestRaw = e.Descrizione
	}

	if latestStatus == "" {
		latestStatus = StatusUnknown
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "ufficiopostale",
		Status:           latestRaw,
		NormalizedStatus: latestStatus,
		OriginalStatus:   latestRaw,
		Events:           trackingEvents,
	}, nil
}

// FetchLabel retrieves the postal acceptance receipt (accettazione) for a
// booked mailing via GET /{product}/{id}/accettazione.
//
// Ufficio Postale issues no carrier label — the accettazione is the closest
// equivalent: a PDF receipt confirming the mailing was accepted by Poste
// Italiane, suitable for record-keeping or proof of posting.
//
// The LabelRequest.TrackingNumber must be the ShipmentID returned by
// BookShipment (format: "{product}/{internalID}", e.g.
// "raccomandate/000000000000000000000000"). Only LabelFormatPDF is supported.
func (a *UfficioPostaleAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("ufficiopostale: fetch label: tracking number must not be empty")
	}
	if req.Format != LabelFormatPDF && req.Format != "" {
		return nil, fmt.Errorf("ufficiopostale: fetch label: only PDF format is supported, got %q", req.Format)
	}

	// ShipmentID is encoded as "{product}/{internalID}".
	slash := strings.IndexByte(req.TrackingNumber, '/')
	if slash < 1 || slash == len(req.TrackingNumber)-1 {
		return nil, fmt.Errorf("ufficiopostale: fetch label: tracking number must be the ShipmentID "+
			"in format \"{product}/{internalID}\" — got %q", req.TrackingNumber)
	}
	product := req.TrackingNumber[:slash]
	internalID := req.TrackingNumber[slash+1:]

	path := fmt.Sprintf("/%s/%s/accettazione", product, internalID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: fetch label: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	httpReq.Header.Set("Accept", "application/pdf")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: fetch label %s: %w", internalID, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck
		return nil, fmt.Errorf("ufficiopostale: fetch label %s returned %d: %s", internalID, resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ufficiopostale: fetch label: read response: %w", err)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "ufficiopostale",
		Format:         LabelFormatPDF,
		Data:           base64.StdEncoding.EncodeToString(data),
		MimeType:       "application/pdf",
	}, nil
}

// CancelShipment is not supported by the Ufficio Postale API.
// The API has no cancellation endpoint; once confirmed a mailing cannot be
// revoked programmatically.
func (a *UfficioPostaleAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("Ufficio Postale", "shipment cancellation",
		"the Ufficio Postale API has no cancellation endpoint")
}

// UpdateShipment is not supported by the Ufficio Postale API.
// PATCH endpoints accept only a confirmation boolean and do not allow
// changes to recipient address, document, or service options after creation.
func (a *UfficioPostaleAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Ufficio Postale", "post-booking update",
		"the Ufficio Postale API does not support updating a shipment after creation")
}
