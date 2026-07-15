// Package adapter provides the DHL eCommerce UK implementation of CarrierAdapter and ManifestAdapter.
// This file is located at /internal/adapter/dhl_ecommerce_uk.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DHLEcomUKAdapter implements CarrierAdapter and ManifestAdapter for the DHL
// eCommerce UK platform (api.dhl.com/parceluk).
//
// Authentication: OAuth2 client_credentials, tokens valid 60 minutes.
// Token endpoint: POST /auth/v1/accesstoken (application/x-www-form-urlencoded).
//
// Label flow: POST /shipping/v1/label returns the label inline as a base64
// string. FetchLabel falls back to GET /reprintlabels/v1/labels when called
// after the initial booking.
//
// Cancellation: POST /shipping/v1/cancellation requires the consignee postal
// code in addition to the shipment ID. This adapter caches the consignee postal
// code at booking time so in-process cancellations work transparently. Shipments
// booked outside this process cannot be cancelled via the interface — call DHL
// customer service in that case.
//
// Manifest: DHL eCommerce UK has no manifest or end-of-day closeout API.
// CloseManifest returns ErrNotSupported.
//
// Amendment: The /shipping/v1/amendment endpoint schema is incompatible with
// UpdateRequest. UpdateShipment returns ErrNotSupported; use the DHL portal
// or the amendment endpoint directly for post-booking changes.
type DHLEcomUKAdapter struct {
	// PickupAccount is the DHL account number used as pickupAccount on every
	// shipment creation and cancellation request.
	PickupAccount string
	// TradingLocationID is the customer trading location used for pickup booking.
	// Obtain via GET /customer/v1/tradingLocations or from your DHL account manager.
	TradingLocationID string
	// ClientID is the OAuth2 client_id.
	ClientID string
	// ClientSecret is the OAuth2 client_secret.
	ClientSecret string
	// DefaultProductCode is the 3-digit DHL UK product/service code applied to
	// every outbound shipment unless DeliveryType overrides it.
	// Example: "220" (Signature At Address Next Day). Consult your DHL account
	// manager for the correct code.
	DefaultProductCode string
	// ReturnProductCode is the 3-digit product code used when DeliveryType is
	// "return". Defaults to DefaultProductCode when empty.
	ReturnProductCode string
	// ReturnAccount is the DHL return account number required when
	// inBoxReturn=true. Configure via DHLECS_UK_RETURN_ACCOUNT.
	ReturnAccount string
	// BaseURL is the DHL eCommerce UK API base URL.
	// Production: https://api.dhl.com/parceluk
	// UAT:        https://api-uat.dhl.com/parceluk
	BaseURL    string
	HTTPClient *http.Client

	tokenCache tokenCache

	// postalCodeCache maps shipmentID → consignee postalCode for cancellation.
	// Populated at BookShipment time; read at CancelShipment time.
	postalCodeMu    sync.Mutex
	postalCodeCache map[string]string

	log *zap.Logger
}

// NewDHLEcomUKAdapter constructs a DHLEcomUKAdapter ready for production use.
//
// pickupAccount is the DHL account number.
// tradingLocationID is required for pickup booking; pass empty string to defer.
// clientID and clientSecret are the OAuth2 credentials.
// defaultProductCode is the 3-digit product code for standard outbound shipments.
func NewDHLEcomUKAdapter(
	pickupAccount, tradingLocationID, clientID, clientSecret, defaultProductCode string,
	log *zap.Logger,
) *DHLEcomUKAdapter {
	return &DHLEcomUKAdapter{
		PickupAccount:      pickupAccount,
		TradingLocationID:  tradingLocationID,
		ClientID:           clientID,
		ClientSecret:       clientSecret,
		DefaultProductCode: defaultProductCode,
		BaseURL:            "https://api.dhl.com/parceluk",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		postalCodeCache: make(map[string]string),
		log:             log,
	}
}

