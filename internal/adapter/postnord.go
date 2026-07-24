// Package adapter provides the PostNord implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/postnord.go.
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

// PostNordAdapter implements CarrierAdapter for PostNord using the v3 EDI API.
//
// PostNordAdapter also implements ManifestAdapter: BookPickup is wired via
// POST /v3/pickups/ids for already-booked items (domestic SE, DK, FI only).
// UpdatePickup, CancelPickup, and CloseManifest return ErrNotSupported —
// PostNord's API exposes no endpoint for updating or cancelling a scheduled
// pickup, and no manifest/end-of-day close endpoint at all (shipments are
// scanned by PostNord at collection instead). GetPickupAvailability also
// returns ErrNotSupported; PostNord has no endpoint that returns a list of
// bookable collection slots (POST /v4/sac/pickup/stopdate returns a single
// cutoff/stop date, not a slot list — see GetCutoffTime on PickupQuerier,
// which this adapter does not yet implement; that endpoint existing but
// being unwired is a genuine secondary gap, tracked in
// docs/postnord-feature-mapping.md, not a confirmed limitation).
type PostNordAdapter struct {
	APIKey         string
	CustomerNumber string // partyId — PostNord account number e.g. "150011208"
	ApplicationID  int    // applicationId — assigned by PostNord for your integration
	BaseURL        string
	HTTPClient     *http.Client
	log            *zap.Logger
}

