// Package adapter provides the Bring implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/bring.go.
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

// BringAdapter implements CarrierAdapter and ManifestAdapter for Bring.
// Authentication uses X-MyBring-API-Uid (CustomerID) and X-MyBring-API-Key (APIKey).
type BringAdapter struct {
	APIKey         string
	CustomerID     string // Mybring login email — used for API authentication
	CustomerNumber string // Bring customer account number — used in product.customerNumber
	CompanyName    string // Company name sent in pickup booking customerInformation
	BaseURL        string
	HTTPClient     *http.Client
	log            *zap.Logger
}

// NewBringAdapter creates a new BringAdapter.
// customerID is the Mybring login email.
// customerNumber is the Bring customer account number (for billing/invoicing).
// companyName is included in the pickup API customerInformation block.
func NewBringAdapter(apiKey, customerID, customerNumber, companyName string, log *zap.Logger) *BringAdapter {
	return &BringAdapter{
		APIKey:         apiKey,
		CustomerID:     customerID,
		CustomerNumber: customerNumber,
		CompanyName:    companyName,
		BaseURL:        "https://api.bring.com",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// bringProductID maps DeliveryType to the Bring product code.
// When DeliveryType is empty the product is inferred from ServicePointID.
func bringProductID(deliveryType string, hasServicePoint bool) string {
	switch strings.ToLower(deliveryType) {
	case "business":
		return "BUSINESS_PARCEL"
	case "return":
		return "PICKUP_PARCEL"
	case "servicepoint":
		return "PICKUP_PARCEL"
	case "home":
		return "HOME_DELIVERY_PARCEL"
	case "cargo_international":
		return "CARGO_INTERNATIONAL"
	default:
		if hasServicePoint {
			return "PICKUP_PARCEL"
		}
		return "HOME_DELIVERY_PARCEL"
	}
}

// bringNatureOfCargo resolves the Bring natureOfCargo value from Customs.
// When NatureOfCargo is set it is used directly; otherwise falls back to
// deriving from ShipmentType. Defaults to "OTHER".
func bringNatureOfCargo(c Customs) string {
	if c.NatureOfCargo != "" {
		return c.NatureOfCargo
	}
	switch c.ShipmentType {
	case "B2B", "B2C":
		return "SALE_OF_GOODS"
	default:
		return "OTHER"
	}
}

// bringCustomsParty builds the party block used in customsInformation.
// It mirrors the shape of bringParty but omits contact nesting — the
// customs party schema only needs address fields and an optional vatNumber.
func bringCustomsParty(a Address, vatNumber string) map[string]any {
	street := a.Street
	if a.HouseNumber != "" {
		street = a.Street + " " + a.HouseNumber
	}
	p := map[string]any{
		"name":        a.Name,
		"addressLine": street,
		"postalCode":  a.PostalCode,
		"city":        a.City,
		"countryCode": a.Country,
	}
	if vatNumber != "" {
		p["vatNumber"] = vatNumber
	}
	return p
}

// buildBringCustomsInformation constructs the customsInformation product block
// required by Bring for international shipments (Business Parcel 0330, PickUp
// Parcel 0340, Letter Packet 3639). Returns nil when no customs data is present.
//
// Wire format (new structure from March 2025):
//
//	{
//	  "consent": true,
//	  "exporter": { "name", "addressLine", "postalCode", "city", "countryCode", "vatNumber"? },
//	  "importer": { same },
//	  "natureOfCargo": "SALE_OF_GOODS",
//	  "articles": [{ "quantity", "description", "customsTariffCode", "grossWeight", "totalValue", "currency", "countryOfOrigin" }]
//	}
func buildBringCustomsInformation(s Shipment) map[string]any {
	c := s.Customs
	if len(c.Items) == 0 && c.HSCode == "" {
		return nil
	}

	articles := make([]map[string]any, 0, len(c.Items))
	for _, item := range c.Items {
		cur := item.Currency
		if cur == "" {
			cur = c.CustomsCurrency
		}
		articles = append(articles, map[string]any{
			"quantity":          item.Quantity,
			"description":       item.Description,
			"customsTariffCode": item.HSCode,
			"grossWeight":       item.NetWeight * float64(item.Quantity),
			"totalValue":        item.Value,
			"currency":          cur,
			"countryOfOrigin":   item.CountryOfOrigin,
		})
	}

	// Top-level fallback when no line items but a top-level HS code is set.
	if len(articles) == 0 {
		articles = append(articles, map[string]any{
			"quantity":          1,
			"description":       "Goods",
			"customsTariffCode": c.HSCode,
			"grossWeight":       0.5,
			"totalValue":        c.CustomsValue,
			"currency":          c.CustomsCurrency,
			"countryOfOrigin":   c.CountryOfOrigin,
		})
	}

	return map[string]any{
		"consent":       true,
		"exporter":      bringCustomsParty(s.Sender, c.ExporterVATNumber),
		"importer":      bringCustomsParty(s.Receiver, c.ImporterVATNumber),
		"natureOfCargo": bringNatureOfCargo(c),
		"articles":      articles,
	}
}

// bringParty builds a Bring sender or recipient address block.
// Contact details are nested under a "contact" object as required by the Bring API.
func bringParty(a Address) map[string]any {
	street := a.Street
	if a.HouseNumber != "" {
		street = a.Street + " " + a.HouseNumber
	}
	party := map[string]any{
		"name":        a.Name,
		"addressLine": street,
		"postalCode":  a.PostalCode,
		"city":        a.City,
		"countryCode": a.Country,
	}
	contact := map[string]any{}
	if a.Name != "" {
		contact["name"] = a.Name
	}
	if a.Phone != "" {
		contact["phoneNumber"] = a.Phone
	}
	if a.Email != "" {
		contact["email"] = a.Email
	}
	if len(contact) > 0 {
		party["contact"] = contact
	}
	return party
}

// bringPackage converts a single Colli to the Bring package wire format.
// Dimensions are nested under a "dimensions" block.
func bringPackage(c Colli) map[string]any {
	desc := "Goods"
	if len(c.Items) > 0 {
		desc = c.Items[0].Description
	}
	pkg := map[string]any{
		"weightInKg":       c.Weight,
		"goodsDescription": desc,
	}
	if c.Dimensions.Length > 0 || c.Dimensions.Width > 0 || c.Dimensions.Height > 0 {
		pkg["dimensions"] = map[string]any{
			"lengthInCm": c.Dimensions.Length,
			"widthInCm":  c.Dimensions.Width,
			"heightInCm": c.Dimensions.Height,
		}
	}
	return pkg
}

// BookShipment books a shipment with Bring and returns the booking response.
//
// Wire format notes:
//   - Auth via X-MyBring-API-Uid and X-MyBring-API-Key headers (no Bearer).
//   - Endpoint: POST /booking/api/shipment.
//   - Payload wrapped in "consignments" array.
//   - Parties are nested under consignments[0].parties.sender / .recipient.
//   - Product code maps DeliveryType to Bring product IDs.
//   - Service point: pickupPointId placed directly on recipient block.
//   - Response tracking number at consignments[0].confirmation.consignmentNumber.
//   - Label URL at consignments[0].confirmation.links.labels.
//
// Push notifications:
//
// Bring supports push-based event delivery via the Event Cast API
// (https://api.bring.com/event-cast/api-docs). Callers that want real-time
// status updates without polling TrackShipment should register their own
// Event Cast webhook directly with Bring after booking:
//
//	POST https://api.bring.com/event-cast/api/v1/webhooks
//	{ "trackingId": "<consignmentNumber>", "event_groups": ["DELIVERED", ...],
//	  "configuration": { "url": "<caller webhook URL>", ... } }
//
// Webhooks are active for 30 days or until delivery. The gateway does not
// register or proxy Event Cast subscriptions — that is the caller's responsibility.
func (a *BringAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	packages := make([]map[string]any, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		packages[i] = bringPackage(c)
	}

	hasServicePoint := request.Shipment.Receiver.ServicePointID != ""
	productID := bringProductID(request.Shipment.DeliveryType, hasServicePoint)

	recipient := bringParty(request.Shipment.Receiver)
	if hasServicePoint {
		recipient["pickupPointId"] = request.Shipment.Receiver.ServicePointID
	}

	product := map[string]any{
		"id":             productID,
		"customerNumber": a.CustomerNumber,
	}

	// Incoterms — mandatory for CARGO_INTERNATIONAL; optional for other
	// international services. Bring accepts: DDP, DAP, FCA, EXW.
	if request.Shipment.Customs.Incoterms != "" {
		product["incotermRule"] = request.Shipment.Customs.Incoterms
	} else if productID == "CARGO_INTERNATIONAL" {
		return nil, fmt.Errorf("bring: incotermRule is mandatory for CARGO_INTERNATIONAL (error BOOK-INPUT-065)")
	}

	// Customs information — embedded in the product block for services that
	// require cross-border declarations (Business Parcel, PickUp Parcel,
	// Letter Packet). Replaces the legacy ediCustomsInformation field.
	if customsInfo := buildBringCustomsInformation(request.Shipment); customsInfo != nil {
		product["customsInformation"] = customsInfo
	}

	// Build additionalServices from AddOns.
	// Bring service codes: 1091=eAdvising (SMS+email), 0041=flex delivery,
	// 1131=direct signature, 1000=cash on delivery.
	var additionalServices []map[string]any
	if hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) ||
		hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) {
		// 1091 handles both SMS and email notification.
		additionalServices = append(additionalServices, map[string]any{
			"id": "1091",
		})
	}
	if flex, ok := getAddOn(request.Shipment.AddOns, AddOnFlexDelivery); ok {
		flexSvc := map[string]any{"id": "0041"}
		if flex.Instructions != "" {
			flexSvc["instructions"] = flex.Instructions
		}
		additionalServices = append(additionalServices, flexSvc)
	}
	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		additionalServices = append(additionalServices, map[string]any{
			"id": "1131", // Direct signature
		})
	}
	if cod, ok := getAddOn(request.Shipment.AddOns, AddOnCashOnDelivery); ok {
		if cod.CODAmount <= 0 {
			return nil, fmt.Errorf("cash_on_delivery add-on requires CODAmount > 0")
		}
		if cod.CODCurrency == "" {
			return nil, fmt.Errorf("cash_on_delivery add-on requires CODCurrency")
		}
		if cod.CODAccountNumber == "" {
			return nil, fmt.Errorf("cash_on_delivery add-on requires CODAccountNumber")
		}
		additionalServices = append(additionalServices, map[string]any{
			"id": "1000",
			"cashOnDelivery": map[string]any{
				"amount":        cod.CODAmount,
				"currency":      cod.CODCurrency,
				"accountNumber": cod.CODAccountNumber,
			},
		})
	}
	if len(additionalServices) > 0 {
		product["additionalServices"] = additionalServices
	}

	consignment := map[string]any{
		"shippingDateTime": time.Now().UTC().Format("2006-01-02T15:04:05"),
		"parties": map[string]any{
			"sender":    bringParty(request.Shipment.Sender),
			"recipient": recipient,
		},
		"product":  product,
		"packages": packages,
	}

	// Return booking — include returnProduct block.
	if strings.EqualFold(request.Shipment.DeliveryType, "return") {
		returnProduct := map[string]any{
			"id": "9350", // Return Drop Off — customer brings to service point
		}
		// Flex delivery on the return label.
		if flex, ok := getAddOn(request.Shipment.AddOns, AddOnFlexDelivery); ok {
			flexSvc := map[string]any{"id": "0041"}
			if flex.Instructions != "" {
				flexSvc["instructions"] = flex.Instructions
			}
			returnProduct["additionalServices"] = []any{flexSvc}
		}
		consignment["returnProduct"] = returnProduct
	}

	if request.IdempotencyKey != "" {
		consignment["clientReference"] = request.IdempotencyKey
	}

	payload := map[string]any{
		"schemaVersion": 1,
		"consignments":  []any{consignment},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Bring request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.BaseURL+"/booking/api/shipment",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bring request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-MyBring-API-Uid", a.CustomerID)
	req.Header.Set("X-MyBring-API-Key", a.APIKey)
	req.Header.Set("X-Bring-Client-URL", "https://github.com/kristiannissen/carrier-gateway")
	req.Header.Set("X-Bring-Test-Indicator", "false")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bring API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Bring response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("bring API returned status %d: %s", resp.StatusCode, string(body))
	}

	var bringResp struct {
		Consignments []struct {
			Confirmation struct {
				ConsignmentNumber string `json:"consignmentNumber"`
				Links             struct {
					Tracking string `json:"tracking"`
					Labels   string `json:"labels"`
				} `json:"links"`
				Packages []struct {
					PackageNumber string `json:"packageNumber"`
					CorrelationID string `json:"correlationId"`
				} `json:"packages"`
			} `json:"confirmation"`
		} `json:"consignments"`
	}
	if err := json.Unmarshal(body, &bringResp); err != nil {
		return nil, fmt.Errorf("failed to decode Bring response: %w", err)
	}

	if len(bringResp.Consignments) == 0 {
		return nil, fmt.Errorf("bring response contained no consignments")
	}

	confirmation := bringResp.Consignments[0].Confirmation

	result := &BookingResponse{
		ShipmentID:     confirmation.ConsignmentNumber,
		TrackingNumber: confirmation.ConsignmentNumber,
		LabelURL:       confirmation.Links.Labels,
		Carrier:        "bring",
		Status:         "booked",
	}

	if hasServicePoint {
		result.ServicePointID = request.Shipment.Receiver.ServicePointID
	}

	if len(confirmation.Packages) > 0 {
		result.Colli = make([]ColliResponse, len(confirmation.Packages))
		for i, p := range confirmation.Packages {
			result.Colli[i] = ColliResponse{
				ID:     p.PackageNumber,
				Status: "booked",
			}
		}
	}

	return result, nil
}