// fetchToken obtains a new OAuth2 access token via the client_credentials flow.
// Endpoint: POST /auth/v1/accesstoken (application/x-www-form-urlencoded).
func (a *DHLEcomUKAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/auth/v1/accesstoken", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create DHL UK token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DHL UK token request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read DHL UK token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DHL UK token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return fmt.Errorf("decode DHL UK token response: %w", err)
	}
	if tok.AccessToken == "" {
		return fmt.Errorf("DHL UK token response contained no access_token")
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tok.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()
	return nil
}

// bearerToken returns a valid Bearer token, refreshing if the cached one is expired.
func (a *DHLEcomUKAdapter) bearerToken(ctx context.Context) (string, error) {
	a.tokenCache.mu.Lock()
	valid := a.tokenCache.valid()
	token := a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()

	if valid {
		return token, nil
	}
	if err := a.fetchToken(ctx); err != nil {
		return "", err
	}
	a.tokenCache.mu.Lock()
	token = a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()
	return token, nil
}

// doRequest executes an authenticated JSON request and returns the response body.
// Non-2xx responses are returned as an error containing the response body.
func (a *DHLEcomUKAdapter) doRequest(ctx context.Context, method, path string, payload []byte) ([]byte, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtain DHL UK token: %w", err)
	}

	var body io.Reader
	if len(payload) > 0 {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create DHL UK request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DHL UK request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read DHL UK response: %w", err)
	}

	// 202 Accepted is normal for cancellation.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("DHL UK %s %s returned %d: %s", method, path, resp.StatusCode, respBody)
	}
	return respBody, nil
}

// dhlUKLabelFormat maps gateway label formats to the DHL UK API format query value.
// PNG_RAW is used instead of PNG to avoid the ZIP wrapper the API applies to plain PNG.
func dhlUKLabelFormat(f LabelFormat) (string, bool) {
	switch f {
	case LabelFormatZPL:
		return "ZPL", true
	case LabelFormatPDF:
		return "PDF", true
	case LabelFormatPNG:
		return "PNG_RAW", true // individual base64 images, no ZIP
	default:
		return "", false
	}
}

// dhlUKProductCode returns the 3-digit product code for a booking request.
func (a *DHLEcomUKAdapter) dhlUKProductCode(deliveryType string) string {
	if strings.EqualFold(deliveryType, "return") && a.ReturnProductCode != "" {
		return a.ReturnProductCode
	}
	return a.DefaultProductCode
}

// dhlUKHSCode normalises an HS code to the 8-digit no-dot format required by
// the DHL UK API. Returns empty string when the result would be shorter than 6
// digits (likely invalid) or longer than 8 digits after stripping.
func dhlUKHSCode(raw string) string {
	stripped := strings.ReplaceAll(raw, ".", "")
	stripped = strings.ReplaceAll(stripped, " ", "")
	switch {
	case len(stripped) < 6:
		return ""
	case len(stripped) > 8:
		return stripped[:8]
	default:
		return stripped
	}
}

// dhlUKAddressBlock builds the consignee or sender address map from a gateway Address.
func dhlUKAddressBlock(addr Address) map[string]any {
	street := addr.Street
	if addr.HouseNumber != "" {
		street = addr.Street + " " + addr.HouseNumber
	}
	m := map[string]any{
		"name":       addr.Name,
		"address1":   street,
		"city":       addr.City,
		"postalCode": addr.PostalCode,
		"country":    addr.Country,
	}
	if addr.Supplement != "" {
		m["address2"] = addr.Supplement
	}
	if addr.State != "" {
		m["state"] = addr.State
	}
	if addr.Phone != "" {
		m["phone"] = addr.Phone
	}
	if addr.Email != "" {
		m["email"] = addr.Email
	}
	return m
}