// NewPostNordAdapter creates a new PostNordAdapter.
// customerNumber is the PostNord account number (partyId).
// applicationID is the integer ID assigned by PostNord to your application.
func NewPostNordAdapter(apiKey, customerNumber string, applicationID int, log *zap.Logger) *PostNordAdapter {
	return &PostNordAdapter{
		APIKey:         apiKey,
		CustomerNumber: customerNumber,
		ApplicationID:  applicationID,
		BaseURL:        "https://api2.postnord.com",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// issuerCode returns the PostNord country issuer code for the given ISO country code.
// Z11=DK, Z12=SE, Z13=NO, Z14=FI.
func issuerCode(countryCode string) string {
	switch strings.ToUpper(countryCode) {
	case "DK":
		return "Z11"
	case "SE":
		return "Z12"
	case "NO":
		return "Z13"
	case "FI":
		return "Z14"
	default:
		return "Z11"
	}
}

// basicServiceCode maps DeliveryType to the PostNord v3 basicServiceCode.
// "18" = standard parcel (home/business delivery).
// "19" = MyPack Collect (service point delivery).
func basicServiceCode(deliveryType string, hasServicePoint bool) string {
	switch strings.ToLower(deliveryType) {
	case "servicepoint":
		return "19"
	default:
		if hasServicePoint {
			return "19"
		}
		return "18"
	}
}

// postNordPickupCountries lists the ISO country codes /v3/pickups/ids
// accepts. PostNord's own API documentation states the endpoint only
// supports domestic pickups of items in SE, DK, FI — Norway is a confirmed
// carrier limitation for this specific endpoint even though PostNord
// otherwise covers NO for booking, tracking, and labels.
var postNordPickupCountries = map[string]bool{
	"SE": true,
	"DK": true,
	"FI": true,
}

// postNordPickupTimestamp combines a "YYYY-MM-DD" date and "HH:MM" time into
// the RFC3339 timestamp the earliestPickupDate/latestPickupDate fields on
// /v3/pickups/ids expect.
func postNordPickupTimestamp(date, hm string) (string, error) {
	t, err := time.Parse("2006-01-02T15:04", date+"T"+hm)
	if err != nil {
		return "", fmt.Errorf("parse pickup time %q %q: %w", date, hm, err)
	}
	return t.UTC().Format(time.RFC3339), nil
}

// postNordStreet concatenates street and house number into a single string.
func postNordStreet(a Address) string {
	if a.HouseNumber != "" {
		return a.Street + " " + a.HouseNumber
	}
	return a.Street
}

// postNordGoodsItem converts a single Colli to the v3 goodsItem structure.
func postNordGoodsItem(c Colli) map[string]any {
	item := map[string]any{
		"itemIdentification": map[string]any{
			"itemId":     "0",
			"itemIdType": "SSCC",
		},
		"grossWeight": map[string]any{
			"value": c.Weight,
			"unit":  "KGM",
		},
	}
	if c.Dimensions.Length > 0 || c.Dimensions.Width > 0 || c.Dimensions.Height > 0 {
		item["dimensions"] = map[string]any{
			"length": map[string]any{"value": c.Dimensions.Length, "unit": "CMT"},
			"width":  map[string]any{"value": c.Dimensions.Width, "unit": "CMT"},
			"height": map[string]any{"value": c.Dimensions.Height, "unit": "CMT"},
		}
	}
	return map[string]any{
		"packageTypeCode": "PC",
		"items":           []any{item},
	}
}

// BookShipment books a shipment with PostNord using the v3 EDI API and returns
// the booking response with an inline PDF label.
//
// Wire format notes:
//   - Endpoint: POST /rest/shipment/v3/edi/labels/pdf?apikey=
//   - API key passed as query parameter.
//   - Payload uses the v3 EDI structure: messageDate, messageId, application,
//     parties.consignor / consignee / deliveryParty, goodsItem.
//   - Service point uses parties.deliveryParty with partyIdType "156".
//   - Notifications sent via consignee.party.contact.emailAddress / smsNo.
//   - Label returned inline in labelPrintout[0].printout.data (base64 PDF).
//   - Tracking ID returned in idInformation[0].ids[0].value.
func (a *PostNordAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	hasServicePoint := request.Shipment.Receiver.ServicePointID != ""
	svcCode := basicServiceCode(request.Shipment.DeliveryType, hasServicePoint)

	// Build goodsItem array from colli.
	goodsItems := make([]any, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		goodsItems[i] = postNordGoodsItem(c)
	}

	// Calculate total weight.
	var totalWeight float64
	for _, c := range request.Shipment.Colli {
		totalWeight += c.Weight
	}

	// Build consignee contact — populated from AddOns and receiver fields.
	consigneeContact := map[string]any{
		"contactName": request.Shipment.Receiver.Name,
	}
	if request.Shipment.Receiver.Email != "" {
		consigneeContact["emailAddress"] = request.Shipment.Receiver.Email
	}
	if request.Shipment.Receiver.Phone != "" {
		consigneeContact["smsNo"] = request.Shipment.Receiver.Phone
	}
	// AddOns override: if SMS or email notification explicitly requested,
	// ensure the fields are present even if not on receiver address.
	if hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) && request.Shipment.Receiver.Phone == "" {
		return nil, fmt.Errorf("SMS notification requested but receiver phone is empty")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) && request.Shipment.Receiver.Email == "" {
		return nil, fmt.Errorf("email notification requested but receiver email is empty")
	}

	// Build additionalServiceCode list from AddOns.
	var additionalServiceCodes []string
	if hasAddOn(request.Shipment.AddOns, AddOnFlexDelivery) {
		additionalServiceCodes = append(additionalServiceCodes, "A7") // Flex delivery
	}
	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		additionalServiceCodes = append(additionalServiceCodes, "A2") // Direct signature
	}
	if ins, ok := getAddOn(request.Shipment.AddOns, AddOnInsurance); ok {
		if ins.InsuranceValue <= 0 {
			return nil, fmt.Errorf("insurance add-on requires InsuranceValue > 0")
		}
		additionalServiceCodes = append(additionalServiceCodes, "A8") // Transport insurance
	}

	// Build the parties block.
	parties := map[string]any{
		"consignor": map[string]any{
			"issuerCode": issuerCode(request.Shipment.Sender.Country),
			"partyIdentification": map[string]any{
				"partyId":     a.CustomerNumber,
				"partyIdType": "160",
			},
			"party": map[string]any{
				"nameIdentification": map[string]any{
					"name": request.Shipment.Sender.Name,
				},
				"address": map[string]any{
					"streets":     []string{postNordStreet(request.Shipment.Sender)},
					"postalCode":  request.Shipment.Sender.PostalCode,
					"city":        request.Shipment.Sender.City,
					"countryCode": request.Shipment.Sender.Country,
				},
			},
		},
		"consignee": map[string]any{
			"party": map[string]any{
				"nameIdentification": map[string]any{
					"name": request.Shipment.Receiver.Name,
				},
				"address": map[string]any{
					"streets":     []string{postNordStreet(request.Shipment.Receiver)},
					"postalCode":  request.Shipment.Receiver.PostalCode,
					"city":        request.Shipment.Receiver.City,
					"countryCode": request.Shipment.Receiver.Country,
				},
				"contact": consigneeContact,
			},
		},
	}

	// Service point — add deliveryParty block.
	if hasServicePoint {
		parties["deliveryParty"] = map[string]any{
			"partyIdentification": map[string]any{
				"partyId":     request.Shipment.Receiver.ServicePointID,
				"partyIdType": "156",
			},
		}
	}

	shipmentBlock := map[string]any{
		"shipmentIdentification": map[string]any{
			"shipmentId": "0",
		},
		"dateAndTimes": map[string]any{
			"loadingDate": time.Now().UTC().Format(time.RFC3339),
		},
		"service": map[string]any{
			"basicServiceCode": svcCode,
		},
		"freeText": []any{},
		"numberOfPackages": map[string]any{
			"value": len(request.Shipment.Colli),
		},
		"totalGrossWeight": map[string]any{
			"value": totalWeight,
			"unit":  "KGM",
		},
		"parties":   parties,
		"goodsItem": goodsItems,
	}

	if len(additionalServiceCodes) > 0 {
		shipmentBlock["service"].(map[string]any)["additionalServiceCode"] = additionalServiceCodes
	}

	if request.IdempotencyKey != "" {
		shipmentBlock["references"] = []map[string]any{
			{"referenceNo": request.IdempotencyKey, "referenceType": "CU"},
		}
	}

	// messageId must be reused verbatim on a later update instruction for the
	// same shipment — see UpdateShipment and APIdocs/postnord_update_cancel.rtf.
	// It's captured below and returned to the caller via
	// BookingResponse.CarrierMessageID.
	messageID := fmt.Sprintf("msg-%d", time.Now().UnixMilli())
	payload := map[string]any{
		"messageDate":     time.Now().UTC().Format(time.RFC3339),
		"messageFunction": "Instruction",
		"messageId":       messageID,
		"application": map[string]any{
			"applicationId": a.ApplicationID,
			"name":          "logistics-gateway",
			"version":       "1.0",
		},
		"updateIndicator": "Original",
		"shipment":        []any{shipmentBlock},
	}

	isReturn := strings.EqualFold(request.Shipment.DeliveryType, "return")

	// Select endpoint based on delivery type.
	// Return bookings use the /returns/ path; regular bookings use /edi/.
	var bookingEndpoint string
	if isReturn {
		bookingEndpoint = fmt.Sprintf("%s/rest/shipment/v3/returns/edi/labels/pdf?apikey=%s",
			a.BaseURL, a.APIKey)

		// functionality: STANDARD or LABELLESS.
		functionality := "STANDARD"
		if strings.EqualFold(request.Shipment.ReturnFunctionality, "labelless") {
			functionality = "LABELLESS"
		}
		bookingEndpoint += "&functionality=" + functionality

		// QR code delivery via existing add-ons.
		if hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) {
			bookingEndpoint += "&smsQRcode=true"
		}
		if hasAddOn(request.Shipment.AddOns, AddOnEmailNotification) {
			bookingEndpoint += "&emailQRcode=true"
		}
	} else {
		bookingEndpoint = fmt.Sprintf("%s/rest/shipment/v3/edi/labels/pdf?apikey=%s",
			a.BaseURL, a.APIKey)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		bookingEndpoint,
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord API call failed: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("PostNord API returned status %d: %s", resp.StatusCode, string(body))
	}

	var pnResp struct {
		BookingResponse struct {
			BookingID     string `json:"bookingId"`
			IDInformation []struct {
				Status string `json:"status"`
				IDs    []struct {
					IDType  string `json:"idType"`
					Value   string `json:"value"`
					PrintID string `json:"printId"`
				} `json:"ids"`
				URLs []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"urls"`
			} `json:"idInformation"`
		} `json:"bookingResponse"`
		LabelPrintout []struct {
			ItemIDs []struct {
				ItemIDs string `json:"itemIds"`
				Status  string `json:"status"`
			} `json:"itemIds"`
			Printout struct {
				LabelFormat string `json:"labelFormat"`
				Encoding    string `json:"encoding"`
				Data        string `json:"data"`
				Type        string `json:"type"`
			} `json:"printout"`
		} `json:"labelPrintout"`
	}

	if err := json.Unmarshal(body, &pnResp); err != nil {
		return nil, fmt.Errorf("failed to decode PostNord response: %w", err)
	}

	if len(pnResp.BookingResponse.IDInformation) == 0 {
		return nil, fmt.Errorf("PostNord response contained no idInformation: %s", string(body))
	}

	info := pnResp.BookingResponse.IDInformation[0]
	if info.Status != "OK" {
		return nil, fmt.Errorf("PostNord booking failed with status: %s", info.Status)
	}

	// Tracking number is the itemId value (barcode number).
	var trackingNumber string
	for _, id := range info.IDs {
		if id.IDType == "itemId" {
			trackingNumber = id.Value
			break
		}
	}

	result := &BookingResponse{
		ShipmentID:       trackingNumber,
		TrackingNumber:   trackingNumber,
		Carrier:          "postnord",
		Status:           "booked",
		CarrierMessageID: messageID,
	}

	// Extract inline label data if present.
	if len(pnResp.LabelPrintout) > 0 {
		printout := pnResp.LabelPrintout[0].Printout
		if printout.Data != "" {
			result.LabelURL = "" // data returned inline, not as URL
			// Store label data on colli response for label endpoint to serve.
			result.Colli = []ColliResponse{
				{
					ID:             trackingNumber,
					TrackingNumber: trackingNumber,
					LabelURL:       printout.Data, // base64 PDF stored here temporarily
					Status:         "booked",
				},
			}
		}
	}

	return result, nil
}

// CancelShipment cancels a PostNord shipment via the v3 EDI endpoint.
// Uses messageFunction "Cancellation" and updateIndicator "Delete".
// The shipment must not yet have been collected by PostNord.
//
// Unconfirmed discrepancy: APIdocs/postnord_update_cancel.rtf (a single,
// AI-research-derived source — not an official schema, and no schema for the
// EDI Instruction message format exists anywhere else in APIdocs/) claims
// PostNord's Booking API has no dedicated cancel/void endpoint at all, and
// that cancellation should instead go through the Delivery Order Modification
// Service (DOMS) or manual support — but gives no endpoint, schema, or
// worked example for DOMS to verify or implement against. That claim has not
// been acted on here: it directly contradicts this already-implemented,
// presumably-tested "Cancellation"/"Delete" EDI instruction, and there's
// nothing concrete in that source to wire even if it's right. Flagging for
// awareness, not changing behavior, until there's a stronger source (e.g. an
// official schema showing messageFunction "Cancellation" is invalid, or a
// real sandbox failure).
func (a *PostNordAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	payload := map[string]any{
		"messageDate":     time.Now().UTC().Format(time.RFC3339),
		"messageFunction": "Cancellation",
		"messageId":       fmt.Sprintf("cancel-%d", time.Now().UnixMilli()),
		"application": map[string]any{
			"applicationId": a.ApplicationID,
			"name":          "logistics-gateway",
			"version":       "1.0",
		},
		"updateIndicator": "Delete",
		"shipment": []any{
			map[string]any{
				"shipmentIdentification": map[string]any{
					"shipmentId": trackingNumber,
				},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord cancel request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/rest/shipment/v3/edi?apikey=%s", a.BaseURL, a.APIKey),
		bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord cancel request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord cancel request failed: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord cancel response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("PostNord cancel returned status %d: %s", resp.StatusCode, string(body))
	}

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "postnord",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment sends a PostNord v3 EDI update instruction.
// Only ReceiverPhone and ReceiverEmail are supported. Per
// APIdocs/postnord_update_cancel.rtf, PostNord's update functionality —
// not just address changes — is currently only supported for Sweden (SE);
// the carrier is expected to reject update requests for DK/NO/FI bookings.
//
// PostNord's documentation states the update instruction must reuse the
// exact messageId from the original booking request. Pass the value from
// BookingResponse.CarrierMessageID back as req.CarrierMessageID to satisfy
// this; if omitted, a new messageId is generated on a best-effort basis,
// which PostNord's API may reject for an existing shipment.
func (a *PostNordAdapter) UpdateShipment(ctx context.Context, req UpdateRequest) (*UpdateResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	if req.ReceiverPhone == "" && req.ReceiverEmail == "" && req.Weight == 0 && req.ServicePointID == "" {
		return nil, fmt.Errorf("at least one field must be specified for update")
	}
	// PostNord does not support weight or service point updates post-booking.
	if req.Weight > 0 {
		return nil, fmt.Errorf("PostNord does not support post-booking weight updates")
	}
	if req.ServicePointID != "" {
		return nil, fmt.Errorf("PostNord does not support post-booking service point changes")
	}

	consigneeContact := map[string]any{}
	if req.ReceiverPhone != "" {
		consigneeContact["smsNo"] = req.ReceiverPhone
	}
	if req.ReceiverEmail != "" {
		consigneeContact["emailAddress"] = req.ReceiverEmail
	}

	// Reuse the original booking's messageId when the caller supplied it
	// (BookingResponse.CarrierMessageID) — PostNord's documentation states
	// updates must reference the exact messageId from the original booking
	// instruction. Falling back to a freshly generated ID is best-effort only.
	messageID := req.CarrierMessageID
	if messageID == "" {
		messageID = fmt.Sprintf("update-%d", time.Now().UnixMilli())
	}

	payload := map[string]any{
		"messageDate":     time.Now().UTC().Format(time.RFC3339),
		"messageFunction": "Instruction",
		"messageId":       messageID,
		"application": map[string]any{
			"applicationId": a.ApplicationID,
			"name":          "logistics-gateway",
			"version":       "1.0",
		},
		"updateIndicator": "Update",
		"shipment": []any{
			map[string]any{
				"shipmentIdentification": map[string]any{
					"shipmentId": req.TrackingNumber,
				},
				"parties": map[string]any{
					"consignee": map[string]any{
						"party": map[string]any{
							"contact": consigneeContact,
						},
					},
				},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord update request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/rest/shipment/v3/edi?apikey=%s", a.BaseURL, a.APIKey),
		bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord update request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("PostNord update request failed: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord update response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("PostNord update returned status %d: %s", resp.StatusCode, string(body))
	}

	var updatedFields []string
	if req.ReceiverPhone != "" {
		updatedFields = append(updatedFields, "phone")
	}
	if req.ReceiverEmail != "" {
		updatedFields = append(updatedFields, "email")
	}

	return &UpdateResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "postnord",
		Status:         "updated",
		UpdatedFields:  updatedFields,
	}, nil
}

// FetchLabel retrieves a shipping label for a PostNord shipment.
// Uses POST /rest/shipment/v3/edi/labels/pdf (or /zpl) with itemIds in the request body.
func (a *PostNordAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	var endpoint string
	switch req.Format {
	case LabelFormatZPL, LabelFormatZPLGK:
		endpoint = fmt.Sprintf("%s/rest/shipment/v3/edi/labels/zpl?apikey=%s", a.BaseURL, a.APIKey)
	default:
		endpoint = fmt.Sprintf("%s/rest/shipment/v3/edi/labels/pdf?apikey=%s", a.BaseURL, a.APIKey)
	}

	// Re-fetch label by resubmitting the itemId reference.
	// PostNord label-only fetch uses the printId or itemId.
	payload := map[string]any{
		"itemIds": []string{req.TrackingNumber},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord label request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord label request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("PostNord label request failed: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord label response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PostNord label API returned status %d: %s", resp.StatusCode, string(body))
	}

	var labelResp struct {
		LabelPrintout []struct {
			Printout struct {
				LabelFormat string `json:"labelFormat"`
				Encoding    string `json:"encoding"`
				Data        string `json:"data"`
			} `json:"printout"`
		} `json:"labelPrintout"`
	}
	if err := json.Unmarshal(body, &labelResp); err != nil {
		return &LabelResponse{
			TrackingNumber: req.TrackingNumber,
			Carrier:        "postnord",
			Format:         req.Format,
			Data:           base64.StdEncoding.EncodeToString(body),
			MimeType:       MimeTypeForFormat(req.Format),
		}, nil
	}

	if len(labelResp.LabelPrintout) == 0 {
		return nil, fmt.Errorf("PostNord returned no labels for tracking number %s", req.TrackingNumber)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "postnord",
		Format:         req.Format,
		Data:           labelResp.LabelPrintout[0].Printout.Data,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// TrackShipment retrieves the tracking status for a PostNord shipment.
// Uses the v5 Track & Trace API with the itemId returned from booking.
func (a *PostNordAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v5/trackandtrace/findByIdentifier.json?apikey=%s&id=%s&locale=en",
			a.BaseURL, a.APIKey, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord tracking request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord tracking API call failed: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord tracking response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PostNord tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var trackResp struct {
		TrackingInformationResponse struct {
			Shipments []struct {
				ShipmentID string `json:"shipmentId"`
				Status     string `json:"status"`
				StatusText struct {
					Header string `json:"header"`
					Body   string `json:"body"`
				} `json:"statusText"`
				DeliveryDate string `json:"deliveryDate"`
				Items        []struct {
					ItemID string `json:"itemId"`
					Events []struct {
						EventTime   string `json:"eventTime"`
						Status      string `json:"status"`
						Description string `json:"eventDescription"`
						Location    struct {
							DisplayName string `json:"displayName"`
							CountryCode string `json:"countryCode"`
							City        string `json:"city"`
						} `json:"location"`
					} `json:"events"`
				} `json:"items"`
			} `json:"shipments"`
		} `json:"TrackingInformationResponse"`
	}

	if err := json.Unmarshal(body, &trackResp); err != nil {
		return nil, fmt.Errorf("failed to decode PostNord tracking response: %w", err)
	}

	shipments := trackResp.TrackingInformationResponse.Shipments
	if len(shipments) == 0 {
		return nil, fmt.Errorf("no tracking information found for %s", trackingNumber)
	}

	s := shipments[0]
	var events []TrackingEvent
	for _, item := range s.Items {
		for _, e := range item.Events {
			location := e.Location.DisplayName
			if location == "" {
				location = e.Location.City
			}
			events = append(events, TrackingEvent{
				Timestamp:        e.EventTime,
				Status:           e.Status,
				NormalizedStatus: normalizeStatus("postnord", e.Status),
				Location:         location,
				Details:          e.Description,
			})
		}
	}

	return &TrackingResponse{
		ShipmentID:        s.ShipmentID,
		TrackingNumber:    s.ShipmentID,
		Carrier:           "postnord",
		Status:            s.Status,
		NormalizedStatus:  normalizeStatus("postnord", s.Status),
		OriginalStatus:    s.Status,
		EstimatedDelivery: s.DeliveryDate,
		Events:            events,
	}, nil
}

// ── ManifestAdapter ───────────────────────────────────────────────────────────

// postnordPickupIDInfo is a single item entry in the /v3/pickups/ids request
// body, per PostNord's own pickupIdInfo schema (itemId plus an optional
// earliest/latest pickup date window).
type postnordPickupIDInfo struct {
	ItemID             string `json:"itemId"`
	EarliestPickupDate string `json:"earliestPickupDate,omitempty"`
	LatestPickupDate   string `json:"latestPickupDate,omitempty"`
}

// BookPickup schedules a domestic collection for already-booked items via
// POST /v3/pickups/ids.
//
// Wire format notes:
//   - Endpoint: POST /rest/shipment/v3/pickups/ids?apikey=
//   - Body: a JSON array of {itemId, earliestPickupDate, latestPickupDate}.
//   - req.TrackingNumbers must hold the carrier item IDs returned from
//     BookShipment (BookingResponse.TrackingNumber / ColliResponse.TrackingNumber
//     — the itemId barcode value), not a human-readable order reference.
//   - Domestic only: SE, DK, FI. If req.Address.Country is supplied and is
//     not one of these, the request is rejected client-side rather than left
//     for PostNord to reject — see postNordPickupCountries.
//   - Response reuses the same bookingResponse envelope as BookShipment;
//     bookingResponse.bookingId is returned to the caller as ConfirmationNumber.
func (a *PostNordAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	if len(req.TrackingNumbers) == 0 {
		return nil, fmt.Errorf("postnord: book pickup: at least one tracking number (carrier item ID) is required")
	}
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("postnord: book pickup: pickup.date is required")
	}
	if req.Address.Country != "" && !postNordPickupCountries[strings.ToUpper(req.Address.Country)] {
		return nil, fmt.Errorf("postnord: book pickup: country %q not supported — /v3/pickups/ids only covers domestic SE, DK, FI", req.Address.Country)
	}

	readyTime := req.Pickup.ReadyTime
	if readyTime == "" {
		readyTime = "09:00"
	}
	closeTime := req.Pickup.CloseTime
	if closeTime == "" {
		closeTime = "18:00"
	}

	earliest, err := postNordPickupTimestamp(req.Pickup.Date, readyTime)
	if err != nil {
		return nil, fmt.Errorf("postnord: book pickup: %w", err)
	}
	latest, err := postNordPickupTimestamp(req.Pickup.Date, closeTime)
	if err != nil {
		return nil, fmt.Errorf("postnord: book pickup: %w", err)
	}

	items := make([]postnordPickupIDInfo, len(req.TrackingNumbers))
	for i, itemID := range req.TrackingNumbers {
		items[i] = postnordPickupIDInfo{
			ItemID:             itemID,
			EarliestPickupDate: earliest,
			LatestPickupDate:   latest,
		}
	}

	payloadBytes, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("postnord: marshal pickup request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/rest/shipment/v3/pickups/ids?apikey=%s", a.BaseURL, a.APIKey),
		bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("postnord: create pickup request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("postnord: pickup API call failed: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("postnord: read pickup response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("postnord: pickup API returned status %d: %s", resp.StatusCode, string(body))
	}

	var pnResp struct {
		BookingResponse struct {
			BookingID     string `json:"bookingId"`
			IDInformation []struct {
				Status string `json:"status"`
			} `json:"idInformation"`
		} `json:"bookingResponse"`
	}
	if err := json.Unmarshal(body, &pnResp); err != nil {
		return nil, fmt.Errorf("postnord: decode pickup response: %w", err)
	}

	if len(pnResp.BookingResponse.IDInformation) == 0 {
		return nil, fmt.Errorf("postnord: pickup response contained no idInformation: %s", string(body))
	}
	if pnResp.BookingResponse.IDInformation[0].Status != "OK" {
		return nil, fmt.Errorf("postnord: pickup booking failed with status: %s", pnResp.BookingResponse.IDInformation[0].Status)
	}

	confirmation := pnResp.BookingResponse.BookingID
	if confirmation == "" {
		confirmation = req.TrackingNumbers[0]
	}

	a.log.Info("postnord: pickup booked",
		zap.String("bookingId", confirmation),
		zap.Int("itemCount", len(req.TrackingNumbers)),
	)

	return &PickupResponse{
		Carrier:            "postnord",
		ConfirmationNumber: confirmation,
		Date:               req.Pickup.Date,
		ReadyTime:          readyTime,
		CloseTime:          closeTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported — /v3/pickups/ids only exposes a create
// (POST) operation; PostNord's API has no endpoint to modify a pickup once
// scheduled. Confirmed carrier limitation, not an implementation gap.
func (a *PostNordAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("PostNord", "pickup update", "no update endpoint exists for /v3/pickups/ids")
}

// CancelPickup is not supported — PostNord's API has no endpoint to cancel a
// scheduled pickup. Confirmed carrier limitation, not an implementation gap.
func (a *PostNordAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("PostNord", "pickup cancellation", "no cancel endpoint exists for /v3/pickups/ids")
}

// CloseManifest is not supported — PostNord has no manifest / end-of-day
// close endpoint. Shipments are scanned by PostNord at collection instead.
// Confirmed carrier limitation, not an implementation gap.
func (a *PostNordAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("PostNord", "close manifest", "PostNord has no manifest/end-of-day close endpoint — shipments are scanned at collection")
}

// GetPickupAvailability is not supported — PostNord has no endpoint that
// returns a list of bookable collection time slots. POST /v4/sac/pickup/stopdate
// exists but returns a single cutoff/stop date rather than a slot list, so it
// does not fulfil this method's contract; confirmed carrier limitation for
// GetPickupAvailability specifically. See the adapter's doc comment for the
// distinct, still-open gap around wiring that endpoint as GetCutoffTime.
func (a *PostNordAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("PostNord", "pickup availability", "no slot-list endpoint exists — see /v4/sac/pickup/stopdate for cutoff-only info")
}