// CancelShipment cancels a Bring shipment via DELETE /booking/api/shipment/{consignmentNumber}.
// The shipment must not yet have been collected by Bring.
func (a *BringAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/booking/api/shipment/%s", a.BaseURL, trackingNumber), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bring cancel request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-MyBring-API-Uid", a.CustomerID)
	req.Header.Set("X-MyBring-API-Key", a.APIKey)
	req.Header.Set("X-Bring-Client-URL", "https://github.com/kristiannissen/carrier-gateway")
	req.Header.Set("X-Bring-Test-Indicator", "false")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bring cancel request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // best-effort error body read
		return nil, fmt.Errorf("bring cancel returned status %d: %s", resp.StatusCode, string(body))
	}

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "bring",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported for Bring.
// Post-booking updates are not available via the Bring Booking API.
func (a *BringAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("Bring", "post-booking update", "")
}

// FetchLabel retrieves a shipping label from Bring.
// Bring returns a label URL in the booking response; this method fetches it.
// Only PDF format is supported.
func (a *BringAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("Bring", req.Format, LabelFormatPDF)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/booking/api/shipment/labels/%s", a.BaseURL, req.TrackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bring label request: %w", err)
	}
	httpReq.Header.Set("X-MyBring-API-Uid", a.CustomerID)
	httpReq.Header.Set("X-MyBring-API-Key", a.APIKey)

	return fetchLabelFromURL(ctx, a.HTTPClient, httpReq, req, "bring")
}