// BookShipment books a shipment with DHL eCommerce UK via POST /shipping/v1/label.
//
// The label is returned inline in the booking response when includeLabel=INCLUDE
// (the default). The label data is stored in ColliResponse.LabelURL as a base64
// string so callers do not need a separate FetchLabel call.
//
// Multi-colli shipments: the API accepts exactly one shipment per request. When
// the booking contains more than one colli, each colli is booked as a separate
// shipment in sequence. The first shipment ID is used as the master tracking
// number; all per-colli tracking numbers are included in the Colli response.
//
// Returns: DeliveryType="return" requires ReturnAccount to be configured.
// Service-point delivery requires Receiver.ServicePointID to be set.
func (a *DHLEcomUKAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("DHL UK: shipment must contain at least one colli")
	}

	productCode := a.dhlUKProductCode(req.Shipment.DeliveryType)

	senderAddr := dhlUKAddressBlock(req.Shipment.Sender)
	senderAddr["email"] = req.Shipment.Sender.Email
	senderAddr["phone"] = req.Shipment.Sender.Phone

	isReturn := strings.EqualFold(req.Shipment.DeliveryType, "return")
	isServicePoint := req.Shipment.Receiver.ServicePointID != ""

	// consigneeAddress
	consigneeAddr := dhlUKAddressBlock(req.Shipment.Receiver)
	if isServicePoint {
		consigneeAddr["addressType"] = "servicePoint"
		consigneeAddr["locationId"] = req.Shipment.Receiver.ServicePointID
		consigneeAddr["locationType"] = "ParcelShop"
		consigneeAddr["recipientType"] = "business"
	} else {
		consigneeAddr["addressType"] = "doorstep"
		consigneeAddr["recipientType"] = "residential"
	}

	// Build per-colli bookings. API accepts exactly one shipment per call.
	var (
		masterTrackingNumber string
		colliResponses       []ColliResponse
		warnings             []string
	)

	for i, colli := range req.Shipment.Colli {
		shipmentDetails := map[string]any{
			"orderedProduct": productCode,
			"totalPieces":    1,
			"totalWeight":    colli.Weight,
		}
		if colli.Reference != "" {
			ref := colli.Reference
			if len(ref) > 20 {
				ref = ref[:20]
			}
			shipmentDetails["customerRef1"] = ref
		}
		if req.Shipment.ShipmentComment != "" {
			instr := req.Shipment.ShipmentComment
			if len(instr) > 60 {
				instr = instr[:60]
			}
			shipmentDetails["deliveryInstructions"] = instr
		}
		if ins, ok := getAddOn(req.Shipment.AddOns, AddOnInsurance); ok && ins.InsuranceValue > 0 {
			shipmentDetails["totalDeclaredValue"] = ins.InsuranceValue
			if ins.InsuranceCurrency != "" {
				shipmentDetails["currency"] = ins.InsuranceCurrency
			}
			// Extended liability is the DHL UK mechanism for declared value coverage.
			// Units 1–5 map to value tiers; use 1 as the base tier.
			shipmentDetails["extendedLiability"] = map[string]any{"extendedLiabilityUnits": 1}
		}

		// Age-verification or signature delivery choice.
		if hasAddOn(req.Shipment.AddOns, AddOnSignatureRequired) {
			shipmentDetails["deliveryChoice"] = "SIG"
		}

		// Incoterms → dutiesPaid.
		customs := req.Shipment.Customs
		if customs.Incoterms != "" {
			shipmentDetails["dutiesPaid"] = strings.ToUpper(customs.Incoterms)
		}

		// IOSS.
		if customs.IossNumber != "" {
			shipmentDetails["iossShipment"] = true
		}

		// In-box return.
		var returnBlock map[string]any
		if isReturn {
			if a.ReturnAccount == "" {
				return nil, fmt.Errorf("DHL UK: DeliveryType=return requires ReturnAccount (configure DHLECS_UK_RETURN_ACCOUNT)")
			}
			shipmentDetails["inBoxReturn"] = true
			returnBlock = map[string]any{
				"returnAccount": a.ReturnAccount,
			}
			if colli.Reference != "" {
				ref := colli.Reference
				if len(ref) > 20 {
					ref = ref[:20]
				}
				returnBlock["customerRef1"] = ref
			}
		}

		// Piece dimensions.
		piece := map[string]any{"weight": colli.Weight}
		if d := colli.Dimensions; d.Length > 0 || d.Width > 0 || d.Height > 0 {
			piece["length"] = d.Length
			piece["width"] = d.Width
			piece["height"] = d.Height
		}

		// Customs items.
		if len(customs.Items) > 0 {
			customsDetails := make([]any, 0, len(customs.Items))
			for _, item := range customs.Items {
				cd := map[string]any{
					"itemDescription":  item.Description,
					"itemValue":        item.Value,
					"itemWeight":       item.NetWeight,
					"packagedQuantity": item.Quantity,
				}
				if item.NetWeight == 0 {
					cd["itemWeight"] = colli.Weight / float64(len(customs.Items))
				}
				if hsCode := dhlUKHSCode(item.HSCode); hsCode != "" {
					cd["hsCode"] = hsCode
				} else if hsCode = dhlUKHSCode(customs.HSCode); hsCode != "" {
					cd["hsCode"] = hsCode
				}
				origin := item.CountryOfOrigin
				if origin == "" {
					origin = customs.CountryOfOrigin
				}
				if origin != "" {
					cd["countryOfOrigin"] = origin
				}
				customsDetails = append(customsDetails, cd)
			}
			piece["customsDetails"] = customsDetails
		}

		// Customs invoice.
		var customsInvoice map[string]any
		if customs.NatureOfCargo != "" || customs.InvoiceNumber != "" {
			customsInvoice = map[string]any{}
			customsInvoice["reasonForExport"] = dhlUKReasonForExport(customs.NatureOfCargo)
			if customs.InvoiceNumber != "" {
				customsInvoice["number"] = customs.InvoiceNumber
			}
			if customs.InvoiceDate != "" {
				customsInvoice["date"] = customs.InvoiceDate
			}
		}

		// Sender/recipient customs registrations.
		var senderRegs, recipientRegs []any
		if customs.IossNumber != "" {
			senderRegs = append(senderRegs, map[string]any{"type": "IOSS", "id": customs.IossNumber})
		}
		if customs.ExporterVATNumber != "" {
			senderRegs = append(senderRegs, map[string]any{"type": "VAT", "id": customs.ExporterVATNumber})
		}
		if customs.ImporterVATNumber != "" {
			recipientRegs = append(recipientRegs, map[string]any{"type": "VAT", "id": customs.ImporterVATNumber})
		}
		if customs.ImporterOfRecord != "" {
			recipientRegs = append(recipientRegs, map[string]any{"type": "EORI", "id": customs.ImporterOfRecord})
		}

		shipment := map[string]any{
			"consigneeAddress": consigneeAddr,
			"shipmentDetails":  shipmentDetails,
			"pieces":           []any{piece},
		}
		if returnBlock != nil {
			shipment["return"] = returnBlock
		}
		if customsInvoice != nil {
			shipment["customsInvoice"] = customsInvoice
		}
		if len(senderRegs) > 0 {
			shipment["senderCustomsRegistrations"] = senderRegs
		}
		if len(recipientRegs) > 0 {
			shipment["recipientCustomsRegistrations"] = recipientRegs
		}

		// recipientAddress is required when the consignee is a service point.
		if isServicePoint {
			shipment["recipientAddress"] = dhlUKAddressBlock(req.Shipment.Receiver)
		}

		body := map[string]any{
			"pickupAccount": a.PickupAccount,
			"dropOffType":   "PICKUP",
			"senderAddress": senderAddr,
			"shipments":     []any{shipment},
		}

		labelFmt := "ZPL" // default: ZPL is thermal-printer ready
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("DHL UK: marshal booking request (colli %d): %w", i, err)
		}

		path := fmt.Sprintf("/shipping/v1/label?includeLabel=INCLUDE&format=%s&pageSize=label6x4", labelFmt)
		respBody, err := a.doRequest(ctx, http.MethodPost, path, payload)
		if err != nil {
			return nil, fmt.Errorf("DHL UK: booking request (colli %d): %w", i, err)
		}

		var dhlResp struct {
			Shipments []struct {
				ShipmentID       string   `json:"shipmentId"`
				ReturnShipmentID string   `json:"returnShipmentId"`
				Labels           []string `json:"labels"`
			} `json:"shipments"`
		}
		if err := json.Unmarshal(respBody, &dhlResp); err != nil {
			return nil, fmt.Errorf("DHL UK: decode booking response (colli %d): %w", i, err)
		}
		if len(dhlResp.Shipments) == 0 || dhlResp.Shipments[0].ShipmentID == "" {
			return nil, fmt.Errorf("DHL UK: booking response (colli %d) missing shipmentId", i)
		}

		s := dhlResp.Shipments[0]
		if i == 0 {
			masterTrackingNumber = s.ShipmentID
		}

		// Cache consignee postal code for CancelShipment.
		a.postalCodeMu.Lock()
		a.postalCodeCache[s.ShipmentID] = req.Shipment.Receiver.PostalCode
		a.postalCodeMu.Unlock()

		cr := ColliResponse{
			ID:             colli.ID,
			Reference:      colli.Reference,
			TrackingNumber: s.ShipmentID,
			Status:         "booked",
		}
		if len(s.Labels) > 0 {
			cr.LabelURL = s.Labels[0]
		}
		colliResponses = append(colliResponses, cr)

		if isReturn && s.ReturnShipmentID != "" {
			colliResponses = append(colliResponses, ColliResponse{
				ID:             colli.ID + "_return",
				Reference:      colli.Reference,
				TrackingNumber: s.ReturnShipmentID,
				Status:         "booked",
				LabelURL: func() string {
					if len(s.Labels) > 1 {
						return s.Labels[1]
					}
					return ""
				}(),
			})
		}

		a.log.Info("DHL UK shipment booked",
			zap.String("shipmentID", s.ShipmentID),
			zap.Int("colliIndex", i),
		)
	}

	if hasAddOn(req.Shipment.AddOns, AddOnCashOnDelivery) {
		warnings = append(warnings, "DHL eCommerce UK does not support cash_on_delivery; add-on ignored")
	}
	if hasAddOn(req.Shipment.AddOns, AddOnEmailNotification) && req.Shipment.Receiver.Email != "" {
		// DHL UK sends pre-delivery notifications automatically when consignee email is set.
		// No extra add-on mapping needed; note it for callers.
		a.log.Debug("DHL UK: consignee email set — DHL will send pre-delivery notifications automatically")
	}

	result := &BookingResponse{
		TrackingNumber: masterTrackingNumber,
		ShipmentID:     masterTrackingNumber,
		Carrier:        "dhl_ecommerce_uk",
		Status:         "booked",
		Colli:          colliResponses,
		BetaWarning:    "DHL eCommerce UK integration is in beta — validate in the UAT environment before going live",
	}
	if len(warnings) > 0 {
		result.AddOnWarnings = warnings
	}
	return result, nil
}

