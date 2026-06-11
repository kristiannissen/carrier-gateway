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

// BringAdapter implements CarrierAdapter for Bring.
// Authentication uses X-MyBring-API-Uid (CustomerID) and X-MyBring-API-Key (APIKey).
type BringAdapter struct {
	APIKey         string
	CustomerID     string // Mybring login email — used for API authentication
	CustomerNumber string // Bring customer account number — used in product.customerNumber
	BaseURL        string
	HTTPClient     *http.Client
	log            *zap.Logger
}

// NewBringAdapter creates a new BringAdapter.
// customerID is the Mybring login email.
// customerNumber is the Bring customer account number (for billing/invoicing).
func NewBringAdapter(apiKey, customerID, customerNumber string, log *zap.Logger) *BringAdapter {
	return &BringAdapter{
		APIKey:         apiKey,
		CustomerID:     customerID,
		CustomerNumber: customerNumber,
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
	default:
		if hasServicePoint {
			return "PICKUP_PARCEL"
		}
		return "HOME_DELIVERY_PARCEL"
	}
}

// bringParty builds a Bring sender or recipient address block.
// Contact details are nested under a "contact" object as required by the Bring API.
func bringParty(a Address) map[string]interface{} {
	street := a.Street
	if a.HouseNumber != "" {
		street = a.Street + " " + a.HouseNumber
	}
	party := map[string]interface{}{
		"name":        a.Name,
		"addressLine": street,
		"postalCode":  a.PostalCode,
		"city":        a.City,
		"countryCode": a.Country,
	}
	contact := map[string]interface{}{}
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
func bringPackage(c Colli) map[string]interface{} {
	desc := "Goods"
	if len(c.Items) > 0 {
		desc = c.Items[0].Description
	}
	pkg := map[string]interface{}{
		"weightInKg":       c.Weight,
		"goodsDescription": desc,
	}
	if c.Dimensions.Length > 0 || c.Dimensions.Width > 0 || c.Dimensions.Height > 0 {
		pkg["dimensions"] = map[string]interface{}{
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
func (a *BringAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	packages := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		packages[i] = bringPackage(c)
	}

	hasServicePoint := request.Shipment.Receiver.ServicePointID != ""
	productID := bringProductID(request.Shipment.DeliveryType, hasServicePoint)

	recipient := bringParty(request.Shipment.Receiver)
	if hasServicePoint {
		recipient["pickupPointId"] = request.Shipment.Receiver.ServicePointID
	}

	product := map[string]interface{}{
		"id":             productID,
		"customerNumber": a.CustomerNumber,
	}

	// Build additionalServices from AddOns.
	// Bring service codes: 1091=eAdvising (SMS+email), 0041=flex delivery,
	// 1131=direct signature, 1000=cash on delivery.
	var additionalServices []map[string]interface{}
	if hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) ||
		hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) {
		// 1091 handles both SMS and email notification.
		additionalServices = append(additionalServices, map[string]interface{}{
			"id": "1091",
		})
	}
	if flex, ok := getAddOn(request.Shipment.AddOns, AddOnFlexDelivery); ok {
		flexSvc := map[string]interface{}{"id": "0041"}
		if flex.Instructions != "" {
			flexSvc["instructions"] = flex.Instructions
		}
		additionalServices = append(additionalServices, flexSvc)
	}
	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		additionalServices = append(additionalServices, map[string]interface{}{
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
		additionalServices = append(additionalServices, map[string]interface{}{
			"id": "1000",
			"cashOnDelivery": map[string]interface{}{
				"amount":        cod.CODAmount,
				"currency":      cod.CODCurrency,
				"accountNumber": cod.CODAccountNumber,
			},
		})
	}
	if len(additionalServices) > 0 {
		product["additionalServices"] = additionalServices
	}

	consignment := map[string]interface{}{
		"shippingDateTime": time.Now().UTC().Format("2006-01-02T15:04:05"),
		"parties": map[string]interface{}{
			"sender":    bringParty(request.Shipment.Sender),
			"recipient": recipient,
		},
		"product":  product,
		"packages": packages,
	}

	// Return booking — include returnProduct block.
	if strings.EqualFold(request.Shipment.DeliveryType, "return") {
		returnProduct := map[string]interface{}{
			"id": "9350", // Return Drop Off — customer brings to service point
		}
		// Flex delivery on the return label.
		if flex, ok := getAddOn(request.Shipment.AddOns, AddOnFlexDelivery); ok {
			flexSvc := map[string]interface{}{"id": "0041"}
			if flex.Instructions != "" {
				flexSvc["instructions"] = flex.Instructions
			}
			returnProduct["additionalServices"] = []interface{}{flexSvc}
		}
		consignment["returnProduct"] = returnProduct
	}

	if request.IdempotencyKey != "" {
		consignment["clientReference"] = request.IdempotencyKey
	}

	payload := map[string]interface{}{
		"schemaVersion": 1,
		"consignments":  []interface{}{consignment},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Bring request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.BaseURL+"/booking/api/create",
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

// CancelShipment cancels a Bring shipment via DELETE /booking/api/create/{consignmentNumber}.
// The shipment must not yet have been collected by Bring.
func (a *BringAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/booking/api/create/%s", a.BaseURL, trackingNumber), nil)
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
// Uses GET /tracking/api/v2/tracking.json?q={trackingNumber}.
func (a *BringAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/tracking/api/v2/tracking.json?q=%s", a.BaseURL, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bring tracking request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-MyBring-API-Uid", a.CustomerID)
	req.Header.Set("X-MyBring-API-Key", a.APIKey)

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
				StatusID          string `json:"statusId"`
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
		statusID := pkg.StatusID
		status = pkg.StatusDescription
		if status == "" {
			status = statusID
		}
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
	}

	normalizedStatus := StatusUnknown
	originalStatus := ""
	if len(consignment.PackageSet) > 0 {
		originalStatus = consignment.PackageSet[0].StatusID
		normalizedStatus = normalizeStatus("bring", originalStatus)
	}

	return &TrackingResponse{
		TrackingNumber:   consignment.ConsignmentID,
		Carrier:          "bring",
		Status:           status,
		NormalizedStatus: normalizedStatus,
		OriginalStatus:   originalStatus,
		Events:           events,
	}, nil
}
