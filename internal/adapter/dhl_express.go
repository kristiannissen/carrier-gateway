// Package adapter provides the DHL Express (MyDHL API) implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/dhl_express.go.
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

// DHLExpressAdapter implements CarrierAdapter for DHL Express via the MyDHL API v3.3.0.
//
// Authentication: HTTP Basic Auth (username:password) on every request.
// No token caching is required — credentials are sent directly on each call.
//
// Product codes are account- and lane-specific. Set DefaultProductCode to the
// DHL Express global product code agreed with your DHL account manager
// (e.g. "P" = EXPRESS WORLDWIDE, "D" = EXPRESS WORLDWIDE Document).
// Configure via DHL_EXPRESS_PRODUCT_CODE; returns "P" when unset.
//
// Cancel: Not supported — no void/cancel AWB endpoint in the API.
// The DispatchConfirmationNumber in BookingResponse can be used with
// DELETE /pickups/{id} to cancel the courier collection independently.
//
// Update: Not supported — no general update shipment endpoint in the API.
type DHLExpressAdapter struct {
	// Username is the MyDHL API Basic Auth username.
	Username string
	// Password is the MyDHL API Basic Auth password.
	Password string
	// AccountNumber is the DHL Express shipper account number.
	AccountNumber string
	// DefaultProductCode is the DHL Express global product code for all
	// non-return shipments (e.g. "P" = EXPRESS WORLDWIDE).
	// Consult your DHL account manager for the correct code per lane.
	DefaultProductCode string
	// ReturnProductCode is the DHL Express global product code for return
	// shipments. Configured via DHL_EXPRESS_RETURN_PRODUCT_CODE.
	ReturnProductCode string
	// BaseURL is the MyDHL API base URL.
	// Production: https://express.api.dhl.com/mydhlapi
	// Test:       https://express.api.dhl.com/mydhlapi/test
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewDHLExpressAdapter constructs a DHLExpressAdapter ready for production use.
// username and password are the MyDHL API Basic Auth credentials.
// accountNumber is the DHL Express shipper account number.
// defaultProductCode is the DHL Express global product code (e.g. "P").
// returnProductCode is the product code for return shipments; may be empty.
func NewDHLExpressAdapter(username, password, accountNumber, defaultProductCode, returnProductCode string, log *zap.Logger) *DHLExpressAdapter {
	return &DHLExpressAdapter{
		Username:           username,
		Password:           password,
		AccountNumber:      accountNumber,
		DefaultProductCode: defaultProductCode,
		ReturnProductCode:  returnProductCode,
		BaseURL:            "https://express.api.dhl.com/mydhlapi",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// doRequest executes an HTTP request with Basic Auth and returns the response body.
// A non-2xx status is returned as an error containing the response body.
func (a *DHLExpressAdapter) doRequest(req *http.Request) ([]byte, error) {
	req.SetBasicAuth(a.Username, a.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DHL Express request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DHL Express response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("DHL Express returned status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// dhlExpressProductCode resolves the DHL Express global product code from the
// gateway delivery type. Return shipments use ReturnProductCode when set.
func (a *DHLExpressAdapter) dhlExpressProductCode(deliveryType string) string {
	if strings.EqualFold(deliveryType, "return") && a.ReturnProductCode != "" {
		return a.ReturnProductCode
	}
	return a.DefaultProductCode
}

// dhlExpressAddressBlock builds the postalAddress + contactInformation block
// used by shipper, receiver, buyer, and importer fields.
func dhlExpressAddressBlock(addr Address) map[string]any {
	postalAddress := map[string]any{
		"cityName":    addr.City,
		"postalCode":  addr.PostalCode,
		"countryCode": addr.Country,
		"addressLine1": func() string {
			if addr.HouseNumber != "" {
				return addr.Street + " " + addr.HouseNumber
			}
			return addr.Street
		}(),
	}
	if addr.Supplement != "" {
		postalAddress["addressLine2"] = addr.Supplement
	}
	if addr.State != "" {
		postalAddress["provinceCode"] = addr.State
	}

	contact := map[string]any{
		"fullName":    addr.Name,
		"companyName": addr.Name,
	}
	if addr.Phone != "" {
		contact["phone"] = addr.Phone
	}
	if addr.Email != "" {
		contact["email"] = addr.Email
	}

	return map[string]any{
		"postalAddress":      postalAddress,
		"contactInformation": contact,
	}
}

// dhlExpressRegistrationNumbers builds a registrationNumbers array from a
// single typeCode and value. Returns nil when value is empty.
func dhlExpressRegistrationNumbers(typeCode, value string) []any {
	if value == "" {
		return nil
	}
	return []any{
		map[string]any{"typeCode": typeCode, "number": value},
	}
}

// dhlExpressShipmentType converts the gateway B2B/B2C shipment type to the
// DHL Express commercial/personal enum value.
func dhlExpressShipmentType(shipmentType string) string {
	switch strings.ToUpper(shipmentType) {
	case "B2B":
		return "commercial"
	case "B2C":
		return "personal"
	default:
		return shipmentType
	}
}

// dhlExpressExportReasonType maps gateway NatureOfCargo values to the DHL
// Express exportReasonType enum.
func dhlExpressExportReasonType(natureOfCargo string) string {
	switch strings.ToUpper(natureOfCargo) {
	case "SALE_OF_GOODS":
		return "commercial_purpose_or_sale"
	case "GIFT":
		return "gift"
	case "RETURNED_GOODS":
		return "return"
	case "COMMERCIAL_SAMPLE":
		return "sample"
	default:
		return "permanent"
	}
}

// dhlExpressShipmentDescription returns a concise shipment description (max 70
// chars) derived from the first customs item, falling back to "Goods".
func dhlExpressShipmentDescription(customs Customs) string {
	if len(customs.Items) > 0 && customs.Items[0].Description != "" {
		d := customs.Items[0].Description
		if len(d) > 70 {
			return d[:70]
		}
		return d
	}
	return "Goods"
}

// dhlExpressExportDeclaration builds the exportDeclaration block.
// Returns nil when the customs struct carries no declarable data.
func dhlExpressExportDeclaration(customs Customs, log *zap.Logger) map[string]any {
	if len(customs.Items) == 0 {
		return nil
	}

	invoiceDate := customs.InvoiceDate
	if invoiceDate == "" {
		invoiceDate = time.Now().UTC().Format("2006-01-02")
	}

	invoice := map[string]any{
		"number": customs.InvoiceNumber,
		"date":   invoiceDate,
	}

	lineItems := make([]any, len(customs.Items))
	for i, item := range customs.Items {
		origin := item.CountryOfOrigin
		if origin == "" {
			origin = customs.CountryOfOrigin
		}
		lineItem := map[string]any{
			"number":              i + 1,
			"description":         item.Description,
			"price":               item.Value,
			"manufacturerCountry": origin,
			"quantity": map[string]any{
				"value":             item.Quantity,
				"unitOfMeasurement": "PCS",
			},
		}
		if item.NetWeight > 0 {
			lineItem["weight"] = map[string]any{"netValue": item.NetWeight}
		}

		hsCode := item.HSCode
		if hsCode == "" {
			hsCode = customs.HSCode
		}
		if hsCode != "" {
			lineItem["commodityCodes"] = []any{
				map[string]any{"typeCode": "outbound", "value": hsCode},
			}
		}
		lineItems[i] = lineItem
	}

	decl := map[string]any{
		"invoice":   invoice,
		"lineItems": lineItems,
	}

	if customs.Incoterms != "" {
		decl["incoterm"] = customs.Incoterms
	}
	if customs.ShipmentType != "" {
		decl["shipmentType"] = dhlExpressShipmentType(customs.ShipmentType)
	}
	if customs.NatureOfCargo != "" {
		decl["exportReasonType"] = dhlExpressExportReasonType(customs.NatureOfCargo)
	}

	if log != nil && customs.InvoiceNumber == "" {
		log.Warn("DHL Express: Customs.InvoiceNumber is empty — DHL Express requires an invoice number for customs-declarable shipments")
	}

	return decl
}

// BookShipment books a shipment with DHL Express via POST /shipments.
//
// Wire format notes:
//   - Auth: HTTP Basic Auth on every request.
//   - Label returned inline as base64 in response.documents[typeCode=label].content.
//   - AWB returned in response.shipmentTrackingNumber.
//   - Pickup booking reference in response.dispatchConfirmationNumber.
//   - Service point delivery uses onDemandDelivery + buyerDetails.
//   - Customs declaration requires InvoiceNumber and InvoiceDate in Customs.
//   - Insurance via valueAddedServices serviceCode "II".
//   - COD, SMS notifications, PNG labels, and labelless returns are not supported.
func (a *DHLExpressAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	productCode := a.dhlExpressProductCode(request.Shipment.DeliveryType)

	// accounts — shipper account is always required.
	accounts := []any{
		map[string]any{"typeCode": "shipper", "number": a.AccountNumber},
	}

	// customerDetails — shipper and receiver are always required.
	customerDetails := map[string]any{
		"shipperDetails":  dhlExpressAddressBlock(request.Shipment.Sender),
		"receiverDetails": dhlExpressAddressBlock(request.Shipment.Receiver),
	}

	// Importer registration numbers (EORI, VAT, IOSS).
	customs := request.Shipment.Customs
	var importerRegs []any
	if customs.ImporterOfRecord != "" {
		importerRegs = append(importerRegs, map[string]any{"typeCode": "EOR", "number": customs.ImporterOfRecord})
	}
	if customs.ImporterVATNumber != "" {
		importerRegs = append(importerRegs, map[string]any{"typeCode": "VAT", "number": customs.ImporterVATNumber})
	}
	if customs.IossNumber != "" {
		importerRegs = append(importerRegs, map[string]any{"typeCode": "SDT", "number": customs.IossNumber})
	}
	if len(importerRegs) > 0 {
		importerBlock := dhlExpressAddressBlock(request.Shipment.Receiver)
		importerBlock["registrationNumbers"] = importerRegs
		customerDetails["importerDetails"] = importerBlock
	}

	// Exporter VAT number.
	if customs.ExporterVATNumber != "" {
		exporterBlock := dhlExpressAddressBlock(request.Shipment.Sender)
		exporterBlock["registrationNumbers"] = dhlExpressRegistrationNumbers("VAT", customs.ExporterVATNumber)
		customerDetails["exporterDetails"] = exporterBlock
	}

	// On Demand Delivery (service point) — requires buyerDetails.
	isODD := request.Shipment.Receiver.ServicePointID != ""
	if isODD {
		customerDetails["buyerDetails"] = dhlExpressAddressBlock(request.Shipment.Receiver)
	}

	// packages block.
	packages := make([]any, len(request.Shipment.Colli))
	for i, colli := range request.Shipment.Colli {
		pkg := map[string]any{
			"weight":          colli.Weight,
			"referenceNumber": i + 1,
		}
		if colli.Dimensions.Length > 0 || colli.Dimensions.Width > 0 || colli.Dimensions.Height > 0 {
			pkg["dimensions"] = map[string]any{
				"length": colli.Dimensions.Length,
				"width":  colli.Dimensions.Width,
				"height": colli.Dimensions.Height,
			}
		}
		if colli.Reference != "" {
			pkg["customerReferences"] = []any{
				map[string]any{"typeCode": "CU", "value": colli.Reference},
			}
		}
		packages[i] = pkg
	}

	// content block.
	isCustomsDeclarable := len(customs.Items) > 0
	content := map[string]any{
		"packages":            packages,
		"isCustomsDeclarable": isCustomsDeclarable,
		"description":         dhlExpressShipmentDescription(customs),
		"unitOfMeasurement":   "metric",
	}
	incoterms := customs.Incoterms
	if incoterms == "" {
		incoterms = "DAP" // default: Delivered At Place
	}
	content["incoterm"] = incoterms

	if customs.CustomsValue > 0 {
		content["declaredValue"] = customs.CustomsValue
		if customs.CustomsCurrency != "" {
			content["declaredValueCurrency"] = customs.CustomsCurrency
		}
	}

	if isCustomsDeclarable {
		if decl := dhlExpressExportDeclaration(customs, a.log); decl != nil {
			content["exportDeclaration"] = decl
		}
	}

	// valueAddedServices — insurance only; COD and signature not supported.
	var vas []any
	if ins, ok := getAddOn(request.Shipment.AddOns, AddOnInsurance); ok {
		vasEntry := map[string]any{"serviceCode": "II"}
		if ins.InsuranceValue > 0 {
			vasEntry["value"] = ins.InsuranceValue
			if ins.InsuranceCurrency != "" {
				vasEntry["currency"] = ins.InsuranceCurrency
			}
		}
		vas = append(vas, vasEntry)
	}
	if hasAddOn(request.Shipment.AddOns, AddOnCashOnDelivery) && a.log != nil {
		a.log.Warn("DHL Express does not support cash_on_delivery; add-on ignored")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) && a.log != nil {
		a.log.Warn("DHL Express: signature_required is implicit per product; add-on has no effect")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) && a.log != nil {
		a.log.Warn("DHL Express does not support sms_notification; add-on ignored")
	}

	// shipmentNotification — email only.
	var notifications []any
	if hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) && request.Shipment.Receiver.Email != "" {
		notifications = append(notifications, map[string]any{
			"typeCode":   "email",
			"receiverId": request.Shipment.Receiver.Email,
		})
	}

	// pickup — inline courier booking.
	pickup := map[string]any{
		"isRequested": true,
	}

	// onDemandDelivery — service point routing.
	var onDemandDelivery map[string]any
	if isODD {
		onDemandDelivery = map[string]any{
			"deliveryOption":             "servicepoint",
			"servicePointId":             request.Shipment.Receiver.ServicePointID,
			"requestOndemandDeliveryURL": true,
		}
	}

	// plannedShippingDateAndTime — current time formatted per DHL Express spec.
	plannedTime := time.Now().UTC().Format("2006-01-02T15:04:05") + "GMT+00:00"

	// Build full payload.
	payload := map[string]any{
		"plannedShippingDateAndTime": plannedTime,
		"pickup":                     pickup,
		"productCode":                productCode,
		"accounts":                   accounts,
		"customerDetails":            customerDetails,
		"content":                    content,
		"getRateEstimates":           true,
		"getAdditionalInformation": []any{
			map[string]any{"typeCode": "optionalShipmentData"},
		},
		"outputImageProperties": map[string]any{
			"encodingFormat": "pdf",
			"imageOptions": []any{
				map[string]any{
					"typeCode":    "label",
					"isRequested": true,
				},
			},
		},
	}
	if len(vas) > 0 {
		payload["valueAddedServices"] = vas
	}
	if len(notifications) > 0 {
		payload["shipmentNotification"] = notifications
	}
	if onDemandDelivery != nil {
		payload["onDemandDelivery"] = onDemandDelivery
	}
	// Message-Reference carries the idempotency key (max 36 chars).
	messageRef := request.IdempotencyKey
	if len(messageRef) > 36 {
		messageRef = messageRef[:36]
	}
	if request.Shipment.Colli[0].Reference != "" && messageRef == "" {
		ref := request.Shipment.Colli[0].Reference
		if len(ref) > 36 {
			ref = ref[:36]
		}
		messageRef = ref
	}
	// shipment-level customer references.
	if messageRef != "" {
		payload["customerReferences"] = []any{
			map[string]any{"typeCode": "CU", "value": messageRef},
		}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DHL Express booking request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/shipments", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create DHL Express booking request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if messageRef != "" {
		req.Header.Set("Message-Reference", messageRef)
	}

	body, err := a.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("DHL Express booking failed: %w", err)
	}

	var dhlResp struct {
		ShipmentTrackingNumber     string `json:"shipmentTrackingNumber"`
		DispatchConfirmationNumber string `json:"dispatchConfirmationNumber"`
		CancelPickupURL            string `json:"cancelPickupUrl"`
		OnDemandDeliveryURL        string `json:"onDemandDeliveryURL"`
		Packages                   []struct {
			ReferenceNumber int    `json:"referenceNumber"`
			TrackingNumber  string `json:"trackingNumber"`
			TrackingURL     string `json:"trackingUrl"`
		} `json:"packages"`
		Documents []struct {
			TypeCode    string `json:"typeCode"`
			ImageFormat string `json:"imageFormat"`
			Content     string `json:"content"`
		} `json:"documents"`
		ShipmentDetails []struct {
			ShipmentCharges []struct {
				CurrencyType  string  `json:"currencyType"`
				PriceCurrency string  `json:"priceCurrency"`
				Price         float64 `json:"price"`
			} `json:"shipmentCharges"`
			ProductShortName string `json:"productShortName"`
		} `json:"shipmentDetails"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(body, &dhlResp); err != nil {
		return nil, fmt.Errorf("failed to decode DHL Express booking response: %w", err)
	}
	if dhlResp.ShipmentTrackingNumber == "" {
		return nil, fmt.Errorf("DHL Express booking response missing shipmentTrackingNumber")
	}

	result := &BookingResponse{
		ShipmentID:                 dhlResp.ShipmentTrackingNumber,
		TrackingNumber:             dhlResp.ShipmentTrackingNumber,
		Carrier:                    "dhl_express",
		Status:                     "booked",
		DispatchConfirmationNumber: dhlResp.DispatchConfirmationNumber,
		BetaWarning:                "DHL Express integration is in beta — validate in the test environment before going live",
	}

	if isODD && dhlResp.OnDemandDeliveryURL != "" {
		result.LabelURL = dhlResp.OnDemandDeliveryURL
	}

	// Extract billing cost from BILLC currency type.
	if len(dhlResp.ShipmentDetails) > 0 {
		sd := dhlResp.ShipmentDetails[0]
		result.ServiceLevel = sd.ProductShortName
		for _, charge := range sd.ShipmentCharges {
			if charge.CurrencyType == "BILLC" {
				result.Cost = charge.Price
				result.Currency = charge.PriceCurrency
				break
			}
		}
	}

	// Extract inline label from shipment-level documents.
	var labelData string
	for _, doc := range dhlResp.Documents {
		if strings.EqualFold(doc.TypeCode, "label") {
			labelData = doc.Content
			break
		}
	}

	// Build per-colli response from packages.
	colliResp := make([]ColliResponse, len(dhlResp.Packages))
	for i, pkg := range dhlResp.Packages {
		cr := ColliResponse{
			ID:             pkg.TrackingNumber,
			TrackingNumber: pkg.TrackingNumber,
			Status:         "booked",
		}
		if i == 0 && labelData != "" {
			cr.LabelURL = labelData
		}
		colliResp[i] = cr
	}
	// Fallback when packages array is empty (single-piece response).
	if len(colliResp) == 0 && labelData != "" {
		colliResp = []ColliResponse{{
			ID:             dhlResp.ShipmentTrackingNumber,
			TrackingNumber: dhlResp.ShipmentTrackingNumber,
			LabelURL:       labelData,
			Status:         "booked",
		}}
	}
	result.Colli = colliResp

	if len(dhlResp.Warnings) > 0 {
		result.AddOnWarnings = dhlResp.Warnings
	}

	return result, nil
}

// TrackShipment retrieves DHL Express tracking via GET /shipments/{id}/tracking.
// Auth: HTTP Basic Auth — same credentials as booking, no separate API key needed.
func (a *DHLExpressAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	url := fmt.Sprintf("%s/shipments/%s/tracking?trackingView=all-checkpoints&levelOfDetail=shipment", a.BaseURL, trackingNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DHL Express tracking request: %w", err)
	}

	body, err := a.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("DHL Express tracking failed: %w", err)
	}

	var trackResp struct {
		Shipments []struct {
			ShipmentTrackingNumber string `json:"shipmentTrackingNumber"`
			Events                 []struct {
				Date        string `json:"date"`
				Time        string `json:"time"`
				TypeCode    string `json:"typeCode"`
				Description string `json:"description"`
				ServiceArea []struct {
					Description string `json:"description"`
				} `json:"serviceArea"`
			} `json:"events"`
			EstimatedDeliveryDate string `json:"estimatedDeliveryDate"`
		} `json:"shipments"`
	}
	if err := json.Unmarshal(body, &trackResp); err != nil {
		return nil, fmt.Errorf("failed to decode DHL Express tracking response: %w", err)
	}
	if len(trackResp.Shipments) == 0 {
		return nil, fmt.Errorf("DHL Express tracking: no shipments found for %s", trackingNumber)
	}

	s := trackResp.Shipments[0]

	events := make([]TrackingEvent, len(s.Events))
	for i, e := range s.Events {
		ts := e.Date
		if e.Time != "" {
			ts = e.Date + "T" + e.Time
		}
		var location string
		if len(e.ServiceArea) > 0 {
			location = e.ServiceArea[0].Description
		}
		events[i] = TrackingEvent{
			Timestamp:        ts,
			Status:           e.TypeCode,
			NormalizedStatus: normalizeStatus("dhl_express", e.TypeCode),
			Location:         location,
			Details:          e.Description,
		}
	}

	// Current status is the most recent event (index 0 — DHL returns newest first).
	rawStatus := ""
	if len(s.Events) > 0 {
		rawStatus = s.Events[0].TypeCode
	}

	return &TrackingResponse{
		ShipmentID:        s.ShipmentTrackingNumber,
		TrackingNumber:    s.ShipmentTrackingNumber,
		Carrier:           "dhl_express",
		Status:            rawStatus,
		NormalizedStatus:  normalizeStatus("dhl_express", rawStatus),
		OriginalStatus:    rawStatus,
		EstimatedDelivery: s.EstimatedDeliveryDate,
		Events:            events,
	}, nil
}

// FetchLabel retrieves a DHL Express shipping label via GET /shipments/{id}/get-image.
// Only PDF, ZPL, and EPL formats are supported. PNG is not available in the Express API.
func (a *DHLExpressAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	switch req.Format {
	case LabelFormatPDF, LabelFormatZPL, LabelFormatEPL:
		// supported
	default:
		return nil, unsupportedFormat("DHL Express", req.Format, LabelFormatPDF, LabelFormatZPL, LabelFormatEPL)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	url := fmt.Sprintf("%s/shipments/%s/get-image?typeCode=label", a.BaseURL, req.TrackingNumber)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DHL Express label request: %w", err)
	}

	body, err := a.doRequest(httpReq)
	if err != nil {
		return nil, fmt.Errorf("DHL Express label fetch failed: %w", err)
	}

	var imgResp struct {
		Documents []struct {
			TypeCode    string `json:"typeCode"`
			ImageFormat string `json:"imageFormat"`
			Content     string `json:"content"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(body, &imgResp); err != nil {
		return nil, fmt.Errorf("failed to decode DHL Express label response: %w", err)
	}

	for _, doc := range imgResp.Documents {
		if strings.EqualFold(doc.TypeCode, "label") && doc.Content != "" {
			return &LabelResponse{
				TrackingNumber: req.TrackingNumber,
				Carrier:        "dhl_express",
				Format:         req.Format,
				Data:           doc.Content,
				MimeType:       MimeTypeForFormat(req.Format),
			}, nil
		}
	}

	return nil, fmt.Errorf("DHL Express label fetch: no label document found for %s", req.TrackingNumber)
}

// CancelShipment is not supported for DHL Express — no void/cancel AWB endpoint exists.
// Use the DispatchConfirmationNumber from BookingResponse with DELETE /pickups/{id}
// to cancel the courier collection booking independently.
func (a *DHLExpressAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DHL Express", "cancel shipment", "no void AWB endpoint; use DispatchConfirmationNumber to cancel the pickup booking via DELETE /pickups/{id}")
}

// UpdateShipment is not supported for DHL Express — no general update shipment endpoint exists.
func (a *DHLExpressAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DHL Express", "update shipment", "contact DHL Express customer service")
}