// dhlUKReasonForExport maps gateway NatureOfCargo values to the DHL UK
// customsInvoice.reasonForExport enum.
func dhlUKReasonForExport(natureOfCargo string) string {
	switch strings.ToUpper(natureOfCargo) {
	case "GIFT":
		return "gift"
	case "DOCUMENTS":
		return "documents"
	case "COMMERCIAL_SAMPLE":
		return "commercialSample"
	default:
		return "commercialSale"
	}
}

// TrackShipment retrieves tracking events via GET /tracking/v1/shipments.
//
// The DHL UK tracking API returns a statusCode of "pre-transit", "transit",
// "delivered", "failure", or "unknown". These are normalised to the gateway
// TrackingStatus values using the "dhl_ecommerce_uk" entry in normalizedStatuses.
func (a *DHLEcomUKAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("DHL UK: trackingNumber must not be empty")
	}

	path := "/tracking/v1/shipments?trackingNumber=" + url.QueryEscape(trackingNumber) + "&service=parcelUk"
	respBody, err := a.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("DHL UK: tracking request: %w", err)
	}

	var trackResp struct {
		Shipments []struct {
			ID     string `json:"id"`
			Status struct {
				Timestamp string `json:"timestamp"`
				Location  struct {
					Address struct{ AddressLocality string }
				} `json:"location"`
				Status      string `json:"status"`
				Description string `json:"description"`
				StatusCode  string `json:"statusCode"`
			} `json:"status"`
			Events []struct {
				Timestamp string `json:"timestamp"`
				Location  struct {
					Address struct{ AddressLocality string }
				} `json:"location"`
				Status      string `json:"status"`
				Description string `json:"description"`
				StatusCode  string `json:"statusCode"`
			} `json:"events"`
			EstimatedDeliveryTime string `json:"estimatedDeliveryTime"`
		} `json:"shipments"`
	}
	if err := json.Unmarshal(respBody, &trackResp); err != nil {
		return nil, fmt.Errorf("DHL UK: decode tracking response: %w", err)
	}
	if len(trackResp.Shipments) == 0 {
		return nil, fmt.Errorf("DHL UK: no tracking data found for %s", trackingNumber)
	}

	s := trackResp.Shipments[0]

	events := make([]TrackingEvent, len(s.Events))
	for i, e := range s.Events {
		events[i] = TrackingEvent{
			Timestamp:        e.Timestamp,
			Status:           e.StatusCode,
			NormalizedStatus: normalizeStatus("dhl_ecommerce_uk", e.StatusCode),
			Location:         e.Location.Address.AddressLocality,
			Details:          e.Description,
		}
	}

	rawStatus := s.Status.StatusCode
	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "dhl_ecommerce_uk",
		Status:            rawStatus,
		NormalizedStatus:  normalizeStatus("dhl_ecommerce_uk", rawStatus),
		OriginalStatus:    rawStatus,
		EstimatedDelivery: s.EstimatedDeliveryTime,
		Events:            events,
	}, nil
}

