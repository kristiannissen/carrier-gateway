// Package handler provides the HTTP handler for booking shipments.
// This file is located at /internal/handler/bookings.go.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"

	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
	"github.com/kristiannissen/carrier-gateway/internal/notification"
	"github.com/kristiannissen/carrier-gateway/internal/parser"
	"github.com/kristiannissen/carrier-gateway/internal/validation"
)

// BookShipment handles POST /bookings.
func (c *Config) BookShipment(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodPost {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only POST is supported")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	p, err := parser.ForRequest(r)
	if err != nil {
		c.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported content type", err.Error())
		return
	}

	request, err := p.Parse(body)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	flagged, err := validateBookingRequest(request)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	if flagged {
		log.Warn("shipment flagged for manual review",
			zap.String("carrier", request.Carrier),
			zap.String("idempotencyKey", request.IdempotencyKey),
			zap.String("senderPostalCode", request.Shipment.Sender.PostalCode),
			zap.String("receiverPostalCode", request.Shipment.Receiver.PostalCode),
		)
	}

	// Restricted items check — block hard-prohibited goods before hitting the carrier.
	blocked, warned := validation.ValidateRestrictedItems(request.Carrier, request.Shipment)
	if err := validation.RestrictedItemsError(blocked); err != nil {
		c.writeError(w, r, http.StatusBadRequest, "shipment contains prohibited items", err.Error())
		return
	}

	// VIES live VAT validation — only for EU VAT numbers, parallel, non-blocking on outage.
	var customsWarnings []string
	if len(warned) > 0 {
		for _, w := range warned {
			customsWarnings = append(customsWarnings, w.Reason)
		}
	}
	customsWarnings = append(customsWarnings, validateVATNumbersLive(r, request)...)

	carrierAdapter, err := c.selectAdapter(request.Carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	response, err := carrierAdapter.BookShipment(r.Context(), *request)
	if err != nil {
		log.Error("failed to book shipment",
			zap.Error(err),
			zap.String("carrier", request.Carrier),
			zap.String("idempotencyKey", request.IdempotencyKey),
		)
		c.writeError(w, r, http.StatusInternalServerError, "booking failed", err.Error())
		return
	}

	response.FlaggedForReview = flagged
	if adapter.IsBeta(request.Carrier) {
		response.BetaWarning = request.Carrier + " support is in beta and may not be fully functional"
	}
	if len(customsWarnings) > 0 {
		response.CustomsWarnings = append(response.CustomsWarnings, customsWarnings...)
	}

	if request.Notifications != nil && c.NotificationService != nil {
		prefs := notificationPrefsFrom(request.Notifications)
		payload := notification.Payload{
			TrackingNumber: response.TrackingNumber,
			Carrier:        response.Carrier,
		}
		sent, failed := c.NotificationService.Dispatch(r.Context(), notification.EventBooked, prefs, payload)
		response.NotificationsSent = notificationRecordsFrom(sent)
		response.NotificationsFailed = notificationRecordsFrom(failed)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to write response", zap.Error(err))
	}
}

// validateVATNumbersLive runs VIES live checks for any EU VAT numbers on the
// customs block. Both importer and exporter are checked in parallel under a
// shared 2-second deadline. Returns warning strings for any VIES unavailability;
// returns hard errors inline (caller adds them to customsWarnings, not errors,
// because a VIES soft failure must not block the booking).
//
// VIES hard failures (number confirmed invalid) are returned as warning strings
// with a "VIES:" prefix so the caller can surface them without blocking.
func validateVATNumbersLive(r *http.Request, request *adapter.BookingRequest) []string {
	c := request.Shipment.Customs
	if c.ImporterVATNumber == "" && c.ExporterVATNumber == "" {
		return nil
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		warnings []string
	)

	check := func(number, country, label string) {
		defer wg.Done()
		valid, unavailable, err := validation.ValidateVATNumberLive(r.Context(), number, country)
		var msg string
		switch {
		case unavailable:
			msg = fmt.Sprintf("VIES unavailable for %s VAT %s — accepted on format only", label, number)
		case err != nil:
			msg = fmt.Sprintf("VIES: %s VAT number %s may be invalid: %s", label, number, err.Error())
		case valid:
			return
		}
		mu.Lock()
		warnings = append(warnings, msg)
		mu.Unlock()
	}

	if c.ImporterVATNumber != "" {
		wg.Add(1)
		go check(c.ImporterVATNumber, request.Shipment.Receiver.Country, "importer")
	}
	if c.ExporterVATNumber != "" {
		wg.Add(1)
		go check(c.ExporterVATNumber, request.Shipment.Sender.Country, "exporter")
	}
	wg.Wait()

	return warnings
}

// validateBookingRequest runs all stateless validation rules against the
// request. It returns (flagged, error) where flagged is true when the request
// passes hard validation but contains an address that could not be fully
// verified (rural/unrecognised format) and should be reviewed manually.
func validateBookingRequest(request *adapter.BookingRequest) (flagged bool, err error) {
	if request.Carrier == "" {
		return false, fmt.Errorf("carrier is required")
	}

	if request.Shipment.Sender.Name == "" || request.Shipment.Sender.Street == "" ||
		request.Shipment.Sender.City == "" || request.Shipment.Sender.Country == "" {
		return false, fmt.Errorf("sender address is incomplete")
	}
	// Receiver: street/city/postalCode are optional when a service point ID is provided.
	if request.Shipment.Receiver.ServicePointID != "" {
		if request.Shipment.Receiver.Name == "" || request.Shipment.Receiver.Country == "" {
			return false, fmt.Errorf("receiver name and country are required for service point deliveries")
		}
	} else {
		if request.Shipment.Receiver.Name == "" || request.Shipment.Receiver.Street == "" ||
			request.Shipment.Receiver.City == "" || request.Shipment.Receiver.Country == "" {
			return false, fmt.Errorf("receiver address is incomplete")
		}
	}

	if len(request.Shipment.Colli) == 0 {
		return false, fmt.Errorf("shipment must have at least one colli")
	}
	if request.Shipment.TotalWeight <= 0 {
		return false, fmt.Errorf("total weight must be greater than 0")
	}
	for i, colli := range request.Shipment.Colli {
		if colli.Weight <= 0 {
			return false, fmt.Errorf("colli %d: weight must be greater than 0", i)
		}
		for j, item := range colli.Items {
			if item.Weight <= 0 {
				return false, fmt.Errorf("colli %d, item %d: weight must be greater than 0", i, j)
			}
			if item.Quantity <= 0 {
				return false, fmt.Errorf("colli %d, item %d: quantity must be greater than 0", i, j)
			}
		}
	}
	var colliTotalWeight float64
	for _, colli := range request.Shipment.Colli {
		colliTotalWeight += colli.Weight
	}
	if math.Abs(colliTotalWeight-request.Shipment.TotalWeight) > 0.001 {
		return false, fmt.Errorf("total weight does not match sum of colli weights (expected %.2f, got %.2f)",
			colliTotalWeight, request.Shipment.TotalWeight)
	}

	// Idempotency key.
	if err := validation.ValidateIdempotencyKey(request.IdempotencyKey); err != nil {
		return false, err
	}

	// Address validation — sender and receiver.
	for _, party := range []struct {
		label string
		addr  adapter.Address
	}{
		{"sender", request.Shipment.Sender},
		{"receiver", request.Shipment.Receiver},
	} {
		if err := validation.ValidateAddress(party.addr, request.Carrier, party.addr.Country); err != nil {
			if validation.IsReviewRequired(err) {
				flagged = true
				continue
			}
			return false, fmt.Errorf("%s: %w", party.label, err)
		}
	}

	// Package validation — weight, dimensions, girth, colli count.
	if err := validation.ValidateShipment(request.Carrier, request.Shipment); err != nil {
		return false, err
	}

	// Customs validation — mandatory block for non-EU destinations.
	c := request.Shipment.Customs
	isCustomsEmpty := c.Incoterms == "" && c.HSCode == "" && c.CustomsValue == 0 &&
		c.ImporterOfRecord == "" && c.ImporterVATNumber == "" && c.ExporterVATNumber == ""

	if isCustomsEmpty && validation.RequiresCustomsBlock(
		request.Shipment.Sender.Country,
		request.Shipment.Receiver.Country,
	) {
		return false, fmt.Errorf(
			"customs data is required for shipments to %s — provide incoterms, HS code, customs value, and importer of record",
			request.Shipment.Receiver.Country,
		)
	}

	if !isCustomsEmpty {
		if err := validation.ValidateCustoms(
			c,
			request.Shipment.Sender.Country,
			request.Shipment.Receiver.Country,
			c.ShipmentType,
		); err != nil {
			if validation.IsReviewRequired(err) {
				flagged = true
			} else {
				return false, err
			}
		}
	}

	return flagged, nil
}

// notificationPrefsFrom converts adapter.NotificationPreferences into the
// notification.Preferences type used by the notification package.
func notificationPrefsFrom(p *adapter.NotificationPreferences) notification.Preferences {
	if p == nil {
		return notification.Preferences{}
	}
	events := make([]notification.Event, 0, len(p.Events))
	for _, e := range p.Events {
		events = append(events, notification.Event(e))
	}
	return notification.Preferences{
		Webhook: &notification.WebhookPrefs{
			URL:    p.WebhookURL,
			Secret: p.WebhookSecret,
			Events: events,
		},
	}
}

// notificationRecordsFrom converts []notification.Record to []adapter.NotificationRecord.
func notificationRecordsFrom(records []notification.Record) []adapter.NotificationRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]adapter.NotificationRecord, len(records))
	for i, r := range records {
		out[i] = adapter.NotificationRecord{
			Event:     string(r.Event),
			Channel:   r.Channel,
			URL:       r.URL,
			Status:    r.Status,
			Error:     r.Error,
			Timestamp: r.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		}
	}
	return out
}