// TrackShipment retrieves the tracking status for a Bring shipment.
// Uses GET /tracking/api/v2/tracking.json?q={trackingNumber}&lang=en.
//
// The v2 response does not include a package-level statusId field. The
// top-level status is derived from the most recent event in eventSet[0].
func (a *BringAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/tracking/api/v2/tracking.json?q=%s&lang=en", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bring tracking request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-MyBring-API-Uid", a.CustomerID)
	req.Header.Set("X-MyBring-API-Key", a.APIKey)
	req.Header.Set("X-Bring-Client-URL", "https://github.com/kristiannissen/carrier-gateway")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bring tracking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Bring tracking response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bring tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var bringResp struct {
		ConsignmentSet []struct {
			ConsignmentID string `json:"consignmentId"`
			PackageSet    []struct {
				StatusDescription string `json:"statusDescription"`
				EventSet          []struct {
					Description string `json:"description"`
					Status      string `json:"status"`
					ISODateTime string `json:"isoDateTime"`
					City        string `json:"city"`
					CountryCode string `json:"countryCode"`
				} `json:"eventSet"`
			} `json:"packageSet"`
		} `json:"consignmentSet"`
	}
	if err := json.Unmarshal(body, &bringResp); err != nil {
		return nil, fmt.Errorf("failed to decode Bring tracking response: %w", err)
	}

	if len(bringResp.ConsignmentSet) == 0 {
		return nil, fmt.Errorf("no tracking information found for %s", trackingNumber)
	}

	consignment := bringResp.ConsignmentSet[0]
	status := "Unknown"
	var events []TrackingEvent

	if len(consignment.PackageSet) > 0 {
		pkg := consignment.PackageSet[0]
		for _, e := range pkg.EventSet {
			location := e.City
			if e.CountryCode != "" {
				location = e.City + ", " + e.CountryCode
			}
			events = append(events, TrackingEvent{
				Timestamp:        e.ISODateTime,
				Status:           e.Status,
				NormalizedStatus: normalizeStatus("bring", e.Status),
				Location:         location,
				Details:          e.Description,
			})
		}
		// Status is the human-readable description for backward compatibility.
		// The v2 API does not include a package-level statusId; the event code
		// from the most recent event is the authoritative value for normalization.
		if pkg.StatusDescription != "" {
			status = pkg.StatusDescription
		} else if len(events) > 0 {
			status = events[0].Status
		}
	}

	// originalStatus is the event code from the most recent event — used for
	// normalization. Falls back to status when there are no events.
	originalStatus := status
	if len(events) > 0 {
		originalStatus = events[0].Status
	}
	normalizedStatus := normalizeStatus("bring", originalStatus)

	return &TrackingResponse{
		TrackingNumber:   consignment.ConsignmentID,
		Carrier:          "bring",
		Status:           status,
		NormalizedStatus: normalizedStatus,
		OriginalStatus:   originalStatus,
		Events:           events,
	}, nil
}