// FetchLabel retrieves a label via GET /reprintlabels/v1/labels.
//
// Supported formats: ZPL, PDF, PNG. For PNG the adapter requests PNG_RAW from
// the DHL UK API to receive individual base64-encoded images rather than a ZIP.
// EPL is not supported by the DHL UK API.
func (a *DHLEcomUKAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	dhlFmt, ok := dhlUKLabelFormat(req.Format)
	if !ok {
		return nil, unsupportedFormat("DHL eCommerce UK", req.Format, LabelFormatZPL, LabelFormatPDF, LabelFormatPNG)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("DHL UK: trackingNumber must not be empty")
	}

	path := fmt.Sprintf("/reprintlabels/v1/labels?shipmentId=%s&format=%s",
		url.QueryEscape(req.TrackingNumber), dhlFmt)
	respBody, err := a.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("DHL UK: label fetch: %w", err)
	}

	var labelResp struct {
		ShipmentID string `json:"shipmentId"`
		Labels     []struct {
			Label string `json:"label"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(respBody, &labelResp); err != nil {
		return nil, fmt.Errorf("DHL UK: decode label response: %w", err)
	}
	if len(labelResp.Labels) == 0 || labelResp.Labels[0].Label == "" {
		return nil, fmt.Errorf("DHL UK: no label returned for shipment %s", req.TrackingNumber)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dhl_ecommerce_uk",
		Format:         req.Format,
		Data:           labelResp.Labels[0].Label,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// CancelShipment requests cancellation via POST /shipping/v1/cancellation.
//
// The DHL UK API requires the consignee postal code alongside the shipment ID.
// This adapter caches the postal code at BookShipment time. Shipments not booked
// through this adapter instance (different process or restart) cannot be cancelled
// — contact DHL customer service in that case.
//
// Shipments already scanned at a DHL depot cannot be cancelled; the carrier will
// return an error.
func (a *DHLEcomUKAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("DHL UK: trackingNumber must not be empty")
	}

	a.postalCodeMu.Lock()
	postalCode := a.postalCodeCache[trackingNumber]
	a.postalCodeMu.Unlock()

	if postalCode == "" {
		return nil, fmt.Errorf("DHL UK: cannot cancel %s — consignee postal code not cached (shipment may have been booked in a different process)", trackingNumber)
	}

	cancellation := []map[string]any{{
		"shipmentId":    trackingNumber,
		"pickupAccount": a.PickupAccount,
		"postalCode":    postalCode,
	}}
	payload, err := json.Marshal(cancellation)
	if err != nil {
		return nil, fmt.Errorf("DHL UK: marshal cancellation request: %w", err)
	}

	if _, err := a.doRequest(ctx, http.MethodPost, "/shipping/v1/cancellation", payload); err != nil {
		return nil, fmt.Errorf("DHL UK: cancellation request: %w", err)
	}

	// Clean up the cache entry.
	a.postalCodeMu.Lock()
	delete(a.postalCodeCache, trackingNumber)
	a.postalCodeMu.Unlock()

	a.log.Info("DHL UK shipment cancelled", zap.String("shipmentID", trackingNumber))

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "dhl_ecommerce_uk",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment is not supported for DHL eCommerce UK.
// The /shipping/v1/amendment endpoint schema is incompatible with UpdateRequest.
// Use the DHL customer portal or the amendment endpoint directly for post-booking changes.
func (a *DHLEcomUKAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "update shipment", "use /shipping/v1/amendment via DHL portal or contact DHL customer service")
}

// ── ManifestAdapter ───────────────────────────────────────────────────────────

// BookPickup schedules a carrier collection via POST /pickup/v1/pickup.
//
// TradingLocationID must be set on the adapter (configure via DHLECS_UK_TRADING_LOCATION_ID).
// Pickup.Date must be in YYYY-MM-DD format. Pickup.ReadyTime and Pickup.CloseTime
// must be in HH:MM format.
func (a *DHLEcomUKAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	if a.TradingLocationID == "" {
		return nil, fmt.Errorf("DHL UK: TradingLocationID is required for pickup booking (configure DHLECS_UK_TRADING_LOCATION_ID)")
	}
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("DHL UK: pickup date is required")
	}

	readyTime := req.Pickup.ReadyTime
	if readyTime == "" {
		readyTime = "09:00"
	}
	latestTime := req.Pickup.CloseTime
	if latestTime == "" {
		latestTime = "18:00"
	}

	body := map[string]any{
		"customerAccountNumber": a.PickupAccount,
		"tradingLocationId":     a.TradingLocationID,
		"pickupDate":            req.Pickup.Date,
		"timeReady":             readyTime,
		"latestTime":            latestTime,
	}
	if req.Pickup.SpecialInstructions != "" {
		body["instructions"] = req.Pickup.SpecialInstructions
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("DHL UK: marshal pickup request: %w", err)
	}

	respBody, err := a.doRequest(ctx, http.MethodPost, "/pickup/v1/pickup", payload)
	if err != nil {
		return nil, fmt.Errorf("DHL UK: pickup request: %w", err)
	}

	var pickupResp struct {
		PickupIdentifier string `json:"pickupIdentifier"`
	}
	if err := json.Unmarshal(respBody, &pickupResp); err != nil {
		return nil, fmt.Errorf("DHL UK: decode pickup response: %w", err)
	}
	if pickupResp.PickupIdentifier == "" {
		return nil, fmt.Errorf("DHL UK: pickup response missing pickupIdentifier")
	}

	a.log.Info("DHL UK pickup booked",
		zap.String("pickupIdentifier", pickupResp.PickupIdentifier),
		zap.String("date", req.Pickup.Date),
	)

	return &PickupResponse{
		Carrier:            "dhl_ecommerce_uk",
		ConfirmationNumber: pickupResp.PickupIdentifier,
		Date:               req.Pickup.Date,
		ReadyTime:          readyTime,
		CloseTime:          latestTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported for DHL eCommerce UK.
// The API provides no update pickup endpoint; cancel and rebook instead.
func (a *DHLEcomUKAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "update pickup", "cancel the existing pickup and book a new one")
}

// CancelPickup is not supported for DHL eCommerce UK via API.
// Contact your DHL account manager to cancel a scheduled collection.
func (a *DHLEcomUKAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DHL eCommerce UK", "cancel pickup", "contact DHL customer service")
}

// CloseManifest is not supported for DHL eCommerce UK.
// The platform has no manifest or end-of-day closeout API.
func (a *DHLEcomUKAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "close manifest", "DHL eCommerce UK has no manifest API — shipments are automatically processed")
}

// GetPickupAvailability is not supported for DHL eCommerce UK.
// Proceed to BookPickup directly; the API will return an error if the requested
// date is unavailable.
func (a *DHLEcomUKAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DHL eCommerce UK", "pickup availability", "proceed to BookPickup directly")
}