// BookPickup schedules a carrier collection at the warehouse via the Bring Pickup API.
//
// Wire format: POST /api/create.
// The service is always "PARCEL" for warehouse parcel pickup.
// EstimatedWeight is converted from kg to grams as required by the Bring API.
// The carrier returns a confirmed time window in isoFormattedEarliestPickupDateTime
// and isoFormattedLatestPickupDateTime, which may differ from the requested window.
func (a *BringAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	street := req.Address.Street
	if req.Address.HouseNumber != "" {
		street = req.Address.Street + " " + req.Address.HouseNumber
	}

	pickupAddress := map[string]any{
		"street":      street,
		"postalCode":  req.Address.PostalCode,
		"city":        req.Address.City,
		"email":       req.Contact.Email,
		"phoneNumber": req.Contact.Phone,
	}
	if req.Contact.Name != "" {
		pickupAddress["contactName"] = req.Contact.Name
	}
	if req.Pickup.SpecialInstructions != "" {
		pickupAddress["message"] = req.Pickup.SpecialInstructions
	}
	if req.Pickup.Location != "" {
		pickupAddress["deliveryInstruction"] = req.Pickup.Location
	}

	body := map[string]any{
		"service": "PARCEL",
		"customerInformation": map[string]any{
			"customerNumber": a.CustomerNumber,
			"companyName":    a.CompanyName,
		},
		"pickupAddress": pickupAddress,
		"pickupDate":    req.Pickup.Date,
		"countryCode":   req.Address.Country,
	}

	if req.EstimatedParcels > 0 || req.EstimatedWeight > 0 {
		packages := map[string]any{}
		if req.EstimatedParcels > 0 {
			packages["count"] = req.EstimatedParcels
		}
		if req.EstimatedWeight > 0 {
			packages["weightInGrams"] = int(req.EstimatedWeight * 1000)
		}
		body["pickupDetails"] = map[string]any{
			"packages": packages,
		}
	}

	payloadBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("bring: failed to marshal pickup request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/api/create", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("bring: failed to create pickup request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-MyBring-API-Uid", a.CustomerID)
	httpReq.Header.Set("X-MyBring-API-Key", a.APIKey)
	httpReq.Header.Set("X-Bring-Client-URL", "https://github.com/kristiannissen/carrier-gateway")
	httpReq.Header.Set("X-Bring-Test-Indicator", "false")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bring: pickup API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bring: failed to read pickup response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bring: pickup API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var bringResp struct {
		PickupConfirmation struct {
			Status                             string `json:"status"`
			PackageNumber                      string `json:"packageNumber"`
			ISOFormattedEarliestPickupDateTime string `json:"isoFormattedEarliestPickupDateTime"`
			ISOFormattedLatestPickupDateTime   string `json:"isoFormattedLatestPickupDateTime"`
		} `json:"pickupConfirmation"`
		Errors any `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &bringResp); err != nil {
		return nil, fmt.Errorf("bring: failed to decode pickup response: %w", err)
	}

	if bringResp.PickupConfirmation.Status != "OK" {
		return nil, fmt.Errorf("bring: pickup booking failed: %v", bringResp.Errors)
	}

	result := &PickupResponse{
		Carrier:            "bring",
		ConfirmationNumber: bringResp.PickupConfirmation.PackageNumber,
		Date:               req.Pickup.Date,
		Status:             "booked",
	}

	// Extract HH:MM from the ISO datetime strings returned by Bring.
	if t, err := time.Parse(time.RFC3339Nano, bringResp.PickupConfirmation.ISOFormattedEarliestPickupDateTime); err == nil {
		result.ReadyTime = t.Format("15:04")
	}
	if t, err := time.Parse(time.RFC3339Nano, bringResp.PickupConfirmation.ISOFormattedLatestPickupDateTime); err == nil {
		result.CloseTime = t.Format("15:04")
	}

	return result, nil
}

// UpdatePickup is not supported for Bring.
// The Bring Pickup API has no endpoint for modifying a scheduled pickup.
func (a *BringAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Bring", "pickup update", "cancel and rebook via BookPickup")
}

// CancelPickup is not supported for Bring.
// The Bring Pickup API has no cancellation endpoint.
func (a *BringAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("Bring", "pickup cancellation", "contact Bring customer service to cancel")
}

// CloseManifest is not supported for Bring.
// Bring has no end-of-day or manifest close endpoint; the handover is managed
// by Bring's own systems when the driver scans parcels at collection.
func (a *BringAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Bring", "manifest close", "")
}
